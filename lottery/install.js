(() => {
  'use strict';

  const button = document.getElementById('install-button');
  const dialog = document.getElementById('install-dialog');
  const description = document.getElementById('install-description');
  const steps = document.getElementById('install-steps');
  const displayMode = window.matchMedia('(display-mode: standalone)');
  let deferredPrompt = null;
  let installed = false;

  const isInstalled = () => installed || displayMode.matches || navigator.standalone === true;
  const canInstall = () => window.isSecureContext && /^https?:$/.test(location.protocol);

  function updateButton() {
    button.hidden = isInstalled();
    if (button.hidden) {
      deferredPrompt = null;
      if (dialog.open) dialog.close();
    }
  }

  function showGuide(message = '') {
    if (isInstalled()) return;
    const agent = navigator.userAgent;
    const ios = /iPhone|iPad|iPod/i.test(agent) || (/Macintosh/i.test(agent) && navigator.maxTouchPoints > 1);
    const embedded = /MicroMessenger|\bQQ\/|Weibo|FBAN|FBAV|Instagram|DingTalk|AlipayClient/i.test(agent);
    let instructions;
    if (!canInstall()) {
      description.textContent = '请先通过 HTTPS 网址打开网站，再添加到桌面。电脑本机开发也可使用 localhost 或 127.0.0.1。';
      instructions = ['打开本站的 HTTPS 地址；本地文件和普通 HTTP 地址无法直接安装网页应用。', '重新点击“添加到桌面”，或使用浏览器菜单中的安装选项。'];
    } else if (embedded) {
      description.textContent = '当前为应用内置浏览器，请先在系统浏览器中打开。';
      instructions = ['点击右上角菜单，选择“在浏览器中打开”，或复制链接到系统浏览器。', ios ? '在 Safari 中打开后，点击分享按钮，再选择“添加到主屏幕”。' : '在 Chrome 或系统浏览器中打开后，选择“安装应用”或“添加到主屏幕”。'];
    } else if (ios) {
      description.textContent = '在 iPhone 或 iPad 上，通过浏览器分享菜单添加到主屏幕。';
      instructions = ['点击浏览器的分享按钮；找不到时请用 Safari 打开本站。', '选择“添加到主屏幕”；若提供“作为网页 App 打开”选项，请开启。', '点击“添加”，之后即可从主屏幕上的 z-lottery 图标打开。'];
    } else if (/Android/i.test(agent)) {
      description.textContent = '在安卓手机上，可以从浏览器菜单安装或添加到主屏幕。';
      instructions = ['用 Chrome、Edge 或支持安装的系统浏览器打开本站。', '打开浏览器菜单，选择“安装应用”或“添加到主屏幕”。', '确认添加后，从主屏幕或应用列表打开 z-lottery。'];
    } else if (/Macintosh/i.test(agent) && /Safari/i.test(agent) && !/Chrome|Chromium|Edg|OPR/i.test(agent)) {
      description.textContent = '在 Mac 上，可以将网站添加到程序坞。';
      instructions = ['在 Safari 的“文件”或分享菜单中，选择“添加到程序坞”。', '确认名称并添加，之后即可从程序坞或应用程序打开。', '若没有此选项，可改用新版 Chrome 或 Edge 安装。'];
    } else {
      description.textContent = '在电脑上安装后，可从桌面、开始菜单或应用列表打开；入口位置由系统和浏览器决定。';
      instructions = ['推荐使用 Chrome 或 Edge，点击地址栏的安装图标，或打开浏览器菜单。', '选择“安装 z-lottery”“将此页面安装为应用”或“将此站点作为应用安装”，按提示确认。', '需要桌面图标时，在安装后的浏览器应用管理中选择创建快捷方式；若无安装选项，可将网站链接拖到桌面。'];
    }
    if (message) description.textContent = message + ' ' + description.textContent;
    steps.replaceChildren(...instructions.map(text => {
      const item = document.createElement('li');
      item.textContent = text;
      return item;
    }));
    if (!dialog.open) dialog.showModal();
  }

  window.addEventListener('beforeinstallprompt', event => {
    if (isInstalled() || !canInstall()) return;
    event.preventDefault();
    deferredPrompt = event;
  });
  window.addEventListener('appinstalled', () => {
    installed = true;
    updateButton();
  });
  displayMode.addEventListener('change', updateButton);

  button.addEventListener('click', async () => {
    if (button.disabled || isInstalled()) return;
    if (!deferredPrompt) {
      showGuide();
      return;
    }
    const prompt = deferredPrompt;
    deferredPrompt = null; // Each browser installation event can only be used once.
    button.disabled = true;
    try {
      await prompt.prompt();
      const choice = await prompt.userChoice;
      if (choice?.outcome === 'dismissed') showGuide('本次未添加，也可以按以下步骤手动添加。');
    } catch {
      showGuide('浏览器未能打开安装窗口，请按以下步骤添加。');
    } finally {
      button.disabled = false;
      updateButton();
    }
  });
  document.getElementById('close-install').addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => {
    if (!button.hidden) button.focus();
  });
  updateButton();
})();
