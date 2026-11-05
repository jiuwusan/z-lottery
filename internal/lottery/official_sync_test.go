package lottery

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

type syncFake struct {
	calls      map[string]int
	exact      map[string]int
	from       map[string]string
	snapshots  map[string]OfficialSnapshot
	failures   map[string]error
	exactDraws map[string]*Draw
}

func newSyncFake() *syncFake {
	return &syncFake{calls: map[string]int{}, exact: map[string]int{}, from: map[string]string{}, snapshots: map[string]OfficialSnapshot{}, failures: map[string]error{}}
}
func (f *syncFake) GetSnapshot(_ context.Context, g, from, through string) (OfficialSnapshot, error) {
	f.calls[g]++
	f.from[g] = from
	return f.snapshots[g], f.failures[g]
}
func (f *syncFake) GetHistory(context.Context, string, string, string) ([]Draw, error) {
	panic("unexpected GetHistory")
}
func (f *syncFake) GetDraw(_ context.Context, g, i string) (*Draw, error) {
	f.exact[g+":"+i]++
	return f.exactDraws[g+":"+i], nil
}
func fullOfficial(g, issue, date string) Draw {
	d := Draw{Game: g, Issue: issue, Date: date, Prizes: map[int]*float64{}, AppendPrizes: map[int]*float64{}}
	max := 6
	if g == "dlt" {
		max = 7
	}
	for n := 1; n <= max; n++ {
		v := float64(n * 100)
		d.Prizes[n] = &v
	}
	if g == "dlt" {
		v := 80.0
		d.AppendPrizes[1] = &v
		d.AppendPrizes[2] = &v
	}
	return d
}
func readySnapshot(d Draw) OfficialSnapshot {
	return OfficialSnapshot{Draws: []Draw{d}, Latest: &d, LatestIssue: d.Issue, LatestDate: d.Date, LatestReady: true}
}
func syncService(t *testing.T) (*Service, *syncFake) {
	t.Helper()
	r, e := OpenRepository(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close() })
	f := newSyncFake()
	f.snapshots["dlt"] = readySnapshot(fullOfficial("dlt", "26125", "2026-11-02"))
	f.snapshots["ssq"] = readySnapshot(fullOfficial("ssq", "2026125", "2026-11-02"))
	s := NewService(r, f)
	s.Clock = func() time.Time { return time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC) }
	return s, f
}
func runOfficial(t *testing.T, s *Service) {
	t.Helper()
	if es := s.syncOfficial(context.Background()); len(es) != 0 {
		t.Fatalf("sync errors: %+v", es)
	}
}
func TestOfficialWindowAndNoBets(t *testing.T) {
	s, f := syncService(t)
	runOfficial(t, s)
	runOfficial(t, s)
	if f.calls["dlt"] != 1 || f.calls["ssq"] != 1 {
		t.Fatal(f.calls)
	}
	for _, tc := range []struct{ at, want string }{{"2026-11-02T21:59:59+08:00", "2026-11-01"}, {"2026-11-02T22:00:00+08:00", "2026-11-02"}, {"2026-11-03T00:00:00+08:00", "2026-11-02"}, {"2026-11-03T21:59:59+08:00", "2026-11-02"}} {
		at, _ := time.Parse(time.RFC3339, tc.at)
		if got := OfficialWindowDate(at); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	s.Clock = func() time.Time { return time.Date(2026, 11, 2, 16, 0, 0, 0, time.UTC) }
	runOfficial(t, s)
	if f.calls["dlt"] != 1 {
		t.Fatal("midnight refetched", f.calls)
	}
	s.Clock = func() time.Time { return time.Date(2026, 11, 3, 14, 0, 0, 0, time.UTC) }
	runOfficial(t, s)
	if f.calls["dlt"] != 2 {
		t.Fatal("new window not fetched", f.calls)
	}
}
func TestOfficialFutureBetsDoNotPoll(t *testing.T) {
	s, f := syncService(t)
	runOfficial(t, s)
	for _, raw := range []string{`{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"periods":100,"placedAt":"2026-11-02T12:00:00+08:00"}`, `{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"issues":["26199"]}`} {
		if _, e := s.Create([]byte(raw)); e != nil {
			t.Fatal(e)
		}
	}
	runOfficial(t, s)
	if f.calls["dlt"] != 1 || len(f.exact) != 0 {
		t.Fatal(f.calls, f.exact)
	}
}
func TestOfficialMissingGradeAndGameIndependence(t *testing.T) {
	s, f := syncService(t)
	d := *f.snapshots["dlt"].Latest
	delete(d.Prizes, 7)
	f.snapshots["dlt"] = readySnapshot(d)
	runOfficial(t, s)
	runOfficial(t, s)
	if f.calls["dlt"] != 2 || f.calls["ssq"] != 1 {
		t.Fatal(f.calls)
	}
	d = fullOfficial("dlt", "26125", "2026-11-02")
	f.snapshots["dlt"] = readySnapshot(d)
	runOfficial(t, s)
	runOfficial(t, s)
	if f.calls["dlt"] != 3 {
		t.Fatal(f.calls)
	}
}
func TestOfficialPreviousDayCompleteStopsWhileFailureRetries(t *testing.T) {
	s, f := syncService(t)
	f.snapshots["dlt"] = readySnapshot(fullOfficial("dlt", "26124", "2026-11-01"))
	f.failures["ssq"] = fmt.Errorf("offline")
	if es := s.syncOfficial(context.Background()); len(es) != 1 {
		t.Fatal(es)
	}
	if es := s.syncOfficial(context.Background()); len(es) != 1 {
		t.Fatal(es)
	}
	if f.calls["dlt"] != 1 || f.calls["ssq"] != 2 {
		t.Fatal(f.calls)
	}
}
func TestOfficialMissingOrUnreviewedLatestKeepsPolling(t *testing.T) {
	for _, unreviewed := range []bool{false, true} {
		s, f := syncService(t)
		snapshot := f.snapshots["dlt"]
		if unreviewed {
			snapshot.LatestReady = false
		} else {
			snapshot.Latest = nil
		}
		f.snapshots["dlt"] = snapshot
		runOfficial(t, s)
		runOfficial(t, s)
		if f.calls["dlt"] != 2 {
			t.Fatal(unreviewed, f.calls)
		}
	}
}
func TestOfficialHistoricalBackfillOnce(t *testing.T) {
	s, f := syncService(t)
	runOfficial(t, s)
	if _, e := s.Create([]byte(`{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"periods":100,"placedAt":"2026-10-01T12:00:00+08:00"}`)); e != nil {
		t.Fatal(e)
	}
	runOfficial(t, s)
	runOfficial(t, s)
	if f.calls["dlt"] != 2 || f.from["dlt"] != "2026-10-01" || f.calls["ssq"] != 1 {
		t.Fatal(f.calls, f.from)
	}
}
func TestOfficialRestartPersistsCompletion(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "official.sqlite")
	r, e := OpenRepository(filename)
	if e != nil {
		t.Fatal(e)
	}
	f := newSyncFake()
	for _, g := range []string{"dlt", "ssq"} {
		i := "26125"
		if g == "ssq" {
			i = "2026125"
		}
		f.snapshots[g] = readySnapshot(fullOfficial(g, i, "2026-11-02"))
	}
	s := NewService(r, f)
	s.Clock = func() time.Time { return time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC) }
	runOfficial(t, s)
	r.Close()
	r, e = OpenRepository(filename)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	s.Repository = r
	runOfficial(t, s)
	if f.calls["dlt"] != 1 || f.calls["ssq"] != 1 {
		t.Fatal(f.calls)
	}
}

func TestOfficialRefreshesCachedPastWithoutRequestingSnapshotIssues(t *testing.T) {
	s, f := syncService(t)
	old := fullOfficial("dlt", "26124", "2026-11-01")
	delete(old.Prizes, 7)
	if e := s.Repository.SaveOfficialSnapshot("dlt", "2026-11-02", "2026-11-01", "2026-11-01", []Draw{old}, false); e != nil {
		t.Fatal(e)
	}
	repaired := fullOfficial("dlt", "26124", "2026-11-01")
	f.exactDraws = map[string]*Draw{"dlt:26124": &repaired}
	runOfficial(t, s)
	runOfficial(t, s)
	if f.calls["dlt"] != 1 || f.exact["dlt:26124"] != 1 || f.exact["dlt:26125"] != 0 {
		t.Fatal(f.calls, f.exact)
	}
	state, e := s.Repository.SyncState("dlt")
	if e != nil || !state.Complete {
		t.Fatal(state, e)
	}
}
func TestOfficialUnsupportedProviderReportsBothGames(t *testing.T) {
	s, _ := syncService(t)
	s.Provider = legacyOfficialFake{}
	failures := s.syncOfficial(context.Background())
	if len(failures) != 2 || failures[0].Game != "dlt" || failures[1].Game != "ssq" {
		t.Fatal(failures)
	}
}

type legacyOfficialFake struct{}

func (legacyOfficialFake) GetDraw(context.Context, string, string) (*Draw, error) { return nil, nil }
func (legacyOfficialFake) GetHistory(context.Context, string, string, string) ([]Draw, error) {
	return nil, nil
}
