'use strict';
const path = require('node:path');
const { Repository } = require('./repository');
const { BetService } = require('./service');
const { DrawProvider } = require('./draw-provider');
const { createServer } = require('./app');

const port = Number(process.env.PORT || 3000);
if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('PORT 必须在 1～65535 之间');
const repository = new Repository(process.env.DATABASE_PATH || path.join(__dirname, '..', 'data', 'lottery.sqlite'));
const service = new BetService(repository, new DrawProvider());
const server = createServer({ service, apiToken: process.env.API_TOKEN, jobToken: process.env.JOB_TOKEN });
const host = process.env.HOST || '127.0.0.1';
server.listen(port, host, () => console.log(`z-lottery listening on http://${host}:${port}`));
server.on('error', error => { console.error(error.message); repository.close(); process.exitCode = 1; });
let closing = false;
function shutdown() {
  if (closing) return;
  closing = true;
  server.close(() => { repository.close(); process.exitCode = 0; });
  setTimeout(() => process.exit(1), 10000).unref();
}
process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
