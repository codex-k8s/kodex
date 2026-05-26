#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
kubectl_bin="${KODEX_TEST_KUBECTL:-kubectl}"

if ! command -v "${kubectl_bin}" >/dev/null 2>&1; then
	echo "test-go-postgres-k8s: kubectl is unavailable" >&2
	exit 1
fi

image="${KODEX_TEST_POSTGRES_IMAGE:-postgres:16-alpine}"
ready_timeout="${KODEX_TEST_POSTGRES_K8S_READY_TIMEOUT:-120s}"
run_id="${KODEX_TEST_POSTGRES_K8S_RUN_ID:-$(date +%s)-${RANDOM}-$$}"
pod="kodex-pg-test-${run_id}"
service="${pod}"
namespace="${KODEX_TEST_POSTGRES_K8S_NAMESPACE:-}"
password="${KODEX_TEST_POSTGRES_PASSWORD:-kodextest${RANDOM}$$}"
created_namespace=0
port_forward_pid=""
port_forward_log=""

cleanup() {
	set +e
	if [[ -n "${port_forward_pid}" ]] && kill -0 "${port_forward_pid}" >/dev/null 2>&1; then
		kill "${port_forward_pid}" >/dev/null 2>&1 || true
		wait "${port_forward_pid}" >/dev/null 2>&1 || true
	fi
	if [[ -n "${namespace}" ]]; then
		if [[ "${created_namespace}" == "1" ]]; then
			"${kubectl_bin}" delete namespace "${namespace}" --ignore-not-found >/dev/null 2>&1 || true
		else
			"${kubectl_bin}" -n "${namespace}" delete service "${service}" --ignore-not-found >/dev/null 2>&1 || true
			"${kubectl_bin}" -n "${namespace}" delete pod "${pod}" --ignore-not-found >/dev/null 2>&1 || true
		fi
	fi
	if [[ -n "${port_forward_log}" ]]; then
		rm -f "${port_forward_log}"
	fi
}
trap cleanup EXIT

if [[ -z "${namespace}" ]]; then
	namespace="${pod}"
	"${kubectl_bin}" create namespace "${namespace}" >/dev/null
	created_namespace=1
elif ! "${kubectl_bin}" get namespace "${namespace}" >/dev/null 2>&1; then
	if [[ "${KODEX_TEST_POSTGRES_K8S_CREATE_NAMESPACE:-true}" != "true" ]]; then
		echo "test-go-postgres-k8s: namespace ${namespace} does not exist" >&2
		exit 1
	fi
	"${kubectl_bin}" create namespace "${namespace}" >/dev/null
	created_namespace=1
fi

"${kubectl_bin}" -n "${namespace}" run "${pod}" \
	--image="${image}" \
	--restart=Never \
	--labels="app.kubernetes.io/name=kodex-test-postgres,app.kubernetes.io/instance=${pod}" \
	--env POSTGRES_DB=kodex_access_manager_test \
	--env POSTGRES_USER=postgres \
	--env "POSTGRES_PASSWORD=${password}" \
	--port=5432 >/dev/null

"${kubectl_bin}" -n "${namespace}" expose pod "${pod}" \
	--name "${service}" \
	--port=5432 \
	--target-port=5432 >/dev/null

"${kubectl_bin}" -n "${namespace}" wait --for=condition=Ready "pod/${pod}" --timeout="${ready_timeout}" >/dev/null

for _ in $(seq 1 60); do
	if "${kubectl_bin}" -n "${namespace}" exec "${pod}" -- pg_isready -U postgres -d kodex_access_manager_test >/dev/null 2>&1; then
		break
	fi
	sleep 1
done

if ! "${kubectl_bin}" -n "${namespace}" exec "${pod}" -- pg_isready -U postgres -d kodex_access_manager_test >/dev/null 2>&1; then
	echo "test-go-postgres-k8s: PostgreSQL did not become ready" >&2
	exit 1
fi

for database in \
	kodex_platform_event_log_test \
	kodex_project_catalog_test \
	kodex_package_hub_test \
	kodex_provider_hub_test; do
	"${kubectl_bin}" -n "${namespace}" exec "${pod}" -- createdb -U postgres "${database}" >/dev/null
done

port_forward_log="$(mktemp)"
"${kubectl_bin}" -n "${namespace}" port-forward "svc/${service}" :5432 >"${port_forward_log}" 2>&1 &
port_forward_pid="$!"

local_port=""
for _ in $(seq 1 100); do
	if ! kill -0 "${port_forward_pid}" >/dev/null 2>&1; then
		cat "${port_forward_log}" >&2
		exit 1
	fi
	local_port="$(sed -nE 's/.*127\.0\.0\.1:([0-9]+).*/\1/p' "${port_forward_log}" | head -n 1)"
	if [[ -n "${local_port}" ]]; then
		break
	fi
	sleep 0.1
done

if [[ -z "${local_port}" ]]; then
	echo "test-go-postgres-k8s: failed to establish port-forward" >&2
	cat "${port_forward_log}" >&2
	exit 1
fi

echo "test-go-postgres-k8s: running PostgreSQL integration tests in namespace ${namespace}" >&2

KODEX_TEST_POSTGRES_MODE=external \
	KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN="postgres://postgres:${password}@127.0.0.1:${local_port}/kodex_access_manager_test?sslmode=disable" \
	KODEX_EVENTLOG_TEST_DATABASE_DSN="postgres://postgres:${password}@127.0.0.1:${local_port}/kodex_platform_event_log_test?sslmode=disable" \
	KODEX_PROJECT_CATALOG_TEST_DATABASE_DSN="postgres://postgres:${password}@127.0.0.1:${local_port}/kodex_project_catalog_test?sslmode=disable" \
	KODEX_PACKAGE_HUB_TEST_DATABASE_DSN="postgres://postgres:${password}@127.0.0.1:${local_port}/kodex_package_hub_test?sslmode=disable" \
	KODEX_PROVIDER_HUB_TEST_DATABASE_DSN="postgres://postgres:${password}@127.0.0.1:${local_port}/kodex_provider_hub_test?sslmode=disable" \
	"${repo_root}/scripts/test-go-postgres.sh"
