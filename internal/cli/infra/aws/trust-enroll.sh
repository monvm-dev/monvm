#!/usr/bin/env bash
# MonVM <https://monvm.dev>
# Copyright The MonVM Authors
# SPDX-License-Identifier: Apache-2.0

set -Eeuo pipefail
# The environment file path is overridden only by the isolated test harness.
# shellcheck disable=SC1090
source "${MONVM_TRUST_ENV:-/etc/monvm/trust.env}"

state=${MONVM_TRUST_STATE:?MONVM_TRUST_STATE must be set}
known="$state/known-keys"
listing=$(mktemp)
candidate=$(mktemp)
trap 'rm -f "$listing" "$candidate"' EXIT

if aws s3api list-objects-v2 --bucket "$MONVM_BUCKET" --prefix trust/ --max-keys 1000 \
  --region "$MONVM_REGION" --endpoint-url "$MONVM_S3_ENDPOINT" --output json >"$listing"; then
  if ! python3 - "$listing" >"$candidate" <<'PYTHON'
import json
import re
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    listing = json.load(source)
if listing.get("IsTruncated", False):
    raise SystemExit("client CA store contains more than 1,000 objects")
pattern = re.compile(r"^trust/[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.pem$")
keys = sorted(item["Key"] for item in listing.get("Contents", []) if pattern.fullmatch(item.get("Key", "")))
for key in keys:
    print(key)
PYTHON
  then
    echo 'MonVM client CA store listing is invalid; retaining the previous enrollment.' >&2
  else
    install -m 0600 "$candidate" "$known.new"
    mv -f "$known.new" "$known"
    shopt -s nullglob
    for bundle in "$state/sources/"*.pem; do
      source_id=${bundle##*/}
      source_id=${source_id%.pem}
      if ! grep -Fqx "trust/$source_id.pem" "$known"; then
        rm -f "$bundle"
      fi
    done
  fi
else
  echo 'Unable to enumerate the MonVM client CA store; retaining the previous enrollment.' >&2
fi

exec "${MONVM_TRUST_REFRESH:-/usr/local/sbin/monvm-trust-refresh}"
