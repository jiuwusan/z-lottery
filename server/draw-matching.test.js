const test = require('node:test');
const assert = require('node:assert/strict');
const { selectDraws, normalizePlacedAt } = require('./draw-matching');
const draws = [
  { game: 'dlt', issue: '26124', date: '2026-10-31' },
  { game: 'dlt', issue: '26123', date: '2026-10-28' },
  { game: 'dlt', issue: '26125', date: '2026-11-02' },
  { game: 'dlt', issue: '26122', date: '2026-10-26' },
  { game: 'dlt', issue: '26126', date: '2026-11-04' },
];

test('10月28日18:45投注连续3期，按实际开奖日期排序包含当天', () => {
  assert.deepEqual(selectDraws('dlt', '2026-10-28T18:45:00+08:00', 3, draws).map(d => d.issue), ['26123', '26124', '26125']);
});

test('销售截止前一毫秒计当期，等于截止和开奖前间隙顺延至下一实际开奖', () => {
  for (const time of ['2026-10-28T21:00:00+08:00', '2026-10-28T21:15:00+08:00']) {
    assert.deepEqual(selectDraws('dlt', time, 3, draws).map(d => d.issue), ['26124', '26125', '26126']);
  }
  assert.equal(selectDraws('dlt', '2026-10-28T20:59:59.999+08:00', 1, draws)[0].issue, '26123');
  const ssq = [{ game: 'ssq', issue: '2026124', date: '2026-10-29' }, { game: 'ssq', issue: '2026125', date: '2026-11-01' }];
  assert.equal(selectDraws('ssq', '2026-10-29T20:00:00+08:00', 1, ssq)[0].issue, '2026125');
});

test('优先使用官方当期截止时间，兼容历史截止时间变化', () => {
  const rows = [{ game: 'dlt', issue: '26123', date: '2026-10-28', salesCloseAt: '2026-10-28T20:30:00+08:00' }, ...draws.filter(d => d.issue !== '26123')];
  assert.equal(selectDraws('dlt', '2026-10-28T20:45:00+08:00', 1, rows)[0].issue, '26124');
});

test('休市和跨年按真实记录自然跨过，开奖不足时只返回已有记录', () => {
  const rows = [{ game: 'ssq', issue: '2027001', date: '2027-01-03' }, { game: 'ssq', issue: '2026151', date: '2026-12-31' }];
  assert.deepEqual(selectDraws('ssq', '2026-12-30T18:45:00+08:00', 3, rows).map(d => d.issue), ['2026151', '2027001']);
  assert.equal(selectDraws('dlt', '2026-12-30T18:45:00+08:00', 3, []).length, 0);
});

test('投注时间必须带时区、日期有效且不能在未来', () => {
  const now = new Date('2026-10-28T12:00:00Z');
  assert.equal(normalizePlacedAt(undefined, now), now.toISOString());
  assert.equal(normalizePlacedAt('2026-10-28T18:45:00+08:00', now), '2026-10-28T10:45:00.000Z');
  for (const value of ['2026-10-28T18:45', '2026-02-30T00:00:00Z', 'bad', '2026-10-29T00:00:00Z']) assert.throws(() => normalizePlacedAt(value, now));
});
