'use strict';
const { DatabaseSync } = require('node:sqlite');
const { mkdirSync } = require('node:fs');
const { dirname, resolve } = require('node:path');
const { AppError } = require('./domain');

const states = `WITH states AS (
  SELECT b.*, CASE
    WHEN COUNT(r.issue) = 0 THEN 'pending'
    WHEN COUNT(r.issue) < COUNT(i.issue) THEN 'partial'
    WHEN SUM(COALESCE(r.complete, 0)) < COUNT(i.issue) THEN 'prize_pending'
    WHEN MAX(COALESCE(r.won, 0)) = 1 THEN 'won'
    ELSE 'lost' END AS status
  FROM bets b JOIN bet_issues i ON i.bet_id = b.id
  LEFT JOIN results r ON r.bet_id = i.bet_id AND r.issue = i.issue GROUP BY b.id
)`;

class Repository {
  constructor(filename) {
    if (filename !== ':memory:') mkdirSync(dirname(resolve(filename)), { recursive: true });
    this.db = new DatabaseSync(filename);
    this.db.exec(`PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000;
      CREATE TABLE IF NOT EXISTS bets (
        id INTEGER PRIMARY KEY AUTOINCREMENT, game TEXT NOT NULL, payload TEXT NOT NULL,
        created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
      );
      CREATE TABLE IF NOT EXISTS bet_issues (
        bet_id INTEGER NOT NULL REFERENCES bets(id) ON DELETE CASCADE,
        issue TEXT NOT NULL, PRIMARY KEY (bet_id, issue)
      );
      CREATE INDEX IF NOT EXISTS idx_bets_game ON bets(game);
      CREATE TABLE IF NOT EXISTS draws (
        game TEXT NOT NULL, issue TEXT NOT NULL, payload TEXT NOT NULL,
        updated_at TEXT NOT NULL, PRIMARY KEY(game, issue)
      );
      CREATE TABLE IF NOT EXISTS results (
        bet_id INTEGER NOT NULL, issue TEXT NOT NULL, payload TEXT NOT NULL,
        complete INTEGER NOT NULL, won INTEGER NOT NULL, checked_at TEXT NOT NULL,
        PRIMARY KEY(bet_id, issue),
        FOREIGN KEY(bet_id, issue) REFERENCES bet_issues(bet_id, issue) ON DELETE CASCADE
      );`);
  }

  transaction(action) {
    this.db.exec('BEGIN IMMEDIATE');
    try { const result = action(); this.db.exec('COMMIT'); return result; }
    catch (error) { this.db.exec('ROLLBACK'); throw error; }
  }

  create(payload) {
    return this.transaction(() => {
      const id = Number(this.db.prepare('INSERT INTO bets(game,payload) VALUES (?,?)').run(payload.config.game, JSON.stringify(payload)).lastInsertRowid);
      const insert = this.db.prepare('INSERT INTO bet_issues(bet_id,issue) VALUES (?,?)');
      for (const issue of payload.issues) insert.run(id, issue);
      return this.get(id);
    });
  }

  hydrate(row) {
    if (!row) return null;
    const payload = JSON.parse(row.payload);
    const results = this.db.prepare('SELECT payload,checked_at FROM results WHERE bet_id=? ORDER BY issue').all(row.id)
      .map(r => ({ ...JSON.parse(r.payload), checkedAt: r.checked_at }));
    const knownCents = results.reduce((sum, r) => sum + Math.round(r.knownAmount * 100), 0);
    const finished = results.length === payload.issues.length && results.every(r => r.complete);
    return { id: row.id, ...payload, createdAt: row.created_at, status: row.status, results,
      checkedPeriods: results.length, knownWinAmount: knownCents / 100, winAmount: finished ? knownCents / 100 : null };
  }

  get(id) { return this.hydrate(this.db.prepare(`${states} SELECT * FROM states WHERE id=?`).get(id)); }

  list({ page, pageSize, game, status }) {
    const filters = [];
    const params = [];
    if (game) { filters.push('game=?'); params.push(game); }
    if (status) { filters.push('status=?'); params.push(status); }
    const where = filters.length ? ' WHERE ' + filters.join(' AND ') : '';
    const total = this.db.prepare(`${states} SELECT COUNT(*) AS total FROM states${where}`).get(...params).total;
    const rows = this.db.prepare(`${states} SELECT * FROM states${where} ORDER BY id DESC LIMIT ? OFFSET ?`).all(...params, pageSize, (page - 1) * pageSize);
    return { items: rows.map(row => this.hydrate(row)), total, page, pageSize };
  }

  remove(id) { return this.db.prepare('DELETE FROM bets WHERE id=?').run(id).changes > 0; }

  pending({ limit, after, game }) {
    return this.db.prepare(`SELECT DISTINCT b.game, i.issue, b.game || ':' || i.issue AS key
      FROM bet_issues i JOIN bets b ON b.id=i.bet_id
      LEFT JOIN results r ON r.bet_id=i.bet_id AND r.issue=i.issue
      WHERE (r.issue IS NULL OR r.complete=0) AND b.game || ':' || i.issue > ?
      ${game ? 'AND b.game=?' : ''} ORDER BY key LIMIT ?`).all(...(game ? [after, game, limit + 1] : [after, limit + 1]));
  }

  forIssue(game, issue) {
    return this.db.prepare(`SELECT b.id,b.payload FROM bets b JOIN bet_issues i ON b.id=i.bet_id WHERE b.game=? AND i.issue=?`)
      .all(game, issue).map(row => ({ id: row.id, ...JSON.parse(row.payload) }));
  }

  saveCheck(draw, evaluations) {
    this.transaction(() => {
      const now = new Date().toISOString();
      this.db.prepare(`INSERT INTO draws(game,issue,payload,updated_at) VALUES (?,?,?,?)
        ON CONFLICT(game,issue) DO UPDATE SET payload=excluded.payload,updated_at=excluded.updated_at`)
        .run(draw.game, draw.issue, JSON.stringify(draw), now);
      const statement = this.db.prepare(`INSERT INTO results(bet_id,issue,payload,complete,won,checked_at) VALUES (?,?,?,?,?,?)
        ON CONFLICT(bet_id,issue) DO UPDATE SET payload=excluded.payload,complete=excluded.complete,won=excluded.won,checked_at=excluded.checked_at`);
      for (const { id, result } of evaluations) {
        const otherResults = this.db.prepare('SELECT payload FROM results WHERE bet_id=? AND issue<>?').all(id, draw.issue);
        const totalCents = otherResults.reduce((sum, row) => sum + Math.round(JSON.parse(row.payload).knownAmount * 100), Math.round(result.knownAmount * 100));
        if (!Number.isSafeInteger(totalCents)) throw new AppError('跨期中奖合计超出安全精度，请拆分投注记录');
        statement.run(id, draw.issue, JSON.stringify(result), Number(result.complete), Number(result.won), now);
      }
    });
  }

  close() { this.db.close(); }
}

module.exports = { Repository };
