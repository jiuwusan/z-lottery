'use strict';
const { AppError, normalizeIssue, validateNumbers } = require('./domain');

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
  if (!/^\d{4}-\d{2}-\d{2}$/.test(draw.date)) throw new AppError('官方开奖日期缺失', 502);
  return draw;
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
    let draw;
    if (game === 'dlt') {
      const url = new URL('https://webapi.sporttery.cn/gateway/lottery/getHistoryPageListV1.qry');
      url.search = new URLSearchParams({ gameNo: '85', provinceId: '0', pageSize: '30', isVerify: '1', pageNo: '1', startTerm: issue, endTerm: issue });
      const data = await this.request(url, 'https://static.sporttery.cn/');
      if (data.success !== true || String(data.errorCode) !== '0' || !Array.isArray(data.value?.list)) throw new AppError('体彩接口响应结构异常', 502);
      if (!data.value.list.length) return null;
      const row = data.value.list.find(row => String(row.lotteryDrawNum) === issue);
      if (!row) throw new AppError('体彩接口未返回指定期号', 502);
      if (Number(row.verify) !== 1 || Number(row.lotteryDrawStatus) !== 20) return null;
      if (!Array.isArray(row.prizeLevelList)) throw new AppError('体彩奖级数据缺失', 502);
      const selected = numbers(row.lotteryDrawResult);
      draw = { game, issue, date: String(row.lotteryDrawTime || '').slice(0, 10), front: selected.slice(0, 5), back: selected.slice(5), prizes: {}, appendPrizes: {} };
      for (const prize of row.prizeLevelList) {
        const match = /^([一二三四五六七八九])等奖(?:\(追加\)|（追加）)?$/.exec(prize.prizeLevel);
        if (!match) continue; // Promotional awards have separate eligibility and are not ordinary prize amounts.
        const level = '一二三四五六七八九'.indexOf(match[1]) + 1;
        const target = prize.prizeLevel.includes('追加') ? draw.appendPrizes : draw.prizes;
        target[level] = money(prize.stakeAmountFormat ?? prize.stakeAmount);
      }
    } else {
      const url = new URL('https://www.cwl.gov.cn/cwl_admin/front/cwlkj/search/kjxx/findDrawNotice');
      url.search = new URLSearchParams({ name: 'ssq', issueStart: issue, issueEnd: issue, pageNo: '1', pageSize: '30', systemType: 'PC' });
      const data = await this.request(url, 'https://www.cwl.gov.cn/ygkj/wqkjgg/ssq/');
      if (data.state !== 0 || !Array.isArray(data.result)) throw new AppError('福彩接口响应结构异常', 502);
      if (!data.result.length) return null;
      const row = data.result.find(row => String(row.code) === issue);
      if (!row) throw new AppError('福彩接口未返回指定期号', 502);
      if (!Array.isArray(row.prizegrades)) throw new AppError('福彩奖级数据缺失', 502);
      draw = { game, issue, date: String(row.date || '').slice(0, 10), front: numbers(row.red), back: numbers(row.blue), prizes: {}, appendPrizes: {} };
      for (const prize of row.prizegrades) {
        const level = Number(prize.type);
        if (Number.isInteger(level) && level >= 1 && level <= 6) draw.prizes[level] = money(prize.typemoney);
      }
    }
    return validateDraw(draw);
  }
}

module.exports = { DrawProvider };
