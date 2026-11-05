package lottery

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type fakeProvider struct {
	draw     func(context.Context, string, string) (*Draw, error)
	history  func(context.Context, string, string, string) ([]Draw, error)
	snapshot func(context.Context, string, string, string) (OfficialSnapshot, error)
}

func (p fakeProvider) GetDraw(c context.Context, g, i string) (*Draw, error) {
	if p.draw == nil {
		return nil, nil
	}
	d, err := p.draw(c, g, i)
	if d != nil {
		official := serviceOfficialFixture(*d)
		d = &official
	}
	return d, err
}
func (p fakeProvider) GetHistory(c context.Context, g, f, u string) ([]Draw, error) {
	if p.history == nil {
		return []Draw{}, nil
	}
	return p.history(c, g, f, u)
}

func serviceOfficialFixture(draw Draw) Draw {
	prizes := map[int]*float64{}
	for level, value := range draw.Prizes {
		prizes[level] = value
	}
	levels := 7
	if draw.Game == "ssq" {
		levels = 6
	} else if draw.Issue < "26014" {
		levels = 9
	}
	for level := 2; level <= levels; level++ {
		if _, exists := prizes[level]; !exists {
			prizes[level] = testAmount(0)
		}
	}
	draw.Prizes = prizes
	draw.AppendPrizes = map[int]*float64{1: testAmount(0), 2: testAmount(0)}
	return draw
}

func serviceSnapshotFixture(draws []Draw) OfficialSnapshot {
	snapshot := OfficialSnapshot{Draws: []Draw{}}
	for _, draw := range draws {
		draw = serviceOfficialFixture(draw)
		snapshot.Draws = append(snapshot.Draws, draw)
		if snapshot.Latest == nil || draw.Date > snapshot.LatestDate || draw.Date == snapshot.LatestDate && draw.Issue > snapshot.LatestIssue {
			latest := draw
			snapshot.Latest, snapshot.LatestIssue, snapshot.LatestDate, snapshot.LatestReady = &latest, draw.Issue, draw.Date, true
		}
	}
	return snapshot
}

func (p fakeProvider) GetSnapshot(c context.Context, g, f, u string) (OfficialSnapshot, error) {
	if p.snapshot != nil {
		return p.snapshot(c, g, f, u)
	}
	if g == "ssq" {
		draw := Draw{Game: "ssq", Issue: "2026125", Date: "2026-11-02", Front: []int{1, 2, 3, 4, 5, 6}, Back: []int{1}, Prizes: map[int]*float64{1: testAmount(0)}}
		return serviceSnapshotFixture([]Draw{draw}), nil
	}
	if p.history != nil {
		draws, err := p.history(c, g, f, u)
		return serviceSnapshotFixture(draws), err
	}
	if p.draw != nil {
		draw, err := p.draw(c, g, "26125")
		if err != nil {
			return OfficialSnapshot{}, err
		}
		if draw != nil {
			return serviceSnapshotFixture([]Draw{*draw}), nil
		}
	}
	return OfficialSnapshot{Draws: []Draw{}}, nil
}

func serviceFixture(r *Repository, p Provider) *Service {
	s := NewService(r, p)
	s.Clock = func() time.Time { return time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC) }
	return s
}
func testRepo(t *testing.T) *Repository {
	t.Helper()
	r, e := OpenRepository(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close() })
	return r
}
func fixtureDraw(issue string, complete bool) Draw {
	amount := 1000000.0
	prices := map[int]*float64{}
	if complete {
		prices[1] = &amount
	}
	return Draw{Game: "dlt", Issue: issue, Date: "2026-11-02", Front: []int{1, 2, 3, 4, 5}, Back: []int{1, 2}, Prizes: prices, AppendPrizes: map[int]*float64{}}
}

const issueInput = `{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"issues":["26125"]}`

func TestServiceSameIssueFailureDoesNotBlockHealthyBet(t *testing.T) {
	r := testRepo(t)
	p := fakeProvider{draw: func(context.Context, string, string) (*Draw, error) { d := fixtureDraw("26125", true); return &d, nil }}
	s := serviceFixture(r, p)
	createFixture(t, s, `{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"issues":["26125"],"multiple":10000000000}`)
	healthy := createFixture(t, s, issueInput)
	report, e := s.Check(context.Background(), "", 20)
	if e != nil {
		t.Fatal(e)
	}
	got, e := r.Get(healthy.ID)
	if e != nil || got.WinAmount == nil || len(report.Errors) != 1 {
		t.Fatalf("healthy bet blocked: %+v report=%+v err=%v", got, report, e)
	}
}

func createFixture(t *testing.T, s *Service, raw string) Bet {
	t.Helper()
	b, e := s.Create([]byte(raw))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestServiceRetriesMissingMoneyAndStopsComplete(t *testing.T) {
	r := testRepo(t)
	complete := false
	heldPrizeReady := false
	calls := 0
	p := fakeProvider{snapshot: func(_ context.Context, game, _, _ string) (OfficialSnapshot, error) {
		if game == "ssq" {
			return (fakeProvider{}).GetSnapshot(context.Background(), game, "", "")
		}
		calls++
		d := fixtureDraw("26125", heldPrizeReady)
		snapshot := serviceSnapshotFixture([]Draw{d})
		if !complete {
			delete(snapshot.Draws[0].Prizes, 7)
		}
		return snapshot, nil
	}}
	s := serviceFixture(r, p)
	b := createFixture(t, s, issueInput)
	future := createFixture(t, s, `{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"issues":["26126"]}`)
	if _, e := s.Check(context.Background(), "", 20); e != nil {
		t.Fatal(e)
	}
	got, _ := r.Get(b.ID)
	if got.Status != "prize_pending" || got.WinAmount != nil {
		t.Fatalf("pending money: %+v", got)
	}
	heldPrizeReady = true
	s.Check(context.Background(), "", 20)
	got, _ = r.Get(b.ID)
	if got.Status != "won" || got.WinAmount == nil || *got.WinAmount != 1000000 {
		t.Fatalf("complete: %+v", got)
	}
	s.Check(context.Background(), "", 20)
	state, err := r.SyncState("dlt")
	if err != nil || state.Complete || calls != 3 {
		t.Fatalf("completed bettor's prize hid missing official seventh grade: %+v calls=%d err=%v", state, calls, err)
	}
	complete = true
	s.Check(context.Background(), "", 20)
	s.Check(context.Background(), "", 20)
	if calls != 4 {
		t.Fatalf("official complete draw fetched again despite future pending bet: %d", calls)
	}
	got, _ = r.Get(future.ID)
	if got.Status != "pending" || got.WinAmount != nil {
		t.Fatalf("future issue prematurely completed: %+v", got)
	}
}
func TestServicePagesFailuresAndLostCompletion(t *testing.T) {
	r := testRepo(t)
	calls := 0
	p := fakeProvider{draw: func(_ context.Context, _ string, i string) (*Draw, error) {
		calls++
		if i == "26125" {
			return nil, errors.New("offline")
		}
		d := fixtureDraw(i, true)
		d.Front = []int{20, 21, 22, 23, 24}
		d.Back = []int{10, 11}
		return &d, nil
	}}
	p.snapshot = func(_ context.Context, game, _, _ string) (OfficialSnapshot, error) {
		if game == "ssq" {
			return (fakeProvider{}).GetSnapshot(context.Background(), game, "", "")
		}
		d := fixtureDraw("26126", true)
		d.Front, d.Back = []int{20, 21, 22, 23, 24}, []int{10, 11}
		return serviceSnapshotFixture([]Draw{d}), nil
	}
	s := serviceFixture(r, p)
	createFixture(t, s, issueInput)
	b := createFixture(t, s, `{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"issues":["26126"]}`)
	report, e := s.Check(context.Background(), "", 1)
	if e != nil || len(report.Errors) != 1 || report.NextCursor == nil {
		t.Fatalf("first: %+v %v", report, e)
	}
	next, e := s.Check(context.Background(), *report.NextCursor, 1)
	if e != nil || next.NextCursor != nil {
		t.Fatal(e, next)
	}
	got, _ := r.Get(b.ID)
	if got.Status != "lost" || got.WinAmount == nil || *got.WinAmount != 0 {
		t.Fatalf("lost: %+v", got)
	}
	s.Check(context.Background(), "", 20)
	if calls != 2 {
		t.Fatal("completed lost record retried", calls)
	}
}
func TestServiceDatePartialKeepsCheckingUntilAllMoneyComplete(t *testing.T) {
	r := testRepo(t)
	rows := []Draw{fixtureDraw("26125", true)}
	p := fakeProvider{history: func(context.Context, string, string, string) ([]Draw, error) { return rows, nil }}
	s := serviceFixture(r, p)
	b := createFixture(t, s, `{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"periods":2,"placedAt":"2026-11-01T10:00:00+08:00"}`)
	s.Check(context.Background(), "", 20)
	got, _ := r.Get(b.ID)
	if got.Status != "partial" || got.WinAmount != nil {
		t.Fatal(got)
	}
	d := fixtureDraw("26126", false)
	d.Date = "2026-11-04"
	rows = append(rows, d)
	s.Clock = func() time.Time { return time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC) }
	s.Check(context.Background(), "", 20)
	got, _ = r.Get(b.ID)
	if got.Status != "prize_pending" {
		t.Fatal(got.Status)
	}
	d = fixtureDraw("26126", true)
	d.Date = "2026-11-04"
	rows[1] = d
	s.Check(context.Background(), "", 20)
	got, _ = r.Get(b.ID)
	if got.WinAmount == nil || *got.WinAmount != 2000000 {
		t.Fatal(got)
	}
	tasks, e := r.Pending("", 20)
	if e != nil || len(tasks) != 0 {
		t.Fatal(tasks, e)
	}
}
func TestRepositoryRestartAndLegacyMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	r, e := OpenRepository(path)
	if e != nil {
		t.Fatal(e)
	}
	// Exact schema used before match_mode / placed_at were introduced.
	r.db.Exec("DROP TABLE results; DROP TABLE bet_issues; DROP TABLE bets; CREATE TABLE bets(id INTEGER PRIMARY KEY AUTOINCREMENT,game TEXT NOT NULL,payload TEXT NOT NULL,created_at TEXT NOT NULL DEFAULT '2026-11-01T00:00:00.000Z')")
	payload := `{"config":{"game":"dlt","front":5,"back":2,"count":1,"multiple":1,"periods":1,"append":false},"tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"issues":["26125"],"note":"旧记录","totalAmount":2,"amountPerPeriod":2}`
	r.db.Exec("INSERT INTO bets(game,payload) VALUES('dlt',?)", payload)
	r.Close()
	r, e = OpenRepository(path)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	r.db.Exec("INSERT INTO bet_issues VALUES(1,'26125')")
	b, e := r.Get(1)
	if e != nil || b.MatchMode != "issue" || b.Note != "旧记录" || b.PlacedAt != "2026-11-01T00:00:00.000Z" {
		t.Fatal(b, e)
	}
	s := serviceFixture(r, fakeProvider{draw: func(context.Context, string, string) (*Draw, error) { d := fixtureDraw("26125", true); return &d, nil }})
	s.Check(context.Background(), "", 20)
	b, e = r.Get(1)
	if e != nil || b.WinAmount == nil {
		t.Fatal(b, e)
	}
	if e = r.Remove(1); e != nil {
		t.Fatal(e)
	}
	var count int
	r.db.QueryRow("SELECT count(*) FROM results").Scan(&count)
	if count != 0 {
		t.Fatal("results not cascaded")
	}
}

func TestServiceCompletedSharedIssueIsNeverRewritten(t *testing.T) {
	r := testRepo(t)
	price := 1000000.0
	s := serviceFixture(r, fakeProvider{draw: func(context.Context, string, string) (*Draw, error) {
		d := fixtureDraw("26125", true)
		d.Prizes[1] = &price
		return &d, nil
	}})
	first := createFixture(t, s, issueInput)
	s.Check(context.Background(), "", 20)
	later := createFixture(t, s, issueInput)
	price = 2000000
	s.Check(context.Background(), "", 20)
	a, _ := r.Get(first.ID)
	b, _ := r.Get(later.ID)
	if a.WinAmount == nil || *a.WinAmount != 1000000 || b.WinAmount == nil || *b.WinAmount != 1000000 {
		t.Fatalf("completed record changed: %+v %+v", a, b)
	}
}

func TestRepositoryDatePrefixAndRollback(t *testing.T) {
	r := testRepo(t)
	s := NewService(r, fakeProvider{})
	b := createFixture(t, s, `{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"periods":2}`)
	first := fixtureDraw("26125", true)
	result, _ := Evaluate(b, first)
	if _, e := r.SaveDateCheck(b.ID, []Draw{first}, []Result{result}); e != nil {
		t.Fatal(e)
	}
	conflicting := fixtureDraw("26124", true)
	if _, e := r.SaveDateCheck(b.ID, []Draw{conflicting}, []Result{result}); e == nil {
		t.Fatal("matched prefix overwritten")
	}
	second := fixtureDraw("26126", true)
	overflow, _ := Evaluate(b, second)
	overflow.KnownAmount = float64(MaxSafeInteger) / 100
	if _, e := r.SaveDateCheck(b.ID, []Draw{first, second}, []Result{result, overflow}); e == nil {
		t.Fatal("cross-period overflow accepted")
	}
	got, _ := r.Get(b.ID)
	if len(got.Issues) != 1 || len(got.Results) != 1 || got.Issues[0] != "26125" {
		t.Fatalf("transaction did not roll back: %+v", got)
	}
	var count int
	r.db.QueryRow("SELECT count(*) FROM draws WHERE issue='26126'").Scan(&count)
	if count != 0 {
		t.Fatal("draw cache escaped transaction")
	}
}

func TestServiceDeleteWhileFetchingAndRejectOverlap(t *testing.T) {
	r := testRepo(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	s := serviceFixture(r, fakeProvider{snapshot: func(_ context.Context, game, _, _ string) (OfficialSnapshot, error) {
		if game == "ssq" {
			return (fakeProvider{}).GetSnapshot(context.Background(), game, "", "")
		}
		close(entered)
		<-release
		d := fixtureDraw("26125", true)
		return serviceSnapshotFixture([]Draw{d}), nil
	}})
	b := createFixture(t, s, issueInput)
	done := make(chan error, 1)
	go func() { _, e := s.Check(context.Background(), "", 20); done <- e }()
	<-entered
	if _, e := s.Check(context.Background(), "", 20); e == nil {
		t.Fatal("overlapping service check allowed")
	}
	if e := r.Remove(b.ID); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	page, e := r.List(ListOptions{Page: 1, PageSize: 10})
	if e != nil || page.Total != 0 {
		t.Fatal(page, e)
	}
}

func TestRepositoryCurrentDateDataPersistsAndFilters(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "current.sqlite")
	r, e := OpenRepository(filename)
	if e != nil {
		t.Fatal(e)
	}
	s := NewService(r, fakeProvider{})
	b := createFixture(t, s, `{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"periods":2}`)
	d := fixtureDraw("26125", true)
	result, _ := Evaluate(b, d)
	if _, e = r.SaveDateCheck(b.ID, []Draw{d}, []Result{result}); e != nil {
		t.Fatal(e)
	}
	r.Close()
	r, e = OpenRepository(filename)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	got, e := r.Get(b.ID)
	if e != nil || got.MatchMode != "date" || got.PlacedAt != b.PlacedAt || got.Status != "partial" || len(got.Results) != 1 {
		t.Fatal(got, e)
	}
	page, e := r.List(ListOptions{Page: 1, PageSize: 1, Game: "dlt", Status: "partial"})
	if e != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatal(page, e)
	}
	page, e = r.List(ListOptions{Page: 2, PageSize: 1})
	if e != nil || page.Total != 1 || len(page.Items) != 0 {
		t.Fatal(page, e)
	}
}
