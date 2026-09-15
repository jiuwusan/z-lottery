const test = require("node:test");
const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const path = require("node:path");

const script = path.join(__dirname, "lottery.js");

function run(args = []) {
  return spawnSync(process.execPath, [script, ...args], {
    encoding: "utf8",
  });
}

function readNumberLines(output, label) {
  return output
    .split(/\r?\n/)
    .filter((line) => line.startsWith(`${label}：`))
    .map((line) => line.slice(label.length + 1).trim().split(/\s+/).map(Number));
}

function assertValidSelections(selections, expectedGroups, expectedCount, max) {
  assert.equal(selections.length, expectedGroups);

  for (const numbers of selections) {
    assert.equal(numbers.length, expectedCount);
    assert.equal(new Set(numbers).size, expectedCount);
    assert.ok(numbers.every((number) => number >= 1 && number <= max));
    assert.deepEqual(numbers, [...numbers].sort((a, b) => a - b));
  }
}

function commonNumbers(selections) {
  return selections[0].filter((number) => selections.every((selection) => selection.includes(number)));
}

test("双色球默认生成四组 7+2 复式并按 14 注/组计费", () => {
  const result = run(["--game", "ssq"]);

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /^使用 双色球 1 倍$/m);
  assert.match(result.stdout, /^第1注 复式$/m);
  assert.doesNotMatch(result.stdout, /^第\d+注.*倍/m);
  assertValidSelections(readNumberLines(result.stdout, "红球"), 4, 7, 33);
  assertValidSelections(readNumberLines(result.stdout, "蓝球"), 4, 2, 16);
  assert.match(result.stdout, /单组注数：14 注/);
  assert.match(result.stdout, /总注数：56 注/);
  assert.match(result.stdout, /连续投注：1 期/);
  assert.match(result.stdout, /每期金额：112 元/);
  assert.match(result.stdout, /实际投注金额：112 元/);
});

test("不传 game 时保持大乐透 6+3 默认行为", () => {
  const result = run();

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /^使用 超级大乐透 1 倍$/m);
  assertValidSelections(readNumberLines(result.stdout, "前区"), 4, 6, 35);
  assertValidSelections(readNumberLines(result.stdout, "后区"), 4, 3, 12);
  assert.match(result.stdout, /单组注数：18 注/);
  assert.match(result.stdout, /总注数：72 注/);
  assert.match(result.stdout, /实际投注金额：144 元/);
});

test("双色球支持自定义红蓝球数量和组数", () => {
  const result = run([
    "--game",
    "ssq",
    "--front",
    "8",
    "--back",
    "3",
    "--count",
    "2",
  ]);

  assert.equal(result.status, 0, result.stderr);
  assertValidSelections(readNumberLines(result.stdout, "红球"), 2, 8, 33);
  assertValidSelections(readNumberLines(result.stdout, "蓝球"), 2, 3, 16);
  assert.match(result.stdout, /单组注数：84 注/);
  assert.match(result.stdout, /总注数：168 注/);
  assert.match(result.stdout, /实际投注金额：336 元/);
});

test("same 可以让多注前区和后区共享指定数量的号码", () => {
  const result = run(["--game", "dlt", "--count", "4", "--same", "3,2"]);

  assert.equal(result.status, 0, result.stderr);
  assert.ok(commonNumbers(readNumberLines(result.stdout, "前区")).length >= 3);
  assert.ok(commonNumbers(readNumberLines(result.stdout, "后区")).length >= 2);
});

test("same 省略第二位时只要求前区共享号码", () => {
  const result = run(["--game", "ssq", "--count", "4", "--same", "3"]);

  assert.equal(result.status, 0, result.stderr);
  assert.ok(commonNumbers(readNumberLines(result.stdout, "红球")).length >= 3);
});

test("same 拒绝无效格式和超过选号数量的共享数量", () => {
  for (const same of ["3,", ",2", "3,2,1", "-1,0", "1.5,0"]) {
    const result = run(["--same", same]);

    assert.equal(result.status, 1);
    assert.match(result.stderr, /same 格式必须是/);
  }

  const tooManyFront = run(["--front", "5", "--same", "6"]);
  assert.equal(tooManyFront.status, 1);
  assert.match(tooManyFront.stderr, /same 前区数量不能超过 5/);

  const tooManyBack = run(["--back", "2", "--same", "0,3"]);
  assert.equal(tooManyBack.status, 1);
  assert.match(tooManyBack.stderr, /same 后区数量不能超过 2/);
});

test("双色球拒绝追加投注", () => {
  const result = run(["--game", "ssq", "--append"]);

  assert.equal(result.status, 1);
  assert.match(result.stderr, /双色球不支持追加投注/);
});

test("双色球拒绝少于六个红球", () => {
  const result = run(["--game", "ssq", "--front", "5"]);

  assert.equal(result.status, 1);
  assert.match(result.stderr, /红球数量必须是 6～33/);
});

test("大乐透仍支持追加投注", () => {
  const result = run([
    "--game",
    "dlt",
    "--front",
    "5",
    "--back",
    "2",
    "--count",
    "1",
    "--append",
  ]);

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /^使用 超级大乐透 1 倍追加$/m);
  assert.match(result.stdout, /^第1注$/m);
  assert.doesNotMatch(result.stdout, /^第1注.*倍/m);
  assert.match(result.stdout, /普通投注：2 元/);
  assert.match(result.stdout, /追加投注：3 元/);
  assert.match(result.stdout, /实际投注金额：3 元/);
});

test("双色球连续多期复用号码并按期数计算总金额", () => {
  const result = run([
    "--game",
    "ssq",
    "--count",
    "1",
    "--periods",
    "10",
  ]);

  assert.equal(result.status, 0, result.stderr);
  assertValidSelections(readNumberLines(result.stdout, "红球"), 1, 7, 33);
  assertValidSelections(readNumberLines(result.stdout, "蓝球"), 1, 2, 16);
  assert.match(result.stdout, /总注数：14 注/);
  assert.match(result.stdout, /连续投注：10 期/);
  assert.match(result.stdout, /每期金额：28 元/);
  assert.match(result.stdout, /实际投注金额：280 元/);
});

test("大乐透追加投注连续多期按追加后的每期金额计算", () => {
  const result = run([
    "--game",
    "dlt",
    "--front",
    "5",
    "--back",
    "2",
    "--count",
    "1",
    "--append",
    "--multiple",
    "5",
    "--periods",
    "10",
  ]);

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /^使用 超级大乐透 5 倍追加$/m);
  assert.match(result.stdout, /投注倍数：5 倍/);
  assert.match(result.stdout, /连续投注：10 期/);
  assert.match(result.stdout, /每期金额：15 元/);
  assert.match(result.stdout, /实际投注金额：150 元/);
});

test("双色球自定义倍数同时影响每期金额和连续多期总金额", () => {
  const result = run([
    "--game",
    "ssq",
    "--count",
    "1",
    "--multiple",
    "3",
    "--periods",
    "10",
  ]);

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /^使用 双色球 3 倍$/m);
  assert.match(result.stdout, /每期金额：84 元/);
  assert.match(result.stdout, /实际投注金额：840 元/);
});

test("拒绝非正整数连续期数", () => {
  for (const periods of ["0", "-1", "1.5"]) {
    const result = run(["--periods", periods]);

    assert.equal(result.status, 1);
    assert.match(result.stderr, /periods 必须是大于 0 的整数/);
  }
});

test("拒绝非正整数投注倍数", () => {
  for (const multiple of ["0", "-1", "1.5"]) {
    const result = run(["--multiple", multiple]);

    assert.equal(result.status, 1);
    assert.match(result.stderr, /multiple 必须是大于 0 的整数/);
  }
});

test("拒绝未知的彩票类型", () => {
  const result = run(["--game", "unknown"]);

  assert.equal(result.status, 1);
  assert.match(result.stderr, /game 必须是 dlt 或 ssq/);
});
