#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)

SONAR_HOST_URL=${SONAR_HOST_URL:-http://localhost:9000}
SONAR_PROJECT_KEY=${SONAR_PROJECT_KEY:-github.com.munawiki:jucobot-v2}
MIN_TOTAL_COVERAGE=${MIN_TOTAL_COVERAGE:-75}
MAX_DUPLICATION_DENSITY=${MAX_DUPLICATION_DENSITY:-1.0}
MAX_OPEN_ISSUES=${MAX_OPEN_ISSUES:-0}
MAX_CRITICAL_ISSUES=${MAX_CRITICAL_ISSUES:-0}
MIN_CORE_PACKAGE_COVERAGE=${MIN_CORE_PACKAGE_COVERAGE:-70}

if [ -z "${SONAR_TOKEN:-}" ]; then
  echo "[quality-gate] SONAR_TOKEN is required" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "[quality-gate] jq is required" >&2
  exit 1
fi

auth="Authorization: Basic $(printf '%s:' "$SONAR_TOKEN" | base64)"
api() {
  curl -fsS -H "$auth" "$@"
}

project_url="$SONAR_HOST_URL/api/measures/component?component=$SONAR_PROJECT_KEY&metricKeys=coverage,duplicated_lines_density,code_smells,bugs,vulnerabilities"
issues_url="$SONAR_HOST_URL/api/issues/search?componentKeys=$SONAR_PROJECT_KEY&resolved=false&ps=1"
critical_url="$SONAR_HOST_URL/api/issues/search?componentKeys=$SONAR_PROJECT_KEY&resolved=false&severities=CRITICAL&ps=1"
dirs_url="$SONAR_HOST_URL/api/measures/component_tree?component=$SONAR_PROJECT_KEY&metricKeys=coverage&qualifiers=DIR&ps=500"

project_json=$(api "$project_url")
issues_json=$(api "$issues_url")
critical_json=$(api "$critical_url")
dirs_json=$(api "$dirs_url")

coverage=$(printf '%s' "$project_json" | jq -r '.component.measures[] | select(.metric=="coverage") | .value')
dup_density=$(printf '%s' "$project_json" | jq -r '.component.measures[] | select(.metric=="duplicated_lines_density") | .value')
open_issues=$(printf '%s' "$issues_json" | jq -r '.total')
critical_issues=$(printf '%s' "$critical_json" | jq -r '.total')

echo "[quality-gate] project=$SONAR_PROJECT_KEY"
echo "[quality-gate] coverage=${coverage}% (target >= ${MIN_TOTAL_COVERAGE}%)"
echo "[quality-gate] duplicated_lines_density=${dup_density}% (target <= ${MAX_DUPLICATION_DENSITY}%)"
echo "[quality-gate] open_issues=${open_issues} (target <= ${MAX_OPEN_ISSUES})"
echo "[quality-gate] critical_issues=${critical_issues} (target <= ${MAX_CRITICAL_ISSUES})"

failed=0
if ! awk -v v="$coverage" -v min="$MIN_TOTAL_COVERAGE" 'BEGIN{exit(v+0 >= min+0 ? 0 : 1)}'; then
  echo "[quality-gate] fail: total coverage below target" >&2
  failed=1
fi
if ! awk -v v="$dup_density" -v max="$MAX_DUPLICATION_DENSITY" 'BEGIN{exit(v+0 <= max+0 ? 0 : 1)}'; then
  echo "[quality-gate] fail: duplication density above target" >&2
  failed=1
fi
if ! awk -v v="$open_issues" -v max="$MAX_OPEN_ISSUES" 'BEGIN{exit(v+0 <= max+0 ? 0 : 1)}'; then
  echo "[quality-gate] fail: open issues above target" >&2
  failed=1
fi
if ! awk -v v="$critical_issues" -v max="$MAX_CRITICAL_ISSUES" 'BEGIN{exit(v+0 <= max+0 ? 0 : 1)}'; then
  echo "[quality-gate] fail: critical issues above target" >&2
  failed=1
fi

check_core_dir() {
  dir_path="$1"
  value=$(printf '%s' "$dirs_json" | jq -r --arg p "$dir_path" '.components[] | select(.path==$p) | (.measures[]? | select(.metric=="coverage") | .value)')
  if [ -z "$value" ]; then
    echo "[quality-gate] fail: missing coverage metric for $dir_path" >&2
    failed=1
    return
  fi
  echo "[quality-gate] $dir_path coverage=${value}% (target >= ${MIN_CORE_PACKAGE_COVERAGE}%)"
  if ! awk -v v="$value" -v min="$MIN_CORE_PACKAGE_COVERAGE" 'BEGIN{exit(v+0 >= min+0 ? 0 : 1)}'; then
    echo "[quality-gate] fail: $dir_path coverage below target" >&2
    failed=1
  fi
}

check_core_dir "internal/app"
check_core_dir "internal/command"
check_core_dir "internal/scraper/providers"
check_core_dir "internal/metrics"

if [ "$failed" -ne 0 ]; then
  exit 1
fi

echo "[quality-gate] passed"

