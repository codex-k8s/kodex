#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() {
  printf 'Kodex k3d node registry configuration failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage: configure-k3d-node-registry.sh --mode apply|readback' \
    '  --context <k3d-context> --material-directory <path>' \
    '  --promoted-pull-host <dns>' >&2
}

mode=""
context=""
material_directory=""
promoted_pull_host=""
while (($# > 0)); do
  case "$1" in
    --mode) mode=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --material-directory) material_directory=${2:-}; shift 2 ;;
    --promoted-pull-host) promoted_pull_host=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ "$context" =~ ^k3d-[a-z0-9]([a-z0-9-]*[a-z0-9])?$ &&
  "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
[[ -d "$material_directory" && ! -L "$material_directory" ]] ||
  fail 'material directory is invalid'
[[ "$promoted_pull_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ &&
  "$promoted_pull_host" == *.* ]] || fail 'promoted pull host is invalid'
for command_name in docker jq k3d kubectl mktemp sha256sum stat yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

username_file="$material_directory/node-pull/username"
password_file="$material_directory/node-pull/password"
ca_file="$material_directory/node-pull/ca.crt"
certificate_file="$material_directory/node-pull/client.crt"
private_key_file="$material_directory/node-pull/client.key"
for input_file in "$username_file" "$password_file" "$ca_file" "$certificate_file" "$private_key_file"; do
  [[ -f "$input_file" && -s "$input_file" && ! -L "$input_file" ]] ||
    fail 'node pull material is incomplete'
  input_mode=$(stat -c '%a' "$input_file")
  (((8#$input_mode & 8#077) == 0)) || fail 'node pull material permissions are too broad'
done

# Не наследуем export attribute одноимённых переменных вызывающей стороны.
export -n username password actual expected actual_host expected_host
username=$(<"$username_file")
password=$(<"$password_file")
[[ -n "$username" && "$username" != *$'\n'* && "$username" != *$'\r'* ]] ||
  fail 'node pull username is invalid'
[[ ${#password} -ge 32 && "$password" != *$'\n'* && "$password" != *$'\r'* ]] ||
  fail 'node pull password is invalid'

cluster=${context#k3d-}
mapfile -t nodes < <(k3d node list -o json | jq -r --arg cluster "$cluster" '
  .[] | select(.name | startswith("k3d-" + $cluster + "-")) |
  select(.role == "server" or .role == "agent") | .name
' | sort)
((${#nodes[@]} > 0)) || fail 'k3d workload nodes are absent'

wait_container_stable() {
  local node=$1 attempt consecutive=0
  for attempt in $(seq 1 120); do
    if [[ "$(docker inspect --format '{{.State.Status}}' "$node" 2>/dev/null || true)" == running ]] &&
      docker exec "$node" true >/dev/null 2>&1; then
      ((consecutive += 1))
      ((consecutive >= 3)) && return 0
    else
      consecutive=0
    fi
    sleep 1
  done
  fail "k3d node did not become stable: $node"
}

for node in "${nodes[@]}"; do
  wait_container_stable "$node"
done
load_balancer="k3d-$cluster-serverlb"
load_balancer_ip=$(docker inspect "$load_balancer" --format \
  '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
[[ "$load_balancer_ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
  fail 'k3d load balancer address is invalid'

system_directory=/etc/rancher/k3s/kodex-registry
system_ca="$system_directory/ca.crt"
system_certificate="$system_directory/client.crt"
system_private_key="$system_directory/client.key"
registry_configuration=/etc/rancher/k3s/registries.yaml
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"; unset username password' EXIT

printf '%s' "$username" >"$temporary_directory/username"
printf '%s' "$password" >"$temporary_directory/password"
unset username password

existing_json_file="$temporary_directory/existing.json"
printf '{}\n' >"$existing_json_file"
existing_digest=""
for node in "${nodes[@]}"; do
  if docker exec "$node" test -f "$registry_configuration"; then
    docker exec "$node" cat "$registry_configuration" |
      yq -o=json | jq -cS . >"$temporary_directory/$node.json"
    node_digest=$(sha256sum "$temporary_directory/$node.json" | awk '{print $1}')
    [[ -z "$existing_digest" || "$node_digest" == "$existing_digest" ]] ||
      fail 'k3d node registry configurations differ'
    existing_digest=$node_digest
    existing_json_file="$temporary_directory/$node.json"
  elif [[ -n "$existing_digest" ]]; then
    fail 'k3d node registry configurations are incomplete'
  fi
done

expected_json_file="$temporary_directory/expected.json"
jq -cn --slurpfile existing "$existing_json_file" --arg host "$promoted_pull_host" \
  --rawfile username "$temporary_directory/username" --rawfile password "$temporary_directory/password" --arg ca "$system_ca" \
  --arg certificate "$system_certificate" --arg private_key "$system_private_key" '
    (if ($existing | length) == 1 then $existing[0]
     else error("registry configuration must contain one JSON value") end) |
    .mirrors = ((.mirrors // {}) + {($host):{endpoint:[("https://" + $host)]}}) |
    .configs = ((.configs // {}) + {($host):{
      auth:{username:$username,password:$password},
      tls:{ca_file:$ca,cert_file:$certificate,key_file:$private_key}
    }})
  ' >"$expected_json_file"
yq -P <"$expected_json_file" >"$temporary_directory/registries.yaml"
chmod 0600 "$temporary_directory/registries.yaml"

changed=false
for node in "${nodes[@]}"; do
  if ! docker exec "$node" test -f "$registry_configuration"; then
    changed=true
    continue
  fi
  actual=$(docker exec "$node" cat "$registry_configuration" | yq -o=json | jq -cS .)
  [[ "$actual" == "$(jq -cS . "$expected_json_file")" ]] || changed=true
  for pair in "$ca_file:$system_ca" "$certificate_file:$system_certificate" "$private_key_file:$system_private_key"; do
    source_path=${pair%%:*}
    target_path=${pair#*:}
    source_digest=$(sha256sum "$source_path" | awk '{print $1}')
    target_digest=$(docker exec "$node" sha256sum "$target_path" 2>/dev/null | awk '{print $1}' || true)
    [[ "$source_digest" == "$target_digest" ]] || changed=true
  done
done

if [[ "$mode" == apply && "$changed" == true ]]; then
  for node in "${nodes[@]}"; do
    docker exec "$node" install -d -m 0700 "$system_directory" /etc/rancher/k3s
    docker cp "$ca_file" "$node:$system_ca" >/dev/null
    docker cp "$certificate_file" "$node:$system_certificate" >/dev/null
    docker cp "$private_key_file" "$node:$system_private_key" >/dev/null
    docker cp "$temporary_directory/registries.yaml" "$node:$registry_configuration" >/dev/null
    docker exec "$node" chmod 0600 "$system_ca" "$system_certificate" \
      "$system_private_key" "$registry_configuration"
  done
  # Перезапускать ноды нужно последовательно: одновременный перезапуск control
  # plane и worker создаёт краткий restart loop на нагруженном dev-стенде.
  for node in "${nodes[@]}"; do
    docker restart "$node" >/dev/null
    wait_container_stable "$node"
  done
fi

for attempt in $(seq 1 120); do
  kubectl get --raw=/readyz >/dev/null 2>&1 &&
    kubectl wait --for=condition=Ready node --all --timeout=5s >/dev/null 2>&1 && break
  ((attempt < 120)) || fail 'Kubernetes API did not recover after registry configuration'
  sleep 2
done

for node in "${nodes[@]}"; do
  if [[ "$mode" == apply ]]; then
    docker exec "$node" sh -c '
      grep -v " $2$" /etc/hosts > /tmp/kodex-hosts
      printf "%s %s\n" "$1" "$2" >> /tmp/kodex-hosts
      cat /tmp/kodex-hosts > /etc/hosts
      rm -f /tmp/kodex-hosts
    ' sh "$load_balancer_ip" "$promoted_pull_host"
  fi
  actual_host=$(docker exec "$node" cat "$registry_configuration" | yq -o=json |
    jq -cS --arg host "$promoted_pull_host" '{mirror:.mirrors[$host],config:.configs[$host]}')
  expected_host=$(jq -cS --arg host "$promoted_pull_host" \
    '{mirror:.mirrors[$host],config:.configs[$host]}' "$expected_json_file")
  [[ "$actual_host" == "$expected_host" ]] || fail 'k3d promoted pull configuration mismatch'
  docker exec "$node" grep -Fqx "$load_balancer_ip $promoted_pull_host" /etc/hosts ||
    fail 'k3d promoted pull host alias mismatch'
  for pair in "$ca_file:$system_ca" "$certificate_file:$system_certificate" "$private_key_file:$system_private_key"; do
    source_path=${pair%%:*}
    target_path=${pair#*:}
    source_digest=$(sha256sum "$source_path" | awk '{print $1}')
    target_digest=$(docker exec "$node" sha256sum "$target_path" | awk '{print $1}')
    [[ "$source_digest" == "$target_digest" ]] ||
      fail 'k3d promoted pull TLS material mismatch'
  done
done

printf 'Kodex k3d node registry configuration completed: %s\n' "$mode"
