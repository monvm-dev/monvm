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
mkdir -p "$state/sources"

validate_bundle() {
  local bundle=$1 certificates authorities
  [[ -s "$bundle" ]] || return 1
  [[ $(wc -c <"$bundle") -le 1048576 ]] || return 1
  ! grep -q 'PRIVATE KEY' "$bundle" || return 1
  certificates=$(grep -c '^-----BEGIN CERTIFICATE-----$' "$bundle")
  [[ "$certificates" -gt 0 ]] || return 1
  [[ $(grep -c '^-----END CERTIFICATE-----$' "$bundle") -eq "$certificates" ]] || return 1
  openssl crl2pkcs7 -nocrl -certfile "$bundle" 2>/dev/null |
    openssl pkcs7 -print_certs -noout >/dev/null 2>&1 || return 1
  authorities=$(openssl crl2pkcs7 -nocrl -certfile "$bundle" 2>/dev/null |
    openssl pkcs7 -print_certs -text 2>/dev/null | grep -c 'CA:TRUE' || true)
  [[ "$authorities" -eq "$certificates" ]]
}

if [[ -f "$known" ]]; then
  while IFS= read -r key; do
    [[ -n "$key" ]] || continue
    source_id=${key#trust/}
    source_id=${source_id%.pem}
    candidate=$(mktemp "$state/.bundle.XXXXXX")
    error=$(mktemp "$state/.error.XXXXXX")
    if aws s3api get-object --bucket "$MONVM_BUCKET" --key "$key" "$candidate" \
      --region "$MONVM_REGION" --endpoint-url "$MONVM_S3_ENDPOINT" >/dev/null 2>"$error"; then
      if validate_bundle "$candidate"; then
        install -m 0644 "$candidate" "$state/sources/$source_id.pem.new"
        mv -f "$state/sources/$source_id.pem.new" "$state/sources/$source_id.pem"
      else
        echo "Trust bundle $key is invalid; retaining its last-known-good version." >&2
      fi
    elif grep -Eq 'NoSuchKey|Not Found|404' "$error"; then
      rm -f "$state/sources/$source_id.pem"
    else
      echo "Unable to refresh trust bundle $key; retaining its last-known-good version." >&2
    fi
    rm -f "$candidate" "$error"
  done <"$known"
fi

combined=$(mktemp "$state/.combined.XXXXXX")
shopt -s nullglob
bundles=("$state/sources/"*.pem)
if [[ ${#bundles[@]} -eq 0 ]]; then
  cat "$state/deny-all.pem" >"$combined"
else
  cat "${bundles[@]}" >"$combined"
fi
if ! validate_bundle "$combined"; then
  echo 'Combined trust bundle is invalid; retaining the previous bundle.' >&2
  rm -f "$combined"
  exit 0
fi
if [[ ! -f "$state/bundle.pem" ]] || ! cmp -s "$combined" "$state/bundle.pem"; then
  chmod 0644 "$combined"
  mv -f "$combined" "$state/bundle.pem"
else
  rm -f "$combined"
fi
