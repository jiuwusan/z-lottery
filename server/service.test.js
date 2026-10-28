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

const datedDraw = (issue, date) => ({ ...draw(issue), date });
const dateInput = { game: 'dlt', tickets: input.tickets, placedAt: '2026-10-28T18:45:00+08:00', periods: 3 };

test('按投注时间匹配真实连续3期开奖，未开奖期保留待匹配，重复任务不累计', async t => {
  const repo = new Repository(':memory:'); t.after(() => repo.close());
  let rows = [datedDraw('26122', '2026-10-26'), datedDraw('26123', '2026-10-28')];
  const service = new BetService(repo, { getHistory: async () => rows }, { clock: () => new Date('2026-11-10T00:00:00Z') });
  const bet = service.create(dateInput);
  assert.equal(bet.status, 'pending');
  assert.equal(bet.config.periods, 3);
  assert.deepEqual(bet.issues, []);
  assert.equal(bet.winAmount, null);
  await service.check();
  assert.deepEqual(service.get(bet.id).issues, ['26123']);
  assert.equal(service.get(bet.id).status, 'partial');
  rows = [datedDraw('26126', '2026-11-04'), datedDraw('26125', '2026-11-02'), datedDraw('26124', '2026-10-31'), ...rows];
  await service.check();
  assert.deepEqual(service.get(bet.id).issues, ['26123', '26124', '26125']);
  assert.equal(service.get(bet.id).winAmount, 3000000);
  assert.equal(service.get(bet.id).status, 'won');
  await service.check();
  assert.equal(service.get(bet.id).winAmount, 3000000);
});

test('截止后到开奖前投注跳过当天，跨年按实际列表匹配', async t => {
  const repo = new Repository(':memory:'); t.after(() => repo.close());
  const service = new BetService(repo, { getHistory: async () => [
    datedDraw('26151', '2026-12-30'), datedDraw('27001', '2027-01-02'), datedDraw('27002', '2027-01-04'),
  ] }, { clock: () => new Date('2027-01-10T00:00:00Z') });
  const bet = service.create({ ...dateInput, placedAt: '2026-12-30T21:10:00+08:00', periods: 2 });
  await service.check();
  assert.deepEqual(service.get(bet.id).issues, ['27001', '27002']);
  assert.equal(service.get(bet.id).status, 'won');
});

test('同彩种日期记录共享历史查询，待匹配记录参与分页，失败不污染数据', async t => {
  const repo = new Repository(':memory:'); t.after(() => repo.close());
  let requests = 0;
  let failed = true;
  const service = new BetService(repo, { getHistory: async () => { requests++; if (failed) throw new Error('history unavailable'); return []; } },
    { clock: () => new Date('2026-11-10T00:00:00Z') });
  service.create(dateInput); service.create(dateInput); service.create(dateInput);
  const first = await service.check({ limit: 2 });
  assert.ok(first.nextCursor);
  assert.equal(requests, 1);
  assert.equal(first.errors.length, 2);
  assert.equal(service.list({ status: 'pending' }).total, 3);
  failed = false;
  const second = await service.check({ after: first.nextCursor, limit: 2 });
  assert.equal(second.nextCursor, null);
  assert.equal(second.waiting, 1);
});

test('日期记录重启后可继续验奖，等待历史请求时删除不会复活', async t => {
  const dir = mkdtempSync(path.join(tmpdir(), 'z-lottery-date-db-'));
  let repo;
  try {
    repo = new Repository(path.join(dir, 'test.sqlite'));
    let service = new BetService(repo, { getHistory: async () => [] }, { clock: () => new Date('2026-11-10T00:00:00Z') });
    const bet = service.create(dateInput);
    repo.close(); repo = new Repository(path.join(dir, 'test.sqlite'));
    let release;
    service = new BetService(repo, { getHistory: () => new Promise(resolve => { release = resolve; }) }, { clock: () => new Date('2026-11-10T00:00:00Z') });
    assert.equal(service.get(bet.id).placedAt, '2026-10-28T10:45:00.000Z');
    const pending = service.check();
    await Promise.resolve();
    service.remove(bet.id);
    release([datedDraw('26123', '2026-10-28')]);
    await pending;
    assert.equal(service.list().total, 0);
  } finally { repo?.close(); rmSync(dir, { recursive: true, force: true }); }
});

test('日期模式奖金未公布时保留首期，不用后面开奖替换，补全后幂等完成', async t => {
  const repo = new Repository(':memory:'); t.after(() => repo.close());
  let prizes = {};
  const service = new BetService(repo, { getHistory: async () => [
    { ...datedDraw('26123', '2026-10-28'), prizes }, datedDraw('26124', '2026-10-31'),
  ] }, { clock: () => new Date('2026-11-10T00:00:00Z') });
  const bet = service.create({ ...dateInput, periods: 1 });
  await service.check();
  assert.deepEqual(service.get(bet.id).issues, ['26123']);
  assert.equal(service.get(bet.id).status, 'prize_pending');
  assert.equal(service.get(bet.id).winAmount, null);
  prizes = { 1: 1000000 };
  await service.check();
  assert.equal(service.get(bet.id).winAmount, 1000000);
});

test('日期模式结果写入失败时，实际期号绑定和中奖结果整体回滚', async t => {
  const repo = new Repository(':memory:'); t.after(() => repo.close());
  const rows = [['26123','2026-10-28'],['26124','2026-10-31'],['26125','2026-11-02'],['26126','2026-11-04']].map(([issue,date]) => datedDraw(issue,date));
  const service = new BetService(repo, { getHistory: async () => rows }, { clock: () => new Date('2026-11-10T00:00:00Z') });
  const bet = service.create({ ...dateInput, periods: 4, multiple: 30000000 });
  const report = await service.check();
  assert.equal(report.errors.length, 1);
  assert.equal(service.get(bet.id).status, 'pending');
  assert.deepEqual(service.get(bet.id).issues, []);
  assert.equal(repo.db.prepare('SELECT COUNT(*) AS count FROM results').get().count, 0);
});

test('日期记录与兼容期号记录混合分页，不被尚未开奖的日期记录阻塞', async t => {
  const repo = new Repository(':memory:'); t.after(() => repo.close());
  const service = new BetService(repo, { getHistory: async () => [], getDraw: async (game, issue) => draw(issue) }, { clock: () => new Date('2026-11-10T00:00:00Z') });
  service.create(dateInput);
  const legacy = service.create(input);
  let after;
  let batches = 0;
  do {
    const result = await service.check({ limit: 1, ...(after ? { after } : {}) });
    after = result.nextCursor;
    batches++;
    assert.ok(batches <= 3);
  } while (after);
  assert.equal(batches, 3);
  assert.equal(service.get(legacy.id).status, 'won');
});
