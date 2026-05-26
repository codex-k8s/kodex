#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

access_package="./services/internal/access-manager/internal/repository/postgres/access"
project_catalog_package="./services/internal/project-catalog/internal/repository/postgres/project"
package_hub_package="./services/internal/package-hub/internal/repository/postgres/catalog"
provider_hub_package="./services/internal/provider-hub/internal/repository/postgres/provider"

dsn_env_names=(
	"KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN"
	"KODEX_EVENTLOG_TEST_DATABASE_DSN"
	"KODEX_PROJECT_CATALOG_TEST_DATABASE_DSN"
	"KODEX_PACKAGE_HUB_TEST_DATABASE_DSN"
	"KODEX_PROVIDER_HUB_TEST_DATABASE_DSN"
)

run_postgres_tests() {
	local access_dsn="$1"
	local eventlog_dsn="$2"
	local project_catalog_dsn="$3"
	local package_hub_dsn="$4"
	local provider_hub_dsn="$5"
	if [[ -n "${KODEX_POSTGRES_TEST_PACKAGE:-}" ]]; then
		KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN="${access_dsn}" \
			KODEX_EVENTLOG_TEST_DATABASE_DSN="${eventlog_dsn}" \
			KODEX_PROJECT_CATALOG_TEST_DATABASE_DSN="${project_catalog_dsn}" \
			KODEX_PACKAGE_HUB_TEST_DATABASE_DSN="${package_hub_dsn}" \
			KODEX_PROVIDER_HUB_TEST_DATABASE_DSN="${provider_hub_dsn}" \
			go test "${KODEX_POSTGRES_TEST_PACKAGE}" -run 'TestRepositoryIntegration' -count=1
		return
	fi
	KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN="${access_dsn}" go test "${access_package}" -run 'TestRepositoryIntegration' -count=1
	KODEX_PROJECT_CATALOG_TEST_DATABASE_DSN="${project_catalog_dsn}" go test "${project_catalog_package}" -run 'TestRepositoryIntegration' -count=1
	KODEX_PACKAGE_HUB_TEST_DATABASE_DSN="${package_hub_dsn}" go test "${package_hub_package}" -run 'TestRepositoryIntegration' -count=1
	KODEX_PROVIDER_HUB_TEST_DATABASE_DSN="${provider_hub_dsn}" go test "${provider_hub_package}" -run 'TestRepositoryIntegration' -count=1
	(
		cd libs/go/eventlog
		KODEX_EVENTLOG_TEST_DATABASE_DSN="${eventlog_dsn}" go test ./... -run 'TestPostgresIntegration' -count=1
	)
}

all_external_dsns_provided() {
	local name
	for name in "${dsn_env_names[@]}"; do
		if [[ -z "${!name:-}" ]]; then
			return 1
		fi
	done
	return 0
}

any_external_dsn_provided() {
	local name
	for name in "${dsn_env_names[@]}"; do
		if [[ -n "${!name:-}" ]]; then
			return 0
		fi
	done
	return 1
}

require_all_external_dsns() {
	local missing=0
	local name
	for name in "${dsn_env_names[@]}"; do
		if [[ -z "${!name:-}" ]]; then
			echo "test-go-postgres: ${name} is required for external PostgreSQL integration tests" >&2
			missing=1
		fi
	done
	return "${missing}"
}

run_external_postgres_tests() {
	require_all_external_dsns
	run_postgres_tests \
		"${KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN}" \
		"${KODEX_EVENTLOG_TEST_DATABASE_DSN}" \
		"${KODEX_PROJECT_CATALOG_TEST_DATABASE_DSN}" \
		"${KODEX_PACKAGE_HUB_TEST_DATABASE_DSN}" \
		"${KODEX_PROVIDER_HUB_TEST_DATABASE_DSN}"
}

kubernetes_runner_available() {
	if ! command -v kubectl >/dev/null 2>&1; then
		return 1
	fi
	if [[ -n "${KODEX_TEST_POSTGRES_K8S_NAMESPACE:-}" ]]; then
		kubectl get namespace "${KODEX_TEST_POSTGRES_K8S_NAMESPACE}" >/dev/null 2>&1 ||
			[[ "${KODEX_TEST_POSTGRES_K8S_CREATE_NAMESPACE:-true}" == "true" ]] ||
			return 1
		kubectl auth can-i create pods -n "${KODEX_TEST_POSTGRES_K8S_NAMESPACE}" >/dev/null 2>&1 || return 1
		kubectl auth can-i create services -n "${KODEX_TEST_POSTGRES_K8S_NAMESPACE}" >/dev/null 2>&1 || return 1
		return 0
	fi
	kubectl auth can-i create namespace >/dev/null 2>&1 || return 1
	kubectl auth can-i create pods --all-namespaces >/dev/null 2>&1 || return 1
	kubectl auth can-i create services --all-namespaces >/dev/null 2>&1 || return 1
}

run_kubernetes_postgres_tests() {
	bash "${repo_root}/scripts/test-go-postgres-k8s.sh"
}

run_docker_postgres_tests() {
	if ! command -v docker >/dev/null 2>&1; then
		echo "test-go-postgres: docker is unavailable; set all KODEX_*_TEST_DATABASE_DSN values or use KODEX_TEST_POSTGRES_MODE=kubernetes" >&2
		exit 1
	fi

	local image="${KODEX_TEST_POSTGRES_IMAGE:-postgres:16-alpine}"
	local container="kodex-postgres-test-${RANDOM}-$$"
	local password="${KODEX_TEST_POSTGRES_PASSWORD:-kodextest${RANDOM}$$}"

	docker run \
		--detach \
		--rm \
		--name "${container}" \
		--env POSTGRES_DB=kodex_access_manager_test \
		--env POSTGRES_PASSWORD="${password}" \
		--env POSTGRES_USER=postgres \
		--publish 127.0.0.1::5432 \
		"${image}" >/dev/null

	cleanup() {
		docker rm --force "${container}" >/dev/null 2>&1 || true
	}
	trap cleanup EXIT

	for _ in $(seq 1 60); do
		if docker exec "${container}" pg_isready -U postgres -d kodex_access_manager_test >/dev/null 2>&1; then
			break
		fi
		sleep 1
	done

	if ! docker exec "${container}" pg_isready -U postgres -d kodex_access_manager_test >/dev/null 2>&1; then
		echo "test-go-postgres: PostgreSQL did not become ready" >&2
		exit 1
	fi
	docker exec "${container}" createdb -U postgres kodex_platform_event_log_test
	docker exec "${container}" createdb -U postgres kodex_project_catalog_test
	docker exec "${container}" createdb -U postgres kodex_package_hub_test
	docker exec "${container}" createdb -U postgres kodex_provider_hub_test

	local port
	port="$(docker port "${container}" 5432/tcp | awk -F: '{print $NF}' | head -n 1)"
	if [[ -z "${port}" ]]; then
		echo "test-go-postgres: failed to resolve mapped PostgreSQL port" >&2
		exit 1
	fi

	run_postgres_tests \
		"postgres://postgres:${password}@127.0.0.1:${port}/kodex_access_manager_test?sslmode=disable" \
		"postgres://postgres:${password}@127.0.0.1:${port}/kodex_platform_event_log_test?sslmode=disable" \
		"postgres://postgres:${password}@127.0.0.1:${port}/kodex_project_catalog_test?sslmode=disable" \
		"postgres://postgres:${password}@127.0.0.1:${port}/kodex_package_hub_test?sslmode=disable" \
		"postgres://postgres:${password}@127.0.0.1:${port}/kodex_provider_hub_test?sslmode=disable"
}

mode="${KODEX_TEST_POSTGRES_MODE:-auto}"

if all_external_dsns_provided; then
	run_external_postgres_tests
	exit 0
fi

if any_external_dsn_provided; then
	require_all_external_dsns
fi

case "${mode}" in
	auto | "")
		if kubernetes_runner_available; then
			run_kubernetes_postgres_tests
		else
			run_docker_postgres_tests
		fi
		;;
	external | dsn)
		run_external_postgres_tests
		;;
	kubernetes | k8s)
		run_kubernetes_postgres_tests
		;;
	docker)
		run_docker_postgres_tests
		;;
	*)
		echo "test-go-postgres: unsupported KODEX_TEST_POSTGRES_MODE=${mode}; use auto, external, kubernetes or docker" >&2
		exit 1
		;;
esac
