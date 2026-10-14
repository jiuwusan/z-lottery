(function (root) {
  'use strict';
  const GAMES = {
    dlt: { name: '超级大乐透', frontLabel: '前区', backLabel: '后区', frontMax: 35, backMax: 12, frontPick: 5, backPick: 2, defaultFront: 6, defaultBack: 3, supportsAppend: true },
    ssq: { name: '双色球', frontLabel: '红球', backLabel: '蓝球', frontMax: 33, backMax: 16, frontPick: 6, backPick: 1, defaultFront: 7, defaultBack: 2, supportsAppend: false },
  };

  // Rejection sampling keeps each outcome equally likely, as crypto.randomInt does in the CLI.
  function randomInt(max) {
    const limit = Math.floor(4294967296 / max) * max;
    const buffer = new Uint32Array(1);
    do { root.crypto.getRandomValues(buffer); } while (buffer[0] >= limit);
    return buffer[0] % max;
  }

  function randomNumbers(max, count, excluded = []) {
    const pool = Array.from({ length: max }, (_, i) => i + 1).filter(n => !excluded.includes(n));
    for (let i = pool.length - 1; i > 0; i--) {
      const j = randomInt(i + 1);
      [pool[i], pool[j]] = [pool[j], pool[i]];
    }
    return pool.slice(0, count).sort((a, b) => a - b);
  }

  function combination(n, k) {
    let result = 1;
    for (let i = 1; i <= k; i++) result = result * (n - i + 1) / i;
    return Math.round(result);
  }

  function validate(input = {}) {
    const gameKey = input.game ?? 'dlt';
    if (!Object.hasOwn(GAMES, gameKey)) throw new Error('请选择大乐透或双色球');
    const game = GAMES[gameKey];
    const config = { game: gameKey, append: Boolean(input.append) };
    const fields = [
      ['front', game.defaultFront, game.frontPick, game.frontMax, game.frontLabel + '数量'],
      ['back', game.defaultBack, game.backPick, game.backMax, game.backLabel + '数量'],
      ['count', 4, 1, 1000, '生成组数'],
      ['multiple', 1, 1, Number.MAX_SAFE_INTEGER, '投注倍数'],
      ['periods', 1, 1, Number.MAX_SAFE_INTEGER, '连续期数'],
      ['sameFront', 0, 0, game.frontMax, '共享' + game.frontLabel + '数量'],
      ['sameBack', 0, 0, game.backMax, '共享' + game.backLabel + '数量'],
    ];
    for (const [key, fallback, min, max, label] of fields) {
      const raw = input[key] ?? fallback;
      const value = typeof raw === 'string' && raw.trim() === '' ? NaN : Number(raw);
      if (!Number.isSafeInteger(value) || value < min || value > max) throw new Error(`${label}必须是 ${min}～${max} 的整数`);
      config[key] = value;
    }
    if (config.sameFront > config.front || config.sameBack > config.back) throw new Error('共享号码数量不能超过所选号码数量');
    if (config.append && !game.supportsAppend) throw new Error('双色球不支持追加投注');
    return { config, game };
  }

  function calculate(input = {}) {
    const { config, game } = validate(input);
    const singleGroupCount = combination(config.front, game.frontPick) * combination(config.back, game.backPick);
    const totalCount = singleGroupCount * config.count;
    const normalAmount = totalCount * 2 * config.multiple;
    const appendAmount = totalCount * 3 * config.multiple;
    const amountPerPeriod = config.append ? appendAmount : normalAmount;
    const totalAmount = amountPerPeriod * config.periods;
    if (![singleGroupCount, totalCount, normalAmount, ...(game.supportsAppend ? [appendAmount] : []), totalAmount].every(Number.isSafeInteger)) {
      throw new Error('投注金额过大，请减少组数、倍数或期数');
    }
    return { config, game, singleGroupCount, totalCount, normalAmount, appendAmount, amountPerPeriod, totalAmount };
  }

  function generate(input = {}) {
    const result = calculate(input);
    const { config, game } = result;
    const commonFront = randomNumbers(game.frontMax, config.sameFront);
    const commonBack = randomNumbers(game.backMax, config.sameBack);
    const tickets = Array.from({ length: config.count }, () => ({
      front: [...commonFront, ...randomNumbers(game.frontMax, config.front - config.sameFront, commonFront)].sort((a, b) => a - b),
      back: [...commonBack, ...randomNumbers(game.backMax, config.back - config.sameBack, commonBack)].sort((a, b) => a - b),
    }));
    return { ...result, commonFront, commonBack, tickets };
  }

  root.Lottery = { GAMES, calculate, generate };
})(globalThis);
