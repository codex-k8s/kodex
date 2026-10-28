#!/usr/bin/env bash
set -euo pipefail

# Новый native dispatch имеет отдельный поддержанный фильтр. Прежние suite,
# default filter и их checkcounts не заменяются и не считаются проверенными здесь.
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
exec bash "$repository_root/scripts/tests/control-plane-postgres-test.sh" '^TestWorkflowDispatchComponent$'
