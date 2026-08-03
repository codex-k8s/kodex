---
id: ARCH-MC-010
title: Runtime controller и жизненный цикл ресурсов сессии
type: architecture
status: approved
owner: architect
version: 1.0.0
updated: 2026-08-03
---

# Runtime controller и жизненный цикл ресурсов сессии

Документ материализует границу `runtime-controller` из `ARCH-MC-004` и
сквозной контракт Issue #188. `control-plane` остаётся владельцем сессий,
ходов, `RuntimeRevision`, попыток, grant/fence/lease и terminal state.
`runtime-controller` владеет только сверкой Kubernetes-ресурсов, допуском по
ёмкости, runtime health и переносом рабочей копии PVC в S3.

## Защищённые виды и команды

| Вид | Специализированные команды владельца | Кто исполняет |
| --- | --- | --- |
| `RuntimeExecution` | `ClaimRuntimeExecution`, `AdmitRuntimeExecution`, `HeartbeatRuntimeExecution`, `RecordRuntimeIncident`, `CompleteRuntimeExecution`, `ExpireRuntimeExecution` | `runtime-controller` |
| Terminal owner transition | `CancelRuntimeExecution`, `RetryRuntimeExecution` | только owner workload, не controller |
| `RuntimeArchive` | `RecordRuntimeArchive` | изолированный archive Job controller |
| `RuntimeRestoreProof` | `VerifyRuntimeRestore` | отдельный `runtime-restore-verifier` |
| `RuntimeCleanupAuthorization` | `AuthorizeRuntimeCleanup`, `ExpireRuntimeCleanupAuthorization` | отдельный `runtime-cleanup-authorizer` |
| Destructive cleanup | `ConsumeRuntimeCleanupAuthorization` после guarded delete | `runtime-controller` |

Универсальные CRUD-команды не участвуют в этих переходах. Actor, организация,
проект, сессия, ход, попытка, workload, grant generation и immutable input
приходят только из проверенного mTLS, application grant и authority proof.
Идентификаторы Kubernetes labels, AsyncAPI payload и аргументы Job не являются
источником полномочий.

## Карта сквозных сценариев

### Изменение конфигурации

```text
control-plane transaction
  -> RuntimeRevision(version, manifest digest)
  -> transactional outbox
  -> NATS control_plane.runtime_configuration_changed
  -> exact durable consumer runtime-controller
  -> postgresinbox receive/claim/effect
  -> protected GetRuntimeRevision(exact id/version)
  -> service-owned projection + inbox + cursor в одной transaction
```

Consumer проверяет canonical envelope и закрытую AsyncAPI schema до inbox.
`duplicate` и `stale` подтверждаются только после durable result; `gap`,
conflict и terminal predecessor не подтверждаются. Подписка не запускается до
готовности inbox, точной конфигурации JetStream и защищённого gRPC read path.
AsyncAPI `1.1.0` закрывает `resourceKind` и `resourceState` фактически
публикуемыми enum; неизвестное значение отклоняется и в consumer, и в
PostgreSQL effect.

### Новый ход и безопасное пересоздание pod

```text
control-plane current turn/grant
  -> ClaimRuntimeExecution
  -> GetRuntimeRevision(exact pinned version)
  -> проверка manifest/version/input/workload/grant tuple
  -> capacity admission
  -> Kubernetes journal с устойчивым idempotency key
  -> server-owned AdmitRuntimeExecution(version+fence, lease token)
  -> PVC + immutable ConfigMap + immutable Secret + role-image Pod
  -> Ready readback exact UID/revision/fence
  -> HeartbeatRuntimeExecution -> RUNNING
```

Перед каждым execution создаются новые ConfigMap и Secret. Pod с именем,
устойчивым для `(role, thread, session)`, пересоздаётся по UID/resourceVersion,
поэтому старые env, volume projection и credentials не могут обслужить новый
turn. PVC имеет имя сессионной области и переживает pod replacement. В один
момент существует не более одного role pod на `(role, thread, session)`.
Два экземпляра controller не выполняют mutation loops одновременно:
`coordination.k8s.io/Lease` допускает одного лидера, а потеря аренды отменяет
consumer и все workers до закрытия зависимостей.

### Ёмкость и вытеснение

Controller учитывает `ResourceQuota`, requests уже назначенных Pod, Node
conditions и выбранный закрытый `RuntimeResourceClass`. Недостаток ёмкости
оставляет execution в `PENDING`. Допустима одна попытка удаления самого старого
idle pod: candidate обязан быть terminal по защищённому readback, не иметь
`cluster-admin`, совпадать по UID/resourceVersion и не иметь нового execution.
PVC, Secret сессионного состояния и очередь не удаляются. Node pressure не
разрешает принимать новую нагрузку и не ослабляет archive gate.
Session PVC ограничен 30 GiB; archive Job запрашивает scratch `PVC+1 GiB`, а
restore verifier — `2*PVC+1 GiB`, чтобы compressed archive и восстановленное
дерево не вышли за объявленный ephemeral-storage budget.

### Heartbeat, terminal и incident

Kubernetes status является наблюдением, а не владельцем состояния. `Ready`
pod продлевает только точные lease/fence/generation. Missed heartbeat,
неустранимый reconcile и исчезновение workload публикуются через
`RecordRuntimeIncident` с закрытым kind и SHA-256 evidence без payload. Успех
или ошибка role Pod завершаются через `CompleteRuntimeExecution`; cancel,
retry и expiry закрывают прежние lease/grant/claim в owner-транзакции
`control-plane`. Stale journal после любого terminal path не способен
продлить lease или создать новый pod.

### Archive, restore proof и cleanup

```text
terminal RuntimeExecution
  -> archive Job mounts exact PVC read-only
  -> deterministic tar stream + SHA-256
  -> versioned S3 PutObject + HeadObject checksum/version readback
  -> RecordRuntimeArchive
  -> independent restore verifier downloads exact version
  -> checksum + safe extraction + manifest proof
  -> VerifyRuntimeRestore
  -> ожидание PVC retention TTL не менее 7 суток от последней активности
  -> independent cleanup authorizer rereads owner graph
  -> AuthorizeRuntimeCleanup(generation, bounded expiry)
  -> controller rereads exact authorization
  -> delete PVC with UID/resourceVersion preconditions
  -> readback NotFound
  -> ConsumeRuntimeCleanupAuthorization
```

Ошибка S3, checksum, version, extraction, proof, owner graph, authorization,
UID или resourceVersion закрыто сохраняет PVC. `NotFound` допустим как
идемпотентный результат только для того же заранее зафиксированного UID.
Четырёхчасовой TTL удаляет только warm Pod; он не сокращает семисуточную
отсрочку PVC.
Backfill автоматически подхватывает каждый принадлежащий controller terminal
journal без archive либо restore proof и проходит тот же
archive/proof/authorization путь. PVC прежнего runtime без server-owned
`RuntimeExecution` и journal не усыновляется по labels: он остаётся в
`inventory-only` до отдельного авторитетного mapping. Режима обхода и удаления
по нехватке диска нет.

Если несколько turn используют один session PVC, controller переносит
server-owned retention-owner annotation на journal нового admitted execution
optimistic update с новым `resourceVersion`. Старые journals закрывают
собственные access/lease, но не архивируют и не удаляют общее состояние;
guarded delete дополнительно сверяет exact owner annotation и PVC
`resourceVersion`.

## Матрица жизненного цикла

| Переход | Блокировка и проверка | Атомарный авторитетный результат | Kubernetes/S3 effect |
| --- | --- | --- | --- |
| claim | current session/turn/attempt, exact workload/SPIFFE/grant/input/revision | новый immutable `PENDING`, version=1, fence=1, receipt/audit | отсутствует |
| admit | capacity success, current owner graph, expected version/fence/generation | `ADMITTED`, новая lease и fence, receipt/audit | после commit создаются fresh resources; replay использует journal key |
| start/heartbeat | exact lease token, UID/revision readback, неистёкший owner graph | `RUNNING`, продлённые turn/runtime leases, version/fence | status read-only |
| complete | terminal Pod result, exact lease/fence/generation | turn/process/execution terminal, старые grants/claims/leases закрыты | pod становится idle candidate |
| cancel | owner permission и полный graph | `CANCELLED`, все lease/grant/claim закрыты | controller удаляет только pod, PVC сохраняет |
| retry | terminal predecessor, limit и immutable input | predecessor `RETRIED`, новый turn/attempt/revision/grant | новый execution всегда получает fresh Pod/ConfigMap/Secret |
| lease expiry | PostgreSQL clock и точная lease | `EXPIRED`, полный graph закрыт одним победителем | stale pod удаляется guarded delete |
| incident | exact current execution/fence и bounded kind/evidence | audit incident; authority не расширяется | отсутствует |
| archive | terminal graph, exact expected version/fence | immutable S3 reference/checksum | versioned object и exact `HeadObject` readback |
| restore proof | отдельный verifier identity, exact archive checksum/version | immutable proof, verifier identity/generation, version/fence | безопасная загрузка и распаковка во временный volume |
| cleanup authorize | отдельный authorizer, terminal graph, proof, отсутствие continuation/hold | bounded ACTIVE authorization generation | отсутствует |
| PVC cleanup | не менее 7 суток простоя, exact active authorization, terminal graph, UID/resourceVersion | после delete readback authorization `CONSUMED` | один guarded delete; mismatch сохраняет PVC |
| authorization expiry | PostgreSQL clock и неиспользованная authorization | `EXPIRED`, новое удаление требует новой generation | отсутствует |
| idle eviction | terminal readback, idle TTL 4 часа/LRU, no queue/new execution, no cluster-admin | authority не меняется | удаляется только exact Pod, не PVC |
| backfill | controller-owned terminal journal, точный execution/PVC tuple | тот же archive/proof/cleanup graph | неизвестный legacy PVC остаётся inventory-only |

`WAITING_OWNER`, `CHANGES_REQUESTED` и integration continuation принадлежат
`control-plane`. Для controller они означают отсутствие допустимого claim и
безусловный запрет очистки; отдельного локального перехода не создаётся.

## Crash recovery и idempotency

После server-owned claim journal сохраняет version/fence-bound idempotency key
до каждого следующего state-changing RPC. При crash после удалённого commit тот же key и request hash
возвращают receipt. Lease token хранится только в отдельном Secret execution и
не попадает в ConfigMap, log, metric или Pod annotation. Controller сначала
останавливает workers, затем transport, ожидает workers и только после этого
закрывает NATS, PostgreSQL, Kubernetes clients и telemetry; обязательные
cleanup получают независимые bounded contexts.

Перед действием над nonterminal либо retention-owner journal controller
выполняет bounded version-pinned rejoin: exact versions `v..v+3` читаются через
защищённый `GetRuntimeExecution`, а найденный tuple обязан сохранить lineage и
одинаковый монотонный delta version/fence. Так cancel/retry/expiry и crash после
heartbeat/archive/restore commit не остаются скрытыми; отсутствие exact версии
в окне закрыто останавливает effect.

## Deploy и NetworkPolicy

Controller, archive worker, restore verifier и cleanup authorizer имеют разные
ServiceAccount/application grants там, где различается permission. Base
policy закрыта по умолчанию. Маршрут Kubernetes API не зашит в переносимую
policy: renderer окружения обязан получить точный API CIDR либо утверждённый
egress gateway, проверить его и материализовать environment-specific patch.
Без такого результата профили `PROJECT_READ_ONLY` и `CLUSTER_ADMIN` fail-closed
не допускаются. S3 использует TLS с exact hostname/CA и scoped credential;
wildcard HTTPS egress и `skipTLSVerify` запрещены.

`PROJECT_READ_ONLY` не использует общий cluster-wide ServiceAccount. Controller
из server-owned `organization/project` выводит имя проектного namespace,
проверяет его exact annotations и создаёт отдельные ServiceAccount и
RoleBinding для одного execution. `CLUSTER_ADMIN` также получает отдельные
ServiceAccount/ClusterRoleBinding. Terminal transition сначала удаляет binding
и ServiceAccount, затем Pod; projected token имеет bounded TTL. Payload не
может выбрать namespace, role или profile.

## Наблюдаемость

Метрики имеют только закрытые labels `operation` и `outcome`. Session, turn,
execution, organization, project, object key,
bucket и URL доступны только как проверенные структурированные log/trace
attributes. Dashboard покрывает claim/admit/reconcile/heartbeat, capacity,
idle eviction, archive/proof/cleanup, inbox backlog и incidents. Каждый alert
содержит абсолютный HTTPS `runbook_url` на `RUN-MC-008`.
