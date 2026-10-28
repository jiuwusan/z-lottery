const test = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const path = require('node:path');
const { spawn } = require('node:child_process');

async function runClient(t, handler) {
  const server = http.createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    assert.equal(request.headers.authorization, undefined);
    const { status, body } = handler(JSON.parse(Buffer.concat(chunks).toString()));
    response.writeHead(status, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify(body));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise(resolve => server.close(resolve)));
  return new Promise((resolve, reject) => {
    const environment = { ...process.env, JOB_URL: `http://127.0.0.1:${server.address().port}/api/jobs/check` };
    delete environment.JOB_TOKEN;
    const child = spawn(process.execPath, [path.join(__dirname, 'check-job.js')], {
      env: environment,
      windowsHide: true,
    });
    let stdout = '', stderr = '';
    child.stdout.on('data', data => { stdout += data; });
    child.stderr.on('data', data => { stderr += data; });
    child.once('error', reject);
    child.once('exit', code => resolve({ code, stdout, stderr }));
  });
}

test('外部任务客户端遍历游标，失败批次继续处理并以非零状态结束', async t => {
  let requests = 0;
  const result = await runClient(t, input => {
    requests++;
    assert.equal(input.limit, 20);
    if (requests === 1) {
      assert.equal(input.after, undefined);
      return { status: 502, body: { checked: 1, waiting: 0, errors: [{ message: 'upstream timeout' }], nextCursor: 'dlt:26119' } };
    }
    assert.equal(input.after, 'dlt:26119');
    return { status: 200, body: { checked: 1, waiting: 0, errors: [], nextCursor: null } };
  });
  assert.equal(requests, 2);
  assert.equal(result.code, 1);
  assert.equal(result.stdout.trim().split('\n').length, 2);
  assert.equal(result.stderr, '');
});

test('外部任务客户端成功退出，409 冲突不继续请求', async t => {
  const success = await runClient(t, () => ({ status: 200, body: { errors: [], nextCursor: null } }));
  assert.equal(success.code, 0);
  let requests = 0;
  const conflict = await runClient(t, () => { requests++; return { status: 409, body: { error: 'task running' } }; });
  assert.equal(requests, 1);
  assert.equal(conflict.code, 1);
  assert.match(conflict.stderr, /task running/);
});
