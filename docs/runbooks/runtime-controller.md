---
id: RUN-MC-008
title: Диагностика и восстановление runtime-controller
type: runbook
status: approved
owner: sre
version: 1.4.0
updated: 2026-08-04
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
на том же Running/Ready Pod лишь при том же `effective_runtime_sha256`; он
получает fresh RuntimeRevision, authority/credential snapshots и сам переводит
`SUCCESSOR_READY` обратно в `CLOSED`.
Застрявший или несовместимый gate требует устранить причину и дать controller
выполнить guarded replacement, не patch Pod вручную.

До первого claim и после каждого warm successor `/readyz` должен оставаться
503, пока runner не выполнит bot-session readiness read и MCP `initialize` по
рабочему TLS 1.3/mTLS exact SNI/CA/client certificate + bearer пути. Ошибка
CA/certificate/bearer обязана сделать Pod NotReady до claim. Plaintext
localhost probe не считается peer/application проверкой; после восстановления
owner-issued immutable snapshot тот же readback должен вернуть 204.

## Archive, restore и rehydrate

Проверить Job identity: `runtime-archive` не равна controller, restore и
rehydrate используют отдельную restore identity. `runtime-s3-*-broker` Jobs не
должны иметь RoleBinding, Kubernetes/Vault token, broker config либо прямой S3
egress; они могут обратиться только к action-specific mTLS exchanger. У execution/action STS
credential должны быть TTL не более 15 минут, exact organization/project/
session/execution/source tags, inline policy/readback digests и immutable
Secret UID/resourceVersion; значения не показывать.
Проверить, что только `runtime-s3-archive-exchanger` либо
`runtime-s3-restore-exchanger` имеет соответствующий Vault action role,
server-side выводит tags/inline policy из owner-signed ticket и создаёт
immutable one-time receipt. Vault action role выдаёт только bootstrap lease для
`AssumeRole`/`TagSession` закреплённой execution role, а final credential
получен по exact TLS S3/STS endpoint с `archive-role-arn` либо
`restore-role-arn`; передавать inline policy/session tags в Vault generate
endpoint запрещено, потому что этот endpoint их не принимает.

Для archive нужны quiesced CSI snapshot/clone, exact S3 `VersionId`, checksum,
size, KMS, COMPLIANCE Object Lock не менее 90 суток, exact retain-until
`HeadObject`+`GetObjectRetention` и metadata provenance. Restore proof обязан
относиться к той же версии.
При новом turn после очищенного PVC `runtime-rehydrate` должен монтировать
новый empty PVC, bind одноразового assignment к generation/name/UID/
resourceVersion и завершить owner `CONSUMED` proof до появления role Pod.
Staging находится на том же filesystem и публикуется atomic rename; после
crash exact-owned incomplete generation можно удалить/повторить только после
проверки marker; regular files, marker, все directories и parent вокруг rename
обязаны быть fsync. Чужой final tree не удалять. Любой
version/checksum/metadata/tree/PVC mismatch сохраняет Pod отсутствующим.

## Двухфазный cleanup

Четыре часа простоя удаляют только warm Pod. Eligibility PVC определяет
`control-plane` по pinned `ResourceRetentionPolicy` id/version и
`pvc_cleanup_eligible_at`, а не текущей конфигурации, Pod/journal timestamp.
Сначала authorizer под session graph lock создаёт `ACTIVE` claim с
exact PVC name/UID/resourceVersion и expiry; это блокирует новый work той же
session. Только затем controller может Delete с preconditions.

После Delete controller обязан получить `NotFound` и передать exact observed
timestamp + deletion proof в `ConsumeRuntimeCleanupAuthorization`. Если claim
истёк, graph изменился, tuple отличается или readback неизвестен, finalize
fail-closed. Для уже удалённой PVC durable NotFound proof под полным owner lock
переносится в новую generation и немедленно finalize-ится; permanent wedge не
допускается. Client
grace, delete-before-claim и признание произвольного NotFound отсутствуют.

Backfill автоматически проходит ту же archive/proof/claim/delete/consume
цепочку. Legacy PVC без server-owned execution/journal остаётся
`inventory-only`. Disk pressure не меняет pinned eligibility и не обходит proof.

Перед ручной/юридической приостановкой получить current Session version и
вызвать специализированный `HoldRuntimeRetention` через утверждённый
control-api identity с `MANUAL` либо `LEGAL`, новым idempotency key и reason.
Сохранить server-assigned `hold_id` и version без bearer/token values. Для
снятия использовать `ReleaseRuntimeRetention` с exact hold/version. До и после
операции `GetResourceRetentionPolicy` должен вернуть current monotonic policy;
active execution продолжает ссылаться на прежний pin. Hold обязан блокировать
issue/consume/reissue/expiry cleanup; SQL update/delete и payload flag не
являются допустимым обходом.

## Access profile incident

Routine controller и `runtime-credential-broker`, `runtime-project-read-broker`,
`runtime-cluster-admin-broker`, `runtime-s3-archive-broker`,
`runtime-s3-restore-broker` не имеют Secret access либо права создавать role Pod/
ServiceAccount/RoleBinding и права создавать/bind-ить `cluster-admin`.
Проверить успешный `runtime-workload-materializer`: только он после
action-specific Ed25519 full-tuple ticket, exact mTLS caller, bearer equality и
one-time receipt выполняет immutable credential readback и exact desired Pod
create. Admission, archive и restore должны читать три
разных public verifier path; private keys существуют только у control-plane.
`PROJECT_READ_ONLY` binding должен соответствовать exact project namespace и
session identity. `CLUSTER_ADMIN` использует только
предустановленную ServiceAccount/ClusterRoleBinding; admission требует
server-owned одноразовый ticket и exact image/command/securityContext/volumes/
token audience/subject/full tuple. Annotation controller-а не является ticket.
Проверить PostgreSQL one-time receipt по ticket/Admission UID и что webhook
имеет только immutable ConfigMap readback, но не Pod/RoleBinding mutation.
Не исправлять отказ добавлением broad RBAC либо wildcard NetworkPolicy.

## Rollback

Откат образа выполняется на предыдущий подписанный digest после отдельного
владельческого шлюза. Миграции forward-only: изменение схемы компенсируется
новой migration. До восстановления совместимого consumer остановить
controller, не удаляя PVC, journals, durable inbox, consumer или S3 objects.
Не откатывать IAM/Object Lock/KMS требования и не выдавать static credentials.
