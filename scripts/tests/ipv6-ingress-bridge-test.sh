#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex IPv6 ingress bridge test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
bridge_script="$repository_root/tools/install/configure-ipv6-ingress-bridge.sh"
temporary_directory=$(mktemp -d)
cleanup() { rm -rf -- "$temporary_directory"; }
trap cleanup EXIT

fake_bin="$temporary_directory/bin"
state_directory="$temporary_directory/systemd-state"
systemd_log="$temporary_directory/systemd.log"
install_root="$temporary_directory/root"
mkdir -p "$fake_bin" "$state_directory" "$install_root"

cat >"$fake_bin/systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
state_directory=${KODEX_TEST_SYSTEMD_STATE:?}
command_name=${1:-}
shift || true
printf '%s %s\n' "$command_name" "$*" >>"${KODEX_TEST_SYSTEMD_LOG:?}"
case "$command_name" in
  daemon-reload) ;;
  enable)
    start_now=false
    if [[ "${1:-}" == --now ]]; then
      start_now=true
      shift
    fi
    for unit in "$@"; do
      : >"$state_directory/$unit.enabled"
      [[ "$start_now" == false ]] || : >"$state_directory/$unit.active"
    done
    ;;
  restart)
    for unit in "$@"; do
      : >"$state_directory/$unit.active"
      if [[ "${KODEX_TEST_NEW_PROXY_FAILURE:-false}" == true ]]; then
        : >"$state_directory/${unit%.socket}.service.failed"
      fi
    done
    ;;
  disable)
    [[ "${1:-}" == --now ]] && shift
    for unit in "$@"; do
      rm -f -- "$state_directory/$unit.enabled" "$state_directory/$unit.active"
    done
    ;;
  stop)
    for unit in "$@"; do
      rm -f -- "$state_directory/$unit.active"
    done
    ;;
  reset-failed)
    for unit in "$@"; do
      rm -f -- "$state_directory/$unit.failed"
    done
    ;;
  is-enabled)
    [[ "${1:-}" == --quiet ]] && shift
    [[ -f "$state_directory/${1:?}.enabled" ]]
    ;;
  is-active)
    [[ "${1:-}" == --quiet ]] && shift
    [[ -f "$state_directory/${1:?}.active" ]]
    ;;
  is-failed)
    [[ "${1:-}" == --quiet ]] && shift
    [[ -f "$state_directory/${1:?}.failed" ]]
    ;;
  show)
    unit=${*: -1}
    [[ -f "$state_directory/$unit.active" ]] || exit 1
    printf '%s\n' "${KODEX_TEST_SOCKET_SUBSTATE:-listening}"
    ;;
  *)
    printf 'unsupported fake systemctl command: %s\n' "$command_name" >&2
    exit 1
    ;;
esac
EOF

cat >"$fake_bin/ip" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '[{"ifname":"eth0","addr_info":[{"family":"inet6","local":"%s","prefixlen":64,"scope":"global"}]}]\n' \
  "${KODEX_TEST_HOST_IPV6:?}"
EOF

cat >"$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${KODEX_TEST_FAILURE_DURING_PROBE:-false}" == true ]]; then
  : >"${KODEX_TEST_SYSTEMD_STATE:?}/kodex-ipv6-ingress-bridge-443.service.failed"
fi
printf '404'
EOF

cat >"$fake_bin/systemd-socket-proxyd" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$fake_bin"/*

export PATH="$fake_bin:$PATH"
export KODEX_INSTALL_TEST_ROOT="$install_root"
export KODEX_TEST_SYSTEMD_STATE="$state_directory"
export KODEX_TEST_SYSTEMD_LOG="$systemd_log"
export KODEX_TEST_HOST_IPV6=2606:4700:4700::1111

run_bridge() {
  "$bridge_script" "$@" >/dev/null
}

expect_failure() {
  if "$@" >/dev/null 2>&1; then
    fail "command unexpectedly succeeded: $*"
  fi
}

run_bridge --mode preflight \
  --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"
run_bridge --mode apply \
  --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"
grep -Fxq \
  'restart kodex-ipv6-ingress-bridge-80.socket kodex-ipv6-ingress-bridge-443.socket' \
  "$systemd_log" || fail 'apply did not restart sockets onto the exact address'

unit_directory="$install_root/etc/systemd/system"
for port in 80 443; do
  socket_file="$unit_directory/kodex-ipv6-ingress-bridge-$port.socket"
  service_file="$unit_directory/kodex-ipv6-ingress-bridge-$port.service"
  [[ -f "$socket_file" && -f "$service_file" ]] || fail "unit pair is absent: $port"
  grep -Fxq "ListenStream=[$KODEX_TEST_HOST_IPV6]:$port" "$socket_file" ||
    fail "socket does not use the exact IPv6 address: $port"
  grep -Fxq 'BindIPv6Only=ipv6-only' "$socket_file" ||
    fail "socket is not IPv6-only: $port"
  grep -Fq -- "--connections-max=256 --exit-idle-time=5min 127.0.0.1:$port" \
    "$service_file" || fail "proxy target or connection bound is invalid: $port"
  grep -Fxq 'NoNewPrivileges=yes' "$service_file" || fail "service hardening is absent: $port"
  grep -Fxq 'Type=notify' "$service_file" || fail "notification readiness is absent: $port"
  grep -Fxq 'RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6' "$service_file" ||
    fail "notification socket family is unavailable: $port"
  grep -Fxq 'CapabilityBoundingSet=' "$service_file" ||
    fail "service capabilities are not empty: $port"
  if grep -Fq 'ListenStream=[::]:' "$socket_file"; then
    fail "wildcard IPv6 listener was rendered: $port"
  fi
done

# Stop/restart не стирает прежний failed state. Apply обязан восстановить
# только свои units; readback и новый отказ не имеют права сбрасывать state.
for port in 80 443; do
  for kind in socket service; do
    : >"$state_directory/kodex-ipv6-ingress-bridge-$port.$kind.failed"
  done
done
: >"$state_directory/unrelated.service.failed"
expect_failure run_bridge --mode readback \
  --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"
run_bridge --mode apply --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"
[[ -f "$state_directory/unrelated.service.failed" ]] || fail 'unrelated failure was reset'
KODEX_TEST_NEW_PROXY_FAILURE=true expect_failure run_bridge --mode apply \
  --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"
[[ -f "$state_directory/kodex-ipv6-ingress-bridge-80.service.failed" ]] ||
  fail 'new service failure was erased'
run_bridge --mode apply --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"
KODEX_TEST_FAILURE_DURING_PROBE=true expect_failure run_bridge --mode readback \
  --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"
run_bridge --mode apply --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"

first_digest=$(sha256sum "$unit_directory"/kodex-ipv6-ingress-bridge-* | sha256sum)
run_bridge --mode apply \
  --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"
second_digest=$(sha256sum "$unit_directory"/kodex-ipv6-ingress-bridge-* | sha256sum)
[[ "$first_digest" == "$second_digest" ]] || fail 'repeated apply changed managed units'
run_bridge --mode readback \
  --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"
KODEX_TEST_SOCKET_SUBSTATE=running \
  run_bridge --mode readback \
    --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"
KODEX_TEST_SOCKET_SUBSTATE=dead \
  expect_failure run_bridge --mode readback \
    --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"
if command -v systemd-analyze >/dev/null 2>&1; then
  systemd-analyze verify \
    "$unit_directory"/kodex-ipv6-ingress-bridge-*.socket \
    "$unit_directory"/kodex-ipv6-ingress-bridge-*.service >/dev/null ||
    fail 'systemd rejected the rendered unit files'
fi

printf '\n# drift\n' >>"$unit_directory/kodex-ipv6-ingress-bridge-80.service"
expect_failure run_bridge --mode readback \
  --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"
sed -i '$d' "$unit_directory/kodex-ipv6-ingress-bridge-80.service"

expect_failure run_bridge --mode preflight --server-public-ipv6-address '::'
KODEX_TEST_HOST_IPV6=2606:4700:4700::1001 \
  expect_failure run_bridge --mode preflight \
    --server-public-ipv6-address 2606:4700:4700::1111

run_bridge --mode apply
run_bridge --mode readback
if find "$unit_directory" -name 'kodex-ipv6-ingress-bridge-*' -print -quit | grep -q .; then
  fail 'disabled profile retained bridge units'
fi

unmanaged_path="$unit_directory/kodex-ipv6-ingress-bridge-80.socket"
printf '[Socket]\nListenStream=[::]:80\n' >"$unmanaged_path"
expect_failure run_bridge --mode apply \
  --server-public-ipv6-address "$KODEX_TEST_HOST_IPV6"
grep -Fxq 'ListenStream=[::]:80' "$unmanaged_path" ||
  fail 'unmanaged conflicting unit was overwritten'
rm -f -- "$unmanaged_path"

fresh_root="$temporary_directory/a-only-root"
fresh_state="$temporary_directory/a-only-state"
mkdir -p "$fresh_root" "$fresh_state"
KODEX_INSTALL_TEST_ROOT="$fresh_root" KODEX_TEST_SYSTEMD_STATE="$fresh_state" \
  KODEX_TEST_SYSTEMD_LOG="$systemd_log" \
  run_bridge --mode preflight
KODEX_INSTALL_TEST_ROOT="$fresh_root" KODEX_TEST_SYSTEMD_STATE="$fresh_state" \
  KODEX_TEST_SYSTEMD_LOG="$systemd_log" \
  run_bridge --mode apply
KODEX_INSTALL_TEST_ROOT="$fresh_root" KODEX_TEST_SYSTEMD_STATE="$fresh_state" \
  KODEX_TEST_SYSTEMD_LOG="$systemd_log" \
  run_bridge --mode readback
[[ ! -e "$fresh_root/etc/systemd/system/kodex-ipv6-ingress-bridge-80.socket" ]] ||
  fail 'A-only profile created bridge units'

printf 'Kodex IPv6 ingress bridge tests passed\n'
