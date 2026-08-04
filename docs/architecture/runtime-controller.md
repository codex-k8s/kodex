---
id: ARCH-MC-010
title: Runtime controller и жизненный цикл ресурсов сессии
type: architecture
status: approved
owner: architect
version: 1.2.0
updated: 2026-08-03
---

# Runtime controller и жизненный цикл ресурсов сессии

Документ материализует границу `runtime-controller` из `ARCH-MC-004` и
сквозной контракт Issue #188. `control-plane` остаётся авторитетным владельцем
сессии, хода, попытки, `RuntimeRevision`, claim/fence/lease/grant и terminal
state. `runtime-controller` владеет только durable Kubernetes journal,
материализацией runtime, допуском по ёмкости, health и архивной цепочкой.
Controller не запускает Codex: role image запускает `agent-runner` в закрытом
режиме `runtime-session`.

## Authority matrix

| Действие | Авторитетный actor и транспорт | Server-owned input | Устойчивый результат | Исполнитель effect |
| --- | --- | --- | --- | --- |
| claim/admit/heartbeat/incident/complete/expire | `runtime-controller`, mTLS + bearer application grant + exact operation policy | organization/project/session/thread/turn/attempt, workload, revision, input, generation | `RuntimeExecution`, receipt, audit, event и отзыв authority в одной owner transaction | controller после commit |
| archive | `runtime-archive`, отдельные SPIFFE/application grant/ServiceAccount | terminal execution и exact PVC | S3 version/checksum/provenance, затем `RecordRuntimeArchive` | archive Job с journal-only RBAC |
| restore proof | `runtime-restore-verifier`, отдельные identity и grant | exact archive reference/version/checksum | immutable `RuntimeRestoreProof` | verifier Job |
| cleanup claim/expire | `runtime-cleanup-authorizer`, отдельные identity и grant | owner-locked session graph и exact PVC name/UID/resourceVersion | bounded `ACTIVE` либо `EXPIRED` generation | authorizer Job |
| guarded PVC delete/finalize | controller может только `ConsumeRuntimeCleanupAuthorization` | active generation, тот же PVC tuple, observed NotFound и deletion proof digest | idempotent `CONSUMED` | controller |
| cancel/retry/continuation | только owner workload | полный session/turn/process graph | atomic terminal/retry/new revision/grant | не controller |
| role runtime | exact ServiceAccount profile; bearer остаётся обязательным на application path | immutable runner input и projected credentials | runner handoff exact execution/revision/input tuple | `agent-runner` |
| bot execution binding | bot-service owner через отдельную закрытую команду и mTLS+bearer | control-plane session/turn/attempt/input/revision + bot AgentSession/Turn/Run versions | server-computed binding digest, immutable в execution | control-plane owner transaction |

mTLS подтверждает peer и не заменяет bearer, application authority, owner
resolution, idempotency или replay protection. Payload, labels и NATS envelope
не назначают actor, tenant, ownership либо access profile.

## Сквозные сценарии

### Durable event consumer

```text
control-plane owner transaction + outbox
  -> CONTROL_PLANE exact subjects:
       control_plane.runtime_configuration_changed
       control_plane.schedule_changed
  -> JetStream durable RUNTIME_CONTROLLER_V1
  -> postgresinbox.Acquire(canonical envelope)
       duplicate/stale => durable ACK без gRPC hydration
       claimable       => protected exact-version gRPC hydration
  -> postgresinbox.ApplyClaim(effect + cursor, одна transaction)
  -> ACK только после durable outcome
```

Startup barrier проверяет exact `StreamInfo` limits/subjects, exact
`ConsumerInfo` filter/ack policy/`MaxDeliver`, PostgreSQL principal, inbox
blockages/dead letters и тот же protected gRPC path, что рабочая обработка.
При последней доставке consumer читает durable outcome, выполняет bounded
`Recover`, повторно читает outcome и ACK только доказанный duplicate/applied;
неизвестное состояние NAK-ится.

### Claim, capacity, admission и crash recovery

```text
ClaimRuntimeExecution -> PostgreSQL durable PENDING
  -> немедленный immutable journal с admission request/idempotency tuple
  -> GetRuntimeRevision(exact id/version)
  -> durable capacity snapshot и admission
  -> AdmitRuntimeExecution(PENDING tuple)
  -> journal ADMITTED_RECOVERABLE с replayable receipt/lease
  -> PVC [rehydrate gate] -> immutable ConfigMap/credentials -> role Pod
  -> journal MATERIALIZED -> Ready readback -> heartbeat/RUNNING
```

Journal создаётся до revision read, capacity и любого следующего Kubernetes
effect. Если controller упал до успешного Create journal, новый server-owned
claim key заново обнаруживает только exact `PENDING` того же
session/turn/attempt/grant/revision/input и сохраняет recovery receipt без
второго state transition. Capacity defer, dependency error или crash поэтому
оставляют повторяемый `PENDING`. По `reschedule_after` либо expiry observation
owner атомарно terminal-ит старый PENDING как `RETRIED`, отзывает старые
authority/claim и создаёт новую attempt с fresh RuntimeRevision и observation;
старый immutable snapshot не мутируется. Recovery повторно читает exact
revision, перепроверяет свежесть capacity snapshot и тем же idempotency key получает admission receipt вместе
с lease token. Lease token не хранится в Kubernetes Secret, ConfigMap,
annotation, log или metric; после crash он восстанавливается только replay
того же owner receipt.

Допуск считает Kubernetes requests и durable `PENDING|ADMITTED|RUNNING`
journals, server-owned organization limit, exact provider credential binding,
provider observation revision/usage/limit/max-age, `ResourceQuota` и node
pressure. Unknown, stale, неполная binding либо pagination переводят execution
в очередь. Самый старый доказанно terminal idle Pod может быть удалён один раз,
после чего capacity вычисляется повторно. PVC при eviction не удаляется.

### Agent runner, terminal handoff и warm Pod

Immutable `mattercodex.agent-runner-input.v1` содержит exact execution
version/fence/grant, RuntimeRevision id/version/SHA-256, input SHA-256,
control-plane session/turn и отдельные bot `AgentSession`/`AgentSessionTurn`/
`RunID` с server-computed binding digest. Control-plane ID не подставляется в
bot `session_key`. Контейнер получает только команду `runtime-session`, session
PVC и immutable execution credential artifacts; каждый artifact связан с
provider content version, Secret UID/resourceVersion и digest. Bot-service и
MCP доступны только по HTTPS/mTLS с exact hostname/SNI, доверенной CA, client
identity и bearer; NetworkPolicy не заменяет peer/application authorization.

`Succeeded`/`Failed` фаза Pod не завершает turn. После фактического результата
runner публикует `mattercodex.runtime-turn-handoff.v1` в собственный Pod:
exact execution version/fence/generation/revision/input, закрытый outcome,
terminal reference и digest. Только этот handoff разрешает controller вызвать
`CompleteRuntimeExecution`; выход Pod без handoff публикует incident и ждёт
owner expiry/cancel/retry.

После terminal controller сначала архивирует PVC и получает restore proof,
затем открывает `archive-gate=OPEN`. Новый claim той же session запрещён owner
query, пока у любого predecessor нет archive+proof. Running/Ready warm Pod
используется повторно только при том же server-owned
`effective_runtime_sha256` и открытом gate: controller публикует новый
immutable RuntimeRevision/ConfigMap и fresh authority/credential snapshots.
Broker обновляет handoff Role точным множеством новых immutable Secret names,
а runner читает их через Kubernetes API в собственный `0700` tmp staging,
сверяет execution/snapshot/purpose и только затем меняет клиенты; старые mounted
Secret не переиспользуются. После `SUCCESSOR_READY` runner сам проверяет новый
tuple и закрывает gate. Любое
несовпадение ведёт к guarded Pod replacement. `CLUSTER_ADMIN` Pod не прогревается
и удаляется после terminal.

### Archive, restore и rehydrate

После runner handoff writer quiesced; Archive Job читает неизменяемый CSI clone
exact PVC read-only. Traversal выполняется fd-relative через `openat2` с
`BENEATH|NO_SYMLINKS`, отклоняет symlink/hardlink/device и изменение inode,
затем загружает deterministic tar в exact execution key `archive.tar.gz` с
SHA-256, KMS encryption
и COMPLIANCE Object Lock. Ненулевой `VersionId`, exact `HeadObject` и
`GetObjectRetention` readback обязаны подтвердить checksum, provenance, mode и
owner-pinned retain-until как на create, так и на idempotent existing path.

S3 identities разделены на archive и restore/rehydrate. Узкий broker выдаёт
на execution/action отдельный immutable Secret с STS credential не дольше 15
минут, trusted server-side session tags exact organization/project/session/
execution/source execution, inline key/version/action/KMS policy и readback
digest. Сначала action-specific Vault role выдаёт bootstrap credential,
ограниченный только `AssumeRole`/`TagSession` закреплённой execution role;
затем broker через exact TLS S3/STS boundary вызывает `AssumeRole` с inline
policy и server-owned tags. Job сверяет immutable Secret UID/resourceVersion, expiry, tags,
policy/readback и data digest до монтирования. IAM source запрещает List/Delete/Bypass,
cross-tenant prefixes и insecure transport; startup дополнительно проверяет
versioning, Object Lock, KMS, public-access block и фактический запрет List.

Если session PVC уже был очищен, новый owner claim атомарно pin-ит последний
`CONSUMED` archive source: source execution, S3 reference/version/checksum,
revision/input provenance и deletion proof. Controller создаёт новый PVC и
owner bind-ит одноразовое assignment к exact generation/name/UID/
resourceVersion. До старта Pod `runtime-rehydrate` требует пустой volume,
скачивает только pinned version, проверяет checksum/metadata, строит
execution-owned staging на том же filesystem, fsync-ит и атомарно
переименовывает его в final `session/`. Owner `CONSUMED` proof фиксируется до
Pod. Crash допускает повтор/очистку staging, но не partial final tree; live PVC
повторно не восстанавливается.

### Двухфазная cleanup transaction

```text
terminal + archive + restore proof
  -> authorizer lock session/full execution graph
  -> authoritative pinned policy/version/eligible_at и отсутствие work/hold
  -> ACTIVE generation с exact PVC name/UID/resourceVersion, claimed_at, expiry
  -> новые ClaimRuntimeExecution той же session запрещены
  -> controller reread ACTIVE exact tuple
  -> Kubernetes Delete с UID/resourceVersion preconditions
  -> readback NotFound
  -> Consume(exact tuple, observed_not_found_at, proof_sha256)
  -> owner lock + повторная graph/expiry проверка -> CONSUMED
```

Client grace отсутствует. `LastTransition` journal/Pod не определяет PVC
eligibility. `NotFound` без заранее сохранённого exact tuple, активной lease и
proof не считается успехом. Ошибка после Delete повторяется через тот же
durable journal и idempotency key. Если PVC уже NotFound, proof прежней
generation сохраняется в immutable runtime journal. После новой full-graph
owner authorization controller связывает тот же NotFound readback с новой
generation и сразу завершает её. Новый work блокируется только до этого
гарантированного finalize, а не навсегда.
Backfill проходит тот же путь. Legacy PVC без server-owned execution/journal
остаётся `inventory-only`.

## Lifecycle matrix

| Переход | Закрытая проверка | Owner transaction | Runtime effect/recovery |
| --- | --- | --- | --- |
| claim | current session/turn/attempt/workload/grant/revision/input; нет active cleanup и unverified predecessor | новый PostgreSQL `PENDING`, version/fence, receipt/audit; новый claim key может получить только тот же exact `PENDING` | journal создаётся первым Kubernetes effect и восстанавливается из owner state |
| capacity defer/error | exact revision; immutable quota/account observation; durable pending accounting | до reschedule authority не меняется | journal остаётся; bounded expiry создаёт fresh attempt/revision/observation |
| admit | current owner graph и expected PENDING tuple | `ADMITTED`, новый fence/lease, receipt/audit | `ADMITTED_RECOVERABLE`; receipt replay восстанавливает lease |
| materialize/rehydrate | exact PVC/revision/input/credential tuple; restore source при новом PVC | authority не меняется | Pod появляется только после rehydrate proof |
| start/heartbeat | Ready UID и exact lease/fence/generation | `RUNNING`, renewed leases/version/fence | readback, journal patch |
| handoff complete | exact runner handoff, current lease/fence/generation | terminal state и atomic revoke всех grants/claims/leases/events | archive gate остаётся CLOSED |
| Pod exit без handoff | current nonterminal execution | incident audit, не complete | Pod не выдаёт terminal authority |
| cancel/delete/expiry | полный owner graph | terminal + revoke одной transaction | stale Pod guarded delete; PVC сохраняется |
| retry/continuation | terminal predecessor и policy | новая attempt, fresh RuntimeRevision/grant | новый admission; warm reuse только совместимый |
| archive | terminal retention owner и exact PVC | immutable archive provenance | versioned S3 + readback |
| restore proof | separate verifier, exact archive version/checksum | immutable proof | safe temporary extraction |
| rehydrate | source только из `CONSUMED` cleanup snapshot; assignment только один раз на новую PVC generation | bind/consume exact PVC UID и immutable proof | same-FS staging, atomic publish, proof до Pod |
| cleanup authorize | session lock, pinned policy/version/eligible_at, terminal graph, proof, no work/hold | `ACTIVE` generation + exact PVC tuple/expiry | новые work claims блокируются |
| PVC delete/finalize | ACTIVE tuple, Kubernetes preconditions, NotFound proof, expiry и повторный graph lock | idempotent `CONSUMED` | partial/unknown fail-closed |
| idle eviction | terminal readback, LRU, не admin | authority не меняется | удаляется только Pod |

`WAITING_OWNER`, `CHANGES_REQUESTED` и integration continuation принадлежат
`control-plane`: они блокируют claim/cleanup и не получают локального
обобщённого transition.

## Access profiles и deploy boundary

Controller credential не может создавать role Pod, ServiceAccount или
RoleBinding, создавать/bind-ить `cluster-admin` ClusterRoleBinding и не читает
исходные mutable credentials. Узкий `runtime-credential-broker` сначала
проверяет HMAC workload ticket полного tuple, затем материализует immutable
credential snapshots, exact Pod и grants. `PROJECT_READ_ONLY` broker создаёт
узкий session ServiceAccount/RoleBinding только после authoritative organization/
project namespace readback; terminal отзывает binding. `CLUSTER_ADMIN`
использует заранее установленную отдельную ServiceAccount/ClusterRoleBinding,
которую controller может только прочитать. Fail-closed admission проверяет
server-owned workload ticket и exact spec: image digest, `runtime-session`,
securityContext, volumes, token audience, ServiceAccount subject,
execution/session/turn/attempt/revision/input/fence и credential snapshot.
Ticket нельзя назначить annotation-ом controller-а; неверный, повторный или
непрочитанный ticket отклоняется. Runner получает bounded projected token, а
Pod удаляется сразу после terminal.

Archive, restore, rehydrate и cleanup Jobs имеют отдельные ServiceAccount,
application grants и per-job Role с `resourceNames` только своего journal.
Общего worker доступа к Secret или journals нет.

Base NetworkPolicy закрыта. Environment renderer добавляет Kubernetes API
Service/EndpointSlice как exact CIDR/ports; маршруты проекта в base не
зашиваются. Он также требует exact `/32|/128` endpoints утверждённого provider
route и разрешает им только TLS/443. Role Pod имеет DNS, exact bot-service/MCP
Service, provider endpoints и environment-rendered Kubernetes API только для
разрешённого access profile. S3
доступен только archive/restore/rehydrate workers.

## Наблюдаемость и shutdown

Business metrics используют закрытые labels. Отдельно наблюдаются durable
pending/admission recovery, capacity reasons, handoff, archive/restore/
rehydrate/cleanup, inbox pending/ack-pending/redelivery/blockage/dead-letter и
incidents. Readiness читает PostgreSQL blockage и exact JetStream info. Все
alerts имеют абсолютный HTTPS `runbook_url` на `RUN-MC-008`.

Workers не действуют до startup barrier. Потеря leader lease и shutdown
сначала cancel-ят workers, затем join-ят их до закрытия PostgreSQL/NATS/
Kubernetes clients. Tracing shutdown, Sentry flush и прочие cleanup получают
независимые bounded contexts от неотменённой базы.
