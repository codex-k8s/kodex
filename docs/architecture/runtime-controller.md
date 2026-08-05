---
id: ARCH-MC-010
title: Runtime controller и жизненный цикл ресурсов сессии
type: architecture
status: approved
owner: architect
version: 1.4.0
updated: 2026-08-04
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
| Mattermost Stop/Retry | `interaction-gateway` как transport producer: verified actor, callback/card lineage и signed event | server-resolved Session/Turn/RuntimeExecution | `ManageRuntimeAction` атомарно закрывает текущий graph либо создаёт новую attempt/RuntimeRevision/grant | controller наблюдает cancel и останавливает Pod; новая attempt проходит обычный reconcile |
| session MCP credential | bot-service только как transport credential producer, вызванный `interaction-gateway` по TLS 1.3/mTLS | exact channel/root card/bot identity и control-plane Session | immutable `AgentSession.TokenSecretRef`, revision/digest и `BindSessionMCP` receipt | credential broker копирует только exact Secret в trusted runner container |

mTLS подтверждает peer и не заменяет bearer, application authority, owner
resolution, idempotency или replay protection. Payload, labels и NATS envelope
не назначают actor, tenant, ownership либо access profile.

## Сквозные сценарии

### Durable event consumer

```text
control-plane owner transaction + outbox
  -> CONTROL_PLANE exact subjects:
       control_plane.runtime_configuration_changed
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

### Agent runner, terminal handoff и successor Pod

Immutable `mattercodex.agent-runner-input.v2` содержит exact
execution/version/fence/grant, RuntimeRevision id/version/SHA-256, Session,
Turn, attempt, input SHA-256, provider binding id/version/SHA-256, Codex policy,
versioned materializations, TLS bindings и только пути immutable credentials.
Control-plane остаётся единственным владельцем Session/Turn/Process и FIFO;
bot-service session API не является authority. Контейнер получает только
`runtime-init-workspace` либо `runtime-session`, session PVC и exact
execution artifacts. Недоверенный `provider-runtime` запускается отдельным UID
без Kubernetes token, application grants, runtime mTLS, MCP bearer, handoff key
и authority socket; provider broker принимает bounded запросы только от trusted
runner UID 10001, а обратный MCP authority UDS — только от provider UID 10002
либо trusted readiness UID 10001 по `SO_PEERCRED` и локальной capability.
Каждый artifact связан с owner id/version/SHA-256 и
читается через `interaction-gateway` по TLS 1.3/mTLS/SNI/CA + bearer без прямого
чтения чужой БД либо broad object-store credential.

Первый `ClaimRuntimeExecution` создаёт durable `PENDING` до role Pod и допускает
только server-owned `QUEUED` attempt без Turn lease. Controller материализует
journal, immutable ConfigMap, credentials и Pod, но ещё не выполняет admission.
Runner своей exact application identity вызывает `ClaimTurn`; следующий
reconcile видит живую Turn lease и только тогда выполняет
`AdmitRuntimeExecution`. Identity controller не подменяет runner. Crash до
Pod, после Pod, после Turn claim либо после admission повторяет тот же
server-owned receipt/tuple и не создаёт второй execution или lease.

Credential materializer не получает namespace-wide `secrets/get`. Он создаёт
отдельный `runtime-access-*` ServiceAccount, exact handoff Role/RoleBinding и
credential projections только для одного execution. Direct access profile
runner всегда `NONE`; Kubernetes token служит только чтению immutable input и
атомарной записи controller-owned handoff ConfigMap. Admission сверяет signed
ticketed Pod целиком. Vault role authority sidecar допускает лишь этот
ServiceAccount prefix в `mattercodex-system`. При удалении Pod controller
проверяет lineage и удаляет RoleBinding, Role и ServiceAccount с
UID/resourceVersion preconditions, поэтому параллельные execution не получают
объединённые полномочия.

`Succeeded`/`Failed` фаза Pod не завершает Turn. После фактического результата
runner публикует подписанный `mattercodex.runtime-turn-handoff.v2` в отдельный
controller-owned ConfigMap: exact execution/revision/input/provider tuple,
закрытый outcome, terminal digest, Codex thread/rollout provenance и bounded
inline Markdown плюс immutable Artifact refs для files, images и крупного
Markdown. До handoff trusted runner отправляет bounded stream в
`interaction-gateway`; gateway владеет private S3 put/readback и регистрирует
Artifact в control-plane. Controller проверяет Ed25519 trust,
lease/fence/generation, каждый inline/ref digest и только затем вызывает
`CompleteRuntimeExecution`. Одна owner transaction закрывает execution,
Turn/Process/leases/grants и создаёт durable interaction deliveries. Выход Pod
без handoff создаёт incident и оставляет owner recovery выбрать
expiry/cancel/retry.

После terminal controller архивирует quiesced PVC и фиксирует exact rollout
JSONL path/SHA-256/provenance вместе с provider binding. Successor всегда новый
execution-scoped Pod с fresh RuntimeRevision, credentials, config, env и MCP
client; старый процесс и mounted secrets не переиспользуются. Retained PVC
сохраняет `CODEX_HOME`, а resume разрешён только для того же provider account и
проверенного rollout. Если PVC уже удалён, owner назначает exact archive source,
rehydrate восстанавливает новый PVC и только после proof разрешает role Pod.
Codex thread lineage при этом читается независимо от cleanup/archive state из
последнего проверенного terminal execution той же Session; reauth той же
logical provider binding допускает свежие credential version/digest.

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
digest. Unprivileged archive/restore broker Job не имеет Kubernetes, Vault,
STS или S3 authority и передаёт owner-signed one-time ticket по mTLS отдельному
`runtime-s3-archive-exchanger` либо `runtime-s3-restore-exchanger`. Только
соответствующий trusted exchanger выводит tags/policy из проверенного snapshot,
получает action-specific Vault bootstrap credential и вызывает закреплённую
execution role; immutable Kubernetes receipt обеспечивает exact replay после
потерянного ответа. Job сверяет immutable Secret UID/resourceVersion, expiry, tags,
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
переименовывает его в final `session/`: сначала sync каждого regular file и
marker, затем directories снизу вверх и parent до/после rename. Retry удаляет
только принадлежащую exact execution/PVC generation незавершённую публикацию;
чужое или неоднозначное final tree закрыто отклоняется. Owner `CONSUMED` proof фиксируется до
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
| retry/continuation | terminal predecessor и policy | новая attempt, fresh RuntimeRevision/grant | новый admission и новый execution-scoped Pod |
| archive | terminal retention owner и exact PVC | immutable archive provenance | versioned S3 + readback |
| restore proof | separate verifier, exact archive version/checksum | immutable proof | safe temporary extraction |
| rehydrate | source только из `CONSUMED` cleanup snapshot; assignment только один раз на новую PVC generation | bind/consume exact PVC UID и immutable proof | same-FS staging, atomic publish, proof до Pod |
| cleanup authorize | session lock, pinned policy/version/eligible_at, terminal graph, proof, no work/hold | `ACTIVE` generation + exact PVC tuple/expiry | новые work claims блокируются |
| PVC delete/finalize | ACTIVE tuple, Kubernetes preconditions, NotFound proof, expiry и повторный graph lock | idempotent `CONSUMED` | partial/unknown fail-closed |
| idle eviction | terminal readback, LRU, не admin | authority не меняется | удаляется только Pod |
| retention policy set/retire/read | exact operator SPIFFE/permission и current version | retire current + insert monotonic next либо exact retire, receipt/audit | active execution сохраняет pinned version; missing/mismatch fail-closed |
| manual/legal hold/release | exact operator authority, server-resolved Session owner/version и hold version | specialized hold/release, receipt/audit под Session lock | active hold входит в authorize/consume/reissue/expiry cleanup predicate |

`WAITING_OWNER`, `CHANGES_REQUESTED` и integration continuation принадлежат
`control-plane`: они блокируют claim/cleanup и не получают локального
обобщённого transition.

Cleanup использует один session/full-graph predicate: любой queued/PENDING/
admitted/running successor, capacity delay, owner callback/gate, manual/legal
hold либо non-rejoined continuation блокирует authorization и finalize.
Terminal predecessor никогда не делает PVC живого successor eligible.

Схема расширяется только forward migrations. Уже применённая
`20260802000100_control_plane_runtime_continuation.sql` остаётся byte-identical
ревизии `main`; binding/retention/restore/cleanup добавлены последующими
`20260803000100`, `20260803000200` и forward-only `20260804000300`: последняя
по exact catalog identity снимает legacy terminal CHECK, канонизирует все
restore-source columns и добавляет policy/hold lifecycle. Admission replay —
отдельная migration runtime-controller.

## Access profiles и deploy boundary

Controller credential не может создавать role Pod, ServiceAccount или
RoleBinding, создавать/bind-ить `cluster-admin` ClusterRoleBinding и не читает
исходные mutable credentials. Routine `runtime-credential-broker` identities
не имеют namespace RBAC и только передают Ed25519 full-tuple ticket по
TLS 1.3/mTLS узкому `runtime-workload-materializer`. Trusted materializer
проверяет bearer=ticket, exact SPIFFE caller и one-time immutable receipt,
затем материализует immutable credential snapshots, exact Pod и grants.
`PROJECT_READ_ONLY` materializer создаёт
узкий session ServiceAccount/RoleBinding только после authoritative organization/
project namespace readback; terminal отзывает binding. `CLUSTER_ADMIN`
использует заранее установленную отдельную ServiceAccount/ClusterRoleBinding,
которую controller может только прочитать. Admission private key доступен
только control-plane owner; broker и webhook получают только public verifier.
Fail-closed webhook проверяет issuer/audience/expiry, durable one-time receipt
по Admission UID и ticket ID и полное равенство server-owned immutable desired
Pod: image digest, `runtime-session`,
securityContext, volumes, token audience, ServiceAccount subject,
execution/session/turn/attempt/revision/input/fence и credential snapshot.
Ticket нельзя назначить annotation-ом controller-а; неверный, повторный или
непрочитанный ticket отклоняется. Runner получает bounded projected token, а
Pod удаляется сразу после terminal.

Pod admission, S3 archive и S3 restore имеют три независимые Ed25519 keypair,
issuer/audience и Vault trust path. Routine, project-read и cluster-admin
broker identities разделены и не имеют Secret/Pod/SA/RBAC mutation;
компрометация одного broker не позволяет прочитать snapshot другого execution
либо выбрать другой subject, image, command, volume или token audience.
Admission webhook
имеет только ConfigMap readback и отдельную PostgreSQL identity для replay
receipts, а не namespace mutation.

Role Pod `/readyz` становится успешным только после bounded рабочего
bot-session read и MCP initialize через тот же TLS 1.3/mTLS exact SNI/CA/client
certificate и bearer, которые использует turn. Digest readback выполняется до
claim и периодически; mismatch снимает readiness. Warm successor сначала
переключает immutable credential snapshot и повторяет тот же barrier, затем
может получить следующий turn. Local plaintext endpoint является только
Kubernetes probe surface и сам не создаёт transport authority.

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
