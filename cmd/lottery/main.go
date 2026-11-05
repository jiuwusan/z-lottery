package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/jiuwusan/z-lottery/internal/lottery"
)

const help = `彩票完全随机选号

用法：
  go run ./cmd/lottery [参数]
  lottery [参数]

参数：
  --game <dlt|ssq>    彩票类型，默认 dlt
  --front <数量>     前区/红球选择数量
  --back <数量>      后区/蓝球选择数量
  --count <数量>     生成组数，默认 4，最多 1000
  --multiple <倍数>  投注倍数，默认 1
  --periods <期数>   连续投注期数，默认 1
  --same <前区[,后区]> 多注共享号码数量，例如 3,2；后区可省略
  --append           大乐透追加投注（双色球不支持）
  --help, -h         查看帮助

示例：
  go run ./cmd/lottery
  go run ./cmd/lottery --game dlt --front 7 --back 3 --count 1 --multiple 5 --append
  go run ./cmd/lottery --game dlt --same 3,2
  go run ./cmd/lottery --game ssq --front 8 --back 3 --count 2 --periods 10
`

var samePattern = regexp.MustCompile(`^[0-9]+(,[0-9]+)?$`)

func parse(args []string) (lottery.Config, bool, bool, error) {
	values := map[string]string{"game": "dlt"}
	appendBet, wantsHelp := false, false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--help", "-h":
			wantsHelp = true
		case "--append":
			appendBet = true
		case "--game", "--front", "--back", "--count", "--multiple", "--periods", "--same":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return lottery.Config{}, false, false, fmt.Errorf("%s 缺少参数值", arg)
			}
			i++
			values[strings.TrimPrefix(arg, "--")] = args[i]
		default:
			return lottery.Config{}, false, false, fmt.Errorf("未知参数：%s", arg)
		}
	}
	if wantsHelp {
		return lottery.Config{}, false, true, nil
	}
	c, err := lottery.DefaultGenerationConfig(values["game"])
	if err != nil {
		return c, false, false, err
	}
	c.Append = appendBet
	for _, field := range []struct {
		name   string
		target *int
	}{{"front", &c.Front}, {"back", &c.Back}, {"count", &c.Count}, {"periods", &c.Periods}} {
		if raw, ok := values[field.name]; ok {
			n, e := strconv.Atoi(raw)
			if e != nil {
				return c, false, false, fmt.Errorf("%s 必须是大于 0 的整数", field.name)
			}
			*field.target = n
		}
	}
	if raw, ok := values["multiple"]; ok {
		c.Multiple, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return c, false, false, fmt.Errorf("multiple 必须是大于 0 的整数")
		}
	}
	raw, hasSame := values["same"]
	if hasSame {
		if !samePattern.MatchString(raw) {
			return c, false, false, fmt.Errorf("same 格式必须是 --same 前区数量[,后区数量]")
		}
		parts := strings.Split(raw, ",")
		c.SameFront, err = strconv.Atoi(parts[0])
		if err == nil && len(parts) == 2 {
			c.SameBack, err = strconv.Atoi(parts[1])
		}
		if err != nil {
			return c, false, false, fmt.Errorf("same 格式必须是非负整数且不超过选号数量")
		}
	}
	return c, hasSame, false, nil
}

func formatNumbers(numbers []int) string {
	parts := make([]string, len(numbers))
	for i, n := range numbers {
		parts[i] = fmt.Sprintf("%02d", n)
	}
	return strings.Join(parts, " ")
}

func run(args []string, out, errOut io.Writer) int {
	c, hasSame, wantsHelp, err := parse(args)
	if err != nil {
		fmt.Fprintf(errOut, "错误：%s\n", err)
		return 1
	}
	if wantsHelp {
		fmt.Fprint(out, help)
		return 0
	}
	r, err := lottery.Generate(c)
	if err != nil {
		fmt.Fprintf(errOut, "错误：%s\n", err)
		return 1
	}
	name, frontLabel, backLabel, frontPick, backPick := "超级大乐透", "前区", "后区", 5, 2
	if c.Game == "ssq" {
		name, frontLabel, backLabel, frontPick, backPick = "双色球", "红球", "蓝球", 6, 1
	}
	appendLabel := ""
	if c.Append {
		appendLabel = "追加"
	}
	fmt.Fprintf(out, "\n使用 %s %d 倍%s\n\n-----------------------\n\n", name, c.Multiple, appendLabel)
	for i, ticket := range r.Tickets {
		kind := ""
		if c.Front > frontPick || c.Back > backPick {
			kind = " 复式"
		}
		fmt.Fprintf(out, "第%d注%s\n%s：%s\n%s：%s\n\n-----------------------\n\n", i+1, kind, frontLabel, formatNumbers(ticket.Front), backLabel, formatNumbers(ticket.Back))
	}
	selection := fmt.Sprintf("%d+%d", c.Front, c.Back)
	if c.Game == "ssq" {
		selection = fmt.Sprintf("%d 红 + %d 蓝", c.Front, c.Back)
	}
	fmt.Fprintf(out, "投注方式：%s\n复式数量：%d 组\n单组注数：%d 注\n总注数：%d 注\n投注倍数：%d 倍\n", selection, c.Count, r.SingleGroupCount, r.TotalCount, c.Multiple)
	if hasSame {
		fmt.Fprintf(out, "多注相同：%d+%d\n", c.SameFront, c.SameBack)
	}
	fmt.Fprintf(out, "\n普通投注：%d 元\n", r.NormalAmount)
	if c.Game == "dlt" {
		fmt.Fprintf(out, "追加投注：%d 元\n追加冗余：+%d 元\n\n", r.AppendAmount, r.AppendAmount-r.NormalAmount)
	}
	fmt.Fprintf(out, "连续投注：%d 期\n每期金额：%d 元\n实际投注金额：%d 元\n\n", c.Periods, r.AmountPerPeriod, r.TotalAmount)
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
