# Coin Reference Asset Generation

`coin_mappings.md`는 런타임에서 직접 읽지 않는다. 먼저 generated coin catalog, generated alias asset, guarded alias asset, report로 변환한 뒤, 런타임 loader가 curated asset과 병합한다.

## Regenerate

```bash
node scripts/coins/generate_coin_aliases_from_markdown.mjs \
  --input /absolute/path/to/coin_mappings.md \
  --curated-aliases internal/scraper/providers/data/coin_aliases.json \
  --curated-local-results internal/scraper/providers/data/coin_local_results.json \
  --output-generated-aliases internal/scraper/providers/data/coin_aliases.generated.json \
  --output-generated-results internal/scraper/providers/data/coin_local_results.generated.json \
  --output-guarded internal/scraper/providers/data/coin_aliases.guarded.json \
  --output-report internal/scraper/providers/data/coin_aliases.generated.report.json
```

## Expected Outputs

- `coin_local_results.generated.json`: runtime에 병합되는 net-new CoinGecko-tier local registry
- `coin_aliases.generated.json`: runtime에 병합되는 net-new exact alias
- `coin_aliases.guarded.json`: `(+코인/토큰 필수)`로 분리된 alias
- `coin_aliases.generated.report.json`: generated catalog, reference-only fallback, collision, override 현황 리포트
