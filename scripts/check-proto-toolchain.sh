#!/usr/bin/env bash
set -euo pipefail

required_buf_version="${BUF_VERSION:?BUF_VERSION is required}"

if ! command -v buf >/dev/null 2>&1; then
  echo "buf is required" >&2
  exit 1
fi

actual_buf_version="$(buf --version)"
if [[ "${actual_buf_version}" != "${required_buf_version}" ]]; then
  echo "buf version mismatch: expected ${required_buf_version}, got ${actual_buf_version}" >&2
  exit 1
fi
