#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";

const autocompleteBaseURL = "https://ac.stock.naver.com/ac";
const userAgent = "jucobot-reference-generator/1.0";
const unsupportedReferencePrefix = "UNSUPPORTED:";

const manualWorldOverrides = {};

function usage() {
  console.error(
    [
      "Usage:",
      "  node scripts/stocks/generate_naver_assets_from_markdown.mjs",
      "    --input <stock_mappings.md>",
      "    --curated-aliases <naver_aliases.json>",
      "    --curated-local-results <naver_local_results.json>",
      "    --output-generated-aliases <naver_aliases.generated.json>",
      "    --output-generated-results <naver_local_results.generated.json>",
      "    --output-report <naver_generated.report.json>",
    ].join("\n"),
  );
}

function parseArgs(argv) {
  const args = {};
  for (let i = 2; i < argv.length; i += 1) {
    const arg = argv[i];
    if (!arg.startsWith("--")) {
      throw new Error(`unexpected argument: ${arg}`);
    }
    const key = arg.slice(2);
    const value = argv[i + 1];
    if (!value || value.startsWith("--")) {
      throw new Error(`missing value for ${arg}`);
    }
    args[key] = value;
    i += 1;
  }
  const required = [
    "input",
    "curated-aliases",
    "curated-local-results",
    "output-generated-aliases",
    "output-generated-results",
    "output-report",
  ];
  for (const key of required) {
    if (!args[key]) {
      throw new Error(`missing required flag --${key}`);
    }
  }
  return args;
}

function readJSON(filePath) {
  return JSON.parse(fs.readFileSync(filePath, "utf8"));
}

function normalizeWhitespace(value) {
  return value.replace(/\s+/g, " ").trim();
}

function cleanAlias(raw) {
  return normalizeWhitespace(raw.replaceAll("**", "").replaceAll("`", "").trim());
}

function parseMarkdownRows(markdown) {
  const rows = [];
  let mode = "";
  for (const line of markdown.split(/\r?\n/)) {
    if (line.startsWith("## 🇰🇷")) {
      mode = "domestic";
      continue;
    }
    if (line.startsWith("## 🇺🇸")) {
      mode = "world";
      continue;
    }
    if (!line.startsWith("|")) {
      continue;
    }
    const parts = line
      .split("|")
      .slice(1, -1)
      .map((part) => part.trim());
    if (mode === "domestic") {
      if (parts.length < 4 || parts[0] === "종목코드" || parts[0] === "---") {
        continue;
      }
      rows.push({
        kind: "domestic",
        code: parts[0],
        name: parts[1],
        market: parts[2],
        aliases: parts[3]
          .split(",")
          .map(cleanAlias)
          .filter(Boolean),
      });
      continue;
    }
    if (mode === "world") {
      if (parts.length < 4 || parts[0] === "심볼" || parts[0] === "---") {
        continue;
      }
      rows.push({
        kind: "world",
        symbol: parts[0].toUpperCase(),
        koreanName: parts[1],
        englishName: parts[2],
        aliases: parts[3]
          .split(",")
          .map(cleanAlias)
          .filter(Boolean),
      });
    }
  }
  return rows;
}

function sortObject(input) {
  const output = {};
  for (const key of Object.keys(input).sort((a, b) => a.localeCompare(b))) {
    output[key] = input[key];
  }
  return output;
}

function uniq(values) {
  return Array.from(new Set(values.filter(Boolean)));
}

function ensureDirFor(filePath) {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
}

function normalizeSymbolKey(value) {
  return String(value || "").toUpperCase().replace(/[^A-Z0-9]/g, "");
}

async function fetchAutocompleteItems(query, cache) {
  if (cache.has(query)) {
    return cache.get(query);
  }
  const url = new URL(autocompleteBaseURL);
  url.searchParams.set("q", query);
  url.searchParams.set("target", "stock");
  url.searchParams.set("st", "111");

  let lastError = null;
  for (let attempt = 1; attempt <= 4; attempt += 1) {
    try {
      const response = await fetch(url, {
        headers: {
          accept: "application/json",
          "user-agent": userAgent,
        },
        signal: AbortSignal.timeout(15000),
      });
      if (!response.ok) {
        throw new Error(`autocomplete request failed for ${query}: ${response.status} ${response.statusText}`);
      }
      const body = await response.json();
      const items = Array.isArray(body?.items) ? body.items : [];
      cache.set(query, items);
      return items;
    } catch (error) {
      lastError = error;
      await new Promise((resolve) => setTimeout(resolve, 250 * attempt));
    }
  }
  throw lastError;
}

function pickExactWorldItem(items, row) {
  const wanted = normalizeSymbolKey(row.symbol);
  let nameFallback = null;

  for (const item of items) {
    if (!item || !item.nationCode || item.nationCode === "KOR") {
      continue;
    }
    const codeKey = normalizeSymbolKey(item.code);
    const reutersKey = normalizeSymbolKey(String(item.reutersCode || "").split(".")[0]);
    if (codeKey === wanted || reutersKey === wanted) {
      return item;
    }
    if (
      !nameFallback &&
      (normalizeWhitespace(item.name || "") === normalizeWhitespace(row.koreanName || "") ||
        normalizeWhitespace(item.name || "") === normalizeWhitespace(row.englishName || ""))
    ) {
      nameFallback = item;
    }
  }

  return nameFallback;
}

async function resolveWorldRow(row, cache) {
  if (manualWorldOverrides[row.symbol]) {
    return manualWorldOverrides[row.symbol];
  }

  const queries = uniq([row.symbol, row.koreanName, row.englishName]);
  for (const query of queries) {
    const items = await fetchAutocompleteItems(query, cache);
    const picked = pickExactWorldItem(items, row);
    if (picked && picked.reutersCode) {
      return {
        code: row.symbol,
        name: row.koreanName || picked.name,
        market: picked.typeCode || "",
        nation_code: picked.nationCode || "",
        reuters_code: picked.reutersCode || "",
        query,
      };
    }
  }
  return null;
}

function pickWorldTarget(row, curatedLocalResults) {
  const existing = curatedLocalResults[row.symbol];
  if (!existing) {
    return row.symbol;
  }
  if (String(existing.code || "").toUpperCase() === row.symbol && String(existing.nation_code || "") === "USA") {
    return row.symbol;
  }
  return row.koreanName || row.symbol;
}

async function mapLimit(items, concurrency, worker) {
  const results = new Array(items.length);
  let nextIndex = 0;

  async function runWorker() {
    while (nextIndex < items.length) {
      const current = nextIndex;
      nextIndex += 1;
      results[current] = await worker(items[current], current);
    }
  }

  await Promise.all(Array.from({ length: Math.min(concurrency, items.length) }, runWorker));
  return results;
}

async function main() {
  const args = parseArgs(process.argv);
  const markdown = fs.readFileSync(args.input, "utf8");
  const curatedAliases = readJSON(args["curated-aliases"]);
  const curatedLocalResults = readJSON(args["curated-local-results"]);
  const rows = parseMarkdownRows(markdown);

  const generatedAliases = {};
  const generatedResults = {};
  const report = {
    source: path.resolve(args.input),
    generatedAt: new Date().toISOString(),
    rowCount: rows.length,
    domesticRowCount: rows.filter((row) => row.kind === "domestic").length,
    worldRowCount: rows.filter((row) => row.kind === "world").length,
    manualWorldOverrides: sortObject(manualWorldOverrides),
    domesticRows: [],
    worldRows: [],
    unsupportedWorldRows: [],
    curatedAliasCollisions: [],
    generatedAliasCollisions: [],
  };

  for (const row of rows.filter((entry) => entry.kind === "domestic")) {
    const target = row.name;
    if (!curatedLocalResults[target]) {
      generatedResults[target] = {
        code: row.code,
        name: row.name,
        market: row.market,
      };
    }
    const aliasSet = uniq([row.code, row.name, ...row.aliases]);
    for (const alias of aliasSet) {
      if (!alias || alias === target) {
        continue;
      }
      if (curatedAliases[alias]) {
        report.curatedAliasCollisions.push({
          alias,
          target,
          curatedTarget: curatedAliases[alias],
          kind: "domestic",
        });
        continue;
      }
      if (generatedAliases[alias] && generatedAliases[alias] !== target) {
        report.generatedAliasCollisions.push({
          alias,
          existingTarget: generatedAliases[alias],
          nextTarget: target,
          kind: "domestic",
        });
        continue;
      }
      generatedAliases[alias] = target;
    }
    report.domesticRows.push({ code: row.code, target, aliasCount: aliasSet.length });
  }

  const cache = new Map();
  const worldRows = rows.filter((entry) => entry.kind === "world");
  const worldResolutions = await mapLimit(worldRows, 4, async (row) => ({
    row,
    resolved:
      curatedLocalResults[row.symbol] &&
      String(curatedLocalResults[row.symbol].code || "").toUpperCase() === row.symbol &&
      String(curatedLocalResults[row.symbol].nation_code || "") === "USA"
        ? {
            code: curatedLocalResults[row.symbol].code || row.symbol,
            name: curatedLocalResults[row.symbol].name || row.koreanName,
            market: curatedLocalResults[row.symbol].market || "",
            nation_code: curatedLocalResults[row.symbol].nation_code || "USA",
            reuters_code: curatedLocalResults[row.symbol].reuters_code || "",
            query: "curated_local_result",
          }
        : await resolveWorldRow(row, cache),
  }));

  for (const item of worldResolutions) {
    const { row, resolved } = item;
    const target = pickWorldTarget(row, curatedLocalResults);
    if (!resolved || !resolved.reuters_code) {
      if (!curatedLocalResults[target]) {
        generatedResults[target] = {
          code: row.symbol,
          name: row.koreanName,
          market: "UNSUPPORTED",
          nation_code: "USA",
          reuters_code: `${unsupportedReferencePrefix}${row.symbol}`,
        };
      }
      const aliasSet = uniq([row.symbol, row.koreanName, row.englishName, ...row.aliases]);
      for (const alias of aliasSet) {
        if (!alias || alias === target) {
          continue;
        }
        if (curatedAliases[alias]) {
          report.curatedAliasCollisions.push({
            alias,
            target,
            curatedTarget: curatedAliases[alias],
            kind: "world",
          });
          continue;
        }
        if (generatedAliases[alias] && generatedAliases[alias] !== target) {
          report.generatedAliasCollisions.push({
            alias,
            existingTarget: generatedAliases[alias],
            nextTarget: target,
            kind: "world",
          });
          continue;
        }
        generatedAliases[alias] = target;
      }
      report.unsupportedWorldRows.push({
        symbol: row.symbol,
        target,
        koreanName: row.koreanName,
        englishName: row.englishName,
        aliases: row.aliases,
        reutersCode: `${unsupportedReferencePrefix}${row.symbol}`,
      });
      continue;
    }
    if (!curatedLocalResults[target]) {
      generatedResults[target] = {
        code: row.symbol,
        name: row.koreanName,
        market: resolved.market,
        nation_code: resolved.nation_code,
        reuters_code: resolved.reuters_code,
      };
    }
    const aliasSet = uniq([row.symbol, row.koreanName, row.englishName, ...row.aliases]);
    for (const alias of aliasSet) {
      if (!alias || alias === target) {
        continue;
      }
      if (curatedAliases[alias]) {
        report.curatedAliasCollisions.push({
          alias,
          target,
          curatedTarget: curatedAliases[alias],
          kind: "world",
        });
        continue;
      }
      if (generatedAliases[alias] && generatedAliases[alias] !== target) {
        report.generatedAliasCollisions.push({
          alias,
          existingTarget: generatedAliases[alias],
          nextTarget: target,
          kind: "world",
        });
        continue;
      }
      generatedAliases[alias] = target;
    }
    report.worldRows.push({
      symbol: row.symbol,
      target,
      market: resolved.market,
      reutersCode: resolved.reuters_code,
      query: resolved.query,
      aliasCount: aliasSet.length,
    });
  }

  report.summary = {
    generatedLocalResultCount: Object.keys(generatedResults).length,
    generatedAliasCount: Object.keys(generatedAliases).length,
    unsupportedWorldRowCount: report.unsupportedWorldRows.length,
    curatedAliasCollisionCount: uniq(report.curatedAliasCollisions.map((item) => item.alias)).length,
    generatedAliasCollisionCount: uniq(report.generatedAliasCollisions.map((item) => item.alias)).length,
  };

  report.samples = {
    domesticRows: report.domesticRows.slice(0, 30),
    worldRows: report.worldRows.slice(0, 30),
    unsupportedWorldRows: report.unsupportedWorldRows.slice(0, 50),
    curatedAliasCollisions: report.curatedAliasCollisions.slice(0, 50),
    generatedAliasCollisions: report.generatedAliasCollisions.slice(0, 50),
  };

  delete report.domesticRows;
  delete report.worldRows;
  delete report.unsupportedWorldRows;
  delete report.curatedAliasCollisions;
  delete report.generatedAliasCollisions;

  for (const file of [
    args["output-generated-aliases"],
    args["output-generated-results"],
    args["output-report"],
  ]) {
    ensureDirFor(file);
  }

  fs.writeFileSync(
    args["output-generated-aliases"],
    `${JSON.stringify(sortObject(generatedAliases), null, 2)}\n`,
  );
  fs.writeFileSync(
    args["output-generated-results"],
    `${JSON.stringify(sortObject(generatedResults), null, 2)}\n`,
  );
  fs.writeFileSync(args["output-report"], `${JSON.stringify(report, null, 2)}\n`);

  console.log(JSON.stringify(report.summary, null, 2));
}

main().catch((error) => {
  usage();
  console.error(String(error && error.stack ? error.stack : error));
  process.exit(1);
});
