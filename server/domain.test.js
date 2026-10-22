const test = require('node:test');
const assert = require('node:assert/strict');
const { normalizeBet, checkTicket, normalizeIssue } = require('./domain');

const ticket = { front: [1, 2, 3, 4, 5], back: [1, 2] };
const draw = (issue = '26014', prices = { 1: 1000000, 2: 100000, 3: 5000, 4: 300, 5: 150, 6: 15, 7: 5 }) => ({
  game: 'dlt', issue, front: [1, 2, 3, 4, 5], back: [1, 2], prizes: prices, appendPrizes: { 1: 800000, 2: 80000 },
});

test('投注验证使用实际号码重算金额并规范化期号', () => {
  const bet = normalizeBet({ game: 'dlt', tickets: [ticket], issues: ['2026014', '26015'], multiple: 2, append: true, totalAmount: 1 });
  assert.deepEqual(bet.issues, ['26014', '26015']);
  assert.equal(bet.totalAmount, 12);
  assert.equal(bet.config.periods, 2);
  assert.equal(normalizeIssue('ssq', '2026120'), '2026120');
});

test('拒绝重复/越界号码、空号码、重复期号、不一致规格和双色球追加', () => {
  const base = { game: 'dlt', tickets: [ticket], issues: ['26014'] };
  for (const input of [
    { ...base, tickets: [{ ...ticket, front: [1, 1, 2, 3, 4] }] },
    { ...base, tickets: [{ ...ticket, back: [0, 13] }] },
    { ...base, tickets: [] }, { ...base, issues: ['26014', '2026014'] },
    { ...base, issues: ['26000'] }, { ...base, issues: ['18001'] },
    { ...base, tickets: [ticket, { ...ticket, front: [1, 2, 3, 4, 5, 6] }] },
    { ...base, append: 'false' }, { ...base, multiple: 0 },
    { game: 'ssq', tickets: [{ front: [1, 2, 3, 4, 5, 6], back: [1] }], issues: ['2026120'], append: true },
  ]) assert.throws(() => normalizeBet(input));
});

test('复式 6+3 同时中多个奖级，追加只参与浮动奖，倍数分别累计', () => {
  const result = checkTicket({ front: [1, 2, 3, 4, 5, 6], back: [1, 2, 3] }, draw(), { multiple: 2, append: true });
  assert.deepEqual(result.winners.map(w => [w.level, w.count]), [[1, 2], [2, 4], [3, 10], [4, 20]]);
  assert.equal(result.amount, 4376000);
  assert.equal(result.complete, true);
});

test('大乐透 26014 起 4+2 为三等奖，之前为四等奖', () => {
  const selected = { ...ticket, front: [1, 2, 3, 4, 6] };
  assert.equal(checkTicket(selected, draw('26014'), {}).winners[0].level, 3);
  assert.equal(checkTicket(selected, draw('26013'), {}).winners[0].level, 4);
});

test('双色球复式按组合计数，每个单式仅取最高奖级', () => {
  const result = checkTicket({ front: [1, 2, 3, 4, 5, 6, 7], back: [1, 2] }, {
    game: 'ssq', issue: '2026120', front: [1, 2, 3, 4, 5, 6], back: [1],
    prizes: { 1: 5000000, 2: 100000, 3: 3000, 4: 200, 5: 10, 6: 5 }, appendPrizes: {},
  }, {});
  assert.deepEqual(result.winners.map(w => [w.level, w.count]), [[1, 1], [2, 1], [3, 6], [4, 6]]);
  assert.equal(result.amount, 5119200);
});

test('缺失奖金仍识别中奖，金额保持待补全；未中奖金额为零', () => {
  const result = checkTicket(ticket, draw('26014', {}), { append: true });
  assert.equal(result.winners[0].level, 1);
  assert.equal(result.complete, false);
  assert.equal(result.amount, null);
  const lost = checkTicket({ front: [20, 21, 22, 23, 24], back: [10, 11] }, draw(), {});
  assert.equal(lost.amount, 0);
  assert.equal(lost.complete, true);
});

test('追加奖金缺失时，已公布的基础奖金仍计入已知奖金', () => {
  const result = checkTicket(ticket, { ...draw(), appendPrizes: {} }, { multiple: 2, append: true });
  assert.equal(result.amount, null);
  assert.equal(result.complete, false);
  assert.equal(result.knownAmount, 2000000);
});
