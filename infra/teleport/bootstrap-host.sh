#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex Teleport host bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --mode preflight|apply|readback --host <exact-DNS>" \
    '  --backend-address <private-IPv4> --kube-cluster-name <name>' \
    '  --kubeconfig <root-owned-path> --github-client-id-file <path>' \
    '  --github-client-secret-file <path> --github-organization <name>' \
    '  --github-team <slug> [--ssh-login <name>] [--kubernetes-group <name>]' >&2
}

mode=""
host=""
backend_address=""
kube_cluster_name=kodex-dev
kubeconfig=/etc/rancher/k3s/k3s.yaml
github_client_id_file=""
github_client_secret_file=""
github_organization=""
github_team=""
ssh_login=kodex-teleport
kubernetes_group=kodex-teleport-dev-observers
while (($# > 0)); do
  case "$1" in
    --mode) mode=${2:-}; shift 2 ;;
    --host) host=${2:-}; shift 2 ;;
    --backend-address) backend_address=${2:-}; shift 2 ;;
    --kube-cluster-name) kube_cluster_name=${2:-}; shift 2 ;;
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --github-client-id-file) github_client_id_file=${2:-}; shift 2 ;;
    --github-client-secret-file) github_client_secret_file=${2:-}; shift 2 ;;
    --github-organization) github_organization=${2:-}; shift 2 ;;
    --github-team) github_team=${2:-}; shift 2 ;;
    --ssh-login) ssh_login=${2:-}; shift 2 ;;
    --kubernetes-group) kubernetes_group=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$mode" in preflight|apply|readback) ;; *) fail 'mode is invalid' ;; esac
((EUID == 0)) || fail 'host bootstrap must run as root'
[[ "$host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$host" == *.* ]] ||
  fail 'Teleport host must be an exact DNS name'
[[ "$kube_cluster_name" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
  fail 'Teleport Kubernetes cluster name is invalid'
[[ "$ssh_login" =~ ^[a-z_][a-z0-9_-]{0,30}$ ]] || fail 'Teleport SSH login is invalid'
[[ "$kubernetes_group" =~ ^[a-z0-9]([a-z0-9:-]*[a-z0-9])?$ ]] ||
  fail 'Teleport Kubernetes group is invalid'
python3 - "$backend_address" <<'PY' || fail 'Teleport backend address must be a private non-loopback IPv4 address'
import ipaddress
import sys

address = ipaddress.ip_address(sys.argv[1])
if address.version != 4 or not address.is_private or address.is_loopback or address.is_unspecified:
    raise SystemExit(1)
PY
for command_name in cmp curl getent id jq openssl passwd python3 sha256sum stat systemctl \
  tctl teleport useradd yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$kubeconfig" == /* && -f "$kubeconfig" && ! -L "$kubeconfig" ]] ||
  fail 'root-owned Kubernetes configuration is invalid'
[[ "$(stat -c '%u' "$kubeconfig")" == 0 && $((8#$(stat -c '%a' "$kubeconfig") & 8#077)) == 0 ]] ||
  fail 'root-owned Kubernetes configuration is not private'

config_file=/etc/teleport.yaml
data_directory=/var/lib/teleport
certificate_directory=$data_directory/certs
system_ca_file=/etc/ssl/certs/ca-certificates.crt
ca_key_file=$certificate_directory/internal-ca.key
ca_certificate_file=$certificate_directory/internal-ca.crt
proxy_key_file=$certificate_directory/proxy.key
proxy_certificate_file=$certificate_directory/proxy.crt
trust_bundle_file=$certificate_directory/trust-bundle.pem
teleport_kubeconfig=$data_directory/kubeconfig
unit_file=/etc/systemd/system/teleport.service
role_name=kodex-dev-access

teleport_ctl() {
  SSL_CERT_FILE="$trust_bundle_file" tctl "$@"
}

validate_credential_file() {
  local path=$1
  [[ "$path" == /* && -f "$path" && -s "$path" && ! -L "$path" &&
    $((8#$(stat -c '%a' "$path") & 8#077)) == 0 ]] ||
    fail 'GitHub OAuth credential file is invalid or not private'
}

validate_identity_inputs() {
  validate_credential_file "$github_client_id_file"
  validate_credential_file "$github_client_secret_file"
  [[ "$github_organization" =~ ^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$ ]] ||
    fail 'GitHub organization is invalid'
  [[ "$github_team" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] ||
    fail 'GitHub team slug is invalid'
}

render_config() {
  local output=$1
  HOST="$host" BACKEND_ADDRESS="$backend_address" KUBE_CLUSTER_NAME="$kube_cluster_name" \
    SSH_LOGIN="$ssh_login" DATA_DIRECTORY="$data_directory" \
    PROXY_KEY_FILE="$proxy_key_file" PROXY_CERTIFICATE_FILE="$proxy_certificate_file" \
    TELEPORT_KUBECONFIG="$teleport_kubeconfig" yq -n -o=yaml '
      .version = "v3" |
      .teleport.nodename = strenv(KUBE_CLUSTER_NAME) |
      .teleport.data_dir = strenv(DATA_DIRECTORY) |
      .teleport.log.output = "stderr" |
      .teleport.log.severity = "INFO" |
      .teleport.log.format.output = "text" |
      .auth_service.enabled = true |
      .auth_service.listen_addr = "127.0.0.1:3025" |
      .auth_service.cluster_name = strenv(HOST) |
      .auth_service.proxy_listener_mode = "multiplex" |
      .auth_service.authentication.type = "github" |
      .auth_service.authentication.connector_name = "github" |
      .auth_service.authentication.local_auth = false |
      .auth_service.authentication.second_factors = ["sso"] |
      .proxy_service.enabled = true |
      .proxy_service.web_listen_addr = strenv(BACKEND_ADDRESS) + ":3080" |
      .proxy_service.public_addr = [strenv(HOST) + ":443"] |
      .proxy_service.trust_x_forwarded_for = true |
      .proxy_service.https_keypairs = [{"key_file":strenv(PROXY_KEY_FILE),"cert_file":strenv(PROXY_CERTIFICATE_FILE)}] |
      .proxy_service.https_keypairs_reload_interval = "12h" |
      .ssh_service.enabled = true |
      .ssh_service.listen_addr = "127.0.0.1:3022" |
      .ssh_service.labels.environment = "development" |
      .kubernetes_service.enabled = true |
      .kubernetes_service.listen_addr = "127.0.0.1:3026" |
      .kubernetes_service.kubeconfig_file = strenv(TELEPORT_KUBECONFIG) |
      .kubernetes_service.labels.environment = "development"
    ' >"$output"
}

render_unit() {
  local output=$1
  cat >"$output" <<'EOF'
[Unit]
Description=Teleport access plane for Kodex development
Documentation=https://goteleport.com/docs/
Wants=network-online.target
After=network-online.target k3s.service kodex-local-api-address.service

[Service]
Type=simple
User=root
Group=root
UMask=0077
ExecStart=/usr/local/bin/teleport start --config=/etc/teleport.yaml
Restart=on-failure
RestartSec=5s
LimitNOFILE=1048576
PrivateTmp=true
Environment=SSL_CERT_FILE=/var/lib/teleport/certs/trust-bundle.pem

[Install]
WantedBy=multi-user.target
EOF
}

ensure_certificates() {
  install -d -m 0700 -o root -g root "$certificate_directory"
  [[ -f "$system_ca_file" && -s "$system_ca_file" ]] ||
    fail 'system CA bundle is unavailable'
  if [[ ! -s "$ca_key_file" || ! -s "$ca_certificate_file" ]]; then
    openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$ca_key_file" >/dev/null 2>&1
    openssl req -x509 -new -sha256 -key "$ca_key_file" -out "$ca_certificate_file" \
      -days 3650 -subj '/CN=Kodex Teleport Host Internal CA' \
      -addext 'basicConstraints=critical,CA:TRUE' \
      -addext 'keyUsage=critical,keyCertSign,cRLSign' >/dev/null 2>&1
  fi
  chmod 0600 "$ca_key_file"
  chmod 0644 "$ca_certificate_file"

  local renew_certificate=false temporary_directory extension_file
  if [[ ! -s "$proxy_key_file" || ! -s "$proxy_certificate_file" ]] ||
    ! openssl x509 -in "$proxy_certificate_file" -noout -checkend 2592000 >/dev/null 2>&1 ||
    ! openssl verify -CAfile "$ca_certificate_file" "$proxy_certificate_file" >/dev/null 2>&1 ||
    ! openssl x509 -in "$proxy_certificate_file" -noout -checkhost "$host" >/dev/null 2>&1; then
    renew_certificate=true
  fi
  if [[ "$renew_certificate" == true ]]; then
    temporary_directory=$(mktemp -d)
    extension_file=$temporary_directory/extensions.cnf
    printf '%s\n' \
      'basicConstraints=critical,CA:FALSE' \
      'keyUsage=critical,digitalSignature,keyEncipherment' \
      'extendedKeyUsage=serverAuth' \
      "subjectAltName=DNS:$host" >"$extension_file"
    openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$proxy_key_file" >/dev/null 2>&1
    openssl req -new -sha256 -key "$proxy_key_file" -subj "/CN=$host" \
      -out "$temporary_directory/proxy.csr" >/dev/null 2>&1
    openssl x509 -req -sha256 -in "$temporary_directory/proxy.csr" \
      -CA "$ca_certificate_file" -CAkey "$ca_key_file" -CAcreateserial \
      -days 397 -extfile "$extension_file" -out "$proxy_certificate_file" >/dev/null 2>&1
    rm -rf -- "$temporary_directory"
  fi
  chmod 0600 "$proxy_key_file"
  chmod 0644 "$proxy_certificate_file"

  [[ ! -e "$trust_bundle_file" || (-f "$trust_bundle_file" && ! -L "$trust_bundle_file") ]] ||
    fail 'Teleport trust bundle path is unsafe'
  local temporary_bundle
  temporary_bundle=$(mktemp "$certificate_directory/.trust-bundle.XXXXXX")
  cat "$system_ca_file" "$ca_certificate_file" >"$temporary_bundle"
  chmod 0644 "$temporary_bundle"
  mv -- "$temporary_bundle" "$trust_bundle_file"
}

readback_certificates() {
  for path in "$ca_certificate_file" "$proxy_certificate_file" "$trust_bundle_file"; do
    [[ -f "$path" && ! -L "$path" ]] || fail 'Teleport certificate is absent or unsafe'
  done
  [[ -f "$ca_key_file" && -f "$proxy_key_file" && ! -L "$ca_key_file" && ! -L "$proxy_key_file" &&
    "$(stat -c '%a' "$ca_key_file")" == 600 && "$(stat -c '%a' "$proxy_key_file")" == 600 ]] ||
    fail 'Teleport private key permissions are invalid'
  cmp --silent "$trust_bundle_file" <(cat "$system_ca_file" "$ca_certificate_file") ||
    fail 'Teleport trust bundle differs from the supported CA set'
  openssl verify -CAfile "$trust_bundle_file" "$proxy_certificate_file" >/dev/null ||
    fail 'Teleport backend certificate chain is invalid'
  openssl x509 -in "$proxy_certificate_file" -noout -checkhost "$host" >/dev/null ||
    fail 'Teleport backend certificate identity is invalid'
  openssl x509 -in "$proxy_certificate_file" -noout -checkend 2592000 >/dev/null ||
    fail 'Teleport backend certificate expires too soon'
}

install_teleport_kubeconfig() {
  local candidate
  candidate=$(mktemp "$data_directory/.kubeconfig.XXXXXX")
  if ! KUBE_CLUSTER_NAME="$kube_cluster_name" yq -o=yaml '
    select(
      (.clusters | length) == 1 and (.contexts | length) == 1 and
      (.users | length) == 1
    ) |
    .clusters[0].name = strenv(KUBE_CLUSTER_NAME) |
    .contexts[0].name = strenv(KUBE_CLUSTER_NAME) |
    .contexts[0].context.cluster = strenv(KUBE_CLUSTER_NAME) |
    ."current-context" = strenv(KUBE_CLUSTER_NAME)
  ' "$kubeconfig" >"$candidate"; then
    rm -f -- "$candidate"
    fail 'root-owned Kubernetes configuration cannot be normalized for Teleport'
  fi
  chmod 0600 "$candidate"
  mv -- "$candidate" "$teleport_kubeconfig"
}

readback_teleport_kubeconfig() {
  [[ -f "$teleport_kubeconfig" && ! -L "$teleport_kubeconfig" &&
    "$(stat -c '%u' "$teleport_kubeconfig")" == 0 &&
    "$(stat -c '%a' "$teleport_kubeconfig")" == 600 ]] ||
    fail 'Teleport Kubernetes configuration is unsafe'
  KUBE_CLUSTER_NAME="$kube_cluster_name" yq -e '
    (.clusters | length) == 1 and (.contexts | length) == 1 and
    (.users | length) == 1 and
    .clusters[0].name == strenv(KUBE_CLUSTER_NAME) and
    .contexts[0].name == strenv(KUBE_CLUSTER_NAME) and
    .contexts[0].context.cluster == strenv(KUBE_CLUSTER_NAME) and
    ."current-context" == strenv(KUBE_CLUSTER_NAME)
  ' "$teleport_kubeconfig" >/dev/null ||
    fail 'Teleport Kubernetes configuration differs from the supported profile'
}

if [[ "$mode" == preflight ]]; then
  validate_identity_inputs
  [[ -d /run/systemd/system ]] || fail 'systemd is required'
  printf 'Kodex Teleport host preflight completed\n'
  exit 0
fi

validate_identity_inputs
if [[ "$mode" == apply ]]; then
  if ! getent passwd "$ssh_login" >/dev/null; then
    useradd --create-home --shell /bin/bash --user-group "$ssh_login"
  fi
  [[ "$(id -u "$ssh_login")" -ge 1000 ]] || fail 'Teleport SSH login is not an unprivileged user'
  passwd --lock "$ssh_login" >/dev/null 2>&1 || true

  install -d -m 0700 -o root -g root "$data_directory"
  ensure_certificates
  install_teleport_kubeconfig

  temporary_directory=$(mktemp -d)
  cleanup() { rm -rf -- "$temporary_directory"; }
  trap cleanup EXIT
  candidate_config=$temporary_directory/teleport.yaml
  candidate_unit=$temporary_directory/teleport.service
  render_config "$candidate_config"
  render_unit "$candidate_unit"
  teleport configure --test="$candidate_config" >/dev/null || fail 'Teleport host configuration is invalid'
  install -m 0600 -o root -g root "$candidate_config" "$config_file"
  install -m 0644 -o root -g root "$candidate_unit" "$unit_file"
  systemctl daemon-reload
  systemctl enable teleport >/dev/null
  systemctl restart teleport
  for _ in $(seq 1 90); do
    systemctl is-active --quiet teleport &&
      teleport_ctl --config="$config_file" status >/dev/null 2>&1 && break
    sleep 2
  done
  systemctl is-active --quiet teleport || fail 'Teleport host service is unavailable'
  teleport_ctl --config="$config_file" status >/dev/null 2>&1 || fail 'Teleport Auth Service is unavailable'

  role_file=$temporary_directory/role.json
  connector_file=$temporary_directory/github.json
  SSH_LOGIN="$ssh_login" KUBERNETES_GROUP="$kubernetes_group" jq -n '
    {
      kind:"role",version:"v8",metadata:{name:"kodex-dev-access"},
      spec:{
        options:{max_session_ttl:"8h0m0s",forward_agent:false,ssh_file_copy:false},
        allow:{
          logins:[env.SSH_LOGIN],
          node_labels:{environment:"development"},
          kubernetes_labels:{environment:"development"},
          kubernetes_groups:[env.KUBERNETES_GROUP]
        }
      }
    }
  ' >"$role_file"
  jq -n --rawfile client_id "$github_client_id_file" \
    --rawfile client_secret "$github_client_secret_file" \
    --arg redirect_url "https://$host/v1/webapi/github/callback" \
    --arg organization "$github_organization" --arg team "$github_team" '
      {
        kind:"github",version:"v3",metadata:{name:"github"},
        spec:{
          display:"GitHub",
          client_id:($client_id | rtrimstr("\n")),
          client_secret:($client_secret | rtrimstr("\n")),
          redirect_url:$redirect_url,
          teams_to_logins:null,
          teams_to_roles:[{organization:$organization,team:$team,roles:["kodex-dev-access"]}]
        }
      }
    ' >"$connector_file"
  chmod 0600 "$role_file" "$connector_file"
  teleport_ctl --config="$config_file" create -f "$role_file" >/dev/null
  teleport_ctl --config="$config_file" create -f "$connector_file" >/dev/null
fi

systemctl is-enabled --quiet teleport || fail 'Teleport host service is not enabled'
systemctl is-active --quiet teleport || fail 'Teleport host service is not active'
[[ -f "$config_file" && ! -L "$config_file" && "$(stat -c '%u' "$config_file")" == 0 &&
  $((8#$(stat -c '%a' "$config_file") & 8#077)) == 0 ]] || fail 'Teleport host configuration is unsafe'
teleport configure --test="$config_file" >/dev/null || fail 'Teleport host configuration readback failed'
readback_certificates
readback_teleport_kubeconfig
HOST="$host" BACKEND_ADDRESS="$backend_address" TELEPORT_KUBECONFIG="$teleport_kubeconfig" \
  yq -e '
  .auth_service.enabled == true and
  .auth_service.authentication.type == "github" and
  .auth_service.authentication.local_auth == false and
  .proxy_service.enabled == true and
  .proxy_service.web_listen_addr == (strenv(BACKEND_ADDRESS) + ":3080") and
  (.proxy_service.public_addr | length) == 1 and
  .proxy_service.public_addr[0] == (strenv(HOST) + ":443") and
  .proxy_service.trust_x_forwarded_for == true and
  .ssh_service.enabled == true and
  .ssh_service.labels.environment == "development" and
  .kubernetes_service.enabled == true and
  .kubernetes_service.kubeconfig_file == strenv(TELEPORT_KUBECONFIG) and
  .kubernetes_service.labels.environment == "development"
' "$config_file" >/dev/null || fail 'Teleport host service configuration differs from the supported profile'
teleport_ctl --config="$config_file" get "role/$role_name" --format=json | jq -e \
  --arg login "$ssh_login" --arg group "$kubernetes_group" '
    length == 1 and
    (.[0].spec | keys | sort) == ["allow","deny","options"] and
    (.[0].spec.allow | keys | sort) ==
      ["kubernetes_groups","kubernetes_labels","kubernetes_resources","logins","node_labels"] and
    .[0].spec.allow.logins == [$login] and
    .[0].spec.allow.node_labels.environment == "development" and
    .[0].spec.allow.node_labels == {"environment":"development"} and
    .[0].spec.allow.kubernetes_labels.environment == "development" and
    .[0].spec.allow.kubernetes_labels == {"environment":"development"} and
    .[0].spec.allow.kubernetes_groups == [$group] and
    .[0].spec.allow.kubernetes_resources ==
      [{"api_group":"*","kind":"*","name":"*","namespace":"*","verbs":["*"]}] and
    .[0].spec.deny == {} and
    .[0].spec.options.forward_agent == false and
    .[0].spec.options.ssh_file_copy == false and
    .[0].spec.options.create_db_user == false and
    .[0].spec.options.create_desktop_user == false and
    .[0].spec.options.max_session_ttl == "8h0m0s"
  ' >/dev/null || fail 'Teleport bounded development role readback failed'
teleport_ctl --config="$config_file" get github/github --format=json --with-secrets | jq -e \
  --arg host "$host" --arg organization "$github_organization" --arg team "$github_team" \
  --rawfile client_id "$github_client_id_file" --rawfile client_secret "$github_client_secret_file" '
    length == 1 and
    (.[0].spec | keys | sort) ==
      ["api_endpoint_url","client_id","client_secret","display","endpoint_url","redirect_url","teams_to_logins","teams_to_roles"] and
    .[0].spec.client_id == ($client_id | rtrimstr("\n")) and
    .[0].spec.client_secret == ($client_secret | rtrimstr("\n")) and
    .[0].spec.display == "GitHub" and
    .[0].spec.endpoint_url == "" and .[0].spec.api_endpoint_url == "" and
    .[0].spec.teams_to_logins == null and
    .[0].spec.redirect_url == ("https://" + $host + "/v1/webapi/github/callback") and
    .[0].spec.teams_to_roles ==
      [{"organization":$organization,"team":$team,"roles":["kodex-dev-access"]}]
  ' >/dev/null || fail 'Teleport GitHub connector readback failed'
curl --proto '=https' --tlsv1.2 --fail --silent --show-error \
  --resolve "$host:3080:$backend_address" --cacert "$ca_certificate_file" \
  "https://$host:3080/webapi/ping" | jq -e '.auth.type == "github"' >/dev/null ||
  fail 'Teleport host TLS endpoint readback failed'
printf 'Kodex Teleport host bootstrap completed: %s\n' "$mode"
