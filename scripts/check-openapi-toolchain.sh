#!/usr/bin/env bash
set -euo pipefail

required_version="${OAPI_CODEGEN_VERSION:?OAPI_CODEGEN_VERSION is required}"

if ! command -v oapi-codegen >/dev/null 2>&1; then
  echo 'oapi-codegen is required' >&2
  exit 1
fi

actual_version="$(oapi-codegen -version 2>/dev/null | tail -n 1)"
if [[ "${actual_version}" != "${required_version}" ]]; then
  echo "oapi-codegen version mismatch: expected ${required_version}, got ${actual_version:-unknown}" >&2
  exit 1
fi
