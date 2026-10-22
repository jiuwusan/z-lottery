const test = require('node:test');
const assert = require('node:assert/strict');
const { mkdtempSync, rmSync } = require('node:fs');
const { tmpdir } = require('node:os');
const path = require('node:path');
const { Repository } = require('./repository');
const { BetService } = require('./service');

const input = { game: 'dlt', tickets: [{ front: [1, 2, 3, 4, 5], back: [1, 2] }], issues: ['26014', '26015'], multiple: 2, append: true };
const draw = issue => ({ game: 'dlt', issue, date: '2026-02-02', front: [1, 2, 3, 4, 5], back: [1, 2], prizes: { 1: 1000000 }, appendPrizes: { 1: 800000 } });

test('数据库重启保留投注，按页列出，删除连带清理验奖记录', async () => {
  const dir = mkdtempSync(path.join(tmpdir(), 'z-lottery-db-test-'));
  let repo;
  try {
    repo = new Repository(path.join(dir, 'test.sqlite'));
    let service = new BetService(repo, { getDraw: async (game, issue) => draw(issue) });
    const bet = service.create(input);
    await service.check({ limit: 1 });
    assert.equal(service.get(bet.id).status, 'partial');
    repo.close();
    repo = new Repository(path.join(dir, 'test.sqlite'));
    service = new BetService(repo, { getDraw: async (game, issue) => draw(issue) });
    assert.equal(service.get(bet.id).results.length, 1);
    service.create({ ...input, issues: ['26016'] });
    assert.equal(service.list({ page: 1, pageSize: 1 }).items.length, 1);
    assert.equal(service.list({ page: 2, pageSize: 1 }).total, 2);
    service.remove(bet.id);
    assert.throws(() => service.get(bet.id), { status: 404 });
    assert.equal(repo.db.prepare('SELECT COUNT(*) AS count FROM results').get().count, 0);
  } finally { repo?.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('重复验奖幂等，多个记录共享同一期只抓取一次，跨页继续处理', async t => {
  const repo = new Repository(':memory:'); t.after(() => repo.close());
  let requests = 0;
  const service = new BetService(repo, { getDraw: async (game, issue) => { requests++; return draw(issue); } });
  const bet = service.create(input);
  service.create({ ...input, issues: ['26014'] });
  const first = await service.check({ limit: 1 });
  assert.ok(first.nextCursor);
  const second = await service.check({ limit: 1, after: first.nextCursor });
  assert.equal(second.nextCursor, null);
  assert.equal(requests, 2);
  assert.equal(service.get(bet.id).winAmount, 7200000);
  assert.equal(service.get(bet.id).status, 'won');
  await service.check();
  assert.equal(requests, 2);
  assert.equal(service.get(bet.id).winAmount, 7200000);
  assert.equal(service.list({ status: 'won' }).total, 2);
});

test('未来开奖不置为未中奖，上游失败不写入结果，缺失奖金可后续补全', async t => {
  const repo = new Repository(':memory:'); t.after(() => repo.close());
  let mode = 'future';
  const service = new BetService(repo, { getDraw: async (game, issue) => {
    if (mode === 'future') return null;
    if (mode === 'error') throw new Error('timeout');
    return { ...draw(issue), prizes: mode === 'missing' ? {} : { 1: 1000000 } };
  } });
  const bet = service.create({ ...input, issues: ['26014'] });
  assert.equal((await service.check()).waiting, 1);
  assert.equal(service.get(bet.id).status, 'pending');
  mode = 'error';
  assert.equal((await service.check()).errors.length, 1);
  assert.equal(service.get(bet.id).results.length, 0);
  mode = 'missing'; await service.check();
  assert.equal(service.get(bet.id).status, 'prize_pending');
  assert.equal(service.get(bet.id).winAmount, null);
  mode = 'complete'; await service.check();
  assert.equal(service.get(bet.id).winAmount, 3600000);
});

test('拒绝并发任务；等待上游时删除投注不被复活', async t => {
  const repo = new Repository(':memory:'); t.after(() => repo.close());
  let release;
  const service = new BetService(repo, { getDraw: () => new Promise(resolve => { release = resolve; }) });
  const bet = service.create({ ...input, issues: ['26014'] });
  const pending = service.check();
  await assert.rejects(service.check(), { status: 409 });
  service.remove(bet.id);
  release(draw('26014'));
  await pending;
  assert.equal(service.list().total, 0);
});

test('跨期奖金合计超出安全精度时拒绝该次结果写入，保留已确认记录', async t => {
  const repo = new Repository(':memory:'); t.after(() => repo.close());
  const service = new BetService(repo, { getDraw: async (game, issue) => draw(issue) });
  const bet = service.create({ ...input, append: false, multiple: 30000000, issues: ['26014', '26015', '26016', '26017'] });
  const report = await service.check();
  assert.equal(report.errors.length, 1);
  assert.match(report.errors[0].message, /精度/);
  assert.equal(service.get(bet.id).results.length, 3);
  assert.equal(service.get(bet.id).knownWinAmount, 90000000000000);
  assert.equal(service.get(bet.id).winAmount, null);
});
