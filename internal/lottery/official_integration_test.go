package lottery

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jiuwusan/z-lottery/internal/domain"
)

func TestOfficialCompletionStopsHTTPWithManyFutureBetsAcrossPages(t *testing.T) {
	s, f := syncService(t)
	for i := 0; i < 41; i++ {
		createFixture(t, s, `{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"periods":100}`)
	}
	scheduler := NewScheduler(nil, s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e := scheduler.RunOnce(context.Background()); e != nil {
		t.Fatal(e)
	}
	firstCalls := f.calls["dlt"]
	if firstCalls != 1 || f.calls["ssq"] != 1 {
		t.Fatal("per-page upstream duplicated", f.calls)
	}
	page, e := s.Repository.List(domain.ListOptions{Page: 1, PageSize: 50})
	if e != nil || page.Total != 41 {
		t.Fatal(page, e)
	}
	for _, b := range page.Items {
		if b.Status != "pending" || b.WinAmount != nil {
			t.Fatalf("future periods prematurely completed: %+v", b)
		}
	}
	s.Clock = func() time.Time { return time.Date(2026, 11, 2, 16, 0, 0, 0, time.UTC) }
	if e := scheduler.RunOnce(context.Background()); e != nil {
		t.Fatal(e)
	}
	if f.calls["dlt"] != firstCalls || f.calls["ssq"] != 1 || len(f.exact) != 0 {
		t.Fatal("future bets drove extra network", f.calls, f.exact)
	}
}

func TestOfficialMissingUnheldGradeKeepsPollingEvenWhenBetIsComplete(t *testing.T) {
	s, f := syncService(t)
	d := fullOfficial("dlt", "26125", "2026-11-02")
	d.Front = []int{1, 2, 3, 4, 5}
	d.Back = []int{1, 2}
	d.Prizes[7] = nil
	f.snapshots["dlt"] = readySnapshot(d)
	b := createFixture(t, s, `{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"placedAt":"2026-11-02T20:00:00+08:00"}`)
	report, e := s.Check(context.Background(), "", 20)
	if e != nil || len(report.Errors) != 0 {
		t.Fatal(report, e)
	}
	bet, e := s.Repository.Get(b.ID)
	if e != nil || bet.Status != "won" || bet.WinAmount == nil {
		t.Fatal(bet, e)
	}
	// The bet only won first prize; the official seventh-prize amount remains unknown.
	report, e = s.Check(context.Background(), "", 20)
	if e != nil || len(report.Errors) != 0 {
		t.Fatal(report, e)
	}
	if f.calls["dlt"] != 2 {
		t.Fatal("bet completion incorrectly stopped official polling", f.calls)
	}
	d.Prizes[7] = domain.AmountPointer(5)
	f.snapshots["dlt"] = readySnapshot(d)
	s.Check(context.Background(), "", 20)
	s.Check(context.Background(), "", 20)
	if f.calls["dlt"] != 3 {
		t.Fatal("complete official record did not stop polling", f.calls)
	}
	after, _ := s.Repository.Get(b.ID)
	if after.Results[0].CheckedAt != bet.Results[0].CheckedAt {
		t.Fatal("completed bet result unnecessarily rewritten")
	}
}

func TestOfficialPublishedListCheckedOutsideBettingDateRange(t *testing.T) {
	s, f := syncService(t)
	older := fullOfficial("dlt", "26124", "2026-10-31")
	older.Prizes[7] = nil
	snapshot := f.snapshots["dlt"]
	snapshot.Published = []domain.Draw{*snapshot.Latest, older}
	f.snapshots["dlt"] = snapshot
	runOfficial(t, s)
	runOfficial(t, s)
	if f.calls["dlt"] != 2 {
		t.Fatal("older listed amount ignored", f.calls)
	}
	older.Prizes[7] = domain.AmountPointer(5)
	snapshot.Published[1] = older
	f.snapshots["dlt"] = snapshot
	runOfficial(t, s)
	runOfficial(t, s)
	if f.calls["dlt"] != 3 {
		t.Fatal("complete published list still polled", f.calls)
	}
	// Even a reviewed latest row cannot hide a different unreviewed listed row.
	s, f = syncService(t)
	snapshot = f.snapshots["dlt"]
	snapshot.HasUnready = true
	f.snapshots["dlt"] = snapshot
	runOfficial(t, s)
	runOfficial(t, s)
	if f.calls["dlt"] != 2 {
		t.Fatal("unreviewed listed row ignored", f.calls)
	}
}
