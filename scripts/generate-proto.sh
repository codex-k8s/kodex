#!/usr/bin/env bash
set -euo pipefail

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/.." && pwd -P)
protobuf_go_version="${PROTOBUF_GO_PLUGIN_LOCAL_VERSION:?PROTOBUF_GO_PLUGIN_LOCAL_VERSION is required}"
grpc_go_version="${GRPC_GO_PLUGIN_LOCAL_VERSION:?GRPC_GO_PLUGIN_LOCAL_VERSION is required}"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
remote_error="$temporary_directory/remote-error.log"

if (cd -- "$repository_root" && buf generate "$@" 2>"$remote_error"); then
  exit 0
fi

if ! grep -Fq 'resource_exhausted: too many requests' "$remote_error"; then
  cat "$remote_error" >&2
  exit 1
fi

command -v protoc-gen-go >/dev/null 2>&1 || {
  printf 'protoc-gen-go is required for the rate-limit fallback\n' >&2
  exit 1
}
command -v protoc-gen-go-grpc >/dev/null 2>&1 || {
  printf 'protoc-gen-go-grpc is required for the rate-limit fallback\n' >&2
  exit 1
}
[[ "$(protoc-gen-go --version)" == "protoc-gen-go v$protobuf_go_version" ]] || {
  printf 'protoc-gen-go version mismatch for the rate-limit fallback\n' >&2
  exit 1
}
[[ "$(protoc-gen-go-grpc --version)" == "protoc-gen-go-grpc $grpc_go_version" ]] || {
  printf 'protoc-gen-go-grpc version mismatch for the rate-limit fallback\n' >&2
  exit 1
}

printf 'Buf remote plugin rate limit reached; using exact local plugins\n' >&2
(cd -- "$repository_root" && buf generate --template buf.gen.local.yaml "$@")
