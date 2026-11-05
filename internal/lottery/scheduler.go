package lottery

import (
	"context"
	"fmt"
	"github.com/robfig/cron/v3"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"
	_ "time/tzdata"
)

// Midnight is a separate expression so that 00:20 and 00:40 are not included.
const DefaultCheckSchedule = "*/20 22-23 * * *;0 0 * * *"

type Schedule struct {
	entries  []cron.Schedule
	location *time.Location
}

func ParseSchedule(expression, zone string) (*Schedule, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("验奖时区无效: %w", err)
	}
	s := &Schedule{location: loc}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	for _, part := range strings.Split(expression, ";") {
		part = strings.TrimSpace(part)
		if part == "" || strings.Contains(part, "TZ=") {
			return nil, fmt.Errorf("验奖调度表达式无效: %q", part)
		}
		entry, e := parser.Parse("CRON_TZ=" + zone + " " + part)
		if e != nil {
			return nil, fmt.Errorf("验奖调度表达式无效: %w", e)
		}
		if entry.Next(time.Now().In(loc)).IsZero() {
			return nil, fmt.Errorf("验奖调度没有有效触发时间")
		}
		s.entries = append(s.entries, entry)
	}
	return s, nil
}
func (s *Schedule) Next(now time.Time) time.Time {
	var next time.Time
	for _, entry := range s.entries {
		candidate := entry.Next(now.In(s.location))
		if !candidate.IsZero() && (next.IsZero() || candidate.Before(next)) {
			next = candidate
		}
	}
	return next
}

type BatchChecker interface {
	Check(context.Context, string, int) (CheckReport, error)
}
type Scheduler struct {
	schedule *Schedule
	checker  BatchChecker
	logger   *slog.Logger
	running  atomic.Bool
}

func NewScheduler(schedule *Schedule, checker BatchChecker, logger *slog.Logger) *Scheduler {
	return &Scheduler{schedule: schedule, checker: checker, logger: logger}
}
func (s *Scheduler) RunOnce(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		s.logger.Info("验奖任务仍在运行，跳过重叠轮次")
		return nil
	}
	defer s.running.Store(false)
	s.logger.Info("开始自动验奖")
	after := ""
	checked, waiting, updated, failures := 0, 0, 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		report, err := s.checker.Check(ctx, after, 20)
		if err != nil {
			return err
		}
		checked += report.Checked
		waiting += report.Waiting
		updated += report.UpdatedBets
		failures += len(report.Errors)
		for _, failure := range report.Errors {
			s.logger.Error("验奖记录失败，下一轮重试", "game", failure.Game, "betId", failure.BetID, "issue", failure.Issue, "error", failure.Message)
		}
		if report.NextCursor == nil {
			break
		}
		if *report.NextCursor <= after {
			return fmt.Errorf("验奖游标未前进")
		}
		after = *report.NextCursor
	}
	s.logger.Info("自动验奖结束", "checked", checked, "waiting", waiting, "updatedBets", updated, "errors", failures)
	return nil
}
func (s *Scheduler) Run(ctx context.Context) {
	// Only this goroutine launches work. It remains responsive to ticks while a
	// round is active, skips overlaps, and joins the worker on cancellation.
	done := make(chan error, 1)
	active := false
	start := func() {
		if active {
			s.logger.Info("验奖任务仍在运行，跳过重叠轮次")
			return
		}
		active = true
		go func() { done <- s.RunOnce(ctx) }()
	}
	start()
	for {
		next := s.schedule.Next(time.Now())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			if active {
				<-done
			}
			return
		case err := <-done:
			timer.Stop()
			active = false
			if err != nil && ctx.Err() == nil {
				s.logger.Error("自动验奖中止，下一轮重试", "error", err)
			}
		case <-timer.C:
			start()
		}
	}
}
