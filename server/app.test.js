const test = require('node:test');
const assert = require('node:assert/strict');
const { Repository } = require('./repository');
const { BetService } = require('./service');
const { createServer } = require('./app');
const apiToken = 'test-api-token-at-least-16';
const jobToken = 'test-job-token-at-least-16';

async function setup(t, provider = { getDraw: async () => null }) {
  const repo = new Repository(':memory:');
  const server = createServer({ service: new BetService(repo, provider), apiToken, jobToken });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(async () => { await new Promise(resolve => server.close(resolve)); repo.close(); });
  const base = `http://127.0.0.1:${server.address().port}`;
  const request = (route, { token = apiToken, method = 'GET', body } = {}) => fetch(base + route, {
    method, headers: { ...(token ? { Authorization: 'Bearer ' + token } : {}), ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  return { request, base };
}
const bet = { game: 'ssq', tickets: [{ front: [1, 2, 3, 4, 5, 6], back: [1] }], issues: ['2026120'] };

test('HTTP 新增、分页、详情、删除闭环，所有投注接口要求管理令牌', async t => {
  const { request } = await setup(t);
  assert.equal((await request('/api/bets', { token: null })).status, 401);
  assert.equal((await request('/api/bets', { token: jobToken })).status, 401);
  const created = await request('/api/bets', { method: 'POST', body: bet });
  assert.equal(created.status, 201);
  const body = await created.json();
  assert.equal(body.totalAmount, 2);
  assert.equal((await (await request('/api/bets')).json()).total, 1);
  assert.equal((await request('/api/bets/' + body.id)).status, 200);
  assert.equal((await request('/api/bets/' + body.id, { method: 'DELETE', token: null })).status, 401);
  assert.equal((await request('/api/bets/' + body.id, { method: 'DELETE' })).status, 204);
  assert.equal((await request('/api/bets/' + body.id)).status, 404);
});

test('任务使用独立令牌，未来期号保持待开奖', async t => {
  const { request } = await setup(t);
  await request('/api/bets', { method: 'POST', body: bet });
  assert.equal((await request('/api/jobs/check', { method: 'POST', body: {} })).status, 401);
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
  assert.equal((await fetch(base + '/api/bets', { method: 'POST', headers: { Authorization: 'Bearer ' + apiToken, 'Content-Type': 'application/json' }, body: '{bad' })).status, 400);
  assert.equal((await request('/api/bets', { method: 'PUT' })).status, 405);
  assert.equal((await request('/api/bets', { method: 'POST', body: { ...bet, issues: [] } })).status, 400);
  assert.equal((await request('/')).status, 200);
  assert.equal((await request('/lottery/app.js')).status, 200);
  assert.equal((await request('/data/lottery.sqlite')).status, 404);
  assert.equal((await request('/server/index.js')).status, 404);
});
