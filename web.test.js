const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function engine() {
  const context = vm.createContext({ crypto: require('node:crypto').webcrypto });
  const source = path.join(__dirname, 'web', 'lottery-core.js');
  assert.ok(fs.existsSync(source), '浏览器选号逻辑应存在');
  vm.runInContext(fs.readFileSync(source, 'utf8'), context);
  return context.Lottery;
}

test('浏览器默认配置与 CLI 一致，计算两种彩票的复式金额', () => {
  const lottery = engine();
  const dlt = lottery.generate({});
  assert.equal(dlt.tickets.length, 4);
  assert.equal(dlt.singleGroupCount, 18);
  assert.equal(dlt.totalCount, 72);
  assert.equal(dlt.totalAmount, 144);
  const ssq = lottery.generate({ game: 'ssq', multiple: 3, periods: 10 });
  assert.equal(ssq.singleGroupCount, 14);
  assert.equal(ssq.amountPerPeriod, 336);
  assert.equal(ssq.totalAmount, 3360);
});

test('浏览器选号在合法范围内、不重复、排序并保留共享号码', () => {
  const lottery = engine();
  for (const game of ['dlt', 'ssq']) {
    const result = lottery.generate({ game, front: 8, back: 3, count: 20, sameFront: 4, sameBack: 2 });
    for (const [zone, length, max] of [['front', 8, game === 'dlt' ? 35 : 33], ['back', 3, game === 'dlt' ? 12 : 16]]) {
      const common = result.tickets[0][zone].filter(n => result.tickets.every(t => t[zone].includes(n)));
      assert.ok(common.length >= (zone === 'front' ? 4 : 2));
      for (const ticket of result.tickets) {
        const numbers = Array.from(ticket[zone]);
        assert.equal(numbers.length, length);
        assert.equal(new Set(numbers).size, length);
        assert.ok(numbers.every(n => n >= 1 && n <= max));
        assert.deepEqual(numbers, [...numbers].sort((a, b) => a - b));
      }
    }
  }
});

test('大乐透追加、倍数和连续期数共同影响金额', () => {
  const result = engine().generate({ front: 5, back: 2, count: 1, append: true, multiple: 5, periods: 10 });
  assert.equal(result.normalAmount, 10);
  assert.equal(result.appendAmount, 15);
  assert.equal(result.totalAmount, 150);
});

test('拒绝非法参数、过多组数和超出安全精度的金额', () => {
  const lottery = engine();
  for (const config of [
    { game: 'unknown' }, { game: 'ssq', append: true },
    { front: 4 }, { back: 13 }, { front: '' }, { count: 0 }, { count: 1001 },
    { multiple: 1.5 }, { periods: -1 }, { sameFront: 7 }, { sameBack: -1 },
    { sameFront: 0.5 }, { multiple: Number.MAX_SAFE_INTEGER },
  ]) assert.throws(() => lottery.generate(config));
});

test('所有号码均共享时，每组号码一致且金额仍按组数累计', () => {
  const result = engine().generate({ sameFront: 6, sameBack: 3 });
  for (const ticket of result.tickets) assert.deepEqual(ticket, result.tickets[0]);
  assert.equal(result.totalAmount, 144);
});
