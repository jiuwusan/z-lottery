package lottery

import (
	"path/filepath"
	"testing"
)

func repoOfficialDraw(issue string) Draw {
	d := fixtureDraw(issue, true)
	for grade := 1; grade <= 7; grade++ {
		v := float64(grade) * 100
		d.Prizes[grade] = &v
	}
	d.AppendPrizes = map[int]*float64{1: amountPointer(80), 2: amountPointer(60)}
	return d
}
func TestOfficialCacheAndWindowPersistIndependentlyOfBets(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "official.sqlite")
	r, e := OpenRepository(filename)
	if e != nil {
		t.Fatal(e)
	}
	d := repoOfficialDraw("26125")
	if e = r.SaveOfficialSnapshot("dlt", "2026-11-02", "2026-11-01", "2026-11-02", []Draw{d}, true); e != nil {
		t.Fatal(e)
	}
	r.Close()
	r, e = OpenRepository(filename)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	state, e := r.SyncState("dlt")
	if e != nil || !state.Complete || state.WindowDate != "2026-11-02" || state.HistoryFrom != "2026-11-01" {
		t.Fatal(state, e)
	}
	cached, e := r.CachedDraw("dlt", "26125")
	if e != nil || cached == nil || !OfficialAmountsComplete(*cached) {
		t.Fatal(cached, e)
	}
	page, e := r.List(ListOptions{Page: 1, PageSize: 10})
	if e != nil || page.Total != 0 {
		t.Fatal(page, e)
	}
}
func TestOfficialSnapshotAtomicAndNeverDowngradesCompleteAmounts(t *testing.T) {
	r := testRepo(t)
	d := repoOfficialDraw("26125")
	e := r.SaveOfficialSnapshot("dlt", "2026-11-02", "2026-11-01", "2026-11-02", []Draw{d}, true)
	if e != nil {
		t.Fatal(e)
	}
	original := *d.Prizes[1]
	d.Prizes[1] = nil
	if e = r.SaveOfficialSnapshot("dlt", "2026-11-02", "2026-11-01", "2026-11-02", []Draw{d}, true); e != nil {
		t.Fatal(e)
	}
	cached, _ := r.CachedDraw("dlt", "26125")
	if cached == nil || cached.Prizes[1] == nil || *cached.Prizes[1] != original {
		t.Fatal("complete official amounts changed", cached)
	}
	next := repoOfficialDraw("26126")
	bad := next
	bad.Game = "ssq"
	if e = r.SaveOfficialSnapshot("dlt", "2026-11-04", "2026-10-01", "2026-11-04", []Draw{next, bad}, false); e == nil {
		t.Fatal("mixed game accepted")
	}
	state, _ := r.SyncState("dlt")
	if state.WindowDate != "2026-11-02" || !state.Complete {
		t.Fatal("sync marker escaped rollback", state)
	}
	cached, e = r.CachedDraw("dlt", "26126")
	if e != nil || cached != nil {
		t.Fatal("draw cache escaped rollback", cached, e)
	}
}
