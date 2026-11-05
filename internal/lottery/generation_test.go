package lottery_test

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/dop251/goja"
	"github.com/jiuwusan/z-lottery/internal/lottery"
	assets "github.com/jiuwusan/z-lottery/lottery"
)

// Execute the actual embedded browser engine, with Web Crypto supplied by Go.
// goja is a pure Go test dependency; no external JavaScript runtime is needed.
func browserEngine(t *testing.T) *goja.Runtime {
	t.Helper()
	vm := goja.New()
	err := vm.Set("randomUint32", func() uint32 {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			panic(vm.NewGoError(err))
		}
		return binary.LittleEndian.Uint32(b[:])
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = vm.RunString(`globalThis.crypto = { getRandomValues(buffer) { for (let i=0;i<buffer.length;i++) buffer[i]=randomUint32(); return buffer; } };`); err != nil {
		t.Fatal(err)
	}
	source, err := assets.Files.ReadFile("lottery-core.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = vm.RunScript("lottery-core.js", string(source)); err != nil {
		t.Fatal(err)
	}
	return vm
}

func browserGenerate(t *testing.T, vm *goja.Runtime, c lottery.Config) lottery.Generation {
	t.Helper()
	input, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	value, err := vm.RunString("JSON.stringify(Lottery.generate(" + string(input) + "))")
	if err != nil {
		t.Fatal(err)
	}
	var r lottery.Generation
	if err = json.Unmarshal([]byte(value.String()), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func checkTickets(t *testing.T, r lottery.Generation) {
	t.Helper()
	c := r.Config
	frontMax, backMax := 35, 12
	if c.Game == "ssq" {
		frontMax, backMax = 33, 16
	}
	if len(r.Tickets) != c.Count {
		t.Fatalf("got %d tickets", len(r.Tickets))
	}
	for zone := 0; zone < 2; zone++ {
		count, max, shared := c.Front, frontMax, c.SameFront
		if zone == 1 {
			count, max, shared = c.Back, backMax, c.SameBack
		}
		var common []int
		for i, ticket := range r.Tickets {
			numbers := ticket.Front
			if zone == 1 {
				numbers = ticket.Back
			}
			if len(numbers) != count || !slices.IsSorted(numbers) {
				t.Fatalf("bad numbers: %v", numbers)
			}
			for j, n := range numbers {
				if n < 1 || n > max || j > 0 && numbers[j-1] == n {
					t.Fatalf("invalid number: %v", numbers)
				}
			}
			if i == 0 {
				common = slices.Clone(numbers)
			} else {
				common = slices.DeleteFunc(common, func(n int) bool { return !slices.Contains(numbers, n) })
			}
		}
		if len(common) < shared {
			t.Fatalf("shared %d < %d", len(common), shared)
		}
	}
}

func TestBrowserAndGoGeneration(t *testing.T) {
	vm := browserEngine(t)
	for _, game := range []string{"dlt", "ssq"} {
		base, err := lottery.DefaultGenerationConfig(game)
		if err != nil {
			t.Fatal(err)
		}
		cases := []lottery.Config{base}
		shared := base
		shared.Count = 20
		shared.Front = 8
		shared.Back = 3
		shared.SameFront = 4
		shared.SameBack = 2
		cases = append(cases, shared)
		all := base
		all.SameFront = all.Front
		all.SameBack = all.Back
		cases = append(cases, all)
		multiple := base
		multiple.Multiple = 3
		multiple.Periods = 10
		cases = append(cases, multiple)
		single := base
		single.Count = 1
		if game == "dlt" {
			single.Front = 5
			single.Back = 2
			single.Append = true
			single.Multiple = 5
			single.Periods = 10
		} else {
			single.Front = 6
			single.Back = 1
		}
		cases = append(cases, single)
		max := base
		max.Count = 1000
		max.Front = 35
		max.Back = 12
		if game == "ssq" {
			max.Front = 33
			max.Back = 16
		}
		max.SameFront = max.Front
		max.SameBack = max.Back
		cases = append(cases, max)
		for i, c := range cases {
			t.Run(fmt.Sprintf("%s/%d", game, i), func(t *testing.T) {
				actual, err := lottery.Generate(c)
				if err != nil {
					t.Fatal(err)
				}
				browser := browserGenerate(t, vm, c)
				checkTickets(t, actual)
				checkTickets(t, browser)
				if !reflect.DeepEqual(actual.Config, browser.Config) {
					t.Fatalf("config mismatch: %+v / %+v", actual.Config, browser.Config)
				}
				if actual.SingleGroupCount != browser.SingleGroupCount || actual.TotalCount != browser.TotalCount || actual.NormalAmount != browser.NormalAmount || actual.AmountPerPeriod != browser.AmountPerPeriod || actual.TotalAmount != browser.TotalAmount || game == "dlt" && actual.AppendAmount != browser.AppendAmount {
					t.Fatalf("amount mismatch: %+v / %+v", actual, browser)
				}
			})
		}
	}
}

func TestBrowserDefaultsAndAmounts(t *testing.T) {
	vm := browserEngine(t)
	for _, tc := range []struct {
		input                 string
		single, total, amount int64
	}{
		{`{}`, 18, 72, 144}, {`{game:'ssq'}`, 14, 56, 112},
		{`{game:'ssq',multiple:3,periods:10}`, 14, 56, 3360},
		{`{front:5,back:2,count:1,append:true,multiple:5,periods:10}`, 1, 1, 150},
		{`{sameFront:6,sameBack:3}`, 18, 72, 144},
	} {
		value, err := vm.RunString("JSON.stringify(Lottery.generate(" + tc.input + "))")
		if err != nil {
			t.Fatal(err)
		}
		var r lottery.Generation
		if err = json.Unmarshal([]byte(value.String()), &r); err != nil {
			t.Fatal(err)
		}
		if r.SingleGroupCount != tc.single || r.TotalCount != tc.total || r.TotalAmount != tc.amount {
			t.Fatalf("wrong amount for %s: %+v", tc.input, r)
		}
		checkTickets(t, r)
	}
}

func TestBrowserRejectsInvalidGeneration(t *testing.T) {
	vm := browserEngine(t)
	for _, input := range []string{
		`{game:'unknown'}`, `{game:'ssq',append:true}`, `{front:4}`, `{back:13}`, `{front:''}`,
		`{count:0}`, `{count:1001}`, `{multiple:1.5}`, `{periods:-1}`, `{sameFront:7}`, `{sameBack:-1}`,
		`{sameFront:0.5}`, `{multiple:Number.MAX_SAFE_INTEGER}`, `{periods:Number.MAX_SAFE_INTEGER}`,
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := vm.RunString("Lottery.generate(" + input + ")"); err == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
}

func TestGoRejectsInvalidGeneration(t *testing.T) {
	base, _ := lottery.DefaultGenerationConfig("dlt")
	for _, mutate := range []func(*lottery.Config){
		func(c *lottery.Config) { c.Game = "unknown" }, func(c *lottery.Config) { c.Game = "ssq"; c.Append = true },
		func(c *lottery.Config) { c.Front = 4 }, func(c *lottery.Config) { c.Back = 13 },
		func(c *lottery.Config) { c.Count = 0 }, func(c *lottery.Config) { c.Count = 1001 },
		func(c *lottery.Config) { c.Multiple = 0 }, func(c *lottery.Config) { c.Periods = 0 },
		func(c *lottery.Config) { c.SameFront = -1 }, func(c *lottery.Config) { c.SameFront = 7 }, func(c *lottery.Config) { c.SameBack = 4 },
		func(c *lottery.Config) { c.Multiple = lottery.MaxSafeInteger },
		func(c *lottery.Config) { c.Periods = int(lottery.MaxSafeInteger) },
		func(c *lottery.Config) { c.Multiple = 1 << 62 },
	} {
		c := base
		mutate(&c)
		if _, err := lottery.Generate(c); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
}
