package lottery

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDomainNormalize(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	raw := `{"game":"dlt","tickets":[{"front":[5,4,3,2,1],"back":[2,1]}],"issues":["2026014","26015"],"multiple":2,"append":true,"totalAmount":1}`
	p, e := NormalizeBet([]byte(raw), now)
	if e != nil {
		t.Fatal(e)
	}
	if p.TotalAmount != 12 || p.Config.Periods != 2 || p.Issues[0] != "26014" || p.Tickets[0].Front[0] != 1 || p.PlacedAt != "2026-11-05T00:00:00.000Z" {
		t.Fatalf("unexpected %#v", p)
	}
	var m map[string]any
	b, _ := json.Marshal(p)
	json.Unmarshal(b, &m)
	if m["matchMode"] != "issue" {
		t.Fatal(string(b))
	}
}
func TestDomainStrictValidation(t *testing.T) {
	base := `{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}]`
	for _, suffix := range []string{`,"append":null}`, `,"append":"false"}`, `,"multiple":null}`, `,"multiple":"2"}`, `,"note":null}`, `,"issues":null}`, `,"placedAt":null}`, `,"note":"` + strings.Repeat("😀", 101) + `"}`, `,"multiple":9007199254740991}`} {
		if _, e := NormalizeBet([]byte(base+suffix), time.Now()); e == nil {
			t.Errorf("accepted %s", suffix)
		}
	}
	p, e := NormalizeBet([]byte(base+`,"periods":null}`), time.Now())
	if e != nil || p.Config.Periods != 1 || p.MatchMode != "date" {
		t.Fatalf("null periods: %#v %v", p, e)
	}
	for _, row := range []struct{ game, issue, want string }{{"dlt", " 2026014 ", "26014"}, {"ssq", "2026120", "2026120"}} {
		v, e := NormalizeIssue(row.game, row.issue)
		if e != nil || v != row.want {
			t.Fatal(v, e)
		}
	}
	for _, s := range []string{"26000", "18001", "202601400"} {
		if _, e := NormalizeIssue("dlt", s); e == nil {
			t.Fatal(s)
		}
	}
}
func testDraw() Draw {
	return Draw{Game: "dlt", Issue: "26014", Front: []int{1, 2, 3, 4, 5}, Back: []int{1, 2}, Prizes: map[int]*float64{1: testAmount(1000000), 2: testAmount(100000), 3: testAmount(5000), 4: testAmount(300)}, AppendPrizes: map[int]*float64{1: testAmount(800000), 2: testAmount(80000)}}
}
func testAmount(v float64) *float64 { return &v }
func TestDomainCompoundPrize(t *testing.T) {
	r, e := CheckTicket(Ticket{[]int{1, 2, 3, 4, 5, 6}, []int{1, 2, 3}}, testDraw(), Config{Multiple: 2, Append: true})
	if e != nil {
		t.Fatal(e)
	}
	if r.Amount == nil || *r.Amount != 4376000 || len(r.Winners) != 4 {
		t.Fatalf("%#v", r)
	}
	for i, c := range []int64{2, 4, 10, 20} {
		if r.Winners[i].Count != c {
			t.Fatalf("%#v", r.Winners)
		}
	}
	d := testDraw()
	d.AppendPrizes = nil
	r, e = CheckTicket(Ticket{[]int{1, 2, 3, 4, 5}, []int{1, 2}}, d, Config{Multiple: 2, Append: true})
	if e != nil || r.Complete || r.Amount != nil || r.KnownAmount != 2000000 {
		t.Fatalf("%#v %v", r, e)
	}
	d.Prizes[1] = testAmount(1e16)
	if _, e = CheckTicket(Ticket{[]int{1, 2, 3, 4, 5}, []int{1, 2}}, d, Config{Multiple: 1}); e == nil {
		t.Fatal("overflow accepted")
	}
}

func TestDomainLegacyAndSSQ(t *testing.T) {
	ticket := Ticket{Front: []int{1, 2, 3, 4, 6}, Back: []int{1, 2}}
	for _, tc := range []struct {
		issue string
		level int
	}{{"26013", 4}, {"26014", 3}} {
		d := testDraw()
		d.Issue = tc.issue
		r, e := CheckTicket(ticket, d, Config{})
		if e != nil || r.Winners[0].Level != tc.level {
			t.Fatal(r, e)
		}
	}
	d := Draw{Game: "ssq", Issue: "2026120", Front: []int{1, 2, 3, 4, 5, 6}, Back: []int{1}, Prizes: map[int]*float64{1: testAmount(5000000), 2: testAmount(100000), 3: testAmount(3000), 4: testAmount(200)}}
	r, e := CheckTicket(Ticket{[]int{1, 2, 3, 4, 5, 6, 7}, []int{1, 2}}, d, Config{})
	if e != nil || r.Amount == nil || *r.Amount != 5119200 {
		t.Fatal(r, e)
	}
	d = testDraw()
	lost := Ticket{[]int{20, 21, 22, 23, 24}, []int{10, 11}}
	result, e := Evaluate(Bet{Payload: Payload{Tickets: []Ticket{lost}, Config: Config{Multiple: 1}}}, d)
	if e != nil || result.Won || !result.Complete || result.Amount == nil || *result.Amount != 0 || len(result.Tickets[0].Winners) != 0 {
		t.Fatal(result, e)
	}
	result, e = Evaluate(Bet{Payload: Payload{Tickets: []Ticket{{[]int{1, 2, 3, 4, 5}, []int{1, 2}}}, Config: Config{Multiple: MaxSafeInteger}}}, d)
	if e == nil {
		t.Fatal("prize overflow", result)
	}
}
func TestDomainWhitespaceAndFuture(t *testing.T) {
	if v, e := NormalizeIssue("dlt", "\ufeff26014\ufeff"); e != nil || v != "26014" {
		t.Fatal(v, e)
	}
	raw := `{"game":"dlt","tickets":[{"front":[1,2,3,4,5],"back":[1,2]}],"placedAt":"2027-01-01T00:00:00Z"}`
	if _, e := NormalizeBet([]byte(raw), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); e == nil {
		t.Fatal("future accepted")
	}
}
