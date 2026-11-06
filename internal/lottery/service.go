package lottery

import (
	"context"
	"errors"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/jiuwusan/z-lottery/internal/domain"
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

// BetRepository is the storage port required by the application use cases.
type BetRepository interface {
	Create(domain.Payload) (domain.Bet, error)
	List(domain.ListOptions) (domain.BetPage, error)
	Get(int64) (domain.Bet, error)
	Remove(int64) error
	Pending(string, int) ([]domain.PendingTask, error)
	ForIssue(string, string) ([]domain.Bet, error)
	SaveCheck(domain.Draw, []domain.Evaluation) error
	SaveDateCheck(int64, []domain.Draw, []domain.Result) (int, error)
	SyncState(string) (domain.SyncState, error)
	SaveOfficialSnapshot(string, string, string, string, []domain.Draw, bool) error
	CachedDraw(string, string) (*domain.Draw, error)
	CachedDraws(string) ([]domain.Draw, error)
	RequiredHistoryFrom(string) (string, error)
	PendingExplicitIssues(string) ([]string, error)
}

type Service struct {
	Repository BetRepository
	Provider   domain.Provider
	Clock      func() time.Time
	running    atomic.Bool
}

func NewService(r BetRepository, p domain.Provider) *Service {
	return &Service{Repository: r, Provider: p, Clock: time.Now}
}
func (s *Service) Create(raw []byte) (domain.Bet, error) {
	p, e := domain.NormalizeBet(raw, s.Clock())
	if e != nil {
		return domain.Bet{}, e
	}
	return s.Repository.Create(p)
}

var cursorPattern = regexp.MustCompile(`^(bet:\d{16}|dlt:\d{5}|ssq:20\d{5})$`)

func (s *Service) Check(ctx context.Context, after string, limit int) (CheckReport, error) {
	report := CheckReport{Errors: []CheckError{}}
	if limit < 1 || limit > 100 {
		return report, domain.NewError(400, "批次数量必须是 1～100 的整数")
	}
	if after != "" && !cursorPattern.MatchString(after) {
		return report, domain.NewError(400, "无效的验奖游标")
	}
	if !s.running.CompareAndSwap(false, true) {
		return report, domain.NewError(409, "验奖任务正在运行，请稍后重试")
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
	cache := map[string][]domain.Draw{}
	for _, task := range tasks {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		taskErr := func() error {
			if task.ID != 0 {
				b, e := s.Repository.Get(task.ID)
				if e != nil {
					var ae *domain.AppError
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
				if state.HistoryFrom == "" || domain.ShanghaiDate(placed) < state.HistoryFrom {
					report.Waiting++
					return nil
				}
				selected, e := domain.SelectDraws(task.Game, b.PlacedAt, b.Config.Periods, rows)
				if e != nil {
					return e
				}
				results := make([]domain.Result, 0, len(selected))
				for _, d := range selected {
					result, e := domain.Evaluate(b, d)
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
			issue, e := domain.NormalizeIssue(draw.Game, draw.Issue)
			if e != nil || draw.Game != task.Game || issue != task.Issue {
				return domain.NewError(502, "开奖期号与请求不一致")
			}
			bets, e := s.Repository.ForIssue(task.Game, task.Issue)
			if e != nil {
				return e
			}
			updated := 0
			for _, b := range bets {
				result, e := domain.Evaluate(b, *draw)
				if e == nil {
					e = s.Repository.SaveCheck(*draw, []domain.Evaluation{{ID: b.ID, Result: result}})
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

func (s *Service) List(o domain.ListOptions) (domain.BetPage, error) { return s.Repository.List(o) }
func (s *Service) Get(id int64) (domain.Bet, error)                  { return s.Repository.Get(id) }
func (s *Service) Remove(id int64) error                             { return s.Repository.Remove(id) }
