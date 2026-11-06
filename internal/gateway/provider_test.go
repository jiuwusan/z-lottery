package gateway

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jiuwusan/z-lottery/internal/domain"
)

type providerTransport func(*http.Request) (*http.Response, error)

func TestProviderSnapshotKeepsPublishedRecordsOutsideBettingRange(t *testing.T) {
	older := providerRow("26124", "2026-10-31")
	p := fixtureProvider(t, func(*http.Request) any { return providerPayload(providerRow("26125", "2026-11-02"), older) })
	snapshot, e := p.GetSnapshot(context.Background(), "dlt", "2026-11-03", "2026-11-03")
	if e != nil || len(snapshot.Published) != 2 || len(snapshot.Draws) != 0 {
		t.Fatal(snapshot, e)
	}
	older["verify"] = 0
	snapshot, e = p.GetSnapshot(context.Background(), "dlt", "2026-11-03", "2026-11-03")
	if e != nil || !snapshot.HasUnready || len(snapshot.Published) != 1 || !snapshot.LatestReady {
		t.Fatal(snapshot, e)
	}
}

func TestProviderRejectsConflictingPublishedIssueOutsideHistoryRange(t *testing.T) {
	first := providerRow("26125", "2026-11-02")
	second := providerRow("26125", "2026-11-02")
	second["lotteryDrawResult"] = "01 02 03 04 05 01 02"
	p := fixtureProvider(t, func(*http.Request) any { return providerPayload(first, second) })
	if _, err := p.GetSnapshot(context.Background(), "dlt", "2026-11-05", "2026-11-05"); err == nil {
		t.Fatal("conflicting official list accepted outside betting range")
	}
}

func (f providerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureProvider(t *testing.T, body func(*http.Request) any) *DrawProvider {
	t.Helper()
	return NewDrawProvider(&http.Client{Transport: providerTransport(func(r *http.Request) (*http.Response, error) {
		b, e := json.Marshal(body(r))
		if e != nil {
			t.Fatal(e)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b))), Header: make(http.Header)}, nil
	})})
}

func TestProviderSnapshotLatestOutsideWindowAndUnreviewed(t *testing.T) {
	for _, ready := range []bool{true, false} {
		t.Run(map[bool]string{true: "reviewed", false: "pending"}[ready], func(t *testing.T) {
			latest := providerRow("26125", "2026-11-02")
			if !ready {
				latest["verify"] = 0
			}
			calls := 0
			p := fixtureProvider(t, func(*http.Request) any {
				calls++
				return providerPayload(providerRow("26126", "2026-11-05"), providerRow("26124", "2026-10-31"), latest)
			})
			s, e := p.GetSnapshot(context.Background(), "dlt", "2026-11-03", "2026-11-04")
			if e != nil || len(s.Draws) != 0 || s.LatestDate != "2026-11-02" || s.LatestIssue != "26125" || s.LatestReady != ready || calls != 1 {
				t.Fatalf("snapshot %+v error %v calls %d", s, e, calls)
			}
			if (s.Latest != nil) != ready {
				t.Fatalf("latest %+v", s.Latest)
			}
		})
	}
}

func TestProviderSnapshotPaginationDoesNotRefetch(t *testing.T) {
	calls := 0
	p := fixtureProvider(t, func(r *http.Request) any {
		calls++
		rows := []map[string]any{providerRow("26125", "2026-11-02")}
		if r.URL.Query().Get("pageNo") == "2" {
			rows = []map[string]any{providerRow("26124", "2026-10-31"), providerRow("26123", "2026-10-28")}
		}
		return map[string]any{"success": true, "errorCode": 0, "value": map[string]any{"pages": 2, "total": 3, "list": rows}}
	})
	s, e := p.GetSnapshot(context.Background(), "dlt", "2026-10-28", "2026-11-04")
	if e != nil || calls != 2 || len(s.Draws) != 3 || s.Latest == nil || s.Latest.Issue != "26125" || !s.LatestReady {
		t.Fatalf("snapshot %+v error %v calls %d", s, e, calls)
	}
}

func TestProviderSnapshotPublishedIncludesOlderIncompleteAndUnreviewedRows(t *testing.T) {
	for _, ready := range []bool{true, false} {
		t.Run(map[bool]string{true: "missing older prize", false: "older unreviewed"}[ready], func(t *testing.T) {
			latest := providerRow("26125", "2026-11-02")
			prizes := []any{}
			for _, label := range []string{"一等奖", "二等奖", "三等奖", "四等奖", "五等奖", "六等奖", "七等奖", "一等奖(追加)", "二等奖(追加)"} {
				prizes = append(prizes, map[string]any{"prizeLevel": label, "stakeAmount": "0"})
			}
			latest["prizeLevelList"] = prizes
			older := providerRow("26124", "2026-10-31")
			if !ready {
				older["verify"] = 0
			}
			calls := 0
			p := fixtureProvider(t, func(*http.Request) any {
				calls++
				return providerPayload(latest, older, providerRow("19018", "2019-02-18"))
			})
			s, err := p.GetSnapshot(context.Background(), "dlt", "2026-11-02", "2026-11-04")
			wantPublished := 1
			if ready {
				wantPublished = 2
			}
			if err != nil || len(s.Draws) != 1 || s.Draws[0].Issue != "26125" || len(s.Published) != wantPublished || s.HasUnready == ready || s.Latest == nil || !domain.OfficialAmountsComplete(*s.Latest) || calls != 1 {
				t.Fatalf("snapshot %+v err=%v calls=%d", s, err, calls)
			}
			if ready && domain.OfficialAmountsComplete(s.Published[1]) {
				t.Fatal("older incomplete official record lost missing grades")
			}
			draws, err := p.GetHistory(context.Background(), "dlt", "2026-11-02", "2026-11-04")
			if err != nil || len(draws) != 1 || draws[0].Issue != "26125" {
				t.Fatalf("range history %+v err=%v", draws, err)
			}
		})
	}
}

func TestOfficialAmountsCompleteRequiresEveryOfficialGrade(t *testing.T) {
	for _, game := range []string{"dlt", "ssq"} {
		for _, issue := range []string{"26013", "26014"} {
			count := 6
			if game == "dlt" {
				count = 7
				if issue < "26014" {
					count = 9
				}
			}
			d := domain.Draw{Game: game, Issue: issue, Prizes: map[int]*float64{}, AppendPrizes: map[int]*float64{}}
			for i := 1; i <= count; i++ {
				d.Prizes[i] = domain.AmountPointer(0)
			}
			d.AppendPrizes[1] = domain.AmountPointer(0)
			d.AppendPrizes[2] = domain.AmountPointer(0)
			if !domain.OfficialAmountsComplete(d) {
				t.Fatalf("zero official amounts incomplete %+v", d)
			}
			for _, bad := range []*float64{nil, domain.AmountPointer(-1), domain.AmountPointer(math.NaN()), domain.AmountPointer(math.Inf(1)), domain.AmountPointer(float64(domain.MaxSafeInteger))} {
				d.Prizes[count] = bad
				if domain.OfficialAmountsComplete(d) {
					t.Fatalf("missing unheld grade accepted game=%s issue=%s bad=%v", game, issue, bad)
				}
			}
			d.Prizes[count] = domain.AmountPointer(0)
			if game == "dlt" {
				delete(d.AppendPrizes, 2)
				if domain.OfficialAmountsComplete(d) {
					t.Fatal("missing append grade accepted")
				}
			}
		}
	}
	if domain.OfficialAmountsComplete(domain.Draw{Game: "bad"}) {
		t.Fatal("invalid game accepted")
	}
}
func providerRow(issue, date string) map[string]any {
	return map[string]any{"lotteryDrawNum": issue, "lotteryDrawTime": date, "lotteryDrawResult": "03 06 20 24 32 04 07", "verify": 1, "lotteryDrawStatus": 20, "prizeLevelList": []any{map[string]any{"prizeLevel": "一等奖", "stakeAmount": "9,909,522"}, map[string]any{"prizeLevel": "一等奖(追加)", "stakeAmount": "---"}}}
}
func providerPayload(rows ...map[string]any) any {
	if rows == nil {
		rows = []map[string]any{}
	}
	return map[string]any{"success": true, "errorCode": "0", "value": map[string]any{"pages": 1, "total": len(rows), "list": rows}}
}
func TestProviderExactDraw(t *testing.T) {
	p := fixtureProvider(t, func(r *http.Request) any {
		if r.URL.Query().Get("startTerm") != "26119" || r.URL.Query().Get("isVerify") != "1" || r.Header.Get("Referer") == "" {
			t.Errorf("bad request %s", r.URL)
		}
		return providerPayload(providerRow("26119", "2026-10-19"))
	})
	d, e := p.GetDraw(context.Background(), "dlt", "26119")
	if e != nil {
		t.Fatal(e)
	}
	if d.Front[0] != 3 || *d.Prizes[1] != 9909522 || d.AppendPrizes[1] != nil || d.SalesCloseAt != "2026-10-19T13:00:00.000Z" {
		t.Fatalf("bad draw %+v", d)
	}
}
func TestProviderRejectsMalformedDraws(t *testing.T) {
	for _, key := range []string{"wrong issue", "numbers", "money", "precision", "deadline", "date", "prizes"} {
		t.Run(key, func(t *testing.T) {
			r := providerRow("26119", "2026-10-19")
			switch key {
			case "wrong issue":
				r["lotteryDrawNum"] = "26118"
			case "numbers":
				r["lotteryDrawResult"] = "03 03 20 24 32 04 07"
			case "money":
				r["prizeLevelList"] = []any{map[string]any{"prizeLevel": "一等奖", "stakeAmount": "1.234"}}
			case "precision":
				r["prizeLevelList"] = []any{map[string]any{"prizeLevel": "一等奖", "stakeAmount": "90071992547410"}}
			case "deadline":
				r["lotterySaleEndtime"] = "2026-10-19 29:00:00"
			case "date":
				r["lotteryDrawTime"] = "2026-02-30"
			case "prizes":
				delete(r, "prizeLevelList")
			}
			p := fixtureProvider(t, func(*http.Request) any { return providerPayload(r) })
			if _, e := p.GetDraw(context.Background(), "dlt", "26119"); e == nil {
				t.Fatal("expected failure")
			}
		})
	}
	p := fixtureProvider(t, func(*http.Request) any { return providerPayload() })
	if _, e := p.GetDraw(context.Background(), "bad", "26119"); e == nil {
		t.Fatal("wrong game accepted")
	}
	r := providerRow("26119", "2026-10-19")
	r["verify"] = 0
	p = fixtureProvider(t, func(*http.Request) any { return providerPayload(r) })
	if d, e := p.GetDraw(context.Background(), "dlt", "26119"); e != nil || d != nil {
		t.Fatalf("unreviewed %+v %v", d, e)
	}
}
func TestProviderHistoryCompleteness(t *testing.T) {
	cases := map[string][]map[string]any{
		"gap":           {providerRow("26125", "2026-11-02"), providerRow("26123", "2026-10-28"), providerRow("26122", "2026-10-26")},
		"preceding gap": {providerRow("26125", "2026-11-02"), providerRow("26124", "2026-10-31"), providerRow("26122", "2026-10-26")},
		"coverage":      {providerRow("26125", "2026-11-02")},
		"conflict":      {providerRow("26123", "2026-10-28"), providerRow("26123", "2026-10-29")},
		"crossyear gap": {providerRow("27002", "2027-01-02"), providerRow("26150", "2026-12-29"), providerRow("26149", "2026-10-26")},
	}
	unready := providerRow("26124", "2026-10-31")
	unready["verify"] = 0
	cases["unreviewed"] = []map[string]any{providerRow("26125", "2026-11-02"), unready, providerRow("26123", "2026-10-28")}
	for name, rows := range cases {
		t.Run(name, func(t *testing.T) {
			p := fixtureProvider(t, func(*http.Request) any { return providerPayload(rows...) })
			if _, e := p.GetHistory(context.Background(), "dlt", "2026-10-28", "2027-01-03"); e == nil {
				t.Fatal("incomplete history accepted")
			}
		})
	}
	p := fixtureProvider(t, func(*http.Request) any { return providerPayload() })
	d, e := p.GetHistory(context.Background(), "dlt", "2026-10-28", "2026-11-04")
	if e != nil || len(d) != 0 {
		t.Fatalf("empty history %v %v", d, e)
	}
}
func TestProviderHistoryPagination(t *testing.T) {
	pages := 0
	p := fixtureProvider(t, func(r *http.Request) any {
		pages++
		rows := []map[string]any{providerRow("26125", "2026-11-02")}
		if r.URL.Query().Get("pageNo") == "2" {
			rows = []map[string]any{providerRow("26124", "2026-10-31"), providerRow("26123", "2026-10-28"), providerRow("26122", "2026-10-26")}
		}
		return map[string]any{"success": true, "errorCode": 0, "value": map[string]any{"pages": 2, "total": 4, "list": rows}}
	})
	d, e := p.GetHistory(context.Background(), "dlt", "2026-10-28", "2026-11-04")
	if e != nil || len(d) != 3 || d[0].Issue != "26123" || pages != 2 {
		t.Fatalf("history %+v %v pages=%d", d, e, pages)
	}
	p = fixtureProvider(t, func(r *http.Request) any {
		rows := []map[string]any{}
		if r.URL.Query().Get("pageNo") == "1" {
			rows = append(rows, providerRow("26125", "2026-11-02"))
		}
		return map[string]any{"success": true, "errorCode": 0, "value": map[string]any{"pages": 2, "total": 2, "list": rows}}
	})
	if _, e = p.GetHistory(context.Background(), "dlt", "2026-10-28", "2026-11-04"); e == nil {
		t.Fatal("interrupted page accepted")
	}
}
func TestProviderRequestBoundsAndCancellation(t *testing.T) {
	for _, body := range []string{"<html>blocked</html>", strings.Repeat(" ", 2*1024*1024+1)} {
		p := NewDrawProvider(&http.Client{Transport: providerTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})})
		if _, e := p.GetDraw(context.Background(), "dlt", "26119"); e == nil {
			t.Fatal("bad response accepted")
		}
	}
	p := NewDrawProvider(&http.Client{Transport: providerTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, e := p.GetDraw(ctx, "dlt", "26119"); e == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestProviderSSQAndCrossYear(t *testing.T) {
	row := func(issue, date string) map[string]any {
		return map[string]any{"code": issue, "date": date, "red": "13,14,16,18,20,31", "blue": "03", "prizegrades": []any{map[string]any{"type": 6, "typemoney": "5"}}}
	}
	p := fixtureProvider(t, func(r *http.Request) any {
		if r.URL.Query().Get("issueStart") != "2026120" || r.URL.Query().Get("systemType") != "PC" {
			t.Errorf("bad SSQ query %s", r.URL)
		}
		return map[string]any{"state": 0, "result": []any{row("2026120", "2026-10-20(二)")}}
	})
	d, e := p.GetDraw(context.Background(), "ssq", "2026120")
	if e != nil {
		t.Fatal(e)
	}
	if d.Date != "2026-10-20" || d.Back[0] != 3 || *d.Prizes[6] != 5 {
		t.Fatalf("bad SSQ %+v", d)
	}
	p = fixtureProvider(t, func(r *http.Request) any {
		if r.URL.Query().Has("issueStart") || r.URL.Query().Get("pageSize") != "100" {
			t.Errorf("bad history query %s", r.URL)
		}
		return map[string]any{"state": 0, "pageNum": 1, "total": 3, "result": []any{row("2027001", "2027-01-03"), row("2026151", "2026-12-31"), row("2026150", "2026-12-29")}}
	})
	history, e := p.GetHistory(context.Background(), "ssq", "2026-12-30", "2027-01-03")
	if e != nil || len(history) != 2 || history[0].Issue != "2026151" || history[1].Issue != "2027001" {
		t.Fatalf("crossyear %+v %v", history, e)
	}
	waiting := providerRow("26123", "2026-10-28")
	waiting["verify"] = 0
	p = fixtureProvider(t, func(*http.Request) any { return providerPayload(waiting, providerRow("26122", "2026-10-26")) })
	history, e = p.GetHistory(context.Background(), "dlt", "2026-10-28", "2026-10-28")
	if e != nil || len(history) != 0 {
		t.Fatalf("unreviewed latest %+v %v", history, e)
	}
}

func TestProviderHistoryPrecedingIssueMayPredateSupportedPrizeRules(t *testing.T) {
	p := fixtureProvider(t, func(*http.Request) any {
		return providerPayload(providerRow("19019", "2019-02-20"), providerRow("19018", "2019-02-18"))
	})
	history, e := p.GetHistory(context.Background(), "dlt", "2019-02-19", "2019-02-20")
	if e != nil || len(history) != 1 || history[0].Issue != "19019" {
		t.Fatalf("preceding issue %+v %v", history, e)
	}
}
