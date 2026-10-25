#!/usr/bin/env bash
set -euo pipefail
umask 077
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
for command in docker yq go timeout python3; do
  command -v "$command" >/dev/null || { echo 'Required NATS component tool is unavailable' >&2; exit 1; }
done
fixture_image=$(yq -r '.spec.template.spec.containers[] | select(.name == "nats") | .image' "$repository_root/deploy/k8s/base/platform-state/nats.yaml")
[[ "$fixture_image" =~ ^docker\.io/library/nats:[0-9.]+-alpine@sha256:[a-f0-9]{64}$ ]] || { echo 'Canonical NATS image is not pinned' >&2; exit 1; }
timeout 10s docker image inspect "$fixture_image" >/dev/null 2>&1 || { echo 'Pinned disposable NATS image is unavailable' >&2; exit 1; }
fixture_directory=$(mktemp -d "${TMPDIR:-/tmp}/kodex-api-nats.XXXXXX")
fixture_container="kodex-api-nats-$(basename "$fixture_directory")"
cleanup() {
  component_exit=$?
  if [[ -f "$fixture_directory/container.json" && ! -L "$fixture_directory/container.json" ]]; then
    fixture_id=$(python3 - "$fixture_directory/container.json" <<'PY'
import json,re,sys
value=json.load(open(sys.argv[1],encoding='utf-8'))
identity=value.get('id','')
if not re.fullmatch('[a-f0-9]{64}',identity): raise SystemExit(1)
print(identity)
PY
    ) || { echo 'NATS fixture cleanup identity is invalid' >&2; exit 1; }
    actual_identity=$(timeout 10s docker inspect --format '{{.Id}} {{.Name}} {{index .Config.Labels "kodex.qa.fixture"}}' "$fixture_id" 2>/dev/null) || actual_identity=''
    if [[ "$actual_identity" == "$fixture_id /$fixture_container control-api-nats-startup" ]]; then
      if timeout 10s docker rm -f "$fixture_id" >/dev/null 2>&1; then
        remaining=$(timeout 10s docker container ls -a --no-trunc --format '{{.ID}} {{.Names}}') || { echo 'NATS fixture cleanup readback unavailable' >&2; exit 1; }
        if python3 - "$fixture_id" "$fixture_container" "$remaining" <<'PYREADBACK'
import sys
for line in sys.argv[3].splitlines():
    parts=line.split()
    if len(parts)==2 and (parts[0]==sys.argv[1] or parts[1]==sys.argv[2]): raise SystemExit(1)
PYREADBACK
        then
          printf 'Disposable NATS container id=%s cleanup=absent\n' "$fixture_id"
        else
          echo 'NATS fixture cleanup remains incomplete' >&2
          component_exit=1
        fi
      else
        echo 'NATS fixture cleanup removal failed' >&2
        component_exit=1
      fi
    else
      echo 'NATS fixture cleanup ownership readback failed' >&2
      component_exit=1
    fi
  fi
  if [[ "$component_exit" == 0 ]]; then
    rm -rf -- "$fixture_directory"
  else
    printf 'NATS component evidence retained at %s\n' "$fixture_directory" >&2
  fi
  exit "$component_exit"
}
trap cleanup EXIT
(
  cd "$repository_root/services/external/control-api-gateway"
  KODEX_API_NATS_TEST_DIRECTORY="$fixture_directory" KODEX_API_NATS_TEST_CONTAINER="$fixture_container" KODEX_API_NATS_TEST_IMAGE="$fixture_image" \
    GOTOOLCHAIN=go1.26.6 GOMAXPROCS=2 timeout --kill-after=5s 110s go test -p=1 -count=1 -timeout=90s ./internal/app -run '^TestNATSStartupDiagnosticComponent$' -v
)
echo 'Control API native NATS JWT and TLS diagnostics completed'
