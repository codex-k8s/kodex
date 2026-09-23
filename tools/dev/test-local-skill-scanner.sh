#!/usr/bin/env bash
set -euo pipefail

# Изолированная проверка настоящего scanner; файлы пользователя не сканируются.
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
image=$(yq -r '.spec.template.spec.containers[] | select(.name == "skill-scanner") | .image' \
  "$repository_root/deploy/k8s/base/control-plane/deployment.yaml")
[[ "$image" =~ ^docker.io/clamav/clamav@sha256:[a-f0-9]{64}$ ]] || {
  echo 'Pinned skill scanner image is required' >&2
  exit 1
}
timeout 120 docker run --rm --network none --user "$(id -u):$(id -g)" \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  --tmpfs /tmp:rw,noexec,nosuid,size=128m \
  --tmpfs /run/kodex-skill-scanner:rw,noexec,nosuid,size=1m,mode=1777 \
  --mount "type=bind,source=$repository_root/deploy/k8s/base/control-plane/skill-scanner,target=/etc/kodex-skill-scanner,readonly" \
  --entrypoint /bin/sh "$image" -ec '
    config=/etc/kodex-skill-scanner/clamd.conf
    clamd --config-file="$config" >/tmp/clamd.log 2>&1 &
    scanner_pid=$!
    trap "kill $scanner_pid 2>/dev/null || true" EXIT
    clamdscan --config-file="$config" --ping=40:1 >/dev/null 2>&1
    sh /etc/kodex-skill-scanner/healthcheck.sh
    printf "Kodex harmless scanner probe\n" |
      clamdscan --config-file="$config" --stream --no-summary - >/dev/null
    printf "SCANNER_CLEAN_PASS\n"
    verdict=0
    printf "%s" '\''X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*'\'' |
      clamdscan --config-file="$config" --stream --no-summary - >/dev/null || verdict=$?
    [ "$verdict" -eq 1 ] || { echo "EICAR detection failed" >&2; exit 1; }
    printf "SCANNER_EICAR_REJECT_PASS\n"
  '
