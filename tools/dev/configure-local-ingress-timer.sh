#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local ingress timer configuration failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --context <exact-context> --kubeconfig <absolute-path> --mode apply|readback\n' "$0" >&2
}

context=""
kubeconfig=""
mode=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" && -n "$kubeconfig" ]] || fail 'context and kubeconfig are required'
case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ "$kubeconfig" == /* && -f "$kubeconfig" && ! -L "$kubeconfig" ]] ||
  fail 'kubeconfig must be an existing absolute regular file'
[[ "$(stat -c %u "$kubeconfig")" == "$(id -u)" && "$(stat -c %a "$kubeconfig")" == 600 ]] ||
  fail 'kubeconfig must be owned by the current user with mode 0600'

repository_root=$(realpath "$(dirname -- "${BASH_SOURCE[0]}")/../..")
reconcile_script="$repository_root/tools/dev/reconcile-local-ingress.sh"
[[ -f "$reconcile_script" ]] || fail 'repo-owned reconcile script is missing'
for value in "$repository_root" "$kubeconfig"; do
  [[ "$value" =~ ^/[A-Za-z0-9_./-]+$ ]] || fail 'unit paths contain unsupported characters'
done
[[ "$context" =~ ^[A-Za-z0-9_.-]+$ ]] || fail 'context contains unsupported characters'
for command_name in kubectl jq systemctl systemd-analyze; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(KUBECONFIG="$kubeconfig" kubectl config current-context)" == "$context" ]] ||
  fail 'Kubernetes context mismatch'
[[ "$(KUBECONFIG="$kubeconfig" kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')" == 'https://127.0.0.1:6443' ]] ||
  fail 'Kubernetes API is not the local loopback cluster'

unit_name=kodex-local-ingress-reconcile
unit_directory="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
[[ "$unit_directory" == /* && ! -L "$unit_directory" ]] ||
  fail 'user systemd unit directory must be an absolute non-symlink path'
candidate_directory=$(mktemp -d)
cleanup() {
  rm -f -- "$candidate_directory/$unit_name.service" "$candidate_directory/$unit_name.timer"
  rmdir -- "$candidate_directory"
}
trap cleanup EXIT

service_candidate="$candidate_directory/$unit_name.service"
timer_candidate="$candidate_directory/$unit_name.timer"
cat >"$service_candidate" <<EOF
# Managed by Kodex tools/dev/configure-local-ingress-timer.sh
[Unit]
Description=Reconcile the local Kodex ingress after host address changes

[Service]
Type=oneshot
Environment=KUBECONFIG=$kubeconfig
ExecStart=/usr/bin/bash $reconcile_script --context $context --mode apply
TimeoutStartSec=4min
EOF
cat >"$timer_candidate" <<EOF
# Managed by Kodex tools/dev/configure-local-ingress-timer.sh
[Unit]
Description=Periodically reconcile the local Kodex ingress

[Timer]
Unit=$unit_name.service
OnStartupSec=45s
OnCalendar=*:0/2
AccuracySec=15s

[Install]
WantedBy=timers.target
EOF
systemd-analyze --user verify "$service_candidate" "$timer_candidate" >/dev/null ||
  fail 'generated systemd units are invalid'

service_path="$unit_directory/$unit_name.service"
timer_path="$unit_directory/$unit_name.timer"
if [[ "$mode" == apply ]]; then
  mkdir -p -- "$unit_directory"
  [[ "$(stat -c %u "$unit_directory")" == "$(id -u)" ]] ||
    fail 'user systemd unit directory is not owned by the current user'
  for target in "$service_path" "$timer_path"; do
    [[ ! -L "$target" ]] || fail 'existing unit is a symlink'
    if [[ -e "$target" ]]; then
      IFS= read -r marker <"$target" || fail 'existing unit cannot be read'
      [[ "$marker" == '# Managed by Kodex tools/dev/configure-local-ingress-timer.sh' ]] ||
        fail 'existing unit is not owned by Kodex'
    fi
  done
  install -m 0644 "$service_candidate" "$service_path"
  install -m 0644 "$timer_candidate" "$timer_path"
  systemctl --user daemon-reload
  systemctl --user enable --now "$unit_name.timer" >/dev/null
fi

[[ -f "$service_path" && -f "$timer_path" ]] || fail 'user systemd units are absent'
cmp -s "$service_candidate" "$service_path" || fail 'installed service differs from repo-owned definition'
cmp -s "$timer_candidate" "$timer_path" || fail 'installed timer differs from repo-owned definition'
systemctl --user is-enabled --quiet "$unit_name.timer" || fail 'timer is not enabled'
systemctl --user is-active --quiet "$unit_name.timer" || fail 'timer is not active'
printf 'Kodex local ingress timer configured: %s\n' "$mode"
