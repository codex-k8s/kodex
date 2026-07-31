#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kubernetes API endpoint resolution failed: %s\n' "$*" >&2
  exit 1
}

command -v kubectl >/dev/null 2>&1 || fail "kubectl is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"

output="cidrs"
if (($# > 0)); then
  [[ "$#" == 2 && "$1" == "--output" && ("$2" == "cidrs" || "$2" == "ports") ]] ||
    fail "usage: $0 [--output cidrs|ports]"
  output="$2"
fi

service_json="$(kubectl get service kubernetes --namespace default --output json)" ||
  fail "Service/default/kubernetes is required"
slices_json="$(
  kubectl get endpointslices.discovery.k8s.io \
    --namespace default \
    --selector kubernetes.io/service-name=kubernetes \
    --output json
)" || fail "Kubernetes Service EndpointSlices are required"

addresses="$(
  jq -ner \
    --argjson service "$service_json" \
    --argjson slices "$slices_json" '
      ($service.spec.clusterIPs // []) as $cluster_ips
      | [
          $slices.items[]
          | select(.addressType == "IPv4" or .addressType == "IPv6")
          | .addressType as $type
          | .endpoints[]
          | select(.conditions.ready == true)
          | .addresses[]
          | select(
              ($type == "IPv4" and (contains(":") | not)) or
              ($type == "IPv6" and contains(":"))
            )
        ] as $ready_addresses
      | [
          $cluster_ips[],
          $ready_addresses[]
        ]
      | map(select(. != "" and . != "None"))
      | unique
      | select(
          ($cluster_ips | length) > 0 and
          ($ready_addresses | length) > 0 and
          length > 0 and
          length <= 32
        )
      | .[]
    '
)" || fail "Service ClusterIPs plus ready EndpointSlice addresses must contain one to 32 exact IPs"

valid_ipv4() {
  local address="$1"
  [[ "$address" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || return 1
  local first second third fourth
  IFS='.' read -r first second third fourth <<<"$address"
  local octet
  for octet in "$first" "$second" "$third" "$fourth"; do
    ((10#$octet <= 255)) || return 1
  done
}

valid_ipv6() {
  local address="${1,,}"
  [[ "$address" == *:* && "$address" =~ ^[0-9a-f:]+$ ]] || return 1
  [[ "$address" != *:::* ]] || return 1
  local after_compression="${address/::/}"
  [[ "$after_compression" != *::* ]] || return 1
  local expanded="${address//:/ }"
  local group count=0
  for group in $expanded; do
    ((${#group} >= 1 && ${#group} <= 4)) || return 1
    ((count += 1))
  done
  if [[ "$address" == *::* ]]; then
    ((count < 8)) || return 1
  else
    ((count == 8)) || return 1
  fi
}

cidrs=()
while IFS= read -r address; do
  [[ -n "$address" ]] || continue
  if valid_ipv4 "$address"; then
    cidrs+=("$address/32")
  elif valid_ipv6 "$address"; then
    cidrs+=("${address,,}/128")
  else
    fail "Kubernetes API returned an invalid address"
  fi
done <<<"$addresses"

((${#cidrs[@]} > 0 && ${#cidrs[@]} <= 32)) ||
  fail "Kubernetes API exact CIDR set is empty or exceeds 32 addresses"
if [[ "$output" == "cidrs" ]]; then
  printf '%s\n' "${cidrs[@]}" | LC_ALL=C sort -u | paste -sd, -
  exit 0
fi

ports="$(
  jq -ner \
    --argjson service "$service_json" \
    --argjson slices "$slices_json" '
      [
        (
          $service.spec.ports[]
          | select((.protocol // "TCP") == "TCP")
          | .port
        ),
        (
          $slices.items[]
          | select(any(.endpoints[]; .conditions.ready == true))
          | .ports[]
          | select((.protocol // "TCP") == "TCP")
          | .port
        )
      ]
      | unique
      | select(index(443) != null and length > 0 and length <= 8)
      | .[]
    '
)" || fail "Service port 443 plus ready EndpointSlice TCP ports are required"
while IFS= read -r port; do
  [[ "$port" =~ ^[0-9]+$ ]] &&
    ((10#$port >= 1 && 10#$port <= 65535)) ||
    fail "Kubernetes API returned an invalid TCP port"
done <<<"$ports"
printf '%s\n' "$ports" | LC_ALL=C sort -n -u | paste -sd, -
