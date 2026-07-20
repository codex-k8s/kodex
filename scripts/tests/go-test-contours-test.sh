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
echo "PASS: Go test contours игнорируют внешние test-affecting GOFLAGS"
