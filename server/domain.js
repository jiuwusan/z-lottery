'use strict';
const { GAMES, calculate } = require('../lottery/lottery-core');
const { AppError } = require('./errors');
const { normalizePlacedAt } = require('./draw-matching');

function normalizeIssue(game, issue) {
  if (typeof issue !== 'string') throw new AppError('期号必须是字符串');
  let value = issue.trim();
  if (game === 'dlt' && /^20\d{5}$/.test(value)) value = value.slice(2);
  const pattern = game === 'dlt' ? /^\d{5}$/ : /^20\d{5}$/;
  if (!Object.hasOwn(GAMES, game) || !pattern.test(value) || Number(value.slice(-3)) === 0) {
    throw new AppError('期号格式错误：大乐透如 26120，双色球如 2026120');
  }
  if (game === 'dlt' && value < '19019') throw new AppError('大乐透验奖支持 19019 期及以后');
  return value;
}

function validateNumbers(numbers, minCount, max, label) {
  if (!Array.isArray(numbers) || numbers.length < minCount || numbers.length > max ||
      numbers.some(n => !Number.isInteger(n) || n < 1 || n > max) || new Set(numbers).size !== numbers.length) {
    throw new AppError(`${label}号码必须在 1～${max} 内、不重复，选择 ${minCount}～${max} 个`);
  }
  return [...numbers].sort((a, b) => a - b);
}

function normalizeBet(input, { now = new Date() } = {}) {
  if (!input || typeof input !== 'object' || !Object.hasOwn(GAMES, input.game)) throw new AppError('请选择大乐透或双色球');
  const game = GAMES[input.game];
  if (!Array.isArray(input.tickets) || input.tickets.length < 1 || input.tickets.length > 1000) throw new AppError('每条投注需要 1～1000 组号码');
  const tickets = input.tickets.map(ticket => ({
    front: validateNumbers(ticket?.front, game.frontPick, game.frontMax, game.frontLabel),
    back: validateNumbers(ticket?.back, game.backPick, game.backMax, game.backLabel),
  }));
  if (tickets.some(t => t.front.length !== tickets[0].front.length || t.back.length !== tickets[0].back.length)) throw new AppError('同一条投注的各组选号数量必须相同');
  const matchMode = Object.hasOwn(input, 'issues') ? 'issue' : 'date';
  if (matchMode === 'issue' && (!Array.isArray(input.issues) || input.issues.length < 1 || input.issues.length > 100)) throw new AppError('请明确列出 1～100 个投注期号');
  const issues = matchMode === 'issue' ? input.issues.map(issue => normalizeIssue(input.game, issue)).sort() : [];
  const periods = matchMode === 'issue' ? issues.length : (input.periods ?? 1);
  if (!Number.isInteger(periods) || periods < 1 || periods > 100) throw new AppError('连续期数必须是 1～100 的整数');
  const placedAt = normalizePlacedAt(input.placedAt, now);
  if (new Set(issues).size !== issues.length) throw new AppError('投注期号不能重复');
  if (input.append !== undefined && typeof input.append !== 'boolean') throw new AppError('追加状态必须为布尔值');
  if (input.multiple !== undefined && (!Number.isSafeInteger(input.multiple) || input.multiple < 1)) throw new AppError('投注倍数必须是正整数');
  if (input.note !== undefined && (typeof input.note !== 'string' || input.note.length > 200)) throw new AppError('备注最多 200 个字符');
  let amounts;
  try {
    amounts = calculate({ game: input.game, front: tickets[0].front.length, back: tickets[0].back.length,
      count: tickets.length, multiple: input.multiple ?? 1, periods, append: input.append ?? false });
  } catch (error) { throw new AppError(error.message); }
  if (!Number.isSafeInteger(amounts.totalAmount * 100)) throw new AppError('投注金额过大');
  return { config: amounts.config, tickets, issues, matchMode, placedAt, note: input.note?.trim() || '', totalAmount: amounts.totalAmount, amountPerPeriod: amounts.amountPerPeriod };
}

function choose(n, k) {
  if (k < 0 || k > n) return 0;
  let value = 1;
  for (let i = 1; i <= k; i++) value = value * (n - i + 1) / i;
  return Math.round(value);
}

function prizeLevel(game, issue, front, back) {
  if (game === 'ssq') {
    if (front === 6) return back === 1 ? 1 : 2;
    if (front === 5 && back === 1) return 3;
    if (front === 5 || (front === 4 && back === 1)) return 4;
    if (front === 4 || (front === 3 && back === 1)) return 5;
    return back === 1 ? 6 : null;
  }
  const key = `${front}+${back}`;
  const table = issue >= '26014'
    ? { '5+2': 1, '5+1': 2, '5+0': 3, '4+2': 3, '4+1': 4, '4+0': 5, '3+2': 5, '3+1': 6, '2+2': 6, '3+0': 7, '2+1': 7, '1+2': 7, '0+2': 7 }
    : { '5+2': 1, '5+1': 2, '5+0': 3, '4+2': 4, '4+1': 5, '3+2': 6, '4+0': 7, '3+1': 8, '2+2': 8, '3+0': 9, '2+1': 9, '1+2': 9, '0+2': 9 };
  return table[key] ?? null;
}

function toCents(value) {
  if (value == null) return null;
  if (!Number.isFinite(value) || value < 0 || !Number.isSafeInteger(Math.round(value * 100))) throw new AppError('开奖奖金超出支持范围', 502);
  return Math.round(value * 100);
}

function checkTicket(ticket, draw, { multiple = 1, append = false } = {}) {
  const game = GAMES[draw.game];
  const frontHits = ticket.front.filter(n => draw.front.includes(n)).length;
  const backHits = ticket.back.filter(n => draw.back.includes(n)).length;
  const levels = new Map();
  for (let f = 0; f <= game.frontPick; f++) for (let b = 0; b <= game.backPick; b++) {
    const count = choose(frontHits, f) * choose(ticket.front.length - frontHits, game.frontPick - f) *
      choose(backHits, b) * choose(ticket.back.length - backHits, game.backPick - b) * multiple;
    const level = prizeLevel(draw.game, draw.issue, f, b);
    if (count && level) levels.set(level, (levels.get(level) || 0) + count);
  }
  let knownCents = 0;
  let complete = true;
  const winners = [...levels].sort((a, b) => a[0] - b[0]).map(([level, count]) => {
    const baseCents = toCents(draw.prizes[level]);
    const appendCents = append && draw.game === 'dlt' && level <= 2 ? toCents(draw.appendPrizes[level]) : 0;
    const amount = baseCents === null || appendCents === null ? null : (baseCents + appendCents) * count;
    if (amount === null) complete = false;
    knownCents += ((baseCents ?? 0) + (appendCents ?? 0)) * count;
    if (!Number.isSafeInteger(knownCents)) throw new AppError('中奖金额超出安全精度');
    return { level, count, unitAmount: baseCents === null ? null : baseCents / 100,
      appendUnitAmount: appendCents === null ? null : appendCents / 100, amount: amount === null ? null : amount / 100 };
  });
  return { frontHits, backHits, winners, complete, knownAmount: knownCents / 100, amount: complete ? knownCents / 100 : null };
}

module.exports = { AppError, normalizeIssue, normalizeBet, validateNumbers, checkTicket };
