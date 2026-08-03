---
id: RUN-MC-008
title: Диагностика и восстановление runtime-controller
type: runbook
status: approved
owner: sre
version: 1.1.0
updated: 2026-08-03
---

# Диагностика и восстановление runtime-controller

## Предварительная проверка

Зафиксировать Git SHA и digest образов. Не выводить lease token, application
grant, DSN, STS credentials, NATS credentials, TLS private key или PVC data.
Kubernetes API CIDR/ports заново разрешать из Service и готовых EndpointSlice;
не добавлять wildcard egress.

```bash
kubectl -n mattercodex-system get deploy,pod,job,pvc,configmap,networkpolicy \
  -l app.kubernetes.io/name=runtime-controller
kubectl -n mattercodex-system get events \
  --field-selector involvedObject.kind=Pod --sort-by=.lastTimestamp
```

Нельзя вручную patch-ить journal, RuntimeExecution, Pod handoff/archive gate,
cleanup claim либо S3 metadata.

## Controller не готов

Проверить по именам, без значений:

- workload TLS, application grant, PostgreSQL, NATS, OTLP/Sentry и Kubernetes
  RBAC;
- `CONTROL_PLANE` exact subject set и limits;
- `RUNTIME_CONTROLLER_V1` filter/ack policy/`MaxDeliver`, pending и
  ack-pending;
- PostgreSQL inbox principal, blockage/dead-letter и protected gRPC readiness;
- S3 versioning/Object Lock/KMS/public-access block и ожидаемый `AccessDenied`
  для List.

Готовность обязана проходить тот же mTLS+bearer+authorization path, что рабочие
RPC. Не удалять consumer и не ослаблять stream/inbox checks для получения 200.

## Durable inbox backlog или dead letter

Dashboard различает pending, ack-pending, redelivery, PostgreSQL blockage и
dead-letter. Сопоставить ordering key, delivery count и durable outcome, не
выводя envelope payload. На `MaxDeliver` consumer сам выполняет
`ReadDeliveryOutcome -> Recover -> ReadDeliveryOutcome`; ACK допустим только
для доказанного applied/duplicate outcome.

Если PostgreSQL blockage сохранён, сначала устранить исходную dependency либо
semantic ошибку. Bounded tenant-scoped repair/skip выполняется только
утверждённой owner-командой с аудитом; ручной `nats consumer next/ack`, удаление
inbox row/cursor или recreation durable запрещены. После восстановления
проверить, что predecessor обработан, blockage исчез, pending уменьшается, а
dead-letter counter не растёт.

## PENDING/admission recovery и capacity

Для deferred execution journal должен существовать в фазе `CLAIMED` до
revision/capacity. Сверить exact execution/revision/input/fence/generation и
immutable provider observation. Stale/unknown observation, organization limit,
provider concurrency, ResourceQuota или node pressure штатно оставляют работу
queued.

Если PostgreSQL уже содержит exact `PENDING`, а journal отсутствует после
неуспешного Create, не создавать ConfigMap вручную. Следующий controller claim
с новым server-owned idempotency key повторно обнаружит только тот же
session/turn/attempt/grant/revision/input tuple и восстановит journal.

При crash после owner admission journal может быть `ADMITTED_RECOVERABLE` без
Pod. Controller replay-ит тот же admission receipt и восстанавливает lease;
создавать lease Secret или новый idempotency key вручную запрещено. Для
несовместимого session Pod controller выполняет guarded replacement; PVC
остаётся.

## Runner handoff и warm Pod

Pod `Succeeded`/`Failed` без `mattercodex.runtime-turn-handoff.v1` не является
terminal доказательством. Проверить incident `runner_exited_without_handoff`,
runner logs без credential values и owner lease/expiry. Не вызывать complete
вручную.

После корректного handoff gate остаётся `CLOSED`, пока archive и restore proof
не подтверждены. Только затем controller открывает `OPEN`. Successor допустим
на том же Running/Ready Pod лишь при том же RuntimeRevision SHA-256; он получает
новый immutable input и сам переводит `SUCCESSOR_READY` обратно в `CLOSED`.
Застрявший или несовместимый gate требует устранить причину и дать controller
выполнить guarded replacement, не patch Pod вручную.

## Archive, restore и rehydrate

Проверить Job identity: `runtime-archive` не равна controller, restore и
rehydrate используют отдельную restore identity. У STS credential должны быть
bounded TTL и exact organization/project/session/execution/source tags;
значения не показывать.

Для archive нужны exact S3 `VersionId`, checksum, size, KMS, COMPLIANCE Object
Lock и metadata provenance. Restore proof обязан относиться к той же версии.
При новом turn после очищенного PVC `runtime-rehydrate` должен монтировать
новый empty PVC и завершить journal proof с exact PVC UID до появления role
Pod. Любой version/checksum/metadata/tree/PVC mismatch сохраняет Pod
отсутствующим. Запрещены ручной `aws s3 cp`, выбор latest object без version и
копирование workspace в обход proof.

## Двухфазный cleanup

Четыре часа простоя удаляют только warm Pod. Eligibility PVC определяет
`control-plane` по authoritative `session.updated_at + 7d`, а не Pod/journal
timestamp. Сначала authorizer под session graph lock создаёт `ACTIVE` claim с
exact PVC name/UID/resourceVersion и expiry; это блокирует новый work той же
session. Только затем controller может Delete с preconditions.

После Delete controller обязан получить `NotFound` и передать exact observed
timestamp + deletion proof в `ConsumeRuntimeCleanupAuthorization`. Если claim
истёк, graph изменился, tuple отличается или readback неизвестен, finalize
fail-closed; новая попытка начинается с новой authorizer generation. Client
grace, delete-before-claim и признание произвольного NotFound отсутствуют.

Backfill автоматически проходит ту же archive/proof/claim/delete/consume
цепочку. Legacy PVC без server-owned execution/journal остаётся
`inventory-only`. Disk pressure не сокращает 7 суток и не обходит proof.

## Access profile incident

Routine controller не имеет Secret access и права создавать/bind-ить
`cluster-admin`. `PROJECT_READ_ONLY` binding должен соответствовать exact
project namespace и session identity. `CLUSTER_ADMIN` использует только
предустановленную ServiceAccount/ClusterRoleBinding; admission policy разрешает
controller создать с ней лишь exact role Pod, который удаляется после terminal.
Не исправлять отказ добавлением broad RBAC либо wildcard NetworkPolicy.

## Rollback

Откат образа выполняется на предыдущий подписанный digest после отдельного
владельческого шлюза. Миграции forward-only: изменение схемы компенсируется
новой migration. До восстановления совместимого consumer остановить
controller, не удаляя PVC, journals, durable inbox, consumer или S3 objects.
Не откатывать IAM/Object Lock/KMS требования и не выдавать static credentials.
