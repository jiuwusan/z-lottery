package main

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func invoke(args ...string) (string, string, int) {
	var out, err bytes.Buffer
	code := run(args, &out, &err)
	return out.String(), err.String(), code
}

func selections(t *testing.T, output, label string, groups, count, max, shared int) {
	t.Helper()
	var rows [][]int
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, label+"：") {
			continue
		}
		var row []int
		for _, raw := range strings.Fields(strings.TrimPrefix(line, label+"：")) {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > max || slices.Contains(row, n) {
				t.Fatalf("invalid %s: %s", label, line)
			}
			row = append(row, n)
		}
		if len(row) != count || !slices.IsSorted(row) {
			t.Fatalf("invalid selection: %v", row)
		}
		rows = append(rows, row)
	}
	if len(rows) != groups {
		t.Fatalf("got %d %s groups, want %d: %s", len(rows), label, groups, output)
	}
	common := 0
	for _, n := range rows[0] {
		found := true
		for _, row := range rows {
			found = found && slices.Contains(row, n)
		}
		if found {
			common++
		}
	}
	if common < shared {
		t.Fatalf("shared %d < %d", common, shared)
	}
}

func TestCLISelectionsAndAmounts(t *testing.T) {
	cases := []struct {
		name                                                        string
		args                                                        []string
		labels                                                      [2]string
		groups, front, back, frontMax, backMax, sameFront, sameBack int
		want                                                        []string
	}{
		{"default", nil, [2]string{"前区", "后区"}, 4, 6, 3, 35, 12, 0, 0, []string{"使用 超级大乐透 1 倍", "第1注 复式", "单组注数：18 注", "总注数：72 注", "实际投注金额：144 元"}},
		{"ssq", []string{"--game", "ssq"}, [2]string{"红球", "蓝球"}, 4, 7, 2, 33, 16, 0, 0, []string{"使用 双色球 1 倍", "单组注数：14 注", "总注数：56 注", "每期金额：112 元"}},
		{"custom", []string{"--game", "ssq", "--front", "8", "--back", "3", "--count", "2"}, [2]string{"红球", "蓝球"}, 2, 8, 3, 33, 16, 0, 0, []string{"单组注数：84 注", "总注数：168 注", "实际投注金额：336 元"}},
		{"shared", []string{"--same", "3,2"}, [2]string{"前区", "后区"}, 4, 6, 3, 35, 12, 3, 2, []string{"多注相同：3+2"}},
		{"shared front", []string{"--game", "ssq", "--same", "3"}, [2]string{"红球", "蓝球"}, 4, 7, 2, 33, 16, 3, 0, []string{"多注相同：3+0"}},
		{"all shared", []string{"--same", "6,3"}, [2]string{"前区", "后区"}, 4, 6, 3, 35, 12, 6, 3, []string{"实际投注金额：144 元"}},
		{"zero shared", []string{"--same", "0,0"}, [2]string{"前区", "后区"}, 4, 6, 3, 35, 12, 0, 0, []string{"多注相同：0+0"}},
		{"append", []string{"--front", "5", "--back", "2", "--count", "1", "--append", "--multiple", "5", "--periods", "10"}, [2]string{"前区", "后区"}, 1, 5, 2, 35, 12, 0, 0, []string{"使用 超级大乐透 5 倍追加", "第1注\n", "普通投注：10 元", "追加投注：15 元", "追加冗余：+5 元", "每期金额：15 元", "实际投注金额：150 元"}},
		{"periods", []string{"--game", "ssq", "--count", "1", "--periods", "10"}, [2]string{"红球", "蓝球"}, 1, 7, 2, 33, 16, 0, 0, []string{"总注数：14 注", "每期金额：28 元", "实际投注金额：280 元"}},
		{"multiple", []string{"--game", "ssq", "--count", "1", "--multiple", "3", "--periods", "10"}, [2]string{"红球", "蓝球"}, 1, 7, 2, 33, 16, 0, 0, []string{"使用 双色球 3 倍", "实际投注金额：840 元"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err, code := invoke(tc.args...)
			if code != 0 || err != "" {
				t.Fatalf("exit %d: %s", code, err)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q: %s", want, out)
				}
			}
			selections(t, out, tc.labels[0], tc.groups, tc.front, tc.frontMax, tc.sameFront)
			selections(t, out, tc.labels[1], tc.groups, tc.back, tc.backMax, tc.sameBack)
			if tc.labels[0] == "红球" && strings.Contains(out, "追加投注：") {
				t.Fatal("SSQ append output")
			}
		})
	}
}

func TestCLIInvalidArguments(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--game", "unknown"}, "game 必须是"},
		{[]string{"--game", "ssq", "--append"}, "双色球不支持追加"},
		{[]string{"--game", "ssq", "--front", "5"}, "红球数量必须是 6～33"},
		{[]string{"--back", "13"}, "后区数量必须是"},
		{[]string{"--front", "5", "--same", "6"}, "same 前区数量不能超过 5"},
		{[]string{"--back", "2", "--same", "0,3"}, "same 后区数量不能超过 2"},
		{[]string{"--multiple", "9007199254740991"}, "投注金额过大"},
		{[]string{"--unknown"}, "未知参数"}, {[]string{"extra"}, "未知参数"},
	}
	for _, v := range []string{"3,", ",2", "3,2,1", "-1,0", "1.5,0", ""} {
		cases = append(cases, struct {
			args []string
			want string
		}{[]string{"--same", v}, "same 格式必须是"})
	}
	for _, flag := range []string{"count", "multiple", "periods"} {
		for _, v := range []string{"0", "-1", "1.5", "NaN", "999999999999999999999999"} {
			cases = append(cases, struct {
				args []string
				want string
			}{[]string{"--" + flag, v}, flag + " 必须是"})
		}
	}
	cases = append(cases, struct {
		args []string
		want string
	}{[]string{"--count", "1001"}, "count 必须是"})
	for _, flag := range []string{"game", "front", "back", "count", "multiple", "periods", "same"} {
		cases = append(cases, struct {
			args []string
			want string
		}{[]string{"--" + flag}, "缺少参数值"})
	}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			out, err, code := invoke(tc.args...)
			if code != 1 || out != "" || !strings.Contains(err, tc.want) {
				t.Fatalf("%v => %d %q %q, want %s", tc.args, code, out, err, tc.want)
			}
		})
	}
}

func TestCLIHelp(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		out, err, code := invoke(arg)
		if code != 0 || err != "" || !strings.Contains(out, "go run ./cmd/lottery") || !strings.Contains(out, "--same") {
			t.Fatalf("help: %d %s %s", code, out, err)
		}
	}
}
