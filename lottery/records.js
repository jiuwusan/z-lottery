(function () {
  'use strict';
  const list = document.querySelector('#bet-list');
  const status = document.querySelector('#records-status');
  const saveButton = document.querySelector('#save-bet');
  const refreshButton = document.querySelector('#connect-button');
  const states = { pending: '待开奖', partial: '部分已开奖', prize_pending: '奖金待补全', won: '已中奖', lost: '未中奖' };
  const money = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 });
  const time = new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', dateStyle: 'medium', timeStyle: 'short', hour12: false });
  let generated;
  let generation = 0;
  let savedGeneration = -1;
  let dirty = false;
  const issuePreview = document.querySelector('#issue-preview');
  const placedInput = document.querySelector('#bet-placed-at');
  const saveStatus = document.querySelector('#save-status');
  let page = 1;
  let loading = false;
  let saving = false;
  let deleting = false;
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
    const response = await fetch('/api' + route, {
      ...options, signal: AbortSignal.timeout(15000), headers: {
        ...(options.body ? { 'Content-Type': 'application/json' } : {}),
      },
    });
    if (response.status === 204) return null;
    let body;
    try { body = await response.json(); }
    catch { throw new Error('服务未返回 JSON，请检查后端服务和 Nginx /api/ 转发配置'); }
    if (!response.ok) {
      const error = new Error(body.error || '请求失败，请重试');
      error.status = response.status;
      throw error;
    }
    return body;
  }

  function saveMessage(text, isError = false) {
    saveStatus.textContent = text;
    saveStatus.classList.toggle('is-error', isError);
  }

  function updateSaveButton() {
    saveButton.disabled = saving || !generated || dirty || savedGeneration === generation;
  }

  function updateDatePreview() {
    if (!generated) return;
    const start = placedInput.value ? `从北京时间 ${placedInput.value.replace('T', ' ')} 起` : '以保存时的北京时间为投注时间';
    issuePreview.textContent = `${start}，匹配后续连续 ${generated.config.periods} 次有效开奖。期号在实际开奖后确定。`;
  }
  placedInput.addEventListener('input', updateDatePreview);

  document.addEventListener('lottery:generated', event => {
    generated = structuredClone(event.detail);
    generation++;
    dirty = false;
    document.querySelector('#save-panel').hidden = false;
    saveMessage('');
    updateDatePreview();
    updateSaveButton();
  });
  document.addEventListener('lottery:settings-changed', () => {
    dirty = true;
    updateSaveButton();
    if (generated) saveMessage('设置已修改，请重新生成号码后保存。');
  });

  function drawDetails(bet) {
    const details = element('details', undefined, 'bet-details');
    details.append(element('summary', '号码与逐期验奖详情'));
    details.append(element('pre', lines(bet.tickets), 'saved-numbers'));
    const byIssue = new Map(bet.results.map(result => [result.issue, result]));
    for (let index = 0; index < bet.config.periods; index++) {
      const issue = bet.issues[index];
      const result = byIssue.get(issue);
      const row = element('div', undefined, 'issue-result');
      row.append(element('strong', issue ? `第 ${index + 1} 次 / ${issue} 期` : `第 ${index + 1} 次开奖 · 待匹配`));
      if (!result) row.append(element('p', issue ? '待开奖 / 待验奖' : '根据投注时间等待下一次实际开奖'));
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
      card.append(element('p', `${bet.config.front}+${bet.config.back} · ${bet.config.count} 组 · ${bet.config.multiple}倍${bet.config.append ? '追加' : ''} · ${bet.config.periods} 期`, 'saved-meta'));
      card.append(element('p', bet.matchMode === 'date' ? `投注时间：${time.format(new Date(bet.placedAt))}（北京时间）` : '历史记录 · 按已保存期号验奖', 'saved-meta'));
      card.append(element('p', bet.issues.length ? `已匹配期号：${bet.issues.join('、')}` : '尚无匹配期号，等待实际开奖', 'saved-meta'));
      card.append(element('p', `投注 ${money.format(bet.totalAmount)} 元 · 已验 ${bet.checkedPeriods}/${bet.config.periods} 期 · ${bet.winAmount === null ? '已知中奖 ' + money.format(bet.knownWinAmount) + ' 元，待全部验奖' : '中奖 ' + money.format(bet.winAmount) + ' 元'}`, 'saved-amount'));
      if (bet.note) card.append(element('p', bet.note, 'saved-note'));
      card.append(drawDetails(bet));
      const bottom = element('div', undefined, 'saved-bottom');
      bottom.append(element('span', '登记于 ' + time.format(new Date(bet.createdAt))));
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
    refresh();
  });
  document.querySelector('#previous-page').addEventListener('click', () => { if (!loading && page > 1) { page--; refresh(); } });
  document.querySelector('#next-page').addEventListener('click', () => { if (!loading) { page++; refresh(); } });

  saveButton.addEventListener('click', async () => {
    if (saveButton.disabled || saving || !generated || dirty) return;
    if (!placedInput.checkValidity()) { placedInput.reportValidity(); return; }
    const selected = structuredClone(generated);
    const selectedGeneration = generation;
    const placedValue = placedInput.value;
    const note = document.querySelector('#bet-note').value;
    saving = true;
    updateSaveButton();
    let saved;
    try {
      saved = await request('/bets', { method: 'POST', body: JSON.stringify({
        game: selected.config.game, tickets: selected.tickets, multiple: selected.config.multiple,
        append: selected.config.append, periods: selected.config.periods, note,
        ...(placedValue ? { placedAt: placedValue + (placedValue.length === 16 ? ':00' : '') + '+08:00' } : {}),
      }) });
      savedGeneration = selectedGeneration;
      if (generation === selectedGeneration && document.querySelector('#bet-note').value === note) document.querySelector('#bet-note').value = '';
      document.querySelector('#filter-game').value = '';
      document.querySelector('#filter-status').value = '';
      page = 1;
      await load();
      const text = `投注 #${saved.id} 已保存，投注时间 ${time.format(new Date(saved.placedAt))}，连续 ${saved.config.periods} 期，${money.format(saved.totalAmount)} 元。`;
      message(text);
      if (generation === selectedGeneration) saveMessage(text);
    } catch (error) {
      const text = (saved ? `投注 #${saved.id} 已保存，列表刷新失败：` : '') + error.message;
      if (generation === selectedGeneration) saveMessage(text, true);
      else message(text, true);
    } finally { saving = false; updateSaveButton(); }
  });
  refresh();
})();
