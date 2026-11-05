package lottery

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultScheduleMidnightBoundary(t *testing.T) {
	schedule, e := ParseSchedule(DefaultCheckSchedule, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	cases := [][2]string{{"2026-11-05T21:59:59+08:00", "2026-11-05T22:00:00+08:00"}, {"2026-11-05T22:00:00+08:00", "2026-11-05T22:20:00+08:00"}, {"2026-11-05T23:40:00+08:00", "2026-11-06T00:00:00+08:00"}, {"2026-11-06T00:00:00+08:00", "2026-11-06T22:00:00+08:00"}}
	for _, c := range cases {
		now, _ := time.Parse(time.RFC3339, c[0])
		want, _ := time.Parse(time.RFC3339, c[1])
		if got := schedule.Next(now.UTC()); !got.Equal(want) {
			t.Fatalf("after %s got %s want %s", now, got, want)
		}
	}
	if _, e := ParseSchedule("garbage", "Asia/Shanghai"); e == nil {
		t.Fatal("invalid cron accepted")
	}
	if _, e := ParseSchedule(DefaultCheckSchedule, "invalid-zone"); e == nil {
		t.Fatal("invalid timezone accepted")
	}
}

type checkerFunc func(context.Context, string, int) (CheckReport, error)

func (f checkerFunc) Check(c context.Context, a string, l int) (CheckReport, error) {
	return f(c, a, l)
}
func TestSchedulerTraversesAllPagesDespiteBatchErrors(t *testing.T) {
	calls := []string{}
	s := NewScheduler(nil, checkerFunc(func(_ context.Context, after string, _ int) (CheckReport, error) {
		calls = append(calls, after)
		next := "bet:0000000000000002"
		if after == "" {
			return CheckReport{Errors: []CheckError{{Message: "upstream failed"}}, NextCursor: &next}, nil
		}
		return CheckReport{}, nil
	}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e := s.RunOnce(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(calls) != 2 || calls[1] == "" {
		t.Fatal(calls)
	}
}
func TestSchedulerNoOverlapAndCancellation(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{})
	checker := checkerFunc(func(c context.Context, _ string, _ int) (CheckReport, error) {
		calls.Add(1)
		close(entered)
		<-c.Done()
		return CheckReport{}, c.Err()
	})
	schedule, _ := ParseSchedule(DefaultCheckSchedule, "Asia/Shanghai")
	s := NewScheduler(schedule, checker, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	<-entered
	if e := s.RunOnce(context.Background()); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatal("overlapped")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked")
	}
}
