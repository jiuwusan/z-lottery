const test = require('node:test');
const assert = require('node:assert/strict');
const { Repository } = require('./repository');
const { BetService } = require('./service');
const { createServer } = require('./app');
const jobToken = 'test-job-token-at-least-16';

async function setup(t, provider = { getDraw: async () => null }, clock = () => new Date()) {
  const repo = new Repository(':memory:');
  const server = createServer({ service: new BetService(repo, provider, { clock }), jobToken });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(async () => { await new Promise(resolve => server.close(resolve)); repo.close(); });
  const base = `http://127.0.0.1:${server.address().port}`;
  const request = (route, { token, method = 'GET', body } = {}) => fetch(base + route, {
    method, headers: { ...(token ? { Authorization: 'Bearer ' + token } : {}), ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  return { request, base };
}
const bet = { game: 'ssq', tickets: [{ front: [1, 2, 3, 4, 5, 6], back: [1] }], issues: ['2026120'] };

test('HTTP 新增、分页、详情、删除直接访问，无需令牌', async t => {
  const { request } = await setup(t);
  assert.equal((await request('/api/bets')).status, 200);
  const created = await request('/api/bets', { method: 'POST', body: bet });
  assert.equal(created.status, 201);
  const body = await created.json();
  assert.equal(body.totalAmount, 2);
  assert.equal((await (await request('/api/bets')).json()).total, 1);
  assert.equal((await request('/api/bets/' + body.id)).status, 200);
  assert.equal((await request('/api/bets/' + body.id, { method: 'DELETE' })).status, 204);
  assert.equal((await request('/api/bets/' + body.id)).status, 404);
});

test('任务使用独立令牌，未来期号保持待开奖', async t => {
  const { request } = await setup(t);
  await request('/api/bets', { method: 'POST', body: bet });
  assert.equal((await request('/api/jobs/check', { method: 'POST', body: {} })).status, 401);
  assert.equal((await request('/api/jobs/check', { method: 'POST', token: 'wrong-token', body: {} })).status, 401);
  const result = await request('/api/jobs/check', { method: 'POST', token: jobToken, body: {} });
  assert.equal(result.status, 200);
  assert.equal((await result.json()).waiting, 1);
});

test('上游故障任务返回 502，原始投注不受影响', async t => {
  const { request } = await setup(t, { getDraw: async () => { throw new Error('upstream timeout'); } });
  await request('/api/bets', { method: 'POST', body: bet });
  const response = await request('/api/jobs/check', { method: 'POST', token: jobToken, body: {} });
  assert.equal(response.status, 502);
  assert.equal((await response.json()).errors.length, 1);
  assert.equal((await (await request('/api/bets')).json()).items[0].status, 'pending');
});

test('验证 JSON、分页、方法、请求大小，静态页面不暴露数据库', async t => {
  const { request, base } = await setup(t);
  assert.equal((await request('/api/bets?page=0')).status, 400);
  assert.equal((await request('/api/bets?game=bad')).status, 400);
  assert.equal((await request('/api/bets', { method: 'POST', body: { ...bet, note: 'x'.repeat(600000) } })).status, 413);
  assert.equal((await fetch(base + '/api/bets', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{bad' })).status, 400);
  assert.equal((await request('/api/bets', { method: 'PUT' })).status, 405);
  assert.equal((await request('/api/bets', { method: 'POST', body: { ...bet, issues: [] } })).status, 400);
  assert.equal((await request('/')).status, 200);
  assert.equal((await request('/lottery/app.js')).status, 200);
  assert.equal((await request('/data/lottery.sqlite')).status, 404);
  assert.equal((await request('/server/index.js')).status, 404);
});

test('保存随机号码、投注时间和连续期数，无需请求预测期号或开奖接口', async t => {
  const now = new Date('2026-10-28T10:45:00Z');
  const { request } = await setup(t, undefined, () => now);
  const { issues, ...withoutIssues } = bet;
  const response = await request('/api/bets', { method: 'POST', body: { ...withoutIssues, periods: 3 } });
  assert.equal(response.status, 201);
  const saved = await response.json();
  assert.deepEqual(saved.tickets, bet.tickets);
  assert.deepEqual(saved.issues, []);
  assert.equal(saved.matchMode, 'date');
  assert.equal(saved.placedAt, now.toISOString());
  assert.equal(saved.totalAmount, 6);
  assert.equal(saved.config.periods, 3);
  assert.equal((await request('/api/bets', { method: 'POST', body: { ...withoutIssues, periods: 101 } })).status, 400);
});

test('HTTP 按实际截止时间匹配开奖，未匹配期数保留待开奖', async t => {
  const { request } = await setup(t, { getHistory: async () => [
    { game: 'ssq', issue: '2026124', date: '2026-10-29', front: [1,2,3,4,5,6], back: [1], prizes: { 1: 5000000 }, appendPrizes: {} },
    { game: 'ssq', issue: '2026125', date: '2026-11-01', front: [1,2,3,4,5,6], back: [1], prizes: { 1: 5000000 }, appendPrizes: {} },
  ] }, () => new Date('2026-11-04T00:00:00Z'));
  const { issues, ...withoutIssues } = bet;
  const response = await request('/api/bets', { method: 'POST', body: { ...withoutIssues, periods: 2, placedAt: '2026-10-29T20:10:00+08:00' } });
  assert.equal(response.status, 201);
  const created = await response.json();
  assert.equal((await request('/api/jobs/check', { method: 'POST', token: jobToken, body: {} })).status, 200);
  const result = await (await request('/api/bets/' + created.id)).json();
  assert.deepEqual(result.issues, ['2026125']);
  assert.equal(result.status, 'partial');
  assert.equal(result.winAmount, null);
});
