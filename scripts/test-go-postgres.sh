#!/usr/bin/env bash
set -euo pipefail

access_package="./services/internal/access-manager/internal/repository/postgres/access"
project_catalog_package="./services/internal/project-catalog/internal/repository/postgres/project"

run_postgres_tests() {
	local access_dsn="$1"
	local eventlog_dsn="$2"
	local project_catalog_dsn="$3"
	if [[ -n "${KODEX_POSTGRES_TEST_PACKAGE:-}" ]]; then
		KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN="${access_dsn}" \
			KODEX_PROJECT_CATALOG_TEST_DATABASE_DSN="${project_catalog_dsn}" \
			go test "${KODEX_POSTGRES_TEST_PACKAGE}" -run 'TestRepositoryIntegration' -count=1
		return
	fi
	KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN="${access_dsn}" go test "${access_package}" -run 'TestRepositoryIntegration' -count=1
	KODEX_PROJECT_CATALOG_TEST_DATABASE_DSN="${project_catalog_dsn}" go test "${project_catalog_package}" -run 'TestRepositoryIntegration' -count=1
	(
		cd libs/go/eventlog
		KODEX_EVENTLOG_TEST_DATABASE_DSN="${eventlog_dsn}" go test ./... -run 'TestPostgresIntegration' -count=1
	)
}

if [[ -n "${KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN:-}" ]]; then
	if [[ -z "${KODEX_EVENTLOG_TEST_DATABASE_DSN:-}" ]]; then
		echo "test-go-postgres: KODEX_EVENTLOG_TEST_DATABASE_DSN is required when external access-manager DSN is provided" >&2
		exit 1
	fi
	if [[ -z "${KODEX_PROJECT_CATALOG_TEST_DATABASE_DSN:-}" ]]; then
		echo "test-go-postgres: KODEX_PROJECT_CATALOG_TEST_DATABASE_DSN is required when external access-manager DSN is provided" >&2
		exit 1
	fi
	run_postgres_tests "${KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN}" "${KODEX_EVENTLOG_TEST_DATABASE_DSN}" "${KODEX_PROJECT_CATALOG_TEST_DATABASE_DSN}"
	exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
	echo "test-go-postgres: docker is unavailable and KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN is empty" >&2
	exit 1
fi

image="${KODEX_TEST_POSTGRES_IMAGE:-postgres:16-alpine}"
container="kodex-access-manager-test-${RANDOM}-$$"
password="${KODEX_TEST_POSTGRES_PASSWORD:-kodex-test-${RANDOM}-$$}"

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

port="$(docker port "${container}" 5432/tcp | awk -F: '{print $NF}' | head -n 1)"
if [[ -z "${port}" ]]; then
	echo "test-go-postgres: failed to resolve mapped PostgreSQL port" >&2
	exit 1
fi

run_postgres_tests \
	"postgres://postgres:${password}@127.0.0.1:${port}/kodex_access_manager_test?sslmode=disable" \
	"postgres://postgres:${password}@127.0.0.1:${port}/kodex_platform_event_log_test?sslmode=disable" \
	"postgres://postgres:${password}@127.0.0.1:${port}/kodex_project_catalog_test?sslmode=disable"
