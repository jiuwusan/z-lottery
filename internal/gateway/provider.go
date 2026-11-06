package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jiuwusan/z-lottery/internal/domain"
)

type DrawProvider struct{ client *http.Client }

func NewDrawProvider(client *http.Client) *DrawProvider {
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	return &DrawProvider{client: client}
}
func providerError(s string) error { return domain.NewError(502, s) }
func providerString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func providerDate(s string) error {
	d, e := time.Parse("2006-01-02", s)
	if e != nil || d.Format("2006-01-02") != s {
		return providerError("日期格式无效")
	}
	return nil
}
func providerDatePart(v any) string {
	s := providerString(v)
	if len(s) > 10 {
		s = s[:10]
	}
	return s
}
func providerInteger(v any) (int, bool) {
	s := providerString(v)
	f, e := strconv.ParseFloat(s, 64)
	if e != nil || f < 0 || f > 1e9 || f != float64(int(f)) {
		return 0, false
	}
	return int(f), true
}

var providerMoneyPattern = regexp.MustCompile(`^\d+(\.\d{1,2})?$`)

func providerMoney(raw any) (*float64, error) {
	s := providerString(raw)
	if s == "" || s == "---" || s == "-1" {
		return nil, nil
	}
	s = strings.ReplaceAll(s, ",", "")
	if !providerMoneyPattern.MatchString(s) {
		return nil, providerError("官方奖金格式无法识别")
	}
	parts := strings.SplitN(s, ".", 2)
	cents := parts[0] + "00"
	if len(parts) == 2 {
		cents = parts[0] + parts[1] + strings.Repeat("0", 2-len(parts[1]))
	}
	n, e := strconv.ParseUint(cents, 10, 64)
	if e != nil || n > uint64(domain.MaxSafeInteger) {
		return nil, providerError("官方奖金超过精度范围")
	}
	f := float64(n) / 100
	return &f, nil
}

var providerNumberSplit = regexp.MustCompile(`[,\s]+`)

func providerNumbers(v any, count, max int) ([]int, error) {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil, providerError("官方开奖号码缺失")
	}
	parts := providerNumberSplit.Split(strings.TrimSpace(s), -1)
	if len(parts) != count {
		return nil, providerError("官方开奖号码数量不正确")
	}
	seen := map[int]bool{}
	out := make([]int, 0, count)
	for _, p := range parts {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > max || seen[n] {
			return nil, providerError("官方开奖号码无效")
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}

var providerPrizePattern = regexp.MustCompile(`^([一二三四五六七八九])等奖(?:\(追加\)|（追加）)?$`)
var providerTimezone = regexp.MustCompile(`Z$|[+-]\d{2}:\d{2}$`)
var providerDLTIssue = regexp.MustCompile(`^\d{5}$`)
var providerSSQIssue = regexp.MustCompile(`^20\d{5}$`)

// The preceding issue is used only to detect gaps, including before the
// first issue whose prize rules the application supports.
func providerPrecedingIssue(game, issue string) (string, error) {
	if game == "dlt" && providerSSQIssue.MatchString(issue) {
		issue = issue[2:]
	}
	if game == "dlt" && providerDLTIssue.MatchString(issue) || game == "ssq" && providerSSQIssue.MatchString(issue) {
		return issue, nil
	}
	return "", providerError("历史开奖期号格式无效")
}

func providerRowToDraw(game string, row map[string]any) (*domain.Draw, error) {
	d := &domain.Draw{Game: game, Prizes: map[int]*float64{}, AppendPrizes: map[int]*float64{}}
	var rawIssue any
	var prizes []any
	if game == "dlt" {
		verify, _ := providerInteger(row["verify"])
		status, _ := providerInteger(row["lotteryDrawStatus"])
		if verify != 1 || status != 20 {
			return nil, nil
		}
		var ok bool
		prizes, ok = row["prizeLevelList"].([]any)
		if !ok {
			return nil, providerError("体彩奖级数据缺失")
		}
		rawIssue = row["lotteryDrawNum"]
		d.Date = providerDatePart(row["lotteryDrawTime"])
		selected, ok := row["lotteryDrawResult"].(string)
		if !ok {
			return nil, providerError("官方开奖号码缺失")
		}
		parts := providerNumberSplit.Split(strings.TrimSpace(selected), -1)
		if len(parts) != 7 {
			return nil, providerError("官方开奖号码数量不正确")
		}
		var e error
		d.Front, e = providerNumbers(strings.Join(parts[:5], " "), 5, 35)
		if e != nil {
			return nil, e
		}
		d.Back, e = providerNumbers(strings.Join(parts[5:], " "), 2, 12)
		if e != nil {
			return nil, e
		}
		deadline := providerString(row["lotterySaleEndtime"])
		if deadline == "" {
			deadline = providerString(row["lotterySaleEndTime"])
		}
		if deadline != "" {
			d.SalesCloseAt = strings.Replace(deadline, " ", "T", 1)
			if !providerTimezone.MatchString(deadline) {
				d.SalesCloseAt += "+08:00"
			}
		}
	} else {
		red, rok := row["red"].(string)
		blue, bok := row["blue"].(string)
		if !rok || !bok || strings.TrimSpace(red) == "" || strings.TrimSpace(blue) == "" {
			return nil, nil
		}
		var ok bool
		prizes, ok = row["prizegrades"].([]any)
		if !ok {
			return nil, providerError("福彩奖级数据缺失")
		}
		rawIssue = row["code"]
		d.Date = providerDatePart(row["date"])
		var e error
		d.Front, e = providerNumbers(red, 6, 33)
		if e != nil {
			return nil, e
		}
		d.Back, e = providerNumbers(blue, 1, 16)
		if e != nil {
			return nil, e
		}
	}
	issue, e := domain.NormalizeIssue(game, providerString(rawIssue))
	if e != nil {
		return nil, providerError("历史开奖期号格式无效")
	}
	d.Issue = issue
	for _, raw := range prizes {
		p, ok := raw.(map[string]any)
		if !ok {
			return nil, providerError("官方奖级数据无效")
		}
		level := 0
		target := d.Prizes
		var amount any
		if game == "dlt" {
			label := providerString(p["prizeLevel"])
			m := providerPrizePattern.FindStringSubmatch(label)
			if m == nil {
				continue
			}
			level = strings.Index("一二三四五六七八九", m[1])/3 + 1
			if strings.Contains(label, "追加") {
				target = d.AppendPrizes
			}
			amount = p["stakeAmountFormat"]
			if amount == nil {
				amount = p["stakeAmount"]
			}
		} else {
			level, ok = providerInteger(p["type"])
			if !ok || level < 1 || level > 6 {
				continue
			}
			amount = p["typemoney"]
		}
		value, e := providerMoney(amount)
		if e != nil {
			return nil, e
		}
		target[level] = value
	}
	if e := providerDate(d.Date); e != nil {
		return nil, e
	}
	d.SalesCloseAt, e = domain.SalesCloseAt(*d)
	if e != nil {
		return nil, e
	}
	return d, nil
}
func (p *DrawProvider) request(ctx context.Context, u *url.URL, referer string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if e != nil {
		return nil, providerError("官方开奖接口请求失败：" + e.Error())
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", referer)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, e := p.client.Do(req)
	if e != nil {
		return nil, providerError("官方开奖接口请求失败：" + e.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, providerError(fmt.Sprintf("官方开奖接口请求失败：HTTP %d", resp.StatusCode))
	}
	const limit = 2 * 1024 * 1024
	b, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if e != nil {
		return nil, providerError("官方开奖接口请求失败：" + e.Error())
	}
	if len(b) > limit {
		return nil, providerError("官方开奖接口请求失败：响应超过大小限制")
	}
	var result map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.UseNumber()
	if e = decoder.Decode(&result); e != nil || result == nil {
		return nil, providerError("官方开奖接口请求失败：响应不是 JSON 对象")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, providerError("官方开奖接口请求失败：响应格式无效")
	}
	return result, nil
}
func (p *DrawProvider) page(ctx context.Context, game string, page int, issue string) ([]map[string]any, int, int, error) {
	if game != "dlt" && game != "ssq" {
		return nil, 0, 0, domain.NewError(400, "请选择大乐透或双色球")
	}
	size := "100"
	if issue != "" {
		size = "30"
	}
	var endpoint, referer string
	var q url.Values
	if game == "dlt" {
		endpoint = "https://webapi.sporttery.cn/gateway/lottery/getHistoryPageListV1.qry"
		referer = "https://static.sporttery.cn/"
		q = url.Values{"gameNo": {"85"}, "provinceId": {"0"}, "pageSize": {size}, "pageNo": {strconv.Itoa(page)}}
		if issue != "" {
			q.Set("isVerify", "1")
			q.Set("startTerm", issue)
			q.Set("endTerm", issue)
		}
	} else {
		endpoint = "https://www.cwl.gov.cn/cwl_admin/front/cwlkj/search/kjxx/findDrawNotice"
		referer = "https://www.cwl.gov.cn/ygkj/wqkjgg/ssq/"
		q = url.Values{"name": {"ssq"}, "pageSize": {size}, "pageNo": {strconv.Itoa(page)}, "systemType": {"PC"}}
		if issue != "" {
			q.Set("issueStart", issue)
			q.Set("issueEnd", issue)
		}
	}
	u, _ := url.Parse(endpoint)
	u.RawQuery = q.Encode()
	data, e := p.request(ctx, u, referer)
	if e != nil {
		return nil, 0, 0, e
	}
	var raw any
	var pagesRaw, totalRaw any
	if game == "dlt" {
		value, ok := data["value"].(map[string]any)
		if !ok || data["success"] != true || providerString(data["errorCode"]) != "0" {
			return nil, 0, 0, providerError("体彩接口响应结构异常")
		}
		raw = value["list"]
		pagesRaw = value["pages"]
		totalRaw = value["total"]
	} else {
		if state, ok := data["state"].(json.Number); !ok || state.String() != "0" {
			return nil, 0, 0, providerError("福彩接口响应结构异常")
		}
		raw = data["result"]
		pagesRaw = data["pageNum"]
		totalRaw = data["total"]
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, 0, 0, providerError("官方接口响应结构异常")
	}
	rows := make([]map[string]any, 0, len(list))
	for _, item := range list {
		row, ok := item.(map[string]any)
		if !ok {
			return nil, 0, 0, providerError("官方接口响应结构异常")
		}
		rows = append(rows, row)
	}
	pages, pok := providerInteger(pagesRaw)
	total, tok := providerInteger(totalRaw)
	if issue == "" && (!pok || !tok) {
		return nil, 0, 0, providerError("历史开奖分页信息缺失")
	}
	return rows, pages, total, nil
}
func (p *DrawProvider) GetDraw(ctx context.Context, game, requestedIssue string) (*domain.Draw, error) {
	issue, e := domain.NormalizeIssue(game, requestedIssue)
	if e != nil {
		return nil, e
	}
	rows, _, _, e := p.page(ctx, game, 1, issue)
	if e != nil {
		return nil, e
	}
	if len(rows) == 0 {
		return nil, nil
	}
	key := "code"
	if game == "dlt" {
		key = "lotteryDrawNum"
	}
	for _, row := range rows {
		if providerString(row[key]) == issue {
			return providerRowToDraw(game, row)
		}
	}
	return nil, providerError("官方接口未返回指定期号")
}
func (p *DrawProvider) GetHistory(ctx context.Context, game, fromDate, throughDate string) ([]domain.Draw, error) {
	snapshot, err := p.GetSnapshot(ctx, game, fromDate, throughDate)
	return snapshot.Draws, err
}

func (p *DrawProvider) GetSnapshot(ctx context.Context, game, fromDate, throughDate string) (domain.OfficialSnapshot, error) {
	snapshot := domain.OfficialSnapshot{Draws: []domain.Draw{}, Published: []domain.Draw{}}
	if game != "dlt" && game != "ssq" {
		return domain.OfficialSnapshot{}, domain.NewError(400, "请选择大乐透或双色球")
	}
	if providerDate(fromDate) != nil || providerDate(throughDate) != nil || fromDate > throughDate {
		return domain.OfficialSnapshot{}, domain.NewError(400, "历史开奖日期范围无效")
	}
	found := map[string]domain.Draw{}
	var preceding *domain.Draw
	covered := false
	unready := []string{}
	for page := 1; page <= 50; page++ {
		rows, pages, total, e := p.page(ctx, game, page, "")
		if e != nil {
			return domain.OfficialSnapshot{}, e
		}
		if len(rows) == 0 {
			if total != 0 || page != 1 {
				return domain.OfficialSnapshot{}, providerError("历史开奖分页不完整，请稍后重试")
			}
			return snapshot, nil
		}
		// Page one's official records control publication completeness independently
		// of the requested betting window. Parse them once and reuse them below.
		parsed := map[int]*domain.Draw{}
		if page == 1 {
			published := map[string]domain.Draw{}
			for index, row := range rows {
				dateKey, issueKey := "date", "code"
				if game == "dlt" {
					dateKey, issueKey = "lotteryDrawTime", "lotteryDrawNum"
				}
				date := providerDatePart(row[dateKey])
				if err := providerDate(date); err != nil {
					return domain.OfficialSnapshot{}, err
				}
				if date > throughDate {
					continue
				}
				issue, err := providerPrecedingIssue(game, providerString(row[issueKey]))
				if err != nil {
					return domain.OfficialSnapshot{}, err
				}
				// Older DLT issues are useful coverage/gap metadata, but their prize
				// rules are outside the supported official cache.
				if game == "dlt" && issue < "19019" {
					continue
				}
				if _, err := domain.NormalizeIssue(game, issue); err != nil {
					return domain.OfficialSnapshot{}, providerError("历史开奖期号格式无效")
				}
				draw, err := providerRowToDraw(game, row)
				if err != nil {
					return domain.OfficialSnapshot{}, err
				}
				parsed[index] = draw
				if draw == nil {
					snapshot.HasUnready = true
				} else {
					if previous, exists := published[draw.Issue]; exists {
						if !reflect.DeepEqual(previous, *draw) {
							return domain.OfficialSnapshot{}, providerError("官方开奖列表存在同一期数据冲突，暂缓同步")
						}
					} else {
						published[draw.Issue] = *draw
						snapshot.Published = append(snapshot.Published, *draw)
					}
				}
				if snapshot.LatestIssue == "" || date > snapshot.LatestDate || date == snapshot.LatestDate && issue > snapshot.LatestIssue {
					snapshot.LatestDate, snapshot.LatestIssue = date, issue
					snapshot.Latest, snapshot.LatestReady = draw, draw != nil
				}
			}
		}
		oldest := ""
		for index, row := range rows {
			dateKey, issueKey := "date", "code"
			if game == "dlt" {
				dateKey = "lotteryDrawTime"
				issueKey = "lotteryDrawNum"
			}
			date := providerDatePart(row[dateKey])
			if e := providerDate(date); e != nil {
				return domain.OfficialSnapshot{}, e
			}
			if oldest == "" || date < oldest {
				oldest = date
			}
			if date < fromDate {
				if preceding == nil || date > preceding.Date {
					issue, e := providerPrecedingIssue(game, providerString(row[issueKey]))
					if e != nil {
						return domain.OfficialSnapshot{}, providerError("历史开奖期号格式无效")
					}
					preceding = &domain.Draw{Issue: issue, Date: date}
				}
				continue
			}
			if date > throughDate {
				continue
			}
			d, alreadyParsed := parsed[index]
			if !alreadyParsed {
				var err error
				d, err = providerRowToDraw(game, row)
				if err != nil {
					return domain.OfficialSnapshot{}, err
				}
			}
			if d == nil {
				unready = append(unready, date)
				continue
			}
			if prior, ok := found[d.Issue]; ok && !reflect.DeepEqual(prior, *d) {
				return domain.OfficialSnapshot{}, providerError("历史开奖数据冲突，请稍后重试")
			}
			found[d.Issue] = *d
		}
		if oldest <= fromDate {
			covered = true
			break
		}
		if page >= pages {
			break
		}
	}
	if !covered {
		return domain.OfficialSnapshot{}, providerError("历史开奖未完整覆盖投注日期，暂缓验奖")
	}
	draws := make([]domain.Draw, 0, len(found))
	for _, d := range found {
		draws = append(draws, d)
	}
	sort.Slice(draws, func(i, j int) bool {
		if draws[i].Date == draws[j].Date {
			return draws[i].Issue < draws[j].Issue
		}
		return draws[i].Date < draws[j].Date
	})
	for _, date := range unready {
		for _, d := range draws {
			if d.Date > date {
				return domain.OfficialSnapshot{}, providerError("历史开奖存在未审核记录，暂缓匹配")
			}
		}
	}
	sequence := draws
	if preceding != nil {
		sequence = append([]domain.Draw{*preceding}, draws...)
	}
	for i := 1; i < len(sequence); i++ {
		a, b := sequence[i-1], sequence[i]
		ap, bp := a.Issue[:len(a.Issue)-3], b.Issue[:len(b.Issue)-3]
		an, _ := strconv.Atoi(a.Issue[len(a.Issue)-3:])
		bn, _ := strconv.Atoi(b.Issue[len(b.Issue)-3:])
		if a.Date == b.Date || (ap == bp && bn != an+1) || (ap != bp && bn != 1) {
			return domain.OfficialSnapshot{}, providerError("历史开奖存在缺口，暂缓匹配")
		}
	}
	snapshot.Draws = draws
	return snapshot, nil
}
