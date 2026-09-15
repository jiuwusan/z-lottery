#!/usr/bin/env node

const crypto = require("node:crypto");

const UNIT_PRICE = 2;
const APPEND_PRICE = 1;

const GAMES = {
  dlt: {
    name: "超级大乐透",
    frontLabel: "前区",
    backLabel: "后区",
    frontMax: 35,
    backMax: 12,
    frontPick: 5,
    backPick: 2,
    defaultFront: 6,
    defaultBack: 3,
    supportsAppend: true,
  },
  ssq: {
    name: "双色球",
    frontLabel: "红球",
    backLabel: "蓝球",
    frontMax: 33,
    backMax: 16,
    frontPick: 6,
    backPick: 1,
    defaultFront: 7,
    defaultBack: 2,
    supportsAppend: false,
  },
};

function randomInt(min, max) {
  return crypto.randomInt(min, max + 1);
}

function shuffle(array) {
  const result = [...array];

  for (let i = result.length - 1; i > 0; i--) {
    const j = randomInt(0, i);
    [result[i], result[j]] = [result[j], result[i]];
  }

  return result;
}

function randomNumbers(max, count, excluded = []) {
  const excludedSet = new Set(excluded);
  const numbers = Array.from({ length: max }, (_, index) => index + 1)
    .filter((number) => !excludedSet.has(number));

  return shuffle(numbers)
    .slice(0, count)
    .sort((a, b) => a - b);
}

function combination(n, k) {
  if (k < 0 || k > n) {
    return 0;
  }

  let result = 1;

  for (let i = 1; i <= k; i++) {
    result = (result * (n - i + 1)) / i;
  }

  return result;
}

function formatNumbers(numbers) {
  return numbers.map((number) => String(number).padStart(2, "0")).join(" ");
}

function parseArgs(args) {
  const config = {
    game: "dlt",
    front: undefined,
    back: undefined,
    count: 4,
    multiple: 1,
    periods: 1,
    same: undefined,
    append: false,
    help: false,
  };

  for (let i = 0; i < args.length; i++) {
    switch (args[i]) {
      case "--game":
        config.game = args[++i];
        break;
      case "--front":
        config.front = Number(args[++i]);
        break;
      case "--back":
        config.back = Number(args[++i]);
        break;
      case "--count":
        config.count = Number(args[++i]);
        break;
      case "--multiple":
        config.multiple = Number(args[++i]);
        break;
      case "--periods":
        config.periods = Number(args[++i]);
        break;
      case "--same":
        config.same = args[++i];
        break;
      case "--append":
        config.append = true;
        break;
      case "--help":
        config.help = true;
        break;
      default:
        throw new Error(`未知参数：${args[i]}`);
    }
  }

  return config;
}

function validateAndCompleteConfig(config) {
  const game = GAMES[config.game];

  if (!game) {
    throw new Error("game 必须是 dlt 或 ssq");
  }

  const result = {
    ...config,
    front: config.front ?? game.defaultFront,
    back: config.back ?? game.defaultBack,
    sameFront: 0,
    sameBack: 0,
  };

  if (result.same !== undefined) {
    if (!/^\d+(,\d+)?$/.test(result.same)) {
      throw new Error("same 格式必须是 --same 前区数量[,后区数量]");
    }
    const [sameFront, sameBack = "0"] = result.same.split(",");
    result.sameFront = Number(sameFront);
    result.sameBack = Number(sameBack);
  }

  if (!Number.isInteger(result.front) || result.front < game.frontPick || result.front > game.frontMax) {
    throw new Error(
      `${game.frontLabel}数量必须是 ${game.frontPick}～${game.frontMax}`,
    );
  }

  if (!Number.isInteger(result.back) || result.back < game.backPick || result.back > game.backMax) {
    throw new Error(
      `${game.backLabel}数量必须是 ${game.backPick}～${game.backMax}`,
    );
  }

  if (result.sameFront > result.front) {
    throw new Error(`same 前区数量不能超过 ${result.front}`);
  }

  if (result.sameBack > result.back) {
    throw new Error(`same 后区数量不能超过 ${result.back}`);
  }

  if (!Number.isInteger(result.count) || result.count < 1) {
    throw new Error("count 必须是大于 0 的整数");
  }

  if (!Number.isInteger(result.multiple) || result.multiple < 1) {
    throw new Error("multiple 必须是大于 0 的整数");
  }

  if (!Number.isInteger(result.periods) || result.periods < 1) {
    throw new Error("periods 必须是大于 0 的整数");
  }

  if (result.append && !game.supportsAppend) {
    throw new Error("双色球不支持追加投注");
  }

  return { config: result, game };
}

function printHelp() {
  console.log(`
彩票完全随机选号

用法：

  node lottery.js [参数]

参数：

  --game <dlt|ssq>  彩票类型，默认 dlt
  --front <数量>     前区/红球选择数量
  --back <数量>      后区/蓝球选择数量
  --count <数量>     生成多少组，默认 4
  --multiple <倍数>  投注倍数，默认 1
  --periods <期数>   连续投注期数，默认 1
  --same <前区[,后区]> 多注共享号码数量，例如 3,2；后区可省略
  --append           大乐透追加投注（双色球不支持）
  --help              查看帮助

示例：

  node lottery.js
  node lottery.js --game dlt --front 7 --back 3 --count 1 --multiple 5 --append
  node lottery.js --game dlt --count 4 --same 3,2
  node lottery.js --game ssq
  node lottery.js --game ssq --front 8 --back 3 --count 2 --periods 10
`);
}

function printResult(config, game) {
  const commonFront = randomNumbers(game.frontMax, config.sameFront);
  const commonBack = randomNumbers(game.backMax, config.sameBack);
  const tickets = Array.from({ length: config.count }, () => ({
    front: [...commonFront, ...randomNumbers(game.frontMax, config.front - config.sameFront, commonFront)]
      .sort((a, b) => a - b),
    back: [...commonBack, ...randomNumbers(game.backMax, config.back - config.sameBack, commonBack)]
      .sort((a, b) => a - b),
  }));

  const singleGroupCount =
    combination(config.front, game.frontPick) *
    combination(config.back, game.backPick);
  const totalCount = singleGroupCount * config.count;
  const normalAmount = totalCount * UNIT_PRICE * config.multiple;
  const appendExtraAmount = totalCount * APPEND_PRICE * config.multiple;
  const appendAmount = normalAmount + appendExtraAmount;

  console.log("");
  console.log(
    `使用 ${game.name} ${config.multiple} 倍${config.append ? "追加" : ""}`,
  );
  console.log("");
  console.log("-----------------------");
  console.log("");

  tickets.forEach((ticket, index) => {
    const isMultiple =
      ticket.front.length > game.frontPick || ticket.back.length > game.backPick;

    console.log(`第${index + 1}注${isMultiple ? " 复式" : ""}`);
    console.log(`${game.frontLabel}：${formatNumbers(ticket.front)}`);
    console.log(`${game.backLabel}：${formatNumbers(ticket.back)}`);
    console.log("");
    console.log("-----------------------");
    console.log("");
  });

  const selection =
    config.game === "ssq"
      ? `${config.front} 红 + ${config.back} 蓝`
      : `${config.front}+${config.back}`;

  console.log(`投注方式：${selection}`);
  console.log(`复式数量：${config.count} 组`);
  console.log(`单组注数：${singleGroupCount} 注`);
  console.log(`总注数：${totalCount} 注`);
  console.log(`投注倍数：${config.multiple} 倍`);
  if (config.same !== undefined) {
    console.log(`多注相同：${config.sameFront}+${config.sameBack}`);
  }
  console.log("");
  console.log(`普通投注：${normalAmount} 元`);

  if (game.supportsAppend) {
    console.log(`追加投注：${appendAmount} 元`);
    console.log(`追加冗余：+${appendExtraAmount} 元`);
    console.log("");
  }

  const amountPerPeriod = config.append ? appendAmount : normalAmount;

  console.log(`连续投注：${config.periods} 期`);
  console.log(`每期金额：${amountPerPeriod} 元`);
  console.log(`实际投注金额：${amountPerPeriod * config.periods} 元`);
  console.log("");
}

function main() {
  try {
    const parsed = parseArgs(process.argv.slice(2));

    if (parsed.help) {
      printHelp();
      return;
    }

    const { config, game } = validateAndCompleteConfig(parsed);
    printResult(config, game);
  } catch (error) {
    console.error(`错误：${error.message}`);
    process.exitCode = 1;
  }
}

main();
