#!/bin/sh
set -eu

provider_arns=$(aws iam list-open-id-connect-providers --query 'OpenIDConnectProviderList[].Arn' --output text 2>/dev/null || true)

found_arn=""
for arn in $provider_arns; do
  url=$(aws iam get-open-id-connect-provider \
    --open-id-connect-provider-arn "$arn" \
    --query 'Url' \
    --output text 2>/dev/null || true)
  if [ "$url" = "token.actions.githubusercontent.com" ]; then
    found_arn=$arn
    break
  fi
done

printf '{"arn":"%s"}\n' "$found_arn"
