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
  const steppers = [];

  for (const input of form.querySelectorAll('input[type="number"]')) {
    const wrapper = document.createElement('div');
    wrapper.className = 'stepper';
    input.readOnly = true;
    input.inputMode = 'none';
    input.tabIndex = -1;
    const buttons = [-1, 1].map(direction => {
      const button = document.createElement('button');
      button.type = 'button';
      button.textContent = direction < 0 ? '−' : '+';
      button.addEventListener('pointerdown', event => {
        if (button.disabled || event.button !== 0) return;
        button.classList.add('is-pressed');
        button.setPointerCapture(event.pointerId);
      });
      for (const event of ['pointerup', 'pointercancel', 'lostpointercapture', 'blur']) {
        button.addEventListener(event, () => button.classList.remove('is-pressed'));
      }
      button.addEventListener('click', () => {
        const min = Number(input.min || 0);
        const max = Number(input.max || Number.MAX_SAFE_INTEGER);
        const value = Math.min(max, Math.max(min, Number(input.value) + direction));
        if (value === Number(input.value)) return;
        input.value = value;
        input.dispatchEvent(new Event('input', { bubbles: true }));
      });
      return button;
    });
    input.before(wrapper);
    wrapper.append(buttons[0], input, buttons[1]);
    steppers.push({ input, buttons });
  }

  function updateSteppers() {
    for (const { input, buttons } of steppers) {
      const title = document.querySelector(`label[for="${input.id}"]`).textContent;
      buttons[0].setAttribute('aria-label', '减少' + title);
      buttons[1].setAttribute('aria-label', '增加' + title);
      buttons[0].disabled = Number(input.value) <= Number(input.min || 0);
      buttons[1].disabled = Number(input.value) >= Number(input.max || Number.MAX_SAFE_INTEGER);
    }
  }

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
    for (const name of ['sameFront', 'sameBack']) {
      const input = form.elements[name];
      input.value = Math.min(Number(input.value), Number(input.max));
    }
    updateSteppers();
  }

  function stat(label, value, unit) {
    return `<div><div class="stat-label">${label}</div><div class="stat-value">${format.format(value)}<small>${unit}</small></div></div>`;
  }

  function render(result) {
    document.querySelector('#result-meta').hidden = false;
    summaryElement.hidden = false;
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
      document.dispatchEvent(new CustomEvent('lottery:generated', { detail: result }));
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
    document.dispatchEvent(new Event('lottery:settings-changed'));
    statusElement.textContent = '设置已修改，点击「生成号码」更新结果和金额。';
  });
  document.querySelector('#reset-button').addEventListener('click', () => {
    form.reset();
    updateGame();
    showError('');
    revision++;
    document.dispatchEvent(new Event('lottery:settings-changed'));
    statusElement.textContent = '已恢复默认设置，点击「生成号码」生成结果。';
  });

  copyButton.addEventListener('click', async () => {
    if (!currentResult) return;
    const copiedRevision = revision;
    const { config, game, tickets } = currentResult;
    const numbers = values => values.map(n => String(n).padStart(2, '0')).join(' ');
    const text = [
      `${game.name} ${config.front}+${config.back} · ${config.multiple}倍${config.append ? '追加' : ''} · ${config.periods} 期`,
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
})();
