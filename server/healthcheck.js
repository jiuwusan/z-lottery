'use strict';

fetch(`http://127.0.0.1:${process.env.PORT || 3000}/api/health`, { signal: AbortSignal.timeout(4000) })
  .then(async response => {
    if (!response.ok || (await response.json()).ok !== true) process.exitCode = 1;
  })
  .catch(() => { process.exitCode = 1; });
