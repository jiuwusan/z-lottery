package domain

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sort"
)

// Generation includes both the generated tickets and their exact integer costs.
type Generation struct {
	Config           Config   `json:"config"`
	Tickets          []Ticket `json:"tickets"`
	SingleGroupCount int64    `json:"singleGroupCount"`
	TotalCount       int64    `json:"totalCount"`
	NormalAmount     int64    `json:"normalAmount"`
	AppendAmount     int64    `json:"appendAmount"`
	AmountPerPeriod  int64    `json:"amountPerPeriod"`
	TotalAmount      int64    `json:"totalAmount"`
}

func DefaultGenerationConfig(game string) (Config, error) {
	c := Config{Game: game, Front: 6, Back: 3, Count: 4, Multiple: 1, Periods: 1}
	if game == "ssq" {
		c.Front, c.Back = 7, 2
	} else if game != "dlt" {
		return c, fmt.Errorf("game 必须是 dlt 或 ssq")
	}
	return c, nil
}

// CalculateGeneration validates before allocating tickets or drawing random numbers.
func CalculateGeneration(c Config) (Generation, error) {
	r := Generation{Config: c}
	spec, ok := domainGames[c.Game]
	if !ok {
		return r, fmt.Errorf("game 必须是 dlt 或 ssq")
	}
	if c.Front < spec.frontPick || c.Front > spec.frontMax {
		return r, fmt.Errorf("%s数量必须是 %d～%d", spec.frontLabel, spec.frontPick, spec.frontMax)
	}
	if c.Back < spec.backPick || c.Back > spec.backMax {
		return r, fmt.Errorf("%s数量必须是 %d～%d", spec.backLabel, spec.backPick, spec.backMax)
	}
	if c.Count < 1 || c.Count > 1000 {
		return r, fmt.Errorf("count 必须是 1～1000 的整数")
	}
	if c.Multiple < 1 || c.Multiple > MaxSafeInteger {
		return r, fmt.Errorf("multiple 必须是大于 0 的整数且不超过 %d", MaxSafeInteger)
	}
	if c.Periods < 1 || int64(c.Periods) > MaxSafeInteger {
		return r, fmt.Errorf("periods 必须是大于 0 的整数且不超过 %d", MaxSafeInteger)
	}
	if c.SameFront < 0 || c.SameBack < 0 {
		return r, fmt.Errorf("共享号码数量必须是非负整数")
	}
	if c.SameFront > c.Front {
		return r, fmt.Errorf("same 前区数量不能超过 %d", c.Front)
	}
	if c.SameBack > c.Back {
		return r, fmt.Errorf("same 后区数量不能超过 %d", c.Back)
	}
	if c.Append && c.Game == "ssq" {
		return r, fmt.Errorf("双色球不支持追加投注")
	}
	r.SingleGroupCount = int64(choose(c.Front, spec.frontPick)) * int64(choose(c.Back, spec.backPick))
	r.TotalCount = r.SingleGroupCount * int64(c.Count)
	// Check each multiplication before performing it, avoiding int64 overflow.
	multiply := func(a, b int64) (int64, error) {
		if a > MaxSafeInteger/b {
			return 0, fmt.Errorf("投注金额过大，请减少组数、倍数或期数")
		}
		return a * b, nil
	}
	base, err := multiply(r.TotalCount, c.Multiple)
	if err != nil {
		return r, err
	}
	r.NormalAmount, err = multiply(base, 2)
	if err != nil {
		return r, err
	}
	if c.Game == "dlt" {
		r.AppendAmount, err = multiply(base, 3)
		if err != nil {
			return r, err
		}
	}
	r.AmountPerPeriod = r.NormalAmount
	if c.Append {
		r.AmountPerPeriod = r.AppendAmount
	}
	r.TotalAmount, err = multiply(r.AmountPerPeriod, int64(c.Periods))
	return r, err
}

func generateNumbers(max, count int, excluded []int) ([]int, error) {
	pool := make([]int, 0, max-len(excluded))
	excludedSet := make(map[int]bool, len(excluded))
	for _, n := range excluded {
		excludedSet[n] = true
	}
	for n := 1; n <= max; n++ {
		if !excludedSet[n] {
			pool = append(pool, n)
		}
	}
	for i := len(pool) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return nil, fmt.Errorf("生成安全随机数: %w", err)
		}
		j := int(n.Int64())
		pool[i], pool[j] = pool[j], pool[i]
	}
	values := append(append(make([]int, 0, len(excluded)+count), excluded...), pool[:count]...)
	sort.Ints(values)
	return values, nil
}

func Generate(c Config) (Generation, error) {
	r, err := CalculateGeneration(c)
	if err != nil {
		return r, err
	}
	spec := domainGames[c.Game]
	commonFront, err := generateNumbers(spec.frontMax, c.SameFront, nil)
	if err != nil {
		return r, err
	}
	commonBack, err := generateNumbers(spec.backMax, c.SameBack, nil)
	if err != nil {
		return r, err
	}
	r.Tickets = make([]Ticket, 0, c.Count)
	for i := 0; i < c.Count; i++ {
		front, err := generateNumbers(spec.frontMax, c.Front-c.SameFront, commonFront)
		if err != nil {
			return r, err
		}
		back, err := generateNumbers(spec.backMax, c.Back-c.SameBack, commonBack)
		if err != nil {
			return r, err
		}
		r.Tickets = append(r.Tickets, Ticket{Front: front, Back: back})
	}
	return r, nil
}
