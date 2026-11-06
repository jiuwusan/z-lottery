package lottery

import (
	"context"
	"time"

	"github.com/jiuwusan/z-lottery/internal/domain"
)

// OfficialWindowDate keeps the midnight retry in the preceding evening's window.
func OfficialWindowDate(now time.Time) string {
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		zone = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	local := now.In(zone)
	if local.Hour() < 22 {
		local = local.AddDate(0, 0, -1)
	}
	return local.Format("2006-01-02")
}

// Official publication state controls polling; pending future bets only read cache.
func (s *Service) syncOfficial(ctx context.Context) []CheckError {
	failures := []CheckError{}
	provider, ok := s.Provider.(domain.SnapshotProvider)
	if !ok {
		return []CheckError{{Game: "dlt", Message: "开奖来源不支持官方发布状态同步"}, {Game: "ssq", Message: "开奖来源不支持官方发布状态同步"}}
	}
	now := s.Clock()
	window := OfficialWindowDate(now)
	today := domain.ShanghaiDate(now)
	for _, game := range []string{"dlt", "ssq"} {
		if ctx.Err() != nil {
			break
		}
		gameFailed := false
		record := func(issue string, err error) {
			if err != nil {
				gameFailed = true
				failures = append(failures, CheckError{Game: game, Issue: issue, Message: err.Error()})
			}
		}
		state, err := s.Repository.SyncState(game)
		if err != nil {
			record("", err)
			continue
		}
		required, err := s.Repository.RequiredHistoryFrom(game)
		if err != nil {
			record("", err)
			continue
		}
		backfill := required != "" && (state.HistoryFrom == "" || required < state.HistoryFrom)
		fetch := state.WindowDate != window || !state.Complete || backfill
		latestIssue := ""
		latestReady := false
		hasUnready := false
		var latest *domain.Draw
		seen := map[string]bool{}
		from, through := state.HistoryFrom, state.HistoryThrough
		snapshotOK := false
		if fetch {
			requestFrom := window
			if state.HistoryThrough != "" {
				requestFrom = state.HistoryThrough
			}
			if backfill && required < requestFrom {
				requestFrom = required
			}
			if requestFrom > today {
				requestFrom = today
			}
			snapshot, e := provider.GetSnapshot(ctx, game, requestFrom, today)
			if e != nil {
				record("", e)
			} else {
				latestIssue, latestReady, latest = snapshot.LatestIssue, snapshot.LatestReady, snapshot.Latest
				hasUnready = snapshot.HasUnready
				draws := append([]domain.Draw{}, snapshot.Published...)
				draws = append(draws, snapshot.Draws...)
				if latest != nil {
					draws = append(draws, *latest)
				}
				for _, d := range draws {
					seen[d.Issue] = true
				}
				newFrom := from
				if newFrom == "" || requestFrom < newFrom {
					newFrom = requestFrom
				}
				if e = s.Repository.SaveOfficialSnapshot(game, window, newFrom, today, draws, false); e != nil {
					record("", e)
				} else {
					from, through = newFrom, today
					snapshotOK = true
				}
			}
		}
		if ctx.Err() != nil {
			break
		}
		cached, e := s.Repository.CachedDraws(game)
		if e != nil {
			record("", e)
			continue
		}
		// Persisted complete windows have a fully published latest cached draw. They
		// need no latest request when a new bet merely extends into future periods.
		if !fetch {
			for i := range cached {
				d := cached[i]
				if d.Issue > latestIssue {
					latestIssue, latest = d.Issue, &d
				}
			}
			latestReady = latest != nil && domain.OfficialAmountsComplete(*latest)
		} else if latestIssue == "" {
			for _, d := range cached {
				if d.Issue > latestIssue {
					latestIssue = d.Issue
				}
			}
		}
		pending, e := s.Repository.PendingExplicitIssues(game)
		if e != nil {
			record("", e)
			continue
		}
		refresh := map[string]bool{}
		byIssue := map[string]domain.Draw{}
		for _, d := range cached {
			byIssue[d.Issue] = d
			if !domain.OfficialAmountsComplete(d) {
				refresh[d.Issue] = true
			}
		}
		for _, issue := range pending {
			if _, exists := byIssue[issue]; !exists {
				refresh[issue] = true
			}
		}
		// A missing or unannounced future issue cannot be established by arithmetic.
		// Request only exact issues bounded by the actual official latest record.
		for issue := range refresh {
			if ctx.Err() != nil {
				break
			}
			if seen[issue] || latestIssue == "" || issue > latestIssue {
				continue
			}
			d, e := s.Provider.GetDraw(ctx, game, issue)
			if e != nil {
				record(issue, e)
				continue
			}
			if d == nil {
				// An absent past issue remains eligible for exact historical
				// backfill, without reopening completed latest-window polling.
				continue
			}
			normalized, e := domain.NormalizeIssue(d.Game, d.Issue)
			if e != nil || d.Game != game || normalized != issue {
				record(issue, domain.NewError(502, "开奖期号与请求不一致"))
				continue
			}
			if e = s.Repository.SaveOfficialSnapshot(game, window, from, through, []domain.Draw{*d}, false); e != nil {
				record(issue, e)
				continue
			}
			byIssue[issue] = *d
			if latest != nil && issue == latest.Issue {
				latest = d
			}
		}
		if ctx.Err() != nil {
			break
		}
		cached, e = s.Repository.CachedDraws(game)
		if e != nil {
			record("", e)
			continue
		}
		// The published official list is authoritative. A new polling window
		// does not imply that the lottery must announce a new issue that day.
		complete := !gameFailed && !hasUnready && latestReady && latest != nil && domain.OfficialAmountsComplete(*latest)
		for _, d := range cached {
			if !domain.OfficialAmountsComplete(d) {
				complete = false
				break
			}
		}
		// A failed range request must never certify coverage it did not retrieve.
		if fetch && !snapshotOK {
			continue
		}
		if e = s.Repository.SaveOfficialSnapshot(game, window, from, through, nil, complete); e != nil {
			record("", e)
		}
	}
	return failures
}
