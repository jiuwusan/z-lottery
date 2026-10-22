(function () {
  'use strict';
  const form = document.querySelector('#bet-form');
  const list = document.querySelector('#bet-list');
  const status = document.querySelector('#records-status');
  const token = document.querySelector('#api-token');
  const saveButton = document.querySelector('#save-bet');
  const refreshButton = document.querySelector('#connect-button');
  const states = { pending: '待开奖', partial: '部分已开奖', prize_pending: '奖金待补全', won: '已中奖', lost: '未中奖' };
  const money = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 });
  let generated;
  let page = 1;
  let loading = false;
  let saving = false;
  let deleting = false;
  let connected = false;
  let requestGeneration = 0;

  function message(text, isError = false) { status.textContent = text; status.classList.toggle('is-error', isError); }
  function element(tag, text, className) {
    const node = document.createElement(tag);
    if (text !== undefined) node.textContent = text;
    if (className) node.className = className;
    return node;
  }
  const numbers = values => values.map(n => String(n).padStart(2, '0')).join(' ');
  const lines = tickets => tickets.map(ticket => `${numbers(ticket.front)} + ${numbers(ticket.back)}`).join('\n');

  async function request(route, options = {}) {
    if (!token.value.trim()) throw new Error('请先填写管理令牌');
    const response = await fetch('/api' + route, {
      ...options, signal: AbortSignal.timeout(15000), headers: {
        Authorization: 'Bearer ' + token.value.trim(), ...(options.body ? { 'Content-Type': 'application/json' } : {}),
      },
    });
    if (response.status === 204) return null;
    let body;
    try { body = await response.json(); }
    catch { throw new Error('服务未返回 JSON，请检查后端服务和 Nginx /api/ 转发配置'); }
    if (!response.ok) throw new Error(body.error || '请求失败，请重试');
    return body;
  }

  function updateGame() {
    const ssq = form.elements.game.value === 'ssq';
    document.querySelector('#bet-append-label').hidden = ssq;
    form.elements.append.disabled = ssq;
    if (ssq) form.elements.append.checked = false;
    form.elements.issues.placeholder = ssq ? '双色球如 2026120；多期如 2026120,2026121' : '大乐透如 26120；多期如 26120,26121';
    form.elements.numbers.placeholder = ssq ? '每行一组：01 02 03 04 05 06 + 01' : '每行一组：01 02 03 04 05 + 01 02';
  }

  document.addEventListener('lottery:generated', event => {
    generated = structuredClone(event.detail);
    document.querySelector('#use-generated').disabled = false;
  });
  document.querySelector('#use-generated').addEventListener('click', () => {
    if (!generated) return;
    form.elements.game.value = generated.config.game;
    form.elements.multiple.value = generated.config.multiple;
    updateGame();
    form.elements.append.checked = generated.config.append;
    form.elements.numbers.value = lines(generated.tickets);
    form.elements.issues.value = '';
    document.querySelector('#save-hint').textContent = `已填入生成的 ${generated.config.count} 组号码。请填写 ${generated.config.periods} 个实际投注期号；保存期数以所填期号数量为准。`;
    form.elements.issues.focus();
  });
  form.elements.game.addEventListener('change', updateGame);

  function drawDetails(bet) {
    const details = element('details', undefined, 'bet-details');
    details.append(element('summary', '号码与逐期验奖详情'));
    details.append(element('pre', lines(bet.tickets), 'saved-numbers'));
    const byIssue = new Map(bet.results.map(result => [result.issue, result]));
    for (const issue of bet.issues) {
      const result = byIssue.get(issue);
      const row = element('div', undefined, 'issue-result');
      row.append(element('strong', `第 ${issue} 期`));
      if (!result) row.append(element('p', '待开奖 / 待验奖'));
      else {
        row.append(element('p', `开奖：${numbers(result.draw.front)} + ${numbers(result.draw.back)} · ${result.draw.date}`));
        row.append(element('p', result.won ? `已中奖 · ${result.amount === null ? '奖金待补全' : money.format(result.amount) + ' 元'}` : '未中奖'));
        result.tickets.forEach((ticket, index) => {
          if (!ticket.winners.length) return;
          row.append(element('p', `第 ${index + 1} 组：` + ticket.winners.map(winner => `${winner.level}等奖 ${winner.count} 注${winner.amount === null ? '（奖金待补全）' : ' / ' + money.format(winner.amount) + ' 元'}`).join('；')));
        });
      }
      details.append(row);
    }
    return details;
  }

  function render(bets) {
    const fragment = document.createDocumentFragment();
    if (!bets.length) fragment.append(element('p', '暂无符合条件的投注记录。', 'empty-records'));
    for (const bet of bets) {
      const card = element('article', undefined, 'panel saved-bet');
      const heading = element('div', undefined, 'section-heading');
      heading.append(element('h3', `${Lottery.GAMES[bet.config.game].name} · #${bet.id}`));
      heading.append(element('span', states[bet.status] || bet.status, 'bet-state state-' + bet.status));
      card.append(heading);
      card.append(element('p', `${bet.config.front}+${bet.config.back} · ${bet.config.count} 组 · ${bet.config.multiple}倍${bet.config.append ? '追加' : ''} · ${bet.issues.length} 期`, 'saved-meta'));
      card.append(element('p', `期号：${bet.issues.join('、')}`, 'saved-meta'));
      card.append(element('p', `投注 ${money.format(bet.totalAmount)} 元 · 已验 ${bet.checkedPeriods}/${bet.issues.length} 期 · ${bet.winAmount === null ? '已知中奖 ' + money.format(bet.knownWinAmount) + ' 元，待全部验奖' : '中奖 ' + money.format(bet.winAmount) + ' 元'}`, 'saved-amount'));
      if (bet.note) card.append(element('p', bet.note, 'saved-note'));
      card.append(drawDetails(bet));
      const bottom = element('div', undefined, 'saved-bottom');
      bottom.append(element('span', new Date(bet.createdAt).toLocaleString('zh-CN')));
      const remove = element('button', '删除', 'delete-button');
      remove.type = 'button';
      remove.setAttribute('aria-label', `删除投注 #${bet.id}`);
      remove.addEventListener('click', async () => {
        if (deleting || !window.confirm(`删除投注 #${bet.id} 及其验奖记录？`)) return;
        deleting = true;
        remove.disabled = true;
        let removed = false;
        try {
          await request('/bets/' + bet.id, { method: 'DELETE' });
          removed = true;
          await load();
          message(`已删除投注 #${bet.id}。`);
        } catch (error) { message((removed ? '已删除，但列表刷新失败：' : '') + error.message, true); }
        finally { deleting = false; remove.disabled = false; }
      });
      bottom.append(remove); card.append(bottom); fragment.append(card);
    }
    list.replaceChildren(fragment);
  }

  async function load() {
    const generation = ++requestGeneration;
    loading = true;
    refreshButton.disabled = true;
    document.querySelector('#previous-page').disabled = true;
    document.querySelector('#next-page').disabled = true;
    try {
      const query = new URLSearchParams({ page: String(page), pageSize: '10' });
      for (const key of ['game', 'status']) {
        const value = document.querySelector('#filter-' + key).value;
        if (value) query.set(key, value);
      }
      const result = await request('/bets?' + query);
      if (generation !== requestGeneration) return;
      if (!result.items.length && page > 1) { page--; return await load(); }
      render(result.items);
      connected = true;
      document.querySelector('#record-count').textContent = `共 ${result.total} 条`;
      document.querySelector('#page-label').textContent = `第 ${page} / ${Math.max(1, Math.ceil(result.total / result.pageSize))} 页`;
      document.querySelector('#previous-page').disabled = page <= 1;
      document.querySelector('#next-page').disabled = page * result.pageSize >= result.total;
    } finally {
      if (generation === requestGeneration) { loading = false; refreshButton.disabled = false; }
    }
  }

  async function refresh() {
    try { await load(); message('投注列表已刷新。'); }
    catch (error) { message(error.message, true); }
  }
  refreshButton.addEventListener('click', refresh);
  for (const name of ['game', 'status']) document.querySelector('#filter-' + name).addEventListener('change', () => {
    page = 1;
    if (connected) refresh();
  });
  document.querySelector('#previous-page').addEventListener('click', () => { if (!loading && page > 1) { page--; refresh(); } });
  document.querySelector('#next-page').addEventListener('click', () => { if (!loading) { page++; refresh(); } });
  token.addEventListener('input', () => {
    requestGeneration++;
    loading = false;
    refreshButton.disabled = false;
    connected = false;
    page = 1;
    list.replaceChildren(element('p', '令牌已更改，请重新连接。', 'empty-records'));
    document.querySelector('#previous-page').disabled = true;
    document.querySelector('#next-page').disabled = true;
  });

  form.addEventListener('submit', async event => {
    event.preventDefault();
    if (saving) return;
    saving = true;
    saveButton.disabled = true;
    let saved;
    try {
      const tickets = form.elements.numbers.value.trim().split(/\r?\n/).filter(line => line.trim()).map((line, index) => {
        const parts = line.trim().split(/[+＋]/);
        if (parts.length !== 2 || parts.some(part => !part.trim())) throw new Error(`第 ${index + 1} 行格式错误，请用 + 分隔前后区`);
        const parse = part => part.trim().split(/[,，\s]+/).map(Number);
        return { front: parse(parts[0]), back: parse(parts[1]) };
      });
      saved = await request('/bets', { method: 'POST', body: JSON.stringify({
        game: form.elements.game.value, tickets, multiple: Number(form.elements.multiple.value),
        append: !form.elements.append.disabled && form.elements.append.checked,
        issues: form.elements.issues.value.trim().split(/[,，\s]+/).filter(Boolean), note: form.elements.note.value,
      }) });
      form.elements.issues.value = '';
      form.elements.numbers.value = '';
      form.elements.note.value = '';
      document.querySelector('#filter-game').value = '';
      document.querySelector('#filter-status').value = '';
      page = 1;
      await load();
      message(`投注 #${saved.id} 已保存，共 ${saved.issues.length} 期，${money.format(saved.totalAmount)} 元。`);
    } catch (error) { message((saved ? `投注 #${saved.id} 已保存，列表刷新失败：` : '') + error.message, true); }
    finally { saving = false; saveButton.disabled = false; }
  });
  updateGame();
})();
