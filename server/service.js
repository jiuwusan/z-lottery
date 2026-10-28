'use strict';
const { AppError, normalizeBet, checkTicket, normalizeIssue } = require('./domain');
const { shanghaiDate, selectDraws } = require('./draw-matching');

function integer(value, fallback, min, max, name) {
  const n = value === undefined ? fallback : Number(value);
  if (!Number.isSafeInteger(n) || n < min || n > max) throw new AppError(`${name}必须是 ${min}～${max} 的整数`);
  return n;
}

class BetService {
  constructor(repository, provider, { clock = () => new Date() } = {}) { this.repository = repository; this.provider = provider; this.clock = clock; this.running = false; }
  create(input) { return this.repository.create(normalizeBet(input, { now: this.clock() })); }
  get(id) {
    const bet = this.repository.get(integer(id, 0, 1, Number.MAX_SAFE_INTEGER, '记录 ID'));
    if (!bet) throw new AppError('投注记录不存在', 404);
    return bet;
  }
  remove(id) { this.get(id); this.repository.remove(Number(id)); }
  list(input = {}) {
    const { game, status } = input;
    if (game !== undefined && !['dlt', 'ssq'].includes(game)) throw new AppError('彩种错误');
    if (status !== undefined && !['pending', 'partial', 'prize_pending', 'won', 'lost'].includes(status)) throw new AppError('状态错误');
    return this.repository.list({ page: integer(input.page, 1, 1, 1000000, '页码'),
      pageSize: integer(input.pageSize, 10, 1, 50, '每页数量'), game, status });
  }

  async check(input = {}) {
    if (this.running) throw new AppError('验奖任务正在运行，请稍后重试', 409);
    const limit = integer(input.limit, 20, 1, 100, '批次数量');
    const after = input.after ?? '';
    if (typeof after !== 'string' || (after && !/^(bet:\d{16}|dlt:\d{5}|ssq:20\d{5})$/.test(after))) throw new AppError('无效的验奖游标');
    if (input.game !== undefined && !['dlt', 'ssq'].includes(input.game)) throw new AppError('彩种错误');
    const tasks = this.repository.pending({ limit, after, game: input.game });
    const page = tasks.slice(0, limit);
    const report = { checked: 0, waiting: 0, updatedBets: 0, errors: [], nextCursor: tasks.length > limit ? page.at(-1).key : null };
    const history = new Map();
    const fromDates = new Map();
    for (const task of page.filter(task => task.id)) {
      const date = shanghaiDate(task.placedAt);
      if (!fromDates.has(task.game) || date < fromDates.get(task.game)) fromDates.set(task.game, date);
    }
    const throughDate = shanghaiDate(this.clock());
    this.running = true;
    try {
      // Bounded concurrency keeps slow upstreams from blocking the whole batch.
      let index = 0;
      const worker = async () => {
        while (index < page.length) {
          const task = page[index++];
          try {
            if (task.id) {
              if (!history.has(task.game)) history.set(task.game, Promise.resolve().then(() => this.provider.getHistory(task.game, { fromDate: fromDates.get(task.game), throughDate })));
              const rows = await history.get(task.game);
              const bet = this.repository.get(task.id);
              if (!bet) continue;
              const selected = selectDraws(task.game, bet.placedAt, bet.config.periods, rows);
              const count = this.repository.saveDateCheck(bet.id, selected, selected.map(draw => this.evaluate(bet, draw)));
              report.checked += count;
              if (count) report.updatedBets++;
              if (count < bet.config.periods) report.waiting++;
              continue;
            }
            const draw = await this.provider.getDraw(task.game, task.issue);
            if (!draw) { report.waiting++; continue; }
            if (draw.game !== task.game || normalizeIssue(draw.game, draw.issue) !== task.issue) throw new AppError('开奖期号与请求不一致', 502);
            const evaluations = this.repository.forIssue(task.game, task.issue).map(bet => ({ id: bet.id, result: this.evaluate(bet, draw) }));
            this.repository.saveCheck(draw, evaluations);
            report.checked++;
            report.updatedBets += evaluations.length;
          } catch (error) { report.errors.push({ game: task.game, ...(task.id ? { betId: task.id } : { issue: task.issue }), message: error.message }); }
        }
      };
      await Promise.all([worker(), worker()]);
      return report;
    } finally { this.running = false; }
  }

  evaluate(bet, draw) {
    const tickets = bet.tickets.map(ticket => checkTicket(ticket, draw, bet.config));
    const knownCents = tickets.reduce((sum, ticket) => sum + Math.round(ticket.knownAmount * 100), 0);
    if (!Number.isSafeInteger(knownCents)) throw new AppError('中奖金额超出安全精度');
    const complete = tickets.every(ticket => ticket.complete);
    return { issue: draw.issue, draw, tickets, complete, won: tickets.some(ticket => ticket.winners.length > 0), knownAmount: knownCents / 100, amount: complete ? knownCents / 100 : null };
  }
}

module.exports = { BetService };
