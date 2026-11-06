package repository

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/jiuwusan/z-lottery/internal/domain"
)

// Official synchronization and history coverage belong to the shared draw cache,
// never to a bettor's configured number of future periods.
func (r *Repository) SyncState(game string) (domain.SyncState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := domain.SyncState{Game: game}
	err := r.db.QueryRow("SELECT window_date,complete,history_from,history_through FROM draw_sync WHERE game=?", game).Scan(&s.WindowDate, &s.Complete, &s.HistoryFrom, &s.HistoryThrough)
	if err == sql.ErrNoRows {
		return s, nil
	}
	return s, err
}
func cacheOfficial(q queryer, draw domain.Draw) error {
	var old string
	err := q.QueryRow("SELECT payload FROM draws WHERE game=? AND issue=?", draw.Game, draw.Issue).Scan(&old)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		var existing domain.Draw
		if err = json.Unmarshal([]byte(old), &existing); err != nil {
			return err
		}
		if domain.OfficialAmountsComplete(existing) {
			return nil
		}
	}
	raw, err := json.Marshal(draw)
	if err != nil {
		return err
	}
	_, err = q.Exec(`INSERT INTO draws(game,issue,payload,updated_at) VALUES(?,?,?,?) ON CONFLICT(game,issue) DO UPDATE SET payload=excluded.payload,updated_at=excluded.updated_at`, draw.Game, draw.Issue, string(raw), time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	return err
}
func (r *Repository) SaveOfficialSnapshot(game, window, from, through string, draws []domain.Draw, complete bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.transaction(func(tx *sql.Tx) error {
		for _, draw := range draws {
			if draw.Game != game {
				return domain.NewError(502, "官方同步彩种不一致")
			}
			if err := cacheOfficial(tx, draw); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`INSERT INTO draw_sync(game,window_date,complete,history_from,history_through) VALUES(?,?,?,?,?) ON CONFLICT(game) DO UPDATE SET window_date=excluded.window_date,complete=excluded.complete,history_from=excluded.history_from,history_through=excluded.history_through`, game, window, complete, from, through)
		return err
	})
}
func (r *Repository) CachedDraw(game, issue string) (*domain.Draw, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var raw string
	err := r.db.QueryRow("SELECT payload FROM draws WHERE game=? AND issue=?", game, issue).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var draw domain.Draw
	if err = json.Unmarshal([]byte(raw), &draw); err != nil {
		return nil, err
	}
	return &draw, nil
}
func (r *Repository) CachedDraws(game string) ([]domain.Draw, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows, err := r.db.Query("SELECT payload FROM draws WHERE game=? ORDER BY json_extract(payload,'$.date'),issue", game)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	draws := []domain.Draw{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var draw domain.Draw
		if err = json.Unmarshal([]byte(raw), &draw); err != nil {
			return nil, err
		}
		draws = append(draws, draw)
	}
	return draws, rows.Err()
}
func (r *Repository) RequiredHistoryFrom(game string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var placed sql.NullString
	if err := r.db.QueryRow(statesSQL+"SELECT min(COALESCE(placed_at,created_at)) FROM states WHERE game=? AND match_mode='date' AND status IN ('pending','partial','prize_pending')", game).Scan(&placed); err != nil {
		return "", err
	}
	if !placed.Valid {
		return "", nil
	}
	at, err := time.Parse(time.RFC3339Nano, placed.String)
	if err != nil {
		return "", err
	}
	return domain.ShanghaiDate(at), nil
}
func (r *Repository) PendingExplicitIssues(game string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows, err := r.db.Query(`SELECT DISTINCT i.issue FROM bets b JOIN bet_issues i ON i.bet_id=b.id LEFT JOIN results r ON r.bet_id=i.bet_id AND r.issue=i.issue WHERE b.game=? AND b.match_mode='issue' AND (r.issue IS NULL OR r.complete=0) ORDER BY i.issue`, game)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	issues := []string{}
	for rows.Next() {
		var issue string
		if err = rows.Scan(&issue); err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}
