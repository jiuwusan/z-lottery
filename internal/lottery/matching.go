package lottery

import (
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

var instantPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}(?::[0-9]{2}(?:\.[0-9]{1,3})?)?(?:Z|[+-][0-9]{2}:[0-9]{2})$`)
var datePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

func formatInstant(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
func parseDate(s string, status int) (time.Time, error) {
	t, e := time.Parse("2006-01-02", s)
	if !datePattern.MatchString(s) || e != nil || t.Format("2006-01-02") != s {
		return time.Time{}, appError(status, "日期格式无效")
	}
	return t, nil
}
func parseInstant(s string, status int) (time.Time, error) {
	if !instantPattern.MatchString(s) {
		return time.Time{}, appError(status, "投注时间必须使用带时区的 ISO 日期时间")
	}
	if _, e := parseDate(s[:10], status); e != nil {
		return time.Time{}, e
	}
	// JavaScript accepts minute-only instants and 24:00 as the next midnight.
	value := s
	end := len(value) - 1
	if !strings.HasSuffix(value, "Z") {
		end = len(value) - 6
		zoneHour, _ := strconv.Atoi(value[end+1 : end+3])
		zoneMinute, _ := strconv.Atoi(value[end+4:])
		if zoneHour > 23 || zoneMinute > 59 {
			return time.Time{}, appError(status, "日期时间无效")
		}
	}
	clock := value[11:end]
	if len(clock) == 5 {
		value = value[:end] + ":00" + value[end:]
		clock += ":00"
	}
	nextDay := false
	if strings.HasPrefix(clock, "24:00:00") {
		if clock != "24:00:00" && strings.Trim(clock[8:], ".0") != "" {
			return time.Time{}, appError(status, "日期时间无效")
		}
		value = value[:11] + "00" + value[13:]
		nextDay = true
	}
	t, e := time.Parse(time.RFC3339Nano, value)
	if e != nil {
		return time.Time{}, appError(status, "日期时间无效")
	}
	if nextDay {
		t = t.AddDate(0, 0, 1)
	}
	return t, nil
}
func ShanghaiDate(t time.Time) string {
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		zone = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return t.In(zone).Format("2006-01-02")
}
func SalesCloseAt(draw Draw) (string, error) {
	if draw.SalesCloseAt != "" {
		t, e := parseInstant(draw.SalesCloseAt, 502)
		if e != nil {
			return "", e
		}
		return formatInstant(t), nil
	}
	if _, e := parseDate(draw.Date, 502); e != nil {
		return "", e
	}
	hour := "20"
	if draw.Game == "dlt" && draw.Date >= "2023-01-01" {
		hour = "21"
	}
	t, e := parseInstant(draw.Date+"T"+hour+":00:00+08:00", 502)
	if e != nil {
		return "", e
	}
	return formatInstant(t), nil
}
func SelectDraws(game, placedAt string, periods int, draws []Draw) ([]Draw, error) {
	if (game != "dlt" && game != "ssq") || periods < 1 || periods > 100 {
		return nil, appError(400, "彩种或连续期数无效")
	}
	placed, e := parseInstant(placedAt, 400)
	if e != nil {
		return nil, e
	}
	byIssue := map[string]Draw{}
	for _, draw := range draws {
		if draw.Game != game {
			return nil, appError(502, "历史开奖彩种不一致")
		}
		if _, e := parseDate(draw.Date, 502); e != nil {
			return nil, e
		}
		close, e := SalesCloseAt(draw)
		if e != nil {
			return nil, e
		}
		draw.SalesCloseAt = close
		if previous, exists := byIssue[draw.Issue]; exists && !reflect.DeepEqual(previous, draw) {
			return nil, appError(502, "同一期历史开奖数据冲突")
		}
		byIssue[draw.Issue] = draw
	}
	rows := make([]Draw, 0, len(byIssue))
	for _, d := range byIssue {
		rows = append(rows, d)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Date != rows[j].Date {
			return rows[i].Date < rows[j].Date
		}
		return rows[i].Issue < rows[j].Issue
	})
	result := []Draw{}
	for _, d := range rows {
		close, _ := parseInstant(d.SalesCloseAt, 502)
		if close.After(placed) {
			result = append(result, d)
			if len(result) == periods {
				break
			}
		}
	}
	return result, nil
}
