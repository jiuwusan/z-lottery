'use strict';

// External schedulers run this client; the HTTP service itself does not install a timer.
async function main() {
  const url = process.env.JOB_URL || `http://127.0.0.1:${process.env.PORT || 3000}/api/jobs/check`;
  let after;
  let failed = false;
  for (let batch = 0; batch < 1000; batch++) {
    const response = await fetch(url, {
      method: 'POST', signal: AbortSignal.timeout(180000),
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ limit: 20, ...(after ? { after } : {}) }),
    });
    const report = await response.json();
    if (response.status !== 200 && response.status !== 502) throw new Error(report.error || `HTTP ${response.status}`);
    if (!Array.isArray(report.errors) || !Object.hasOwn(report, 'nextCursor')) throw new Error('验奖接口返回格式异常');
    console.log(JSON.stringify({ time: new Date().toISOString(), ...report }));
    failed ||= report.errors.length > 0;
    if (!report.nextCursor) { if (failed) process.exitCode = 1; return; }
    if (typeof report.nextCursor !== 'string' || (after && report.nextCursor <= after)) throw new Error('验奖游标未前进');
    after = report.nextCursor;
  }
  throw new Error('验奖批次超过 1000，请检查投注规模');
}

main().catch(error => { console.error(error.message); process.exitCode = 1; });
