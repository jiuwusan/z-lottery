const test = require('node:test');
const assert = require('node:assert/strict');
const { DrawProvider } = require('./draw-provider');

const dlt = { success: true, errorCode: '0', value: { list: [{
  lotteryDrawNum: '26119', lotteryDrawResult: '03 06 20 24 32 04 07', lotteryDrawTime: '2026-10-19', verify: 1, lotteryDrawStatus: 20,
  prizeLevelList: [{ prizeLevel: '一等奖', stakeAmount: '9,909,522' }, { prizeLevel: '一等奖(追加)', stakeAmount: '---' }, { prizeLevel: '二等奖(追加)', stakeAmount: '130,131' }],
}] } };
const ssq = { state: 0, result: [{ code: '2026120', date: '2026-10-20(二)', red: '13,14,16,18,20,31', blue: '03', prizegrades: [{ type: 1, typemoney: '7232345' }, { type: 6, typemoney: '5' }] }] };
function provider(body, onUrl = () => {}) { return new DrawProvider({ fetchImpl: async url => { onUrl(new URL(url)); return new Response(JSON.stringify(body)); } }); }

test('准确查询大乐透期号，解析千分位和追加奖金，缺失追加奖金不能当零', async () => {
  const draw = await provider(dlt, url => {
    assert.equal(url.searchParams.get('startTerm'), '26119');
    assert.equal(url.searchParams.get('endTerm'), '26119');
  }).getDraw('dlt', '26119');
  assert.deepEqual(draw.front, [3, 6, 20, 24, 32]);
  assert.deepEqual(draw.back, [4, 7]);
  assert.equal(draw.prizes[1], 9909522);
  assert.equal(draw.appendPrizes[1], null);
  assert.equal(draw.appendPrizes[2], 130131);
});

test('双色球查询指定期号，解析奖级、号码及日期', async () => {
  const draw = await provider(ssq, url => assert.equal(url.searchParams.get('issueStart'), '2026120')).getDraw('ssq', '2026120');
  assert.equal(draw.prizes[6], 5);
  assert.equal(draw.date, '2026-10-20');
  assert.deepEqual(draw.back, [3]);
});

test('空列表表示尚未开奖，非法响应、错期号或无效号码必须报错', async () => {
  assert.equal(await provider({ success: true, errorCode: '0', value: { list: [] } }).getDraw('dlt', '26120'), null);
  for (const payload of [
    { success: false }, {},
    { ...dlt, value: { list: [{ ...dlt.value.list[0], lotteryDrawNum: '26118' }] } },
    { ...dlt, value: { list: [{ ...dlt.value.list[0], lotteryDrawResult: '03 03 20 24 32 04 07' }] } },
  ]) await assert.rejects(provider(payload).getDraw('dlt', '26119'));
  await assert.rejects(new DrawProvider({ fetchImpl: async () => new Response('<html>blocked</html>') }).getDraw('dlt', '26119'));
});

test('未审核完成的大乐透开奖不验奖', async () => {
  const response = { ...dlt, value: { list: [{ ...dlt.value.list[0], verify: 0 }] } };
  assert.equal(await provider(response).getDraw('dlt', '26119'), null);
});

const historyRow = (issue, date, extra = {}) => ({ ...dlt.value.list[0], lotteryDrawNum: issue, lotteryDrawTime: date, ...extra });

test('历史开奖按日期范围翻页直至覆盖投注日，再按升序返回实际开奖', async () => {
  const pages = [];
  const p = new DrawProvider({ fetchImpl: async url => {
    const page = Number(new URL(url).searchParams.get('pageNo')); pages.push(page);
    return new Response(JSON.stringify({ success: true, errorCode: '0', value: { pages: 2, total: 4, list: page === 1
      ? [historyRow('26125', '2026-11-02')]
      : [historyRow('26124', '2026-10-31'), historyRow('26123', '2026-10-28', { lotterySaleEndtime: '2026-10-28 21:00:00' }), historyRow('26122', '2026-10-26')] } }));
  } });
  const result = await p.getHistory('dlt', { fromDate: '2026-10-28', throughDate: '2026-11-04' });
  assert.deepEqual(pages, [1, 2]);
  assert.deepEqual(result.map(d => d.issue), ['26123', '26124', '26125']);
  assert.equal(result[0].salesCloseAt, '2026-10-28T13:00:00.000Z');
});

test('历史分页中断、记录缺口和不覆盖投注日期时拒绝返回不完整结果', async () => {
  const params = { fromDate: '2026-10-28', throughDate: '2026-11-04' };
  for (const rows of [
    [historyRow('26125', '2026-11-02'), historyRow('26123', '2026-10-28'), historyRow('26122', '2026-10-26')],
    [historyRow('26125', '2026-11-02'), historyRow('26124', '2026-10-31'), historyRow('26122', '2026-10-26')],
    [historyRow('26125', '2026-11-02'), historyRow('26124', '2026-10-31', { verify: 0 }), historyRow('26123', '2026-10-28')],
    [historyRow('26125', '2026-11-02')],
  ]) await assert.rejects(provider({ success: true, errorCode: '0', value: { pages: 1, total: rows.length, list: rows } }).getHistory('dlt', params));
  const broken = new DrawProvider({ fetchImpl: async url => {
    if (new URL(url).searchParams.get('pageNo') === '2') return new Response('blocked', { status: 503 });
    return new Response(JSON.stringify({ success: true, errorCode: '0', value: { pages: 2, total: 2, list: [historyRow('26125', '2026-11-02')] } }));
  } });
  await assert.rejects(broken.getHistory('dlt', params));
});

test('双色球历史列表支持跨年，未开奖最新一期不会提前纳入', async () => {
  const old = ssq.result[0];
  const rows = [
    { ...old, code: '2027001', date: '2027-01-03' },
    { ...old, code: '2026151', date: '2026-12-31' },
    { ...old, code: '2026150', date: '2026-12-29' },
  ];
  const result = await provider({ state: 0, total: 3, pageNum: 1, result: rows }).getHistory('ssq', { fromDate: '2026-12-30', throughDate: '2027-01-03' });
  assert.deepEqual(result.map(d => d.issue), ['2026151', '2027001']);
  const waiting = await provider({ success: true, errorCode: '0', value: { pages: 1, total: 2, list: [historyRow('26123', '2026-10-28', { verify: 0 }), historyRow('26122', '2026-10-26')] } })
    .getHistory('dlt', { fromDate: '2026-10-28', throughDate: '2026-10-28' });
  assert.deepEqual(waiting, []);
});
