'use strict';
const { AppError } = require('./errors');

function dateValue(value, status = 400) {
  const parsed = new Date(value + 'T00:00:00Z');
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value) || !Number.isFinite(parsed.getTime()) || parsed.toISOString().slice(0, 10) !== value) throw new AppError('日期格式无效', status);
  return parsed.getTime();
}

function instant(value, status = 400) {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2}(?:\.\d{1,3})?)?(?:Z|[+-]\d{2}:\d{2})$/.test(value)) throw new AppError('投注时间必须使用带时区的 ISO 日期时间', status);
  dateValue(value.slice(0, 10), status);
  const ms = Date.parse(value);
  if (!Number.isFinite(ms)) throw new AppError('日期时间无效', status);
  return ms;
}

function normalizePlacedAt(value, now = new Date()) {
  const ms = value === undefined ? now.getTime() : instant(value);
  if (ms > now.getTime()) throw new AppError('投注时间不能晚于当前时间');
  return new Date(ms).toISOString();
}

function shanghaiDate(value) {
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(value));
}

function salesCloseAt(draw) {
  if (draw.salesCloseAt) return new Date(instant(draw.salesCloseAt, 502)).toISOString();
  dateValue(draw.date, 502);
  // DLT moved from 20:00 to 21:00 starting 23001; SSQ uses 20:00.
  const hour = draw.game === 'dlt' && draw.date >= '2023-01-01' ? '21' : '20';
  return new Date(`${draw.date}T${hour}:00:00+08:00`).toISOString();
}

function selectDraws(game, placedAt, periods, draws) {
  if (!['dlt', 'ssq'].includes(game) || !Number.isInteger(periods) || periods < 1 || periods > 100) throw new AppError('彩种或连续期数无效');
  const placedMs = instant(placedAt);
  const byIssue = new Map();
  for (const draw of draws) {
    if (draw.game !== game) throw new AppError('历史开奖彩种不一致', 502);
    dateValue(draw.date, 502);
    const row = { ...draw, salesCloseAt: salesCloseAt(draw) };
    const previous = byIssue.get(draw.issue);
    if (previous && JSON.stringify(previous) !== JSON.stringify(row)) throw new AppError('同一期历史开奖数据冲突', 502);
    byIssue.set(draw.issue, row);
  }
  return [...byIssue.values()].sort((a, b) => a.date.localeCompare(b.date) || a.issue.localeCompare(b.issue))
    .filter(draw => Date.parse(draw.salesCloseAt) > placedMs).slice(0, periods);
}

module.exports = { normalizePlacedAt, shanghaiDate, salesCloseAt, selectDraws, dateValue };
