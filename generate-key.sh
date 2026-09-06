#!/usr/bin/env bash
# Generate a sponsor token for a Jenkins username.
# Usage: ./generate-key.sh <username>
# Requires: sponsor_private.pem (Ed25519 private key, never commit this)
set -euo pipefail

PRIVATE_KEY="${SPONSOR_PRIVATE_KEY:-$(dirname "$0")/sponsor_private.pem}"
USERNAME="${1:?Usage: $0 <username>}"

TMPFILE=$(mktemp)
trap 'rm -f "$TMPFILE"' EXIT

printf '%s' "${USERNAME,,}" > "$TMPFILE"
openssl pkeyutl -sign -inkey "$PRIVATE_KEY" -rawin -in "$TMPFILE" | xxd -p -c 256
