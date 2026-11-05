package lottery

import (
	"context"
	"errors"
	"regexp"
	"sync/atomic"
	"time"
)

type CheckError struct {
	Game    string `json:"game"`
	BetID   int64  `json:"betId,omitempty"`
	Issue   string `json:"issue,omitempty"`
	Message string `json:"message"`
}
type CheckReport struct {
	Checked     int          `json:"checked"`
	Waiting     int          `json:"waiting"`
	UpdatedBets int          `json:"updatedBets"`
	Errors      []CheckError `json:"errors"`
	NextCursor  *string      `json:"nextCursor"`
}
type Service struct {
	Repository *Repository
	Provider   Provider
	Clock      func() time.Time
	running    atomic.Bool
}

func NewService(r *Repository, p Provider) *Service {
	return &Service{Repository: r, Provider: p, Clock: time.Now}
}
func (s *Service) Create(raw []byte) (Bet, error) {
	p, e := NormalizeBet(raw, s.Clock())
	if e != nil {
		return Bet{}, e
	}
	return s.Repository.Create(p)
}

var cursorPattern = regexp.MustCompile(`^(bet:\d{16}|dlt:\d{5}|ssq:20\d{5})$`)

func (s *Service) Check(ctx context.Context, after string, limit int) (CheckReport, error) {
	report := CheckReport{Errors: []CheckError{}}
	if limit < 1 || limit > 100 {
		return report, appError(400, "批次数量必须是 1～100 的整数")
	}
	if after != "" && !cursorPattern.MatchString(after) {
		return report, appError(400, "无效的验奖游标")
	}
	if !s.running.CompareAndSwap(false, true) {
		return report, appError(409, "验奖任务正在运行，请稍后重试")
	}
	defer s.running.Store(false)
	if after == "" {
		report.Errors = append(report.Errors, s.syncOfficial(ctx)...)
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	tasks, err := s.Repository.Pending(after, limit)
	if err != nil {
		return report, err
	}
	if len(tasks) > limit {
		key := tasks[limit-1].Key
		report.NextCursor = &key
		tasks = tasks[:limit]
	}
	cache := map[string][]Draw{}
	for _, task := range tasks {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		taskErr := func() error {
			if task.ID != 0 {
				b, e := s.Repository.Get(task.ID)
				if e != nil {
					var ae *AppError
					if errors.As(e, &ae) && ae.Status == 404 {
						return nil
					}
					return e
				}
				rows, ok := cache[task.Game]
				if !ok {
					rows, e = s.Repository.CachedDraws(task.Game)
					if e != nil {
						return e
					}
					cache[task.Game] = rows
				}
				state, e := s.Repository.SyncState(task.Game)
				if e != nil {
					return e
				}
				placed, e := time.Parse(time.RFC3339Nano, b.PlacedAt)
				if e != nil {
					return e
				}
				if state.HistoryFrom == "" || ShanghaiDate(placed) < state.HistoryFrom {
					report.Waiting++
					return nil
				}
				selected, e := SelectDraws(task.Game, b.PlacedAt, b.Config.Periods, rows)
				if e != nil {
					return e
				}
				results := make([]Result, 0, len(selected))
				for _, d := range selected {
					result, e := Evaluate(b, d)
					if e != nil {
						return e
					}
					results = append(results, result)
				}
				count, e := s.Repository.SaveDateCheck(b.ID, selected, results)
				if e != nil {
					return e
				}
				report.Checked += count
				if count > 0 {
					report.UpdatedBets++
				}
				if count < b.Config.Periods {
					report.Waiting++
				}
				return nil
			}
			draw, e := s.Repository.CachedDraw(task.Game, task.Issue)
			if e != nil {
				return e
			}
			if draw == nil {
				report.Waiting++
				return nil
			}
			issue, e := NormalizeIssue(draw.Game, draw.Issue)
			if e != nil || draw.Game != task.Game || issue != task.Issue {
				return appError(502, "开奖期号与请求不一致")
			}
			bets, e := s.Repository.ForIssue(task.Game, task.Issue)
			if e != nil {
				return e
			}
			updated := 0
			for _, b := range bets {
				result, e := Evaluate(b, *draw)
				if e == nil {
					e = s.Repository.SaveCheck(*draw, []Evaluation{{ID: b.ID, Result: result}})
				}
				if e != nil {
					report.Errors = append(report.Errors, CheckError{Game: task.Game, BetID: b.ID, Issue: task.Issue, Message: e.Error()})
					continue
				}
				updated++
			}
			if updated > 0 {
				report.Checked++
			}
			report.UpdatedBets += updated
			return nil
		}()
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		if taskErr != nil {
			report.Errors = append(report.Errors, CheckError{Game: task.Game, BetID: task.ID, Issue: task.Issue, Message: taskErr.Error()})
		}
	}
	return report, nil
}
