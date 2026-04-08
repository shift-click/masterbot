#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";

const coinGeckoListURL = "https://api.coingecko.com/api/v3/coins/list?include_platform=false";
const unsupportedReferencePrefix = "UNSUPPORTED:";

const manualDocIDOverrides = {
  "axl-usdc": "axlusdc",
  havven: "SNX",
  iost: "iostoken",
  "pundi-x": "pundi-x-2",
  "render-token": "RENDER",
  "rocket-pool": "RPL",
  "safe-coin": "safe-coin-2",
  "simon - s - cat": "simon-s-cat",
  stormx: "storm",
  wemix: "wemix-token",
  wormhole: "W",
};

function usage() {
  console.error(
    [
      "Usage:",
      "  node scripts/coins/generate_coin_aliases_from_markdown.mjs",
      "    --input <coin_mappings.md>",
      "    --curated-aliases <coin_aliases.json>",
      "    --curated-local-results <coin_local_results.json>",
      "    --output-generated-aliases <coin_aliases.generated.json>",
      "    --output-generated-results <coin_local_results.generated.json>",
      "    --output-guarded <coin_aliases.guarded.json>",
      "    --output-report <coin_aliases.generated.report.json>",
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
    "output-guarded",
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
  let value = raw.replaceAll("**", "").replaceAll("`", "").trim();
  const guarded = value.includes("(+코인/토큰 필수)");
  value = value.replace(/\s*\(\+코인\/토큰 필수\)/g, "");
  value = normalizeWhitespace(value);
  return { value, guarded };
}

function parseMarkdownRows(markdown) {
  const rows = [];
  for (const line of markdown.split(/\r?\n/)) {
    if (!line.startsWith("|")) {
      continue;
    }
    const parts = line
      .split("|")
      .slice(1, -1)
      .map((part) => part.trim());
    if (parts.length < 2 || parts[0] === "기준 이름 (ID)" || parts[0] === "---") {
      continue;
    }
    const docID = parts[0].replaceAll("`", "").trim();
    const aliases = [];
    for (const token of parts[1].split(",")) {
      const parsed = cleanAlias(token);
      if (!parsed.value) {
        continue;
      }
      aliases.push(parsed);
    }
    rows.push({ docID, aliases });
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

function takeFirst(items, count) {
  return items.slice(0, count);
}

function uniqSorted(values) {
  return Array.from(new Set(values)).sort((a, b) => a.localeCompare(b));
}

function buildCuratedSearchIndex(curatedAliases, curatedLocalResults) {
  const index = new Map();
  const add = (term, target) => {
    const normalized = normalizeWhitespace(String(term || ""));
    if (!normalized) {
      return;
    }
    const key = normalized.toLowerCase();
    if (!index.has(key)) {
      index.set(key, new Set());
    }
    index.get(key).add(target);
  };

  for (const [canonicalKey, result] of Object.entries(curatedLocalResults)) {
    add(canonicalKey, canonicalKey);
    add(result.symbol, canonicalKey);
    add(result.name, canonicalKey);
    add(result.coingecko_id, canonicalKey);
  }

  for (const [alias, target] of Object.entries(curatedAliases)) {
    if (curatedLocalResults[target]) {
      add(alias, target);
    }
  }

  return index;
}

function resolveCuratedTarget(row, searchIndex) {
  const override = manualDocIDOverrides[row.docID];
  if (override) {
    return { target: override, reason: "manual_override" };
  }

  const hits = new Set();
  const docIDTargets = searchIndex.get(row.docID.toLowerCase());
  if (docIDTargets) {
    for (const target of docIDTargets) {
      hits.add(target);
    }
  }

  for (const alias of row.aliases) {
    const targets = searchIndex.get(alias.value.toLowerCase());
    if (!targets) {
      continue;
    }
    for (const target of targets) {
      hits.add(target);
    }
  }

  if (hits.size === 1) {
    return { target: Array.from(hits)[0], reason: "curated_match" };
  }
  if (hits.size > 1) {
    return { ambiguous: Array.from(hits).sort((a, b) => a.localeCompare(b)) };
  }
  return { target: "", reason: "none" };
}

async function fetchCoinGeckoList() {
  const response = await fetch(coinGeckoListURL, {
    headers: {
      accept: "application/json",
      "user-agent": "jucobot-reference-generator/1.0",
    },
  });
  if (!response.ok) {
    throw new Error(`CoinGecko list request failed: ${response.status} ${response.statusText}`);
  }
  const data = await response.json();
  if (!Array.isArray(data)) {
    throw new Error("CoinGecko list response is not an array");
  }
  return data;
}

function buildCoinGeckoIndex(items) {
  const byID = new Map();
  for (const item of items) {
    const id = normalizeWhitespace(item?.id || "");
    if (!id) {
      continue;
    }
    byID.set(id, {
      id,
      symbol: String(item.symbol || "").trim().toUpperCase(),
      name: normalizeWhitespace(String(item.name || "")),
    });
  }
  return byID;
}

function pickReferenceSymbol(row) {
  const candidates = row.aliases
    .map((alias) => alias.value)
    .filter((value) => /^[A-Za-z0-9+.-]{2,10}$/.test(value))
    .sort((a, b) => a.length - b.length || a.localeCompare(b));
  if (candidates.length > 0) {
    return candidates[0].replace(/[^A-Za-z0-9]/g, "").toUpperCase() || row.docID.toUpperCase();
  }
  return row.docID.replace(/[^A-Za-z0-9]/g, "").toUpperCase() || row.docID.toUpperCase();
}

function pickReferenceName(row) {
  const korean = row.aliases.find((alias) => /[가-힣]/.test(alias.value));
  if (korean) {
    return korean.value;
  }
  const spaced = row.docID.replace(/-/g, " ").trim();
  return spaced || row.docID;
}

function ensureDirFor(filePath) {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
}

async function main() {
  const args = parseArgs(process.argv);
  const markdown = fs.readFileSync(args.input, "utf8");
  const curatedAliases = readJSON(args["curated-aliases"]);
  const curatedLocalResults = readJSON(args["curated-local-results"]);
  const rows = parseMarkdownRows(markdown);
  const searchIndex = buildCuratedSearchIndex(curatedAliases, curatedLocalResults);
  const coinGeckoIndex = buildCoinGeckoIndex(await fetchCoinGeckoList());

  const generatedAliases = {};
  const generatedResults = {};
  const guardedAliases = {};
  const report = {
    source: path.resolve(args.input),
    coinGeckoListURL,
    generatedAt: new Date().toISOString(),
    rowCount: rows.length,
    manualOverrides: sortObject(manualDocIDOverrides),
    summary: {},
    samples: {},
    curatedReuseRows: [],
    generatedCatalogRows: [],
    referenceOnlyRows: [],
    ambiguousCuratedRows: [],
    guardedRows: [],
    curatedAliasCollisions: [],
    generatedAliasCollisions: [],
    generatedLocalResultCollisions: [],
  };

  for (const row of rows) {
    const curatedResolution = resolveCuratedTarget(row, searchIndex);
    if (curatedResolution.ambiguous) {
      if (coinGeckoIndex.has(row.docID)) {
        report.ambiguousCuratedRows.push({
          docID: row.docID,
          candidates: curatedResolution.ambiguous,
          action: "generated_catalog_fallback",
        });
      } else {
        report.ambiguousCuratedRows.push({
          docID: row.docID,
          candidates: curatedResolution.ambiguous,
          action: "reference_only_fallback",
        });
      }
    }

    let target = curatedResolution.target;
    let targetReason = curatedResolution.reason;
    if (!target) {
      const coin = coinGeckoIndex.get(row.docID);
      if (!coin) {
        target = row.docID;
        targetReason = "reference_only";
        if (!curatedLocalResults[target] && !generatedResults[target]) {
          generatedResults[target] = {
            symbol: pickReferenceSymbol(row),
            name: pickReferenceName(row),
            tier: "coingecko",
            coingecko_id: `${unsupportedReferencePrefix}${row.docID}`,
          };
        }
      } else {
        target = row.docID;
        targetReason = "generated_catalog";
        if (!curatedLocalResults[target] && !generatedResults[target]) {
          generatedResults[target] = {
            symbol: coin.symbol || row.docID.toUpperCase(),
            name: coin.name || row.docID,
            tier: "coingecko",
            coingecko_id: coin.id,
          };
        } else if (generatedResults[target]) {
          report.generatedLocalResultCollisions.push({
            docID: row.docID,
            target,
            reason: "duplicate_generated_target",
          });
        }
      }
    }
    if (!curatedLocalResults[target] && !generatedResults[target]) {
      const coin = coinGeckoIndex.get(target) || coinGeckoIndex.get(row.docID);
      if (!coin) {
        generatedResults[target] = {
          symbol: pickReferenceSymbol(row),
          name: pickReferenceName(row),
          tier: "coingecko",
          coingecko_id: `${unsupportedReferencePrefix}${row.docID}`,
        };
        targetReason = "reference_only";
      } else {
        generatedResults[target] = {
          symbol: coin.symbol || target.toUpperCase(),
          name: coin.name || target,
          tier: "coingecko",
          coingecko_id: coin.id,
        };
      }
    }

    const mappingEntry = {
      docID: row.docID,
      target,
      reason: targetReason,
      aliases: [],
    };
    const guardedEntry = {
      docID: row.docID,
      target,
      aliases: [],
    };

    for (const alias of row.aliases) {
      if (alias.guarded) {
        guardedEntry.aliases.push(alias.value);
        if (!guardedAliases[alias.value]) {
          guardedAliases[alias.value] = target;
        }
        continue;
      }

      mappingEntry.aliases.push(alias.value);
      if (curatedAliases[alias.value]) {
        report.curatedAliasCollisions.push({
          alias: alias.value,
          target,
          curatedTarget: curatedAliases[alias.value],
          docID: row.docID,
        });
        continue;
      }
      if (curatedLocalResults[alias.value] && alias.value !== target) {
        report.curatedAliasCollisions.push({
          alias: alias.value,
          target,
          curatedTarget: alias.value,
          docID: row.docID,
        });
        continue;
      }
      if (generatedAliases[alias.value] && generatedAliases[alias.value] !== target) {
        report.generatedAliasCollisions.push({
          alias: alias.value,
          existingTarget: generatedAliases[alias.value],
          nextTarget: target,
          docID: row.docID,
        });
        continue;
      }
      generatedAliases[alias.value] = target;
    }

    if (targetReason === "generated_catalog" || targetReason === "reference_only") {
      report.generatedCatalogRows.push(mappingEntry);
    } else {
      report.curatedReuseRows.push(mappingEntry);
    }
    if (targetReason === "reference_only") {
      report.referenceOnlyRows.push({
        docID: row.docID,
        target,
        aliases: row.aliases.map((alias) => alias.value),
        reason: "reference_only_fallback",
      });
    }
    if (guardedEntry.aliases.length > 0) {
      report.guardedRows.push(guardedEntry);
    }
  }

  report.summary = {
    generatedLocalResultCount: Object.keys(generatedResults).length,
    generatedAliasCount: Object.keys(generatedAliases).length,
    guardedAliasCount: Object.keys(guardedAliases).length,
    curatedReuseCount: report.curatedReuseRows.length,
    generatedCatalogCount: report.generatedCatalogRows.length,
    referenceOnlyRowCount: report.referenceOnlyRows.length,
    ambiguousCuratedCount: report.ambiguousCuratedRows.length,
    curatedAliasCollisionCount: uniqSorted(report.curatedAliasCollisions.map((item) => item.alias)).length,
    generatedAliasCollisionCount: uniqSorted(report.generatedAliasCollisions.map((item) => item.alias)).length,
  };

  report.samples = {
    curatedReuseRows: takeFirst(report.curatedReuseRows, 30),
    generatedCatalogRows: takeFirst(report.generatedCatalogRows, 30),
    referenceOnlyRows: takeFirst(report.referenceOnlyRows, 50),
    guardedRows: takeFirst(report.guardedRows, 50),
    curatedAliasCollisions: takeFirst(report.curatedAliasCollisions, 50),
    generatedAliasCollisions: takeFirst(report.generatedAliasCollisions, 50),
  };

  delete report.curatedReuseRows;
  delete report.generatedCatalogRows;
  delete report.referenceOnlyRows;
  delete report.guardedRows;
  delete report.curatedAliasCollisions;
  delete report.generatedAliasCollisions;

  for (const file of [
    args["output-generated-aliases"],
    args["output-generated-results"],
    args["output-guarded"],
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
  fs.writeFileSync(
    args["output-guarded"],
    `${JSON.stringify(sortObject(guardedAliases), null, 2)}\n`,
  );
  fs.writeFileSync(args["output-report"], `${JSON.stringify(report, null, 2)}\n`);

  console.log(JSON.stringify(report.summary, null, 2));
}

main().catch((error) => {
  usage();
  console.error(String(error && error.stack ? error.stack : error));
  process.exit(1);
});
