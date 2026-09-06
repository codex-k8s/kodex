#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
guard="$repository_root/scripts/check-go-toolchain.sh"

"$guard" >/dev/null

jq -e '
  .schema_version == 2 and
  (.images | length > 0) and
  ([.images[].component] | length == (unique | length)) and
  all(.images[];
    (.component | test("^[a-z0-9-]+$")) and
    (.dockerfile | type == "string" and length > 0) and
    ((has("target") | not) or
      (.component == "role-image-builder" and .target == "runtime") or
      (.component == "email-bridge" and .target == "runtime") or
      (.component == "email-bridge-migration" and .target == "migration") or
      (.component == "image-admission" and .target == "admission-runtime")))
' "$repository_root/tools/release/images.json" >/dev/null || {
  printf 'Release image inventory is invalid\n' >&2
  exit 1
}

while IFS=$'\t' read -r component dockerfile; do
	"$repository_root/tools/release/validate-image-dockerfile-path.sh" "$dockerfile" || {
		printf 'Dockerfile path is invalid for %s: %s\n' "$component" "$dockerfile" >&2
		exit 1
	}
  dockerfile_path="$repository_root/$dockerfile"
  [[ -f "$dockerfile_path" ]] || {
    printf 'Dockerfile is absent for %s\n' "$component" >&2
    exit 1
  }

  module_directory=$(dirname -- "$dockerfile_path")
  module_file="$module_directory/go.mod"
  [[ -f "$module_file" ]] && grep -Fq 'go mod download' "$dockerfile_path" || continue

  while IFS= read -r replacement_path; do
    [[ -n "$replacement_path" ]] || continue
    replacement_directory=$(realpath -m -- "$module_directory/$replacement_path")
    [[ "$replacement_directory" == "$repository_root"/* && -f "$replacement_directory/go.mod" ]] || {
      printf 'Local Go replacement is invalid for %s: %s\n' "$component" "$replacement_path" >&2
      exit 1
    }
    repository_relative_path=${replacement_directory#"$repository_root/"}
    if ! grep -Fq "COPY $repository_relative_path/" "$dockerfile_path" &&
       ! grep -Fq "COPY $repository_relative_path/go.mod " "$dockerfile_path" &&
       ! grep -Fq 'COPY libs/go ' "$dockerfile_path" &&
       ! grep -Fq 'COPY libs/go/ ' "$dockerfile_path"; then
      printf 'Dockerfile does not materialize local Go replacement for %s: %s\n' \
        "$component" "$repository_relative_path" >&2
      exit 1
    fi
  done < <(awk \
    '$1 == "replace" && $2 ~ /^github\.com\/codex-k8s\/kodex\/libs\/go\// && $3 == "=>" {print $4}' \
    "$module_file")
done < <(jq -r '.images[] | [.component,.dockerfile] | @tsv' "$repository_root/tools/release/images.json")

agent_runner_dockerfile="$repository_root/services/jobs/agent-runner/Dockerfile"
mapfile -t guard_payloads < <(awk -F "'" \
  '$0 ~ /base64 -d > \/tmp\/kodex-go-toolchain-guard\.go/ {print $4}' \
  "$agent_runner_dockerfile")
[[ ${#guard_payloads[@]} -eq 1 && -n "${guard_payloads[0]}" ]] || {
  printf 'Agent runner toolchain guard payload is absent or ambiguous\n' >&2
  exit 1
}
guard_payload=${guard_payloads[0]}
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
guard_source="$temporary_directory/kodex-go-toolchain-guard.go"
printf '%s' "$guard_payload" | base64 -d >"$guard_source"
if rg -q 'mattercodex|matter-codex' "$guard_source"; then
  printf 'Agent runner toolchain guard contains a retired runtime name\n' >&2
  exit 1
fi
for required_value in \
  /opt/kodex/bootstrap-go \
  /opt/kodex/protected-artifacts \
  kodex-go-toolchain-guard \
  kodex-init \
  kodex-agent-runner; do
  grep -Fq "$required_value" "$guard_source" || {
    printf 'Agent runner toolchain guard misses required value: %s\n' "$required_value" >&2
    exit 1
  }
done
env -u GOFLAGS GOENV=off GOTOOLCHAIN=local GOWORK=off \
  go build -trimpath -buildvcs=false -o "$temporary_directory/kodex-go-toolchain-guard" "$guard_source"

if rg -q 'bot-service|legacy-data-migration' \
  "$repository_root/tools/release/images.json" "$repository_root/Makefile"; then
  printf 'Retired unit returned to an active entrypoint\n' >&2
  exit 1
fi

printf 'Go toolchain contract tests passed\n'
