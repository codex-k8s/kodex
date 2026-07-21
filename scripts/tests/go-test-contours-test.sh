#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

test_go_command="$(make -C "$repo_root" --no-print-directory -n test-go)"
case "$test_go_command" in
  *"env -u GOFLAGS GOENV=off GOWORK=off go test -tags= ./..."*) ;;
  *)
    echo "FAIL: test-go не фиксирует герметичную Go-конфигурацию" >&2
    exit 1
    ;;
esac
case "$test_go_command" in
  *"-tags=postgres"*)
    echo "FAIL: test-go включает PostgreSQL build tag" >&2
    exit 1
    ;;
esac

postgres_command="$(make -C "$repo_root" --no-print-directory -n test-go-postgres)"
case "$postgres_command" in
  *"env -u GOFLAGS GOENV=off GOWORK=off go run"*"--majors 15,16"*"go test -tags=postgres"*) ;;
  *)
    echo "FAIL: test-go-postgres не фиксирует обе major-версии и PostgreSQL build tag" >&2
    exit 1
    ;;
esac

GOFLAGS='-run=^$ -tags=postgres' make -C "$repo_root" --no-print-directory -n test-go >/dev/null

controller_source="$repo_root/services/external/bot-service/cmd/postgres-test-target/controller.go"
for image in \
  'pgvector/pgvector:0.8.5-pg15@sha256:18d16372b8406bb38a9f94cbff15d125c463d71fde2770aa8b5c64bfcc1578ee' \
  'pgvector/pgvector:0.8.5-pg16@sha256:1d533553fefe4f12e5d80c7b80622ba0c382abb5758856f52983d8789179f0fb'
do
  if ! grep -Fq "$image" "$controller_source"; then
    echo "FAIL: PostgreSQL controller image не закреплён на проверенный OCI digest" >&2
    exit 1
  fi
done
if grep -Eq 'return "pgvector/pgvector:0\.8\.5-pg(15|16)"' "$controller_source"; then
  echo "FAIL: PostgreSQL controller допускает mutable image tag" >&2
  exit 1
fi

for lifetime_guard in \
  '"--rm"' \
  '"--entrypoint", "/usr/bin/timeout"' \
  'ActiveDeadlineSeconds:' \
  'TTLSecondsAfterFinished:' \
  'NewControllerRef(owner'
do
  if ! grep -Fq "$lifetime_guard" "$controller_source"; then
    echo "FAIL: PostgreSQL controller не имеет независимого kill/OOM-safe lifetime guard" >&2
    exit 1
  fi
done
if grep -Fq '"network", "create"' "$controller_source"; then
  echo "FAIL: Docker controller создаёт persistent custom network" >&2
  exit 1
fi

target_source="$repo_root/services/external/bot-service/cmd/postgres-test-target/main.go"
for cache_guard in \
  'materializeGoCacheEnvironment' \
  '"GOENV=off", "GOFLAGS=", "GOWORK=off"' \
  '"GOMODCACHE", "GOCACHE", "GOPATH"'
do
  if ! grep -Fq "$cache_guard" "$target_source"; then
    echo "FAIL: PostgreSQL target не материализует server-owned Go cache paths до смены HOME" >&2
    exit 1
  fi
done

echo "PASS: Go test contours игнорируют внешние test-affecting GOFLAGS"
