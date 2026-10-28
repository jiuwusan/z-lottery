'use strict';
const { AppError, normalizeIssue, validateNumbers } = require('./domain');
const { dateValue, salesCloseAt } = require('./draw-matching');

function money(raw) {
  if (raw === undefined || raw === null || raw === '' || raw === '---' || raw === '-1') return null;
  const cleaned = String(raw).replaceAll(',', '');
  if (!/^\d+(\.\d{1,2})?$/.test(cleaned)) throw new AppError('官方奖金格式无法识别', 502);
  const value = Number(cleaned);
  if (!Number.isSafeInteger(Math.round(value * 100))) throw new AppError('官方奖金超过精度范围', 502);
  return value;
}

function numbers(raw) {
  if (typeof raw !== 'string' || !raw.trim()) throw new AppError('官方开奖号码缺失', 502);
  return raw.trim().split(/[,\s]+/).map(Number);
}

function validateDraw(draw) {
  const isDlt = draw.game === 'dlt';
  const frontCount = isDlt ? 5 : 6;
  const backCount = isDlt ? 2 : 1;
  try {
    draw.front = validateNumbers(draw.front, frontCount, isDlt ? 35 : 33, '开奖前区');
    draw.back = validateNumbers(draw.back, backCount, isDlt ? 12 : 16, '开奖后区');
  } catch (error) { throw new AppError(error.message, 502); }
  if (draw.front.length !== frontCount || draw.back.length !== backCount) throw new AppError('官方开奖号码数量不正确', 502);
  dateValue(draw.date, 502);
  draw.salesCloseAt = salesCloseAt(draw);
  return draw;
}

function rowToDraw(game, row) {
  let draw;
  if (game === 'dlt') {
    if (Number(row.verify) !== 1 || Number(row.lotteryDrawStatus) !== 20) return null;
    if (!Array.isArray(row.prizeLevelList)) throw new AppError('体彩奖级数据缺失', 502);
    const selected = numbers(row.lotteryDrawResult);
    draw = { game, issue: normalizeIssue(game, String(row.lotteryDrawNum)), date: String(row.lotteryDrawTime || '').slice(0, 10), front: selected.slice(0, 5), back: selected.slice(5), prizes: {}, appendPrizes: {} };
    const deadline = row.lotterySaleEndtime || row.lotterySaleEndTime;
    if (deadline) draw.salesCloseAt = String(deadline).replace(' ', 'T') + (/Z$|[+-]\d{2}:\d{2}$/.test(String(deadline)) ? '' : '+08:00');
    for (const prize of row.prizeLevelList) {
      const match = /^([一二三四五六七八九])等奖(?:\(追加\)|（追加）)?$/.exec(prize.prizeLevel);
      if (!match) continue;
      const level = '一二三四五六七八九'.indexOf(match[1]) + 1;
      const target = prize.prizeLevel.includes('追加') ? draw.appendPrizes : draw.prizes;
      target[level] = money(prize.stakeAmountFormat ?? prize.stakeAmount);
    }
  } else {
    if (!row.red?.trim() || !row.blue?.trim()) return null;
    if (!Array.isArray(row.prizegrades)) throw new AppError('福彩奖级数据缺失', 502);
    draw = { game, issue: normalizeIssue(game, String(row.code)), date: String(row.date || '').slice(0, 10), front: numbers(row.red), back: numbers(row.blue), prizes: {}, appendPrizes: {} };
    for (const prize of row.prizegrades) {
      const level = Number(prize.type);
      if (Number.isInteger(level) && level >= 1 && level <= 6) draw.prizes[level] = money(prize.typemoney);
    }
  }
  return validateDraw(draw);
}

class DrawProvider {
  constructor({ fetchImpl = fetch, timeout = 8000 } = {}) { this.fetchImpl = fetchImpl; this.timeout = timeout; }

  async request(url, referer) {
    try {
      const response = await this.fetchImpl(url.toString(), { signal: AbortSignal.timeout(this.timeout), headers: {
        Accept: 'application/json', Referer: referer, 'User-Agent': 'Mozilla/5.0', 'X-Requested-With': 'XMLHttpRequest',
      } });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const text = await response.text();
      if (text.length > 2 * 1024 * 1024) throw new Error('响应超过大小限制');
      return JSON.parse(text);
    } catch (error) { throw new AppError(`官方开奖接口请求失败：${error.message}`, 502); }
  }

  async getDraw(game, requestedIssue) {
    const issue = normalizeIssue(game, requestedIssue);
    if (game === 'dlt') {
      const url = new URL('https://webapi.sporttery.cn/gateway/lottery/getHistoryPageListV1.qry');
      url.search = new URLSearchParams({ gameNo: '85', provinceId: '0', pageSize: '30', isVerify: '1', pageNo: '1', startTerm: issue, endTerm: issue });
      const data = await this.request(url, 'https://static.sporttery.cn/');
      if (data.success !== true || String(data.errorCode) !== '0' || !Array.isArray(data.value?.list)) throw new AppError('体彩接口响应结构异常', 502);
      if (!data.value.list.length) return null;
      const row = data.value.list.find(row => String(row.lotteryDrawNum) === issue);
      if (!row) throw new AppError('体彩接口未返回指定期号', 502);
      return rowToDraw(game, row);
    } else {
      const url = new URL('https://www.cwl.gov.cn/cwl_admin/front/cwlkj/search/kjxx/findDrawNotice');
      url.search = new URLSearchParams({ name: 'ssq', issueStart: issue, issueEnd: issue, pageNo: '1', pageSize: '30', systemType: 'PC' });
      const data = await this.request(url, 'https://www.cwl.gov.cn/ygkj/wqkjgg/ssq/');
      if (data.state !== 0 || !Array.isArray(data.result)) throw new AppError('福彩接口响应结构异常', 502);
      if (!data.result.length) return null;
      const row = data.result.find(row => String(row.code) === issue);
      if (!row) throw new AppError('福彩接口未返回指定期号', 502);
      return rowToDraw(game, row);
    }
  }

  async historyPage(game, page) {
    if (!['dlt', 'ssq'].includes(game)) throw new AppError('请选择大乐透或双色球');
    if (game === 'dlt') {
      const url = new URL('https://webapi.sporttery.cn/gateway/lottery/getHistoryPageListV1.qry');
      url.search = new URLSearchParams({ gameNo: '85', provinceId: '0', pageSize: '100', pageNo: String(page) });
      const data = await this.request(url, 'https://static.sporttery.cn/');
      if (data.success !== true || String(data.errorCode) !== '0' || !Array.isArray(data.value?.list)) throw new AppError('体彩接口响应结构异常', 502);
      return { rows: data.value.list, pages: Number(data.value.pages), total: Number(data.value.total) };
    } else {
      const url = new URL('https://www.cwl.gov.cn/cwl_admin/front/cwlkj/search/kjxx/findDrawNotice');
      url.search = new URLSearchParams({ name: 'ssq', pageNo: String(page), pageSize: '100', systemType: 'PC' });
      const data = await this.request(url, 'https://www.cwl.gov.cn/ygkj/wqkjgg/ssq/');
      if (data.state !== 0 || !Array.isArray(data.result)) throw new AppError('福彩接口响应结构异常', 502);
      return { rows: data.result, pages: Number(data.pageNum), total: Number(data.total) };
    }
  }

  async getHistory(game, { fromDate, throughDate }) {
    dateValue(fromDate); dateValue(throughDate);
    if (fromDate > throughDate) throw new AppError('历史开奖日期范围无效');
    const found = new Map();
    let preceding;
    let covered = false;
    const unready = [];
    for (let page = 1; page <= 50; page++) {
      const { rows, pages, total } = await this.historyPage(game, page);
      if (!Number.isInteger(pages) || !Number.isInteger(total) || pages < 0 || total < 0) throw new AppError('历史开奖分页信息缺失', 502);
      if (!rows.length) {
        if (total !== 0 || page !== 1) throw new AppError('历史开奖分页不完整，请稍后重试', 502);
        return [];
      }
      let oldest;
      for (const row of rows) {
        const date = String(game === 'dlt' ? row.lotteryDrawTime || '' : row.date || '').slice(0, 10);
        dateValue(date, 502);
        if (!oldest || date < oldest) oldest = date;
        if (date < fromDate) {
          if (!preceding || date > preceding.date) {
            let issue = String(game === 'dlt' ? row.lotteryDrawNum : row.code);
            if (game === 'dlt' && /^20\d{5}$/.test(issue)) issue = issue.slice(2);
            if (!(game === 'dlt' ? /^\d{5}$/ : /^20\d{5}$/).test(issue)) throw new AppError('历史开奖期号格式无效', 502);
            preceding = { issue, date };
          }
          continue;
        }
        if (date > throughDate) continue;
        const draw = rowToDraw(game, row);
        if (!draw) { unready.push(date); continue; }
        const previous = found.get(draw.issue);
        if (previous && JSON.stringify(previous) !== JSON.stringify(draw)) throw new AppError('历史开奖数据冲突，请稍后重试', 502);
        found.set(draw.issue, draw);
      }
      if (oldest <= fromDate) { covered = true; break; }
      if (page >= pages) break;
    }
    if (!covered) throw new AppError('历史开奖未完整覆盖投注日期，暂缓验奖', 502);
    const draws = [...found.values()].sort((a, b) => a.date.localeCompare(b.date) || a.issue.localeCompare(b.issue));
    if (unready.some(date => draws.some(draw => draw.date > date))) throw new AppError('历史开奖存在未审核记录，暂缓匹配', 502);
    const sequence = preceding ? [preceding, ...draws] : draws;
    for (let i = 1; i < sequence.length; i++) {
      const a = sequence[i - 1], b = sequence[i];
      if (a.date === b.date || (a.issue.slice(0, -3) === b.issue.slice(0, -3) && Number(b.issue.slice(-3)) !== Number(a.issue.slice(-3)) + 1)
          || (a.issue.slice(0, -3) !== b.issue.slice(0, -3) && Number(b.issue.slice(-3)) !== 1)) throw new AppError('历史开奖存在缺口，暂缓匹配', 502);
    }
    return draws;
  }
}

module.exports = { DrawProvider };
