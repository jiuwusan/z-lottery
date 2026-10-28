const test = require('node:test');
const assert = require('node:assert/strict');
const { DatabaseSync } = require('node:sqlite');
const { mkdtempSync, rmSync } = require('node:fs');
const path = require('node:path');
const { tmpdir } = require('node:os');
const { Repository } = require('./repository');
const { normalizeBet } = require('./domain');

test('旧版数据库自动补充匹配模式字段，旧期号和投注不被改写', () => {
  const dir = mkdtempSync(path.join(tmpdir(), 'z-lottery-migration-'));
  let repo;
  try {
    const filename = path.join(dir, 'test.sqlite');
    const old = new DatabaseSync(filename);
    old.exec(`CREATE TABLE bets(id INTEGER PRIMARY KEY AUTOINCREMENT,game TEXT NOT NULL,payload TEXT NOT NULL,created_at TEXT NOT NULL DEFAULT '2026-10-01T00:00:00Z');
      CREATE TABLE bet_issues(bet_id INTEGER NOT NULL REFERENCES bets(id) ON DELETE CASCADE,issue TEXT NOT NULL,PRIMARY KEY(bet_id,issue));`);
    const payload = normalizeBet({ game: 'dlt', tickets: [{ front: [1,2,3,4,5], back: [1,2] }], issues: ['26123'] });
    delete payload.placedAt; delete payload.matchMode;
    old.prepare('INSERT INTO bets(id,game,payload) VALUES (7,?,?)').run('dlt', JSON.stringify(payload));
    old.prepare('INSERT INTO bet_issues(bet_id,issue) VALUES (7,?)').run('26123');
    old.close();
    repo = new Repository(filename);
    const result = repo.get(7);
    assert.equal(result.matchMode, 'issue');
    assert.deepEqual(result.issues, ['26123']);
    assert.deepEqual(result.tickets, payload.tickets);
    assert.equal(result.status, 'pending');
    assert.equal(repo.db.prepare('PRAGMA quick_check').get().quick_check, 'ok');
  } finally { repo?.close(); rmSync(dir, { recursive: true, force: true }); }
});
