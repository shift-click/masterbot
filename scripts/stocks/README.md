# Stock Reference Asset Generation

`stock_mappings.md`는 런타임에서 직접 읽지 않는다. 먼저 generated local results, generated alias asset, report로 변환한 뒤, 런타임 loader가 curated asset과 병합한다.

## Regenerate

```bash
node scripts/stocks/generate_naver_assets_from_markdown.mjs \
  --input /absolute/path/to/stock_mappings.md \
  --curated-aliases internal/scraper/providers/data/naver_aliases.json \
  --curated-local-results internal/scraper/providers/data/naver_local_results.json \
  --output-generated-aliases internal/scraper/providers/data/naver_aliases.generated.json \
  --output-generated-results internal/scraper/providers/data/naver_local_results.generated.json \
  --output-report internal/scraper/providers/data/naver_generated.report.json
```

## Expected Outputs

- `naver_local_results.generated.json`: runtime에 병합되는 generated domestic/world stock local registry
- `naver_aliases.generated.json`: runtime에 병합되는 generated exact alias
- `naver_generated.report.json`: Reuters lookup, unsupported world row, collision 현황 리포트
