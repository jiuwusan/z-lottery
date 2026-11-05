package lottery

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const statesSQL = `WITH states AS (
 SELECT b.*, CASE WHEN COUNT(r.issue)=0 THEN 'pending'
 WHEN COUNT(r.issue)<json_extract(b.payload,'$.config.periods') THEN 'partial'
 WHEN SUM(COALESCE(r.complete,0))<json_extract(b.payload,'$.config.periods') THEN 'prize_pending'
 WHEN MAX(COALESCE(r.won,0))=1 THEN 'won' ELSE 'lost' END AS status
 FROM bets b LEFT JOIN bet_issues i ON i.bet_id=b.id
 LEFT JOIN results r ON r.bet_id=i.bet_id AND r.issue=i.issue GROUP BY b.id
) `

type Repository struct {
	db *sql.DB
	mu sync.Mutex
}
type queryer interface {
	Exec(string, ...any) (sql.Result, error)
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}
type ListOptions struct {
	Page, PageSize int
	Game, Status   string
}
type BetPage struct {
	Items    []Bet `json:"items"`
	Total    int   `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}
type PendingTask struct {
	ID                         int64
	Game, Issue, PlacedAt, Key string
}
type Evaluation struct {
	ID     int64
	Result Result
}

func OpenRepository(filename string) (*Repository, error) {
	if filename != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	r := &Repository{db: db}
	fail := func(e error) (*Repository, error) { db.Close(); return nil, e }
	_, err = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS bets(id INTEGER PRIMARY KEY AUTOINCREMENT,game TEXT NOT NULL,payload TEXT NOT NULL,match_mode TEXT NOT NULL DEFAULT 'issue',placed_at TEXT,created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')));
 CREATE TABLE IF NOT EXISTS bet_issues(bet_id INTEGER NOT NULL REFERENCES bets(id) ON DELETE CASCADE,issue TEXT NOT NULL,PRIMARY KEY(bet_id,issue));
 CREATE INDEX IF NOT EXISTS idx_bets_game ON bets(game);
 CREATE TABLE IF NOT EXISTS draws(game TEXT NOT NULL,issue TEXT NOT NULL,payload TEXT NOT NULL,updated_at TEXT NOT NULL,PRIMARY KEY(game,issue));
 CREATE TABLE IF NOT EXISTS draw_sync(game TEXT PRIMARY KEY,window_date TEXT NOT NULL,complete INTEGER NOT NULL DEFAULT 0,history_from TEXT NOT NULL DEFAULT '',history_through TEXT NOT NULL DEFAULT '');
 CREATE TABLE IF NOT EXISTS results(bet_id INTEGER NOT NULL,issue TEXT NOT NULL,payload TEXT NOT NULL,complete INTEGER NOT NULL,won INTEGER NOT NULL,checked_at TEXT NOT NULL,PRIMARY KEY(bet_id,issue),FOREIGN KEY(bet_id,issue) REFERENCES bet_issues(bet_id,issue) ON DELETE CASCADE);`)
	if err != nil {
		return fail(err)
	}
	rows, err := db.Query("PRAGMA table_info(bets)")
	if err != nil {
		return fail(err)
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return fail(err)
		}
		columns[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail(err)
	}
	for _, column := range []struct{ name, sql string }{{"match_mode", "ALTER TABLE bets ADD COLUMN match_mode TEXT NOT NULL DEFAULT 'issue'"}, {"placed_at", "ALTER TABLE bets ADD COLUMN placed_at TEXT"}} {
		if !columns[column.name] {
			if _, err = db.Exec(column.sql); err != nil {
				return fail(err)
			}
		}
	}
	return r, nil
}
func (r *Repository) Close() error { r.mu.Lock(); defer r.mu.Unlock(); return r.db.Close() }
func (r *Repository) transaction(fn func(*sql.Tx) error) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

type storedBet struct {
	id                             int64
	payload, mode, created, status string
	placed                         sql.NullString
}

func scanStored(rows interface{ Scan(...any) error }) (storedBet, error) {
	var row storedBet
	err := rows.Scan(&row.id, &row.payload, &row.mode, &row.placed, &row.created, &row.status)
	return row, err
}

const selectStored = "SELECT id,payload,match_mode,placed_at,created_at,status FROM states "

func hydrate(q queryer, row storedBet) (Bet, error) {
	var b Bet
	if err := json.Unmarshal([]byte(row.payload), &b.Payload); err != nil {
		return b, err
	}
	b.ID = row.id
	b.MatchMode = row.mode
	b.CreatedAt = row.created
	b.PlacedAt = row.created
	if row.placed.Valid {
		b.PlacedAt = row.placed.String
	}
	b.Status = row.status
	b.Issues = []string{}
	b.Results = []Result{}
	rows, err := q.Query("SELECT issue FROM bet_issues WHERE bet_id=? ORDER BY issue", row.id)
	if err != nil {
		return b, err
	}
	for rows.Next() {
		var i string
		if err = rows.Scan(&i); err != nil {
			break
		}
		b.Issues = append(b.Issues, i)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return b, err
	}
	rows, err = q.Query("SELECT payload,checked_at FROM results WHERE bet_id=? ORDER BY issue", row.id)
	if err != nil {
		return b, err
	}
	var cents int64
	complete := true
	for rows.Next() {
		var text, at string
		if err = rows.Scan(&text, &at); err != nil {
			break
		}
		var result Result
		if err = json.Unmarshal([]byte(text), &result); err != nil {
			break
		}
		result.CheckedAt = at
		b.Results = append(b.Results, result)
		cents += int64(math.Round(result.KnownAmount * 100))
		complete = complete && result.Complete
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return b, err
	}
	b.CheckedPeriods = len(b.Results)
	b.KnownWinAmount = float64(cents) / 100
	if complete && len(b.Results) == b.Config.Periods {
		amount := b.KnownWinAmount
		b.WinAmount = &amount
	}
	return b, nil
}
func getWith(q queryer, id int64) (Bet, error) {
	row, err := scanStored(q.QueryRow(statesSQL+selectStored+"WHERE id=?", id))
	if err == sql.ErrNoRows {
		return Bet{}, appError(404, "投注记录不存在")
	}
	if err != nil {
		return Bet{}, err
	}
	return hydrate(q, row)
}
func (r *Repository) Get(id int64) (Bet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return getWith(r.db, id)
}
func (r *Repository) Create(payload Payload) (Bet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b Bet
	err := r.transaction(func(tx *sql.Tx) error {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		result, err := tx.Exec("INSERT INTO bets(game,payload,match_mode,placed_at) VALUES(?,?,?,?)", payload.Config.Game, string(raw), payload.MatchMode, payload.PlacedAt)
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		for _, issue := range payload.Issues {
			if _, err = tx.Exec("INSERT INTO bet_issues(bet_id,issue) VALUES(?,?)", id, issue); err != nil {
				return err
			}
		}
		b, err = getWith(tx, id)
		return err
	})
	return b, err
}
func (r *Repository) Remove(id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	result, err := r.db.Exec("DELETE FROM bets WHERE id=?", id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return appError(404, "投注记录不存在")
	}
	return nil
}
func (r *Repository) List(o ListOptions) (BetPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := BetPage{Items: []Bet{}, Page: o.Page, PageSize: o.PageSize}
	where := " WHERE 1=1"
	args := []any{}
	if o.Game != "" {
		where += " AND game=?"
		args = append(args, o.Game)
	}
	if o.Status != "" {
		where += " AND status=?"
		args = append(args, o.Status)
	}
	if err := r.db.QueryRow(statesSQL+"SELECT COUNT(*) FROM states"+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := r.db.Query(statesSQL+selectStored+where+" ORDER BY id DESC LIMIT ? OFFSET ?", append(args, o.PageSize, (o.Page-1)*o.PageSize)...)
	if err != nil {
		return out, err
	}
	stored := []storedBet{}
	for rows.Next() {
		row, e := scanStored(rows)
		if e != nil {
			err = e
			break
		}
		stored = append(stored, row)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, row := range stored {
		b, e := hydrate(r.db, row)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, b)
	}
	return out, nil
}
func (r *Repository) Pending(after string, limit int) ([]PendingTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows, err := r.db.Query(statesSQL+`SELECT id,game,issue,placed_at,key FROM (
 SELECT id,game,'' AS issue,placed_at,'bet:'||printf('%016d',id) AS key FROM states WHERE match_mode='date' AND status IN ('pending','partial','prize_pending')
 UNION ALL SELECT DISTINCT 0,b.game,i.issue,'' AS placed_at,b.game||':'||i.issue AS key FROM bets b JOIN bet_issues i ON i.bet_id=b.id LEFT JOIN results r ON r.bet_id=i.bet_id AND r.issue=i.issue WHERE b.match_mode='issue' AND (r.issue IS NULL OR r.complete=0)
 ) WHERE key>? ORDER BY key LIMIT ?`, after, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []PendingTask{}
	for rows.Next() {
		var task PendingTask
		var placed sql.NullString
		if err = rows.Scan(&task.ID, &task.Game, &task.Issue, &placed, &task.Key); err != nil {
			return nil, err
		}
		task.PlacedAt = placed.String
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}
func (r *Repository) ForIssue(game, issue string) ([]Bet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows, err := r.db.Query(`SELECT b.id FROM bets b JOIN bet_issues i ON i.bet_id=b.id LEFT JOIN results r ON r.bet_id=i.bet_id AND r.issue=i.issue WHERE b.match_mode='issue' AND b.game=? AND i.issue=? AND (r.issue IS NULL OR r.complete=0)`, game, issue)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	bets := []Bet{}
	for _, id := range ids {
		b, e := getWith(r.db, id)
		if e != nil {
			return nil, e
		}
		bets = append(bets, b)
	}
	return bets, nil
}
func writeCheck(tx *sql.Tx, draw Draw, evaluations []Evaluation) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if err := cacheOfficial(tx, draw); err != nil {
		return err
	}
	var raw []byte
	var err error
	for _, evaluation := range evaluations {
		// A request may delete a bet while the upstream HTTP request is pending.
		var exists int
		if err = tx.QueryRow("SELECT COUNT(*) FROM bet_issues WHERE bet_id=? AND issue=?", evaluation.ID, draw.Issue).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		result := evaluation.Result
		total := math.Round(result.KnownAmount * 100)
		rows, err := tx.Query("SELECT payload FROM results WHERE bet_id=? AND issue<>?", evaluation.ID, draw.Issue)
		if err != nil {
			return err
		}
		for rows.Next() {
			var text string
			if err = rows.Scan(&text); err != nil {
				break
			}
			var other Result
			if err = json.Unmarshal([]byte(text), &other); err != nil {
				break
			}
			total += math.Round(other.KnownAmount * 100)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		if total > float64(MaxSafeInteger) {
			return appError(400, "跨期中奖合计超出安全精度，请拆分投注记录")
		}
		result.CheckedAt = ""
		raw, err = json.Marshal(result)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO results(bet_id,issue,payload,complete,won,checked_at) VALUES(?,?,?,?,?,?) ON CONFLICT(bet_id,issue) DO UPDATE SET payload=excluded.payload,complete=excluded.complete,won=excluded.won,checked_at=excluded.checked_at`, evaluation.ID, draw.Issue, string(raw), result.Complete, result.Won, now); err != nil {
			return err
		}
	}
	return nil
}
func (r *Repository) SaveCheck(draw Draw, evals []Evaluation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.transaction(func(tx *sql.Tx) error { return writeCheck(tx, draw, evals) })
}
func (r *Repository) SaveDateCheck(id int64, draws []Draw, results []Result) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	err := r.transaction(func(tx *sql.Tx) error {
		b, err := getWith(tx, id)
		if e, ok := err.(*AppError); ok && e.Status == 404 {
			return nil
		}
		if err != nil {
			return err
		}
		if b.MatchMode != "date" || len(draws) > b.Config.Periods || len(draws) != len(results) {
			return appError(500, "投注匹配模式或期数异常")
		}
		if len(b.Issues) > len(draws) {
			return appError(502, "历史开奖与已匹配结果不一致，暂缓验奖")
		}
		for i, issue := range b.Issues {
			if draws[i].Issue != issue {
				return appError(502, "历史开奖与已匹配结果不一致，暂缓验奖")
			}
		}
		finished := map[string]bool{}
		for _, result := range b.Results {
			finished[result.Issue] = result.Complete
		}
		for i, d := range draws {
			if finished[d.Issue] {
				continue
			}
			if _, err = tx.Exec("INSERT INTO bet_issues(bet_id,issue) VALUES(?,?) ON CONFLICT DO NOTHING", id, d.Issue); err != nil {
				return err
			}
			if err = writeCheck(tx, d, []Evaluation{{ID: id, Result: results[i]}}); err != nil {
				return fmt.Errorf("保存日期验奖: %w", err)
			}
		}
		count = len(draws)
		return nil
	})
	return count, err
}
