#!/usr/bin/env bash
set -euo pipefail

required_buf_version="${BUF_VERSION:?BUF_VERSION is required}"
required_protobuf_go_remote="${PROTOBUF_GO_PLUGIN_REMOTE:?PROTOBUF_GO_PLUGIN_REMOTE is required}"
required_protobuf_go_revision="${PROTOBUF_GO_PLUGIN_REVISION:?PROTOBUF_GO_PLUGIN_REVISION is required}"
required_grpc_go_remote="${GRPC_GO_PLUGIN_REMOTE:?GRPC_GO_PLUGIN_REMOTE is required}"
required_grpc_go_revision="${GRPC_GO_PLUGIN_REVISION:?GRPC_GO_PLUGIN_REVISION is required}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"

if ! command -v buf >/dev/null 2>&1; then
  echo "buf is required" >&2
  exit 1
fi

actual_buf_version="$(buf --version)"
if [[ "${actual_buf_version}" != "${required_buf_version}" ]]; then
  echo "buf version mismatch: expected ${required_buf_version}, got ${actual_buf_version}" >&2
  exit 1
fi

check_remote_plugin() {
  local required_remote="$1"
  local required_revision="$2"
  local actual_revision

  actual_revision="$(
    awk -v required_remote="${required_remote}" '
      $1 == "-" && $2 == "remote:" {
        current_remote = $3
        next
      }
      current_remote == required_remote && $1 == "revision:" {
        if (found) {
          exit 2
        }
        found = 1
        revision = $2
        next
      }
      END {
        if (!found) {
          exit 1
        }
        print revision
      }
    ' "${repo_root}/buf.gen.yaml"
  )" || {
    echo "remote plugin contract missing or duplicated: ${required_remote}" >&2
    exit 1
  }

  if [[ "${actual_revision}" != "${required_revision}" ]]; then
    echo "remote plugin revision mismatch for ${required_remote}: expected ${required_revision}, got ${actual_revision}" >&2
    exit 1
  fi
}

check_remote_plugin "${required_protobuf_go_remote}" "${required_protobuf_go_revision}"
check_remote_plugin "${required_grpc_go_remote}" "${required_grpc_go_revision}"
