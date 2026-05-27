#!/usr/bin/env bash
set -euo pipefail

log() { echo "[$(date -Is)] $*"; }
die() { echo "ERROR: $*" >&2; exit 1; }

require_root() {
  [ "${EUID}" -eq 0 ] || die "Run as root"
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Missing required command: $1"
}

ensure_domain_resolves() {
  local domain="$1"
  local ips
  ips="$(resolve_ipv4_records "$domain")"
  [ -n "$ips" ] || die "Configured production domain does not resolve via IPv4"
  log "Configured production domain resolves via IPv4"
}

resolve_ipv4_records() {
  local name="$1"
  getent ahostsv4 "$name" 2>/dev/null | awk '{print $1}' | sort -u || true
}

is_ipv4_literal() {
  local value="$1"
  [[ "$value" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]
}

current_host_ipv4_records() {
  hostname -I 2>/dev/null | tr ' ' '\n' | awk '/^[0-9]+(\.[0-9]+){3}$/ { print }' | sort -u || true
}

has_ipv4_intersection() {
  local left="$1"
  local right="$2"
  local candidate
  while IFS= read -r candidate; do
    [ -n "$candidate" ] || continue
    if printf '%s\n' "$right" | grep -Fxq "$candidate"; then
      return 0
    fi
  done <<< "$left"
  return 1
}

ensure_domain_targets_host() {
  local domain="$1"
  local target_host="$2"
  local domain_ips target_ips

  [ -n "$target_host" ] || die "TARGET_HOST is required for production DNS binding check"
  domain_ips="$(resolve_ipv4_records "$domain")"
  if is_ipv4_literal "$target_host"; then
    target_ips="$target_host"
  else
    target_ips="$(resolve_ipv4_records "$target_host")"
  fi

  [ -n "$domain_ips" ] || die "Configured production domain does not resolve via IPv4"
  [ -n "$target_ips" ] || die "Configured target host does not resolve via IPv4"
  if has_ipv4_intersection "$domain_ips" "$target_ips"; then
    log "Configured production domain resolves to the target host"
    return
  fi
  die "Configured production domain does not resolve to the target host"
}

ensure_domain_targets_current_host() {
  local domain="$1"
  local domain_ips local_ips

  domain_ips="$(resolve_ipv4_records "$domain")"
  local_ips="$(current_host_ipv4_records)"
  [ -n "$domain_ips" ] || die "Configured production domain does not resolve via IPv4"
  [ -n "$local_ips" ] || die "Current host IPv4 addresses are not discoverable"
  if has_ipv4_intersection "$domain_ips" "$local_ips"; then
    log "Configured production domain resolves to the current host"
    return
  fi
  die "Configured production domain does not resolve to the current host"
}

load_env_file() {
  local env_file="$1"
  [ -f "$env_file" ] || die "Env file not found: $env_file"
  set -a
  # shellcheck disable=SC1090
  source "$env_file"
  set +a
}

kube_env() {
  export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
}

repo_dir() {
  echo "/opt/kodex"
}

inventory_version() {
  local key="$1"
  local services_file="${2:-$(repo_dir)/services.yaml}"
  [ -f "$services_file" ] || die "services.yaml not found in repository snapshot"
  awk -v key="$key" '
    $0 ~ "^    " key ":" { found = 1; next }
    found && $1 == "value:" {
      value = $2
      gsub(/"/, "", value)
      print value
      exit
    }
    found && $0 ~ "^    [A-Za-z0-9_-]+:" { exit }
  ' "$services_file"
}

escape_squote() {
  printf "%s" "$1" | sed "s/'/'\\\\''/g"
}

set_env_var() {
  local env_file="$1"
  local key="$2"
  local value="$3"
  local escaped

  [ -f "$env_file" ] || die "Env file not found: $env_file"
  escaped="$(printf '%s' "$value" | sed "s/'/'\\\\''/g")"

  if grep -q "^${key}=" "$env_file"; then
    sed -i "s|^${key}=.*$|${key}='${escaped}'|" "$env_file"
  else
    printf "%s='%s'\n" "$key" "$escaped" >> "$env_file"
  fi
  export "${key}=${value}"
}

set_env_default() {
  local env_file="$1"
  local key="$2"
  local value="$3"
  if [ -z "${!key:-}" ]; then
    set_env_var "$env_file" "$key" "$value"
  fi
}

random_hex() {
  local bytes="${1:-32}"
  openssl rand -hex "$bytes"
}

postgres_uri() {
  local user="$1"
  local password="$2"
  local host="$3"
  local port="$4"
  local database="$5"
  printf "postgres://%s:%s@%s:%s/%s?sslmode=disable" "$user" "$password" "$host" "$port" "$database"
}

validate_integer() {
  local name="$1"
  local value="$2"
  case "$value" in
    ''|*[!0-9]*) die "${name} must be an integer";;
  esac
}

validate_bool() {
  local name="$1"
  local value="$2"
  case "$value" in
    true|false) ;;
    *) die "${name} must be true or false";;
  esac
}
