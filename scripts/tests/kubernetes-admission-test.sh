#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
KUBERNETES_VERSION="v1.36.1"
ETCD_VERSION="v3.6.6"
KUBE_APISERVER_SHA256="9b4dba0a5b945f1fe0ce18f47535c5ff0c46ae384f9222047bce39fe91b6023e"
ETCD_ARCHIVE_SHA256="887afaa4a99f22d802ccdfbe65730a5e79aa5c9ce2c8799c67e9d804c50ecedb"
ETCD_BINARY_SHA256="e3d7d2950b4efbebb7c585a5876c2dc7db4a0c4b8ed95939121be853e0c55a27"
ADMISSION_CACHE_DIR="${MATTERCODEX_KUBERNETES_ADMISSION_CACHE_DIR:-/tmp/mattercodex-kubernetes-admission-binaries}"
TEMP_DIR="$(mktemp -d)"
APISERVER_PID=""
ETCD_PID=""

cleanup() {
  if [ -n "$APISERVER_PID" ] && kill -0 "$APISERVER_PID" 2>/dev/null; then
    kill "$APISERVER_PID" 2>/dev/null || true
    wait "$APISERVER_PID" 2>/dev/null || true
  fi
  if [ -n "$ETCD_PID" ] && kill -0 "$ETCD_PID" 2>/dev/null; then
    kill "$ETCD_PID" 2>/dev/null || true
    wait "$ETCD_PID" 2>/dev/null || true
  fi
  rm -rf "$TEMP_DIR"
}
trap cleanup EXIT

for command_name in curl tar sha256sum openssl kubectl go nc awk; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'не найдена обязательная команда real Kubernetes admission contour: %s\n' "$command_name" >&2
    exit 1
  }
done

mkdir -p "$ADMISSION_CACHE_DIR"

fetch_kube_apiserver() {
  local target="$ADMISSION_CACHE_DIR/kube-apiserver-${KUBERNETES_VERSION}"
  if [ -x "$target" ] && printf '%s  %s\n' "$KUBE_APISERVER_SHA256" "$target" | sha256sum -c - >/dev/null 2>&1; then
    printf '%s\n' "$target"
    return
  fi
  local download="$TEMP_DIR/kube-apiserver"
  curl -fsSL "https://dl.k8s.io/release/${KUBERNETES_VERSION}/bin/linux/amd64/kube-apiserver" -o "$download"
  printf '%s  %s\n' "$KUBE_APISERVER_SHA256" "$download" | sha256sum -c - >/dev/null
  install -m 0755 "$download" "$target"
  printf '%s\n' "$target"
}

fetch_etcd() {
  local target="$ADMISSION_CACHE_DIR/etcd-${ETCD_VERSION}"
  if [ -x "$target" ] && printf '%s  %s\n' "$ETCD_BINARY_SHA256" "$target" | sha256sum -c - >/dev/null 2>&1; then
    printf '%s\n' "$target"
    return
  fi
  local archive="$TEMP_DIR/etcd.tar.gz"
  local extract="$TEMP_DIR/etcd-extract"
  curl -fsSL "https://github.com/etcd-io/etcd/releases/download/${ETCD_VERSION}/etcd-${ETCD_VERSION}-linux-amd64.tar.gz" -o "$archive"
  printf '%s  %s\n' "$ETCD_ARCHIVE_SHA256" "$archive" | sha256sum -c - >/dev/null
  mkdir -p "$extract"
  tar -xzf "$archive" -C "$extract"
  install -m 0755 "$extract/etcd-${ETCD_VERSION}-linux-amd64/etcd" "$target"
  printf '%s\n' "$target"
}

allocate_port() {
  local candidate
  for _ in $(seq 1 100); do
    candidate="$((20000 + RANDOM % 20000))"
    if ! nc -z 127.0.0.1 "$candidate" >/dev/null 2>&1; then
      printf '%s\n' "$candidate"
      return
    fi
  done
  printf 'не удалось выбрать loopback port для одноразового API server\n' >&2
  return 1
}

wait_http_ready() {
  local url="$1"
  local pid="$2"
  local log_file="$3"
  local deadline="$(( $(date +%s) + 30 ))"
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      tail -n 80 "$log_file" >&2 || true
      return 1
    fi
    if curl -fsS "$url" >/dev/null 2>&1; then
      return
    fi
    sleep 0.2
  done
  tail -n 80 "$log_file" >&2 || true
  return 1
}

KUBE_APISERVER_BIN="${MATTERCODEX_TEST_KUBE_APISERVER_BIN:-$(fetch_kube_apiserver)}"
ETCD_BIN="${MATTERCODEX_TEST_ETCD_BIN:-$(fetch_etcd)}"
ETCD_PORT="$(allocate_port)"
ETCD_PEER_PORT="$(allocate_port)"
while [ "$ETCD_PEER_PORT" = "$ETCD_PORT" ]; do
  ETCD_PEER_PORT="$(allocate_port)"
done
APISERVER_PORT="$(allocate_port)"
while [ "$APISERVER_PORT" = "$ETCD_PORT" ] || [ "$APISERVER_PORT" = "$ETCD_PEER_PORT" ]; do
  APISERVER_PORT="$(allocate_port)"
done

openssl req -x509 -newkey rsa:2048 -nodes -keyout "$TEMP_DIR/ca.key" -out "$TEMP_DIR/ca.crt" -subj '/CN=mattercodex-admission-ca' -days 1 >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -keyout "$TEMP_DIR/apiserver.key" -out "$TEMP_DIR/apiserver.csr" -subj '/CN=kube-apiserver' -addext 'subjectAltName=IP:127.0.0.1,DNS:localhost' >/dev/null 2>&1
openssl x509 -req -in "$TEMP_DIR/apiserver.csr" -CA "$TEMP_DIR/ca.crt" -CAkey "$TEMP_DIR/ca.key" -CAcreateserial -out "$TEMP_DIR/apiserver.crt" -days 1 -copy_extensions copy >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -keyout "$TEMP_DIR/admin.key" -out "$TEMP_DIR/admin.csr" -subj '/CN=mattercodex-admission-admin/O=system:masters' >/dev/null 2>&1
openssl x509 -req -in "$TEMP_DIR/admin.csr" -CA "$TEMP_DIR/ca.crt" -CAkey "$TEMP_DIR/ca.key" -CAcreateserial -out "$TEMP_DIR/admin.crt" -days 1 >/dev/null 2>&1
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$TEMP_DIR/service-account.key" >/dev/null 2>&1
openssl rsa -in "$TEMP_DIR/service-account.key" -pubout -out "$TEMP_DIR/service-account.pub" >/dev/null 2>&1

"$ETCD_BIN" \
  --name mattercodex-admission \
  --data-dir "$TEMP_DIR/etcd-data" \
  --listen-client-urls "http://127.0.0.1:${ETCD_PORT}" \
  --advertise-client-urls "http://127.0.0.1:${ETCD_PORT}" \
  --listen-peer-urls "http://127.0.0.1:${ETCD_PEER_PORT}" \
  --initial-advertise-peer-urls "http://127.0.0.1:${ETCD_PEER_PORT}" \
  --initial-cluster "mattercodex-admission=http://127.0.0.1:${ETCD_PEER_PORT}" \
  --initial-cluster-state new \
  --log-level error >"$TEMP_DIR/etcd.log" 2>&1 &
ETCD_PID="$!"
wait_http_ready "http://127.0.0.1:${ETCD_PORT}/health" "$ETCD_PID" "$TEMP_DIR/etcd.log"

"$KUBE_APISERVER_BIN" \
  --advertise-address=127.0.0.1 \
  --bind-address=127.0.0.1 \
  --secure-port="$APISERVER_PORT" \
  --etcd-servers="http://127.0.0.1:${ETCD_PORT}" \
  --service-cluster-ip-range=10.240.0.0/24 \
  --authorization-mode=RBAC \
  --anonymous-auth=false \
  --client-ca-file="$TEMP_DIR/ca.crt" \
  --tls-cert-file="$TEMP_DIR/apiserver.crt" \
  --tls-private-key-file="$TEMP_DIR/apiserver.key" \
  --service-account-key-file="$TEMP_DIR/service-account.pub" \
  --service-account-signing-key-file="$TEMP_DIR/service-account.key" \
  --service-account-issuer=https://kubernetes.default.svc \
  --api-audiences=https://kubernetes.default.svc \
  --enable-priority-and-fairness=false \
  --profiling=false >"$TEMP_DIR/kube-apiserver.log" 2>&1 &
APISERVER_PID="$!"

KUBECONFIG_PATH="$TEMP_DIR/kubeconfig"
kubectl config --kubeconfig "$KUBECONFIG_PATH" set-cluster admission \
  --server="https://127.0.0.1:${APISERVER_PORT}" \
  --certificate-authority="$TEMP_DIR/ca.crt" \
  --embed-certs=true >/dev/null
kubectl config --kubeconfig "$KUBECONFIG_PATH" set-credentials admin \
  --client-certificate="$TEMP_DIR/admin.crt" \
  --client-key="$TEMP_DIR/admin.key" \
  --embed-certs=true >/dev/null
kubectl config --kubeconfig "$KUBECONFIG_PATH" set-context admission --cluster=admission --user=admin >/dev/null
kubectl config --kubeconfig "$KUBECONFIG_PATH" use-context admission >/dev/null

deadline="$(( $(date +%s) + 30 ))"
while ! KUBECONFIG="$KUBECONFIG_PATH" kubectl get --raw=/readyz >/dev/null 2>&1; do
  if ! kill -0 "$APISERVER_PID" 2>/dev/null || [ "$(date +%s)" -ge "$deadline" ]; then
    tail -n 120 "$TEMP_DIR/kube-apiserver.log" >&2 || true
    exit 1
  fi
  sleep 0.2
done

mkdir -p "$TEMP_DIR/bin" "$TEMP_DIR/render"
cat >"$TEMP_DIR/bin/envsubst" <<'EOF'
#!/usr/bin/env bash

awk '
  {
    line = $0
    while (match(line, /\$\{[A-Za-z_][A-Za-z0-9_]*\}/)) {
      variable = substr(line, RSTART + 2, RLENGTH - 3)
      line = substr(line, 1, RSTART - 1) ENVIRON[variable] substr(line, RSTART + RLENGTH)
    }
    print line
  }
'
EOF
chmod 0755 "$TEMP_DIR/bin/envsubst"

ADMISSION_NAMESPACE="mc-admission-$((RANDOM % 10000))-$$"
ADMISSION_PRIORITY_CLASS="mc-admission-$((RANDOM % 10000))-$$"
cat >"$TEMP_DIR/admission.env" <<EOF
TARGET_HOST=synthetic.invalid
TARGET_PORT=22
TARGET_ROOT_USER=synthetic
TARGET_ROOT_SSH_KEY=/tmp/synthetic-key
OPERATOR_USER=synthetic
OPERATOR_SSH_PUBKEY_PATH=/tmp/synthetic-pubkey
PRODUCTION_NAMESPACE=${ADMISSION_NAMESPACE}
PRODUCTION_DOMAIN=synthetic.invalid
PUBLIC_BASE_URL=https://mattermost.synthetic.invalid
LETSENCRYPT_EMAIL=synthetic@example.invalid
MATTERCODEX_NAMESPACE=${ADMISSION_NAMESPACE}
MATTERCODEX_RUNTIME_NAMESPACE=${ADMISSION_NAMESPACE}
MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY=4Gi
MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET=3Gi
MATTERCODEX_RUNTIME_SYSTEM_MEMORY_RESERVE=1Gi
MATTERCODEX_AGENT_SESSION_MEMORY_REQUEST=1Gi
MATTERCODEX_AGENT_SESSION_MEMORY_LIMIT=1Gi
MATTERCODEX_AGENT_UTILITY_MEMORY_LIMIT=1Gi
MATTERCODEX_AGENT_DEV_SHM_SIZE_LIMIT=1Gi
MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS=${ADMISSION_PRIORITY_CLASS}
MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT=matter-codex-agent-runner
EOF

PATH="$TEMP_DIR/bin:$PATH" bash "$REPO_ROOT/scripts/k8s/render-bot-service.sh" \
  --env-file "$TEMP_DIR/admission.env" \
  --render-dir "$TEMP_DIR/render" >/dev/null

KUBECONFIG="$KUBECONFIG_PATH" \
MATTERCODEX_ADMISSION_NAMESPACE="$ADMISSION_NAMESPACE" \
MATTERCODEX_ADMISSION_RENDER_DIR="$TEMP_DIR/render" \
MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS="$ADMISSION_PRIORITY_CLASS" \
MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT="matter-codex-agent-runner" \
env -u GOFLAGS GOENV=off GOWORK=off go test -tags=kubeadmission \
  ./services/external/bot-service/internal/integration/kubernetes \
  -run '^TestAgentMemoryGuardRealKubernetesAdmission$' -count=1 -v

printf 'real Kubernetes admission contour: PASS\n'
