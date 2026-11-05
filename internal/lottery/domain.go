package lottery

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

type gameSpec struct {
	frontPick, backPick, frontMax, backMax int
	frontLabel, backLabel                  string
}

var domainGames = map[string]gameSpec{"dlt": {5, 2, 35, 12, "前区", "后区"}, "ssq": {6, 1, 33, 16, "红球", "蓝球"}}
var issuePattern = regexp.MustCompile(`^[0-9]{5}$`)
var ssqIssuePattern = regexp.MustCompile(`^20[0-9]{5}$`)

func NormalizeIssue(game, issue string) (string, error) {
	value := domainTrim(issue)
	if game == "dlt" && ssqIssuePattern.MatchString(value) {
		value = value[2:]
	}
	_, ok := domainGames[game]
	valid := issuePattern.MatchString(value)
	if game == "ssq" {
		valid = ssqIssuePattern.MatchString(value)
	}
	if !ok || !valid || strings.HasSuffix(value, "000") {
		return "", appError(400, "期号格式错误：大乐透如 26120，双色球如 2026120")
	}
	if game == "dlt" && value < "19019" {
		return "", appError(400, "大乐透验奖支持 19019 期及以后")
	}
	return value, nil
}
func domainNumber(raw json.RawMessage) (float64, bool) {
	var n float64
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &n) != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return n, math.Trunc(n) == n
}
func domainNumbers(raw json.RawMessage, min, max int, label string) ([]int, error) {
	var list []json.RawMessage
	valid := json.Unmarshal(raw, &list) == nil && len(list) >= min && len(list) <= max
	values := make([]int, 0, len(list))
	seen := map[int]bool{}
	for _, r := range list {
		n, ok := domainNumber(r)
		if !ok || n < 1 || n > float64(max) || seen[int(n)] {
			valid = false
			break
		}
		seen[int(n)] = true
		values = append(values, int(n))
	}
	if !valid {
		return nil, appError(400, fmt.Sprintf("%s号码必须在 1～%d 内、不重复，选择 %d～%d 个", label, max, min, max))
	}
	sort.Ints(values)
	return values, nil
}
func NormalizeBet(data []byte, now time.Time) (Payload, error) {
	fail := func(s string) (Payload, error) { return Payload{}, appError(400, s) }
	var input map[string]json.RawMessage
	if json.Unmarshal(data, &input) != nil || input == nil {
		return fail("请选择大乐透或双色球")
	}
	var game string
	if json.Unmarshal(input["game"], &game) != nil {
		return fail("请选择大乐透或双色球")
	}
	spec, ok := domainGames[game]
	if !ok {
		return fail("请选择大乐透或双色球")
	}
	var rows []map[string]json.RawMessage
	if json.Unmarshal(input["tickets"], &rows) != nil || len(rows) < 1 || len(rows) > 1000 {
		return fail("每条投注需要 1～1000 组号码")
	}
	p := Payload{Tickets: make([]Ticket, 0, len(rows)), Issues: []string{}, MatchMode: "date"}
	for _, r := range rows {
		front, e := domainNumbers(r["front"], spec.frontPick, spec.frontMax, spec.frontLabel)
		if e != nil {
			return Payload{}, e
		}
		back, e := domainNumbers(r["back"], spec.backPick, spec.backMax, spec.backLabel)
		if e != nil {
			return Payload{}, e
		}
		p.Tickets = append(p.Tickets, Ticket{front, back})
	}
	for _, t := range p.Tickets {
		if len(t.Front) != len(p.Tickets[0].Front) || len(t.Back) != len(p.Tickets[0].Back) {
			return fail("同一条投注的各组选号数量必须相同")
		}
	}
	periods := 1
	if raw, exists := input["issues"]; exists {
		p.MatchMode = "issue"
		var issues []json.RawMessage
		if json.Unmarshal(raw, &issues) != nil || len(issues) < 1 || len(issues) > 100 {
			return fail("请明确列出 1～100 个投注期号")
		}
		for _, r := range issues {
			var issue string
			if string(r) == "null" || json.Unmarshal(r, &issue) != nil {
				return fail("期号必须是字符串")
			}
			v, e := NormalizeIssue(game, issue)
			if e != nil {
				return Payload{}, e
			}
			p.Issues = append(p.Issues, v)
		}
		sort.Strings(p.Issues)
		periods = len(p.Issues)
	} else if raw, exists := input["periods"]; exists && string(raw) != "null" {
		n, ok := domainNumber(raw)
		if !ok || n < 1 || n > 100 {
			return fail("连续期数必须是 1～100 的整数")
		}
		periods = int(n)
	}
	placed := now
	if raw, exists := input["placedAt"]; exists {
		var value string
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
			return fail("投注时间必须使用带时区的 ISO 日期时间")
		}
		v, e := parseInstant(value, 400)
		if e != nil {
			return Payload{}, e
		}
		placed = v
	}
	if placed.UnixMilli() > now.UnixMilli() {
		return fail("投注时间不能晚于当前时间")
	}
	p.PlacedAt = formatInstant(placed)
	for i := 1; i < len(p.Issues); i++ {
		if p.Issues[i] == p.Issues[i-1] {
			return fail("投注期号不能重复")
		}
	}
	appendBet := false
	if raw, exists := input["append"]; exists {
		if string(raw) == "null" || json.Unmarshal(raw, &appendBet) != nil {
			return fail("追加状态必须为布尔值")
		}
	}
	multiple := int64(1)
	if raw, exists := input["multiple"]; exists {
		n, ok := domainNumber(raw)
		if !ok || n < 1 || n > float64(MaxSafeInteger) {
			return fail("投注倍数必须是正整数")
		}
		multiple = int64(n)
	}
	if raw, exists := input["note"]; exists {
		var note string
		if string(raw) == "null" || json.Unmarshal(raw, &note) != nil || len(utf16.Encode([]rune(note))) > 200 {
			return fail("备注最多 200 个字符")
		}
		p.Note = domainTrim(note)
	}
	if appendBet && game == "ssq" {
		return fail("双色球不支持追加投注")
	}
	p.Config = Config{Game: game, Front: len(p.Tickets[0].Front), Back: len(p.Tickets[0].Back), Count: len(p.Tickets), Multiple: multiple, Periods: periods, Append: appendBet}
	totalCount := choose(p.Config.Front, spec.frontPick) * choose(p.Config.Back, spec.backPick) * float64(p.Config.Count)
	normal := totalCount * 2 * float64(multiple)
	extra := totalCount * 3 * float64(multiple)
	p.AmountPerPeriod = normal
	if appendBet {
		p.AmountPerPeriod = extra
	}
	p.TotalAmount = p.AmountPerPeriod * float64(periods)
	for _, n := range []float64{totalCount, normal, p.TotalAmount} {
		if !safeNumber(n) {
			return fail("投注金额过大，请减少组数、倍数或期数")
		}
	}
	if game == "dlt" && !safeNumber(extra) {
		return fail("投注金额过大，请减少组数、倍数或期数")
	}
	if !safeNumber(p.TotalAmount * 100) {
		return fail("投注金额过大")
	}
	return p, nil
}
func safeNumber(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0) && math.Trunc(n) == n && math.Abs(n) <= float64(MaxSafeInteger)
}
func domainTrim(s string) string {
	return strings.Trim(s, "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff")
}
func choose(n, k int) float64 {
	if k < 0 || k > n {
		return 0
	}
	v := 1.0
	for i := 1; i <= k; i++ {
		v = v * float64(n-i+1) / float64(i)
	}
	return math.Round(v)
}
func domainPrizeLevel(game, issue string, f, b int) int {
	if game == "ssq" {
		switch {
		case f == 6:
			if b == 1 {
				return 1
			}
			return 2
		case f == 5 && b == 1:
			return 3
		case f == 5 || f == 4 && b == 1:
			return 4
		case f == 4 || f == 3 && b == 1:
			return 5
		case b == 1:
			return 6
		}
		return 0
	}
	key := [2]int{f, b}
	modern := map[[2]int]int{{5, 2}: 1, {5, 1}: 2, {5, 0}: 3, {4, 2}: 3, {4, 1}: 4, {4, 0}: 5, {3, 2}: 5, {3, 1}: 6, {2, 2}: 6, {3, 0}: 7, {2, 1}: 7, {1, 2}: 7, {0, 2}: 7}
	old := map[[2]int]int{{5, 2}: 1, {5, 1}: 2, {5, 0}: 3, {4, 2}: 4, {4, 1}: 5, {3, 2}: 6, {4, 0}: 7, {3, 1}: 8, {2, 2}: 8, {3, 0}: 9, {2, 1}: 9, {1, 2}: 9, {0, 2}: 9}
	if issue >= "26014" {
		return modern[key]
	}
	return old[key]
}
func domainCents(v *float64) (*float64, error) {
	if v == nil {
		return nil, nil
	}
	c := math.Round(*v * 100)
	if *v < 0 || !safeNumber(c) {
		return nil, appError(502, "开奖奖金超出支持范围")
	}
	return &c, nil
}
func amountPointer(v float64) *float64 { return &v }
func CheckTicket(ticket Ticket, draw Draw, config Config) (TicketResult, error) {
	spec, ok := domainGames[draw.Game]
	if !ok {
		return TicketResult{}, appError(502, "开奖彩种无效")
	}
	r := TicketResult{Winners: []Winner{}, Complete: true}
	for _, n := range ticket.Front {
		for _, v := range draw.Front {
			if n == v {
				r.FrontHits++
				break
			}
		}
	}
	for _, n := range ticket.Back {
		for _, v := range draw.Back {
			if n == v {
				r.BackHits++
				break
			}
		}
	}
	multiple := config.Multiple
	if multiple == 0 {
		multiple = 1
	}
	levels := map[int]float64{}
	for f := 0; f <= spec.frontPick; f++ {
		for b := 0; b <= spec.backPick; b++ {
			count := choose(r.FrontHits, f) * choose(len(ticket.Front)-r.FrontHits, spec.frontPick-f) * choose(r.BackHits, b) * choose(len(ticket.Back)-r.BackHits, spec.backPick-b) * float64(multiple)
			level := domainPrizeLevel(draw.Game, draw.Issue, f, b)
			if count != 0 && level != 0 {
				levels[level] += count
			}
		}
	}
	keys := make([]int, 0, len(levels))
	for k := range levels {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	known := 0.0
	for _, level := range keys {
		count := levels[level]
		base, e := domainCents(draw.Prizes[level])
		if e != nil {
			return TicketResult{}, e
		}
		extra := amountPointer(0)
		if config.Append && draw.Game == "dlt" && level <= 2 {
			extra, e = domainCents(draw.AppendPrizes[level])
			if e != nil {
				return TicketResult{}, e
			}
		}
		w := Winner{Level: level, Count: int64(count)}
		unit := 0.0
		if base != nil {
			unit += *base
			w.UnitAmount = amountPointer(*base / 100)
		}
		if extra != nil {
			unit += *extra
			w.AppendUnitAmount = amountPointer(*extra / 100)
		}
		if base == nil || extra == nil {
			r.Complete = false
		} else {
			w.Amount = amountPointer(unit * count / 100)
		}
		known += unit * count
		if !safeNumber(known) {
			return TicketResult{}, appError(400, "中奖金额超出安全精度")
		}
		r.Winners = append(r.Winners, w)
	}
	r.KnownAmount = known / 100
	if r.Complete {
		r.Amount = amountPointer(r.KnownAmount)
	}
	return r, nil
}
func Evaluate(bet Bet, draw Draw) (Result, error) {
	r := Result{Issue: draw.Issue, Draw: draw, Tickets: []TicketResult{}, Complete: true}
	known := 0.0
	for _, ticket := range bet.Tickets {
		v, e := CheckTicket(ticket, draw, bet.Config)
		if e != nil {
			return Result{}, e
		}
		r.Tickets = append(r.Tickets, v)
		known += math.Round(v.KnownAmount * 100)
		r.Complete = r.Complete && v.Complete
		r.Won = r.Won || len(v.Winners) > 0
	}
	if !safeNumber(known) {
		return Result{}, appError(400, "中奖金额超出安全精度")
	}
	r.KnownAmount = known / 100
	if r.Complete {
		r.Amount = amountPointer(r.KnownAmount)
	}
	return r, nil
}
