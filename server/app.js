'use strict';
const http = require('node:http');
const { createHash, timingSafeEqual } = require('node:crypto');
const { readFile } = require('node:fs/promises');
const path = require('node:path');
const { AppError } = require('./domain');

const staticFiles = {
  '': ['index.html', 'text/html'], 'index.html': ['index.html', 'text/html'],
  'styles.css': ['styles.css', 'text/css'], 'app.js': ['app.js', 'text/javascript'],
  'lottery-core.js': ['lottery-core.js', 'text/javascript'], 'records.js': ['records.js', 'text/javascript'],
};

function authenticate(request, token) {
  const actual = request.headers.authorization || '';
  const hash = value => createHash('sha256').update(value).digest();
  if (!timingSafeEqual(hash(actual), hash('Bearer ' + token))) throw new AppError('访问令牌无效或未提供', 401);
}

async function readJson(request) {
  if (request.headers['content-type']?.split(';')[0].trim() !== 'application/json') throw new AppError('请使用 application/json', 415);
  const limit = 512 * 1024;
  if (Number(request.headers['content-length']) > limit) { request.resume(); throw new AppError('请求体不能超过 512 KiB', 413); }
  let length = 0;
  const chunks = [];
  for await (const chunk of request) {
    length += chunk.length;
    if (length > limit) throw new AppError('请求体不能超过 512 KiB', 413);
    chunks.push(chunk);
  }
  try {
    const parsed = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error();
    return parsed;
  } catch { throw new AppError('请求体必须为 JSON 对象'); }
}

function createServer({ service, jobToken, logger = console }) {
  if (typeof jobToken !== 'string' || jobToken.length < 16) {
    throw new Error('JOB_TOKEN 必须配置为至少 16 位令牌');
  }
  const server = http.createServer(async (request, response) => {
    response.setHeader('X-Content-Type-Options', 'nosniff');
    response.setHeader('Cache-Control', 'no-store');
    const send = (status, body) => {
      response.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8' });
      response.end(status === 204 ? undefined : JSON.stringify(body));
    };
    try {
      const url = new URL(request.url, 'http://localhost');
      const route = url.pathname;
      if (route === '/api/health' && request.method === 'GET') return send(200, { ok: true });
      if (route.startsWith('/api/')) {
        const isJob = route === '/api/jobs/check';
        if (isJob) {
          authenticate(request, jobToken);
          if (request.method !== 'POST') throw new AppError('该接口仅支持 POST', 405);
          const report = await service.check(await readJson(request));
          return send(report.errors.length ? 502 : 200, report);
        }
        if (route === '/api/bets') {
          if (request.method === 'GET') return send(200, service.list(Object.fromEntries(url.searchParams)));
          if (request.method === 'POST') {
            const input = await readJson(request);
            return send(201, service.create(input));
          }
          throw new AppError('该接口仅支持 GET 和 POST', 405);
        }
        const match = /^\/api\/bets\/([1-9]\d*)$/.exec(route);
        if (match) {
          if (request.method === 'GET') return send(200, service.get(match[1]));
          if (request.method === 'DELETE') { service.remove(match[1]); return send(204); }
          throw new AppError('该接口仅支持 GET 和 DELETE', 405);
        }
        throw new AppError('接口不存在', 404);
      }
      if (!['GET', 'HEAD'].includes(request.method)) throw new AppError('该路径仅支持 GET 和 HEAD', 405);
      const name = route.replace(/^\/lottery(?:\/|$)/, '/').slice(1);
      if (!Object.hasOwn(staticFiles, name)) throw new AppError('文件不存在', 404);
      const [filename, type] = staticFiles[name];
      const data = await readFile(path.join(__dirname, '..', 'lottery', filename));
      response.writeHead(200, { 'Content-Type': type + '; charset=utf-8' });
      response.end(request.method === 'HEAD' ? undefined : data);
    } catch (error) {
      if (response.destroyed) return;
      if (!(error instanceof AppError)) logger.error('Request failed:', error);
      if (error.status === 401) response.setHeader('WWW-Authenticate', 'Bearer');
      send(error instanceof AppError ? error.status : 500, { error: error instanceof AppError ? error.message : '服务内部错误' });
    }
  });
  server.requestTimeout = 30000;
  server.headersTimeout = 15000;
  return server;
}

module.exports = { createServer };
