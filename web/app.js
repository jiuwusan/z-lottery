(function () {
  'use strict';
  const form = document.querySelector('#settings-form');
  const ticketsElement = document.querySelector('#tickets');
  const errorElement = document.querySelector('#form-error');
  const statusElement = document.querySelector('#status');
  const copyButton = document.querySelector('#copy-button');
  const summaryElement = document.querySelector('#summary');
  const format = new Intl.NumberFormat('zh-CN');
  let currentResult;
  let revision = 0;

  function inputConfig() {
    const data = new FormData(form);
    return {
      game: data.get('game'), front: data.get('front'), back: data.get('back'),
      count: data.get('count'), multiple: data.get('multiple'), periods: data.get('periods'),
      sameFront: data.get('sameFront'), sameBack: data.get('sameBack'), append: data.has('append'),
    };
  }

  function showError(message) {
    errorElement.textContent = message;
    errorElement.hidden = !message;
  }

  function updateGame() {
    const game = Lottery.GAMES[form.elements.game.value];
    for (const zone of ['front', 'back']) {
      const title = game[zone + 'Label'];
      const input = form.elements[zone];
      input.min = game[zone + 'Pick'];
      input.max = game[zone + 'Max'];
      input.value = game[zone === 'front' ? 'defaultFront' : 'defaultBack'];
      document.querySelector('#' + zone + '-label').textContent = title + '数量';
      document.querySelector('#' + zone + '-hint').textContent = `选择 ${input.min}～${input.max} 个号码`;
      document.querySelector('#same-' + zone + '-label').textContent = '共享' + title;
      form.elements[zone === 'front' ? 'sameFront' : 'sameBack'].value = 0;
    }
    document.querySelector('#append-option').hidden = !game.supportsAppend;
    form.elements.append.disabled = !game.supportsAppend;
    if (!game.supportsAppend) form.elements.append.checked = false;
    updateSharedLimits();
  }

  function updateSharedLimits() {
    form.elements.sameFront.max = form.elements.front.value;
    form.elements.sameBack.max = form.elements.back.value;
  }

  function stat(label, value, unit) {
    return `<div><div class="stat-label">${label}</div><div class="stat-value">${format.format(value)}<small>${unit}</small></div></div>`;
  }

  function render(result) {
    const { config, game } = result;
    const isMultiple = config.front > game.frontPick || config.back > game.backPick;
    const fragment = document.createDocumentFragment();
    result.tickets.forEach((ticket, index) => {
      const card = document.createElement('article');
      card.className = 'ticket';
      card.setAttribute('aria-label', '第 ' + (index + 1) + ' 组号码');
      card.innerHTML = `<div class="ticket-top"><span class="ticket-index">GROUP ${String(index + 1).padStart(2, '0')}</span><span class="ticket-kind">${isMultiple ? '复式' : '单式'} · ${config.front} + ${config.back}</span></div>`;
      for (const zone of ['front', 'back']) {
        const row = document.createElement('div');
        row.className = 'number-row';
        const label = document.createElement('span');
        label.className = 'zone-label';
        label.textContent = game[zone + 'Label'];
        const balls = document.createElement('div');
        balls.className = 'balls';
        for (const number of ticket[zone]) {
          const ball = document.createElement('span');
          const shared = result[zone === 'front' ? 'commonFront' : 'commonBack'].includes(number);
          ball.className = 'ball' + (zone === 'back' ? ' blue' : '') + (shared ? ' shared' : '');
          ball.textContent = String(number).padStart(2, '0');
          if (shared) ball.title = '每组共享的号码';
          balls.append(ball);
        }
        row.append(label, balls);
        card.append(row);
      }
      fragment.append(card);
    });
    ticketsElement.replaceChildren(fragment);
    ticketsElement.scrollTop = 0;
    document.querySelector('#result-description').textContent = `${game.name} · ${config.front} + ${config.back} · ${config.count} 组${config.append ? ' · 追加' : ''}`;
    summaryElement.innerHTML = `<div class="summary-heading"><span>投注金额</span><span>${config.multiple} 倍 · ${config.periods} 期${config.append ? ' · 追加' : ''}</span></div><div class="summary-grid">${stat('单组注数', result.singleGroupCount, '注')}${stat('总注数 / 每期', result.totalCount, '注')}${stat('每期金额', result.amountPerPeriod, '元')}</div><div class="total"><div class="total-label">实际投注金额<small>${config.count} 组 × ${config.multiple} 倍 × ${config.periods} 期</small></div><div class="total-amount"><small>¥</small>${format.format(result.totalAmount)}</div></div>${config.sameFront || config.sameBack ? '<div class="preview-note">描边号码为每组共享的号码</div>' : ''}`;
  }

  function generate() {
    try {
      const result = Lottery.generate(inputConfig());
      render(result);
      currentResult = result;
      revision++;
      copyButton.disabled = false;
      showError('');
      statusElement.textContent = `已生成 ${result.config.count} 组${result.game.name}号码。`;
    } catch (error) {
      showError(error.message);
      statusElement.textContent = currentResult ? '当前展示的仍是上一次生成结果。' : '';
    }
  }

  form.addEventListener('submit', event => { event.preventDefault(); generate(); });
  form.addEventListener('input', event => {
    if (event.target.name === 'game') updateGame();
    updateSharedLimits();
    showError('');
    revision++;
    statusElement.textContent = '设置已修改，点击「生成号码」更新结果和金额。';
  });
  document.querySelector('#reset-button').addEventListener('click', () => {
    form.reset();
    updateGame();
    generate();
  });

  copyButton.addEventListener('click', async () => {
    if (!currentResult) return;
    const copiedRevision = revision;
    const { config, game, tickets } = currentResult;
    const numbers = values => values.map(n => String(n).padStart(2, '0')).join(' ');
    const text = [
      `${game.name} ${config.front}+${config.back} · ${config.multiple} 倍 · ${config.periods} 期`,
      ...tickets.map((ticket, i) => `第 ${i + 1} 组\n${game.frontLabel}：${numbers(ticket.front)}\n${game.backLabel}：${numbers(ticket.back)}`),
    ].join('\n\n');
    try {
      if (window.isSecureContext && navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
      } else {
        const textarea = document.createElement('textarea');
        textarea.value = text;
        textarea.style.cssText = 'position:fixed;left:-9999px;top:0';
        document.body.append(textarea);
        const previousFocus = document.activeElement;
        textarea.select();
        let copied;
        try { copied = document.execCommand('copy'); }
        finally { textarea.remove(); previousFocus?.focus(); }
        if (!copied) throw new Error('copy failed');
      }
      if (revision === copiedRevision) statusElement.textContent = '号码已复制到剪贴板。';
    } catch (_) {
      statusElement.textContent = '浏览器未允许复制，请选中页面上的号码手动复制。';
    }
  });

  updateGame();
  generate();
})();
