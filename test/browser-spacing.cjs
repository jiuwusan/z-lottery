const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const { before, after, test } = require('node:test');
const { chromium } = require(require.resolve('playwright', {
  paths: [process.env.PLAYWRIGHT_NODE_MODULES || path.join(os.tmpdir(), 'z-lottery-browser-check'), process.cwd()],
}));

const root = path.resolve(__dirname, '../lottery');
const widths = [320, 360, 375, 380, 381, 390, 699, 700, 701, 768, 900, 1024, 1280, 1440, 1716];
const publicFiles = new Map([
  ['index.html', 'text/html'], ['styles.css', 'text/css'],
  ['lottery-core.js', 'text/javascript'], ['app.js', 'text/javascript'],
  ['records.js', 'text/javascript'], ['install.js', 'text/javascript'],
  ['favicon.svg', 'image/svg+xml'], ['manifest.webmanifest', 'application/manifest+json'],
  ...[180, 192, 512].map(size => [`icon-${size}.png`, 'image/png']),
]);
const fixture = {
  id: 1, config: { game: 'ssq', front: 7, back: 2, count: 1, multiple: 1, periods: 3, append: false },
  tickets: [{ front: [1, 2, 3, 4, 5, 6, 7], back: [1, 2] }], totalAmount: 126,
  placedAt: '2026-11-15T10:00:00+08:00', createdAt: '2026-11-15T10:00:00+08:00',
  matchMode: 'date', issues: [], checkedPeriods: 0, status: 'pending',
  winAmount: null, knownWinAmount: 0, note: '合成间距回归数据', results: [],
};
let browser, server, origin;

before(async () => {
  server = http.createServer(async (request, response) => {
    const url = new URL(request.url, 'http://fixture.invalid');
    if (url.pathname.startsWith('/api/')) {
      response.writeHead(404).end();
      return;
    }
    const file = url.pathname === '/' ? 'index.html' : url.pathname.slice(1);
    if (!publicFiles.has(file)) { response.writeHead(404).end(); return; }
    try {
      response.setHeader('Content-Type', publicFiles.get(file));
      response.end(await fs.readFile(path.join(root, file)));
    } catch { response.writeHead(404).end(); }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  origin = `http://127.0.0.1:${server.address().port}`;
  browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_PATH || (process.platform === 'win32' ? 'C:/Program Files/Google/Chrome/Application/chrome.exe' : undefined) });
});
after(async () => {
  await browser?.close();
  if (server) await new Promise(resolve => { server.close(resolve); server.closeAllConnections(); });
});

async function withPage(width, touch, work) {
  const context = await browser.newContext({ viewport: { width, height: 900 }, isMobile: touch, hasTouch: touch, reducedMotion: 'reduce' });
  const page = await context.newPage();
  const errors = [];
  page.setDefaultTimeout(6000);
  page.on('pageerror', error => errors.push(error.message));
  let listMode = 'records';
  await page.route('**/*', route => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) return route.abort();
    if (url.pathname.startsWith('/api/')) {
      const failed = route.request().method() !== 'GET' || listMode === 'error';
      return route.fulfill({ status: failed ? 500 : 200, contentType: 'application/json', body: JSON.stringify(failed ? { error: '合成错误：请稍后重试。' } : { items: listMode === 'empty' ? [] : [fixture], total: listMode === 'empty' ? 0 : 1, pageSize: 10 }) });
    }
    return route.continue();
  });
  await page.addInitScript(() => window.addEventListener('beforeinstallprompt', event => { event.preventDefault(); event.stopImmediatePropagation(); }, true));
  try {
    await page.goto(origin);
    await page.locator('.saved-bet').waitFor();
    await work(page, mode => { listMode = mode; });
    assert.deepEqual(errors, [], `${width}px runtime errors`);
  } finally { await context.close(); }
}

function geometry(selectors) {
  const result = {};
  for (const [name, selector] of Object.entries(selectors)) {
    const element = document.querySelector(selector), box = element.getBoundingClientRect();
    result[name] = { top: box.top, bottom: box.bottom, left: box.left, right: box.right, width: box.width, height: box.height };
  }
  return result;
}
async function screenshot(page, name) {
  if (!process.env.LOTTERY_BROWSER_ARTIFACTS) return;
  await fs.mkdir(process.env.LOTTERY_BROWSER_ARTIFACTS, { recursive: true });
  await page.screenshot({ path: path.join(process.env.LOTTERY_BROWSER_ARTIFACTS, name + '.png') });
}
function withinDialog() {
  const dialog = document.querySelector('#new-bet-dialog[open], #install-dialog[open]');
  return { page: document.documentElement.scrollWidth <= innerWidth + 1, dialog: !dialog || dialog.scrollWidth <= dialog.clientWidth + 1 };
}

test('both games retain a section gap when append controls or form errors change', async () => {
  const failures = [];
  for (const width of [320, 390, 768, 1440]) await withPage(width, width < 701, async page => {
    await page.locator('#new-bet-button').click();
    const dlt = await page.evaluate(geometry, { shared: '.shared-section', append: '#append-option', generate: '#settings-form button[type=submit]' });
    if (dlt.append.top - dlt.shared.bottom < 16 || dlt.generate.top - dlt.append.bottom < 16) failures.push(`${width}px dlt action gap`);
    await page.evaluate(() => { const form = document.getElementById('settings-form'); form.elements.count.value = '0'; form.requestSubmit(); });
    await page.locator('#form-error').waitFor();
    const dltError = await page.evaluate(geometry, { append: '#append-option', error: '#form-error', generate: '#settings-form button[type=submit]' });
    if (dltError.error.top - dltError.append.bottom < 16 || dltError.generate.top - dltError.error.bottom < 16) failures.push(`${width}px dlt form-error action gap`);
    await page.locator('input[name=game][value=ssq]').check();
    const ssq = await page.evaluate(geometry, { shared: '.shared-section', generate: '#settings-form button[type=submit]' });
    if (ssq.generate.top - ssq.shared.bottom < 16) failures.push(`${width}px ssq gap=${ssq.generate.top - ssq.shared.bottom}`);
    await page.evaluate(() => { const form = document.getElementById('settings-form'); form.elements.count.value = '0'; form.requestSubmit(); });
    await page.locator('#form-error').waitFor();
    const error = await page.evaluate(geometry, { shared: '.shared-section', error: '#form-error', generate: '#settings-form button[type=submit]' });
    if (error.error.top - error.shared.bottom < 16 || error.generate.top - error.error.bottom < 16) failures.push(`${width}px form-error gaps=${error.error.top - error.shared.bottom}/${error.generate.top - error.error.bottom}`);
    assert.deepEqual(await page.evaluate(withinDialog), { page: true, dialog: true });
    if (width === 1440 || width === 320) await screenshot(page, `${width}-form-error`);
  });
  assert.deepEqual(failures, []);
});

test('numeric boundary values fit and periods keep the full row at every breakpoint', async () => {
  const failures = [];
  for (const width of widths) await withPage(width, width < 701, async page => {
    await page.locator('#new-bet-button').click();
    await page.evaluate(() => {
      const form = document.getElementById('settings-form');
      for (const [name, value] of Object.entries({ front: 35, back: 12, count: 1000, multiple: 10000, periods: 100, sameFront: 35, sameBack: 12 })) form.elements[name].value = value;
      form.dispatchEvent(new Event('input', { bubbles: true }));
    });
    const values = await page.evaluate(() => [...document.querySelectorAll('.stepper input')].map(input => {
      const context = document.createElement('canvas').getContext('2d'); context.font = getComputedStyle(input).font;
      return { id: input.id, width: input.getBoundingClientRect().width, required: context.measureText(input.value).width };
    }));
    for (const value of values) if (value.width + 1 < value.required) failures.push(`${width}px ${value.id}: ${value.width}px < ${value.required}px`);
    const row = await page.evaluate(geometry, { grid: '#settings-form>.field-grid', periods: '.field.wide', dialog: '#new-bet-dialog' });
    if (Math.abs(row.grid.width - row.periods.width) > 1) failures.push(`${width}px periods use ${row.periods.width}/${row.grid.width}px`);
    if (width <= 700 && Math.abs(row.dialog.width - (width - 24)) > 1) failures.push(`${width}px dialog width=${row.dialog.width}`);
    assert.deepEqual(await page.evaluate(withinDialog), { page: true, dialog: true });
    if (width === 320 || width === 390) { await page.locator('#setting-count').scrollIntoViewIfNeeded(); await screenshot(page, `${width}-numeric-boundaries`); }
  });
  assert.deepEqual(failures, []);
});

test('touch actions have room to tap and wrap inside record cards', async () => {
  const failures = [];
  for (const width of [320, 360, 390, 768]) await withPage(width, true, async page => {
    const records = await page.evaluate(() => {
      const buttons = [...document.querySelectorAll('.saved-actions button')].map(button => { const r = button.getBoundingClientRect(); return { left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width, height: r.height }; });
      const card = document.querySelector('.saved-bet').getBoundingClientRect();
      return { buttons, card: { left: card.left, right: card.right } };
    });
    for (const b of records.buttons) if (b.height < 44 || b.width < 44 || b.left < records.card.left || b.right > records.card.right) failures.push(`${width}px record button ${b.width}x${b.height}`);
    const [first, second] = records.buttons;
    if (first.top === second.top && second.left - first.right < 8) failures.push(`${width}px record gap=${second.left - first.right}`);
    for (const selector of ['#connect-button', '#previous-page', '#next-page']) if ((await page.locator(selector).boundingBox()).height < 44) failures.push(`${width}px ${selector} touch height`);
    await page.locator('#new-bet-button').click();
    for (const selector of ['#close-new-bet', '#copy-button', '#reset-button']) if ((await page.locator(selector).boundingBox()).height < 44) failures.push(`${width}px ${selector} touch height`);
    await page.locator('#close-new-bet').click();
    await page.locator('#install-button').click();
    if ((await page.locator('#close-install').boundingBox()).height < 44) failures.push(`${width}px install close touch height`);
    assert.deepEqual(await page.evaluate(withinDialog), { page: true, dialog: true });
  });
  assert.deepEqual(failures, []);
});

test('generated results, save failures, list states and install guide stay within the viewport', async () => {
  for (const width of widths) await withPage(width, width < 701, async (page, setMode) => {
    await page.locator('#new-bet-button').click();
    await page.locator('input[name=game][value=ssq]').check();
    await page.locator('#settings-form button[type=submit]').click();
    await page.locator('#save-panel').waitFor();
    const panels = await page.evaluate(geometry, { summary: '#summary', save: '#save-panel' });
    assert.ok(panels.save.top - panels.summary.bottom >= 16);
    await page.locator('#save-bet').click();
    await page.waitForFunction(() => document.getElementById('save-status').textContent.includes('合成错误'));
    assert.deepEqual(await page.evaluate(withinDialog), { page: true, dialog: true }, `${width}px generated/save failure`);
    await page.locator('#close-new-bet').click();
    for (const mode of ['empty', 'error']) {
      setMode(mode); await page.locator('#connect-button').click();
      await page.waitForFunction(() => !document.getElementById('connect-button').disabled);
      assert.deepEqual(await page.evaluate(withinDialog), { page: true, dialog: true }, `${width}px list ${mode}`);
    }
    await page.locator('#install-button').click();
    assert.deepEqual(await page.evaluate(withinDialog), { page: true, dialog: true }, `${width}px install guide`);
    await page.locator('#close-install').click();
    await page.waitForFunction(() => document.activeElement.id === 'install-button');
  });
});
