#!/usr/bin/env bash
set -euo pipefail

package="${KODEX_POSTGRES_TEST_PACKAGE:-./services/internal/access-manager/internal/repository/postgres/access}"

if [[ -n "${KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN:-}" ]]; then
	go test "${package}" -run 'TestRepositoryIntegration' -count=1
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

port="$(docker port "${container}" 5432/tcp | awk -F: '{print $NF}' | head -n 1)"
if [[ -z "${port}" ]]; then
	echo "test-go-postgres: failed to resolve mapped PostgreSQL port" >&2
	exit 1
fi

export KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN="postgres://postgres:${password}@127.0.0.1:${port}/kodex_access_manager_test?sslmode=disable"
go test "${package}" -run 'TestRepositoryIntegration' -count=1
