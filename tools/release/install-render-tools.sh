#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Release render tool installation failed: %s\n' "$*" >&2
  exit 1
}

kubectl_version=v1.35.5
kubectl_sha256=90f75ea6ecc9ea5633262e1c0b83a40560003b30fc94a04cb099404fcef0c224
yq_version=v4.53.6
yq_sha256=c5f056448f973ae7d39b5401949648a78f2dc1947d6a8eb65be60d5c504b9385

for command_name in curl install jq mktemp sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ -n "${RUNNER_TEMP:-}" && -d "$RUNNER_TEMP" ]] || fail 'RUNNER_TEMP is required'
[[ -n "${GITHUB_PATH:-}" && -f "$GITHUB_PATH" ]] || fail 'GITHUB_PATH is required'

temporary_directory=$(mktemp -d "$RUNNER_TEMP/mattercodex-render-tools.XXXXXX")
trap 'rm -rf -- "$temporary_directory"' EXIT
output_directory="$RUNNER_TEMP/mattercodex-render-tools-bin"
mkdir -p -- "$output_directory"

download() {
  local url=$1 output=$2 expected_sha256=$3
  curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error \
    --retry 3 --retry-delay 2 --output "$output" "$url"
  printf '%s  %s\n' "$expected_sha256" "$output" | sha256sum --check --status ||
    fail "download checksum mismatch: $(basename -- "$output")"
}

download "https://dl.k8s.io/release/$kubectl_version/bin/linux/amd64/kubectl" \
  "$temporary_directory/kubectl" "$kubectl_sha256"
download "https://github.com/mikefarah/yq/releases/download/$yq_version/yq_linux_amd64" \
  "$temporary_directory/yq" "$yq_sha256"

install -m 0555 "$temporary_directory/kubectl" "$output_directory/kubectl"
install -m 0555 "$temporary_directory/yq" "$output_directory/yq"

actual_kubectl_version=$(
  "$output_directory/kubectl" version --client -o json | jq -er '.clientVersion.gitVersion'
)
[[ "$actual_kubectl_version" == "$kubectl_version" ]] || fail 'kubectl version mismatch'
"$output_directory/yq" --version | grep -Fq "version $yq_version" || fail 'yq version mismatch'

printf '%s\n' "$output_directory" >>"$GITHUB_PATH"
printf 'Release render tools installed\n'
