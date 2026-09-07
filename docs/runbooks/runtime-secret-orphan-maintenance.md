---
id: RUNBOOK-DOC-1141
title: Очистка старой disposable RuntimeSecret materialization
type: runbook
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-07
---

# Очистка старой disposable materialization

Refs #1141, #1031. Это операторская CLI владельца CP, а не runtime recovery
и не новый deployable. Production запрещён. Исполнение допускается после
завершения текущего `up`; параллельный rollout и maintenance не запускаются.

Старый `reset-local.sh` удалял только `kodex-system`. Сохранившийся
`kodex-runtime` мог содержать materialization, которая старше новой owner DB.
Этот путь воспроизводим по source; исторический запуск именно этого reset
на staging не доказан. Неизвестный объект не считается тестовой fixture.

## План и исполнение

Оператор использует clean checkout согласованного SHA, тот же private env,
что и штатный `remote-dev.sh`, и приватный каталог вне checkout с mode0700.
В примерах переменные содержат только пути и metadata name; значения Secret
не запрашиваются и не сохраняются.

```bash
bash tools/dev/remote-dev.sh orphan-plan --env-file "$env_file" \
  --expected-sha "$source_sha" --plan-file "$private_plan" --secret-name "$metadata_name"

bash tools/dev/remote-dev.sh orphan-apply --env-file "$env_file" \
  --expected-sha "$source_sha" --plan-file "$private_plan"
```

Wrapper создаёт штатный временный kubeconfig и удаляет его после команды.
Go CLI самостоятельно читает root-owned marker
`/var/lib/kodex-dev/cluster-identity.json` через `sudo -n`, проверяет его mode,
owner и отсутствие symlink, сверяет cluster UID, HTTPS endpoint и CA digest.
Произвольный digest из аргумента не принимается. План требует фактический
clean HEAD; после merge используется новый actual SHA, а не старый source commit.

План0600 создаётся только через O_EXCL/O_NOFOLLOW и fsync. Он связывает один
Secret UID/resourceVersion, creation epoch, annotations digest, namespace UID,
source/tree и writer UID/resourceVersion/spec digest/исходные replicas.
JSON содержит только metadata descriptor. API Accept не содержит fallback
к полному Secret; actual `immutable`, `type`, `data` и content bytes не читаются
и не объявляются проверенными. Имя выводится из канонического secretRef/revision.

Допуск требует: object старше `kodex-system`, но не старше `kodex-runtime`;
оба namespace принадлежат одному disposable profile; нет owner references,
finalizers, deletion marker, глобальных CP owner/history/JSON/cross-purpose
references и Kubernetes consumers. SQL выполняется read-only с timeout,
row_security=off и проверенным session_user=current_user SUPERUSER; tenant
RLS scope не может выдать ложное отсутствие. Все исторические references
блокируют cleanup, независимо от terminal состояния.

Перед эффектом CLI атомарно ставит broker replicas0 с UID/RV checks, ждёт
отсутствия всех его Pod, повторяет metadata, owner и consumer проверки.
HPA для broker запрещает maintenance. До единственного DELETE сохраняется
durable `DELETE_UNKNOWN`; запрос содержит UID и resourceVersion preconditions.
Отдельный metadata NotFound и повторный owner/reference readback завершают
receipt. При любом выходе после паузы выполняется bounded restoration исходных
replicas только для того же Deployment UID и spec digest.

`orphan-apply` после UNKNOWN не повторяет DELETE: разрешены только readback
и guarded restoration. Если объект остаётся либо появился иной UID, команда
закрыто завершается. Не удаляйте receipt и не создавайте новый plan, чтобы
обойти UNKNOWN; manager рассматривает сохранённое состояние отдельно.
Ошибка restoration сохраняется как незавершённое восстановление; повтор
также не повторяет DELETE. Удалённый orphan не восстанавливается под прежним UID.

## Scoped reset

После прежнего явного destructive confirmation используется:

```bash
bash tools/dev/remote-dev.sh reset-local --env-file "$env_file" \
  --expected-sha "$source_sha" --confirm DELETE-KODEX-LOCAL-DATA
```

Прямой `tools/dev/reset-local.sh` сохраняет `--context` и `--confirm`, дополнительно
требует `--expected-sha` и root-owned cluster marker, как maintenance CLI.
Оба namespace проверяются до первого DELETE; чужой runtime profile закрывает
операцию до удаления CP. Runtime удаляется первым с UID/RV preconditions,
затем system; после каждого эффекта выполняются NotFound и UID readback всех
чужих namespace, включая identity. Отсутствующий runtime допустим.

## Поддерживаемые проверки

- `make test-runtime-orphan-api` с заранее локальным
  `KODEX_ORPHAN_TEST_IMAGE=sha256:...`: disposable k3s без agent/network,
  настоящий metadata Accept и DELETE preconditions, writer fence, reset и cleanup.
- `make test-runtime-orphan-postgres`: canonical CP migrations и каждая группа
  direct/historical JSON/cross-purpose references, отказ ограниченному reader.
- Unit/race пакета `internal/maintenance/runtimeorphan`: durable UNKNOWN,
  потерянный ACK, однократный эффект, restoration, consumer groups, private files.

Локальные проверки не означают успешную очистку staging. После исполнения
согласованного плана отдельно проверяются штатный up, protected Secret Broker
readiness и API/session. До этого live outcome и QA остаются NOT RUN.
