package domain

import (
	"testing"
	"time"
)

func TestMatchingCutoff(t *testing.T) {
	rows := []Draw{{Game: "dlt", Issue: "26124", Date: "2026-10-31"}, {Game: "dlt", Issue: "26123", Date: "2026-10-28"}, {Game: "dlt", Issue: "26125", Date: "2026-11-02"}}
	for _, tc := range []struct{ at, want string }{{"2026-10-28T20:59:59.999+08:00", "26123"}, {"2026-10-28T21:00:00+08:00", "26124"}} {
		r, e := SelectDraws("dlt", tc.at, 3, rows)
		if e != nil || len(r) == 0 || r[0].Issue != tc.want {
			t.Fatal(r, e)
		}
	}
	for _, d := range []Draw{{Game: "dlt", Date: "2022-12-31"}, {Game: "ssq", Date: "2026-10-28"}} {
		v, e := SalesCloseAt(d)
		if e != nil || v[11:13] != "12" {
			t.Fatal(v, e)
		}
	}
	if ShanghaiDate(time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC)) != "2026-01-02" {
		t.Fatal("timezone")
	}
}
func TestMatchingRejectsBadData(t *testing.T) {
	for _, s := range []string{"2026-02-30T00:00:00Z", "2026-10-28T18:45", "bad"} {
		if _, e := SelectDraws("dlt", s, 1, nil); e == nil {
			t.Fatal(s)
		}
	}
	rows := []Draw{{Game: "dlt", Issue: "26123", Date: "2026-10-28"}, {Game: "dlt", Issue: "26123", Date: "2026-10-29"}}
	if _, e := SelectDraws("dlt", "2026-10-28T00:00:00Z", 1, rows); e == nil {
		t.Fatal("conflict")
	}
	if _, e := SalesCloseAt(Draw{Date: "2026-02-30"}); e == nil {
		t.Fatal("invalid date")
	}
}

func TestMatchingInstantBounds(t *testing.T) {
	for _, s := range []string{"2026-10-28T00:00:00+24:00", "2026-10-28T00:00:00+08:60", "2026-10-28T24:01:00Z", "2026-10-28T24:00:00.001Z"} {
		if _, e := SelectDraws("dlt", s, 1, nil); e == nil {
			t.Fatal("accepted", s)
		}
	}
	for _, s := range []string{"2026-10-28T18:45+08:00", "2026-10-28T24:00:00Z", "2026-10-28T24:00:00.000Z"} {
		if _, e := SelectDraws("dlt", s, 1, nil); e != nil {
			t.Fatal("rejected", s, e)
		}
	}
}
