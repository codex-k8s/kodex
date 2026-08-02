---
id: SVC-MC-004
title: Контракт исполнения и продолжения control-plane
type: service-contract
status: approved
owner: developer
version: 1.0.0
updated: 2026-08-02
---

# Контракт исполнения и продолжения control-plane

Документ фиксирует границу GitHub Issue
[#221](https://github.com/codex-k8s/matter-codex/issues/221). Он дополняет
утверждённые `ARCH-MC-004`, `ARCH-MC-005`, `GUIDE-DOC-003` и
`GUIDE-DOC-006`. `control-plane` остаётся единственным владельцем состояния,
а `runtime-controller`, `integration-gateway` и будущий `agent-runner` не
могут выдавать себе tenant, lineage, attempt, RuntimeRevision, grant или fence
через поля запроса.

## Матрица A: исполнение runtime

| Переход | Инициатор и authority | Authoritative input и transaction | Новый state/fence | Факт или read/rejoin | Отказ и закрытие старого graph |
| --- | --- | --- | --- | --- | --- |
| Event/read-rejoin | `runtime-controller`, exact workload/SPIFFE, audience, full method и `controlplane.runtime_execution.read`; доказательство `RUNTIME_REVISION_GRANT` разрешено `control-plane` из состояния проекта | Запрос не содержит organization/project/process/session/thread/role/turn/attempt. Владелец под tenant/project lock разрешает exact turn/attempt/input/generation из verified grant и единый current process tuple | Нет перехода; скрытая или чужая строка возвращает одинаковый `NotFound` | Специализированные `ClaimRuntimeExecution` и `GetRuntimeExecution`; новый AsyncAPI fact не объявляется, потому что #188 ещё не материализовал inbox/effect | Неизвестная/stale grant generation или несовпавший workload закрыто отклоняется до выдачи snapshot |
| Immutable snapshot | Тот же `runtime-controller`; идентификатор исполнения выдаёт сервер | Одна `SERIALIZABLE` transaction блокирует turn, session, process и RuntimeRevision, проверяет единый current execution tuple и сохраняет organization/project/process/session/thread/role/turn/attempt, RuntimeRevision version+digest, immutable input digest, закрытые `ResourceClass`/`ClusterAccessProfile`, exact workload/grant generation | Создаётся `PENDING`, `version=1`, monotonic `fence=1`; snapshot-поля после вставки не изменяются | Полный snapshot возвращается из сохранённой строки; повтор key+hash возвращает тот же результат | Любой неполный tuple, terminal turn/process, stale revision/input/attempt и занятый tuple отклоняются без частичной записи |
| Admission/reconcile | `runtime-controller`, server-issued execution ID не является authority; owner заново разрешает tenant/project/workload | Row lock, expected version/fence/grant generation и exact snapshot; выдаётся server-generated lease ID и новый fence | `PENDING -> ADMITTED`, fence возрастает; lease связан с workload, SPIFFE, attempt и grant generation | Authoritative `GetRuntimeExecution` | Конкуренты получают `Aborted`; прежняя lease отсутствует и не может быть восстановлена payload |
| Heartbeat/renew | Владелец той же admitted lease и exact workload/SPIFFE | Row lock, lease digest, version, fence, attempt и generation; bounded server duration | `ADMITTED/RUNNING -> RUNNING`, version/fence возрастают, deadline движется только вперёд | Authoritative read | Stale lease/fence/attempt/generation получает `FailedPrecondition`; новый grant не оживляет прежнюю lease |
| Watchdog/incident | `runtime-controller` с отдельным permission; incident ID и evidence digest являются данными, не authority | Row lock и semantic idempotency receipt; один incident на exact execution/fence/digest | Сохраняется bounded incident; state не открывает дополнительного исполнителя | Audit + authoritative read; событие неприменимо без фактического consumer | Повтор exact intent идемпотентен, другой digest конфликтует; incident не меняет terminal winner |
| Terminal success/error | Текущий владелец lease с exact fence | Одна owner transaction блокирует execution, lease, turn, attempt, process и связанные claims/grants; сохраняет result/error digest и terminal receipt | `RUNNING/ADMITTED -> SUCCEEDED/FAILED`; lease/revocable authority закрываются; fence возрастает | Audit + authoritative read; новый speculative event отсутствует | Только первый terminal CAS побеждает; terminal replay возвращает receipt, opposite terminal получает `Aborted` |
| Cancel | Полномочие `runtime_execution.cancel`; actor/tenant/project берутся из verified context | Та же полная owner transaction | Любое допустимое nonterminal -> `CANCELLED`; старые lease/claims/grants закрыты | Audit + authoritative read | Race с complete/expiry/retry имеет одного победителя |
| Retry | Полномочие `runtime_execution.retry`; request не задаёт новый tuple | Owner transaction закрывает прежний graph и создаёт новый queued Turn со свежими attempt, RuntimeRevision и immutable input; новый runtime execution материализуется только следующим `ClaimRuntimeExecution` по свежему grant | Старый execution -> `RETRIED`; новый Turn -> `QUEUED`, а новый execution после claim -> `PENDING`; fence/generation прежней попытки больше не принимаются | Authoritative read прежней попытки и нового Turn | Retry terminal/stale attempt запрещён; повтор не создаёт второй Turn |
| Lease expiry/stale attempt | Watchdog `runtime-controller`, database time authoritative | Row lock, `lease_expires_at <= database clock`; закрывает lease/claim/grant/attempt и связанный current graph | `ADMITTED/RUNNING -> EXPIRED`; fence возрастает | Audit + authoritative read | Caller timestamp не принимается; renew и expiry имеют одного CAS winner |
| Archive reference/checksum | После terminal, exact archive writer workload; reference bounded, checksum exact SHA-256 | Row lock; immutable archive reference/checksum сохраняются один раз | Terminal state сохраняется, archive state `RECORDED`, version/fence возрастают | Authoritative read | Mutated checksum/reference и archive до terminal отклоняются |
| Independent restore proof | Отдельная проверенная restore identity/purpose, не archive writer; proof содержит exact archive checksum | Row lock; verifier identity, proof reference и checksum сохраняются append-only | Restore state `VERIFIED`, version/fence возрастают | Authoritative read | Самоподтверждение archive writer, checksum mismatch и stale fence запрещены |
| Cleanup authorization | Отдельный exact method/permission; owner выводит scope | Transaction требует terminal execution, exact immutable archive checksum и independent restore proof того же checksum; создаёт один server ID, связанный с execution/checksums и TTL 15 минут | Cleanup state `AUTHORIZED`, fence возрастает | Authoritative read возвращает expiry; authorization повтор exact intent возвращает тот же ID | До archive+restore proof запрещено; replay не создаёт вторую очистку; mutation конфликтует |

## Матрица B: integration approval и continuation

| Переход | Инициатор и authority | Неизменяемая привязка | State и competition | Durable continuation/read-rejoin | Отказ |
| --- | --- | --- | --- | --- | --- |
| Invocation | `integration-gateway`, exact workload/SPIFFE/audience/full method/permission; `AGENT_SESSION_GRANT` разрешается owner по активной session/turn authority | Server-owned organization/project/process/session/thread/role/turn/attempt, RuntimeRevision version+digest, immutable input digest, fence + exact invocation ID, approval ID и request hash | Создаётся одна строка `PENDING_APPROVAL`; активные turn lease/claim/grant закрываются той же transaction, graph переходит в `WAITING_EXTERNAL` | Сохранённая typed suspension является источником истины после restart | Изменение любого tuple member или request hash образует другой semantic intent; reuse idempotency key конфликтует |
| Exact suspension retry | Тот же verified context | Idempotency scope включает tenant/project, caller workload, operation, key и canonical request hash | Без нового effect возвращается сохранённая suspension | Ответ хранится в receipt | Exact key+hash возвращает исходный результат; key+другой hash — `AlreadyExists` |
| Approved | Отдельный `ApproveIntegrationInvocation`; decision payload не выбирает owner/tuple | Row lock, expected version/fence, exact approval/invocation/request hash | Первый decision CAS: `PENDING_APPROVAL -> APPROVED`; другие decision/expiry/cancel проигрывают | До execution continuation не готова | Stale/mismatched approval, hash, revision или fence закрыто отклоняется |
| Rejected | `RejectIntegrationInvocation` | Та же immutable binding и bounded reason digest | `PENDING_APPROVAL -> REJECTED`, одновременно материализуется ровно один continuation turn | Новый server-owned turn получает fresh RuntimeRevision, input digest и source reference; future #192 читает structured row по authority нового turn | Execution после rejection запрещено; replay не создаёт второй turn |
| Expired | `ExpireIntegrationInvocation`; database clock authoritative | Exact approval deadline, tuple и fence | `PENDING_APPROVAL -> EXPIRED`, один continuation turn | Тот же version-pinned rejoin | Caller timestamp и ранняя expiry запрещены |
| Cancelled | Специализированный `CancelIntegrationInvocation`; verified integration workload | Exact pending tuple/fence | `PENDING -> CANCELLED`, один continuation turn | Тот же version-pinned rejoin | Approve/reject/expire/cancel конкурируют за один decision; после `APPROVED` отмена этим pending-only методом запрещена |
| Execution begin | `integration-gateway`, `BeginIntegrationExecution` | Exact approved row, request hash, version/fence | `APPROVED -> EXECUTING`, fence возрастает | Состояние переживает restart | Начало до approval, stale approval/fence или повтор с mutation запрещены |
| Execution success | `CompleteIntegrationExecution`; structured result reference+digest | Exact invocation/request/tuple; terminal result фиксируется один раз | `EXECUTING -> SUCCEEDED`, одновременно ровно один continuation turn | `GetIntegrationContinuation` разрешает future #192 только по session-bound authority нового turn и exact expected version; response несёт typed decision/result/error и полный pinned tuple | Opposite error/result, decision/cancel или второй terminal CAS проигрывает |
| Execution error | `FailIntegrationExecution`; structured error code/reference/digest | Та же binding | `EXECUTING -> FAILED`, один continuation turn | Тот же read/rejoin | Result/error mutation и second terminal отклоняются |
| Consumer rejoin | Будущий `agent-runner` #192, exact `AGENT_SESSION_GRANT` нового continuation turn | Request не принимает continuation/invocation/tenant IDs; owner находит строку по проверенному current turn и сверяет version, revision, input и fence | Read не меняет state; `AcknowledgeIntegrationContinuation` даёт один terminal delivery CAS | Structured response version-pinned; restart повторяет read до ACK | Чужой tenant/session/turn, stale version/revision/input/fence или повтор ACK с mutation закрыто отклоняется |

## Сквозная contract/authority map

| Сценарий | Requirement | Actor и источник authority | Transport identity и полный RPC | Authoritative owner/scope | Idempotency, OCC, fence и transaction | Result/fact/consumer/readiness | Ошибки |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Claim runtime snapshot | #221, `ARCH-MC-004`, `GUIDE-DOC-006` | Workload actor из signed `RUNTIME_REVISION_GRANT`; IDs запроса отсутствуют | `runtime-controller`, exact SPIFFE, `/controlplane.v1.ControlPlaneService/ClaimRuntimeExecution` | `control-plane`; org/project из proof, execution tuple из PostgreSQL | UUID key + canonical empty semantic intent; `SERIALIZABLE`, row locks, unique tuple, version/fence | Typed immutable snapshot; polling/read-rejoin; readiness через protected `CheckReadiness` той же operation profile | `NotFound`, `FailedPrecondition`, `Aborted`, `AlreadyExists`, `Unavailable` |
| Runtime mutations | #221, `GUIDE-DOC-003/006` | Exact workload/grant from verified context plus server-owned execution row | Отдельные full methods для admission, heartbeat, incident, complete, cancel, retry, expiry, archive, restore proof и cleanup | `control-plane`; tenant/project resolved before ID lookup | Key+request hash receipt, expected version/fence, one owner transaction closes all leases/claims/grants | Audit and authoritative Get; AsyncAPI неприменим: #188 не реализует consumer inbox/effect в #221 | Hidden/cross-tenant `NotFound`; stale `FailedPrecondition`; race `Aborted`; mutation `AlreadyExists` |
| Resolve integration session | #220 contract need, #221 | `integration-gateway` session grant; request carries no business IDs | `/controlplane.v1.ControlPlaneService/ResolveIntegrationSession` | Session/turn/process/role/integration/credential state in owner DB | Read-only exact current tuple; RuntimeRevision component version+projection digest и credential expiry сверяются до ответа; no receipt | Typed bounded context с integration definition/capabilities/endpoint и credential metadata reference без secret values; protected readiness | `NotFound`, `PermissionDenied`, `FailedPrecondition` |
| Suspend approval | #221, `ADR-MC-006`, `ARCH-MC-005` | Session/turn/grant authority from signed context; invocation/approval/request hash are data | `/controlplane.v1.ControlPlaneService/SuspendForIntegrationApproval` | `control-plane` tenant + current execution graph | Semantic receipt, expected fence from authority, one transaction: continuation + graph wait + revoke | Typed stored suspension; no speculative event | `FailedPrecondition`, `AlreadyExists`, `Aborted` |
| Approval/execute/terminal | #221 | Exact integration workload; caller IDs re-resolved inside continuation boundary | Specialized approve/reject/expire/cancel/begin/complete/fail RPCs | Locked continuation row and owner graph | version/fence CAS; decision and execution have one terminal winner; terminal transaction creates one continuation turn | Durable row + fresh RuntimeRevision/turn; existing runtime polling can discover turn | Same bounded errors; unavailable persistence is retryable without partial effect |
| Structured consumer rejoin | #221, future #192 | Future `agent-runner` session grant bound to server-created continuation turn | `GetIntegrationContinuation` and `AcknowledgeIntegrationContinuation` | Owner resolves by verified current turn; request has no continuation/tenant IDs | Exact expected version/revision/input/fence; ACK receipt/CAS | Version-pinned structured read. #192 consumer/deploy/inbox are intentionally not implemented or declared active in #221 | Hidden/mismatch `NotFound`; stale `FailedPrecondition`; replay mutation `AlreadyExists` |

## Неприменимые звенья и ownership

- Новый AsyncAPI fact неприменим: #188 и #192 не входят в Issue #221 и не
  предоставляют фактический durable inbox/effect/readiness. Источником rejoin
  остаётся version-pinned RPC владельца; существующие утверждённые facts не
  переименовываются.
- Внешний HTTP endpoint и gateway mapping неприменимы: внешний unit #220
  заморожен и изучен только read-only. Этот PR предоставляет только внутренние
  Proto/full methods и client operation profile.
- `libs/go/eventing/**` и `postgresinbox` принадлежат Issue #222. `control-plane`
  использует существующие receipt/audit/outbox ports без изменения общей
  библиотеки; для новых переходов обязательного события нет.
- Consumer implementation, inbox и deploy ownership будущего `agent-runner`
  принадлежат #192. В #221 фиксируется только безопасный read/rejoin wire
  contract и профиль операции, не заявление о готовом consumer.
- Развёртывание `runtime-controller` #188 и `integration-gateway` #189/#220,
  их readiness и sidecar configuration не принадлежат этому unit.

## Ручные негативные и конкурентные сценарии

1. Попытаться передать tenant/owner/process/session/turn/attempt в claim/read
   request: таких полей в Proto нет.
2. Повторить claim/heartbeat/terminal с чужим SPIFFE, project, stale attempt,
   RuntimeRevision version/digest, input digest, grant generation или fence:
   ни state, ни receipt не меняются.
3. Одновременно выполнить complete/cancel/retry/expiry: успешен один CAS,
   прежние lease/claim/grant закрыты.
4. Запросить cleanup до archive checksum, с checksum другого archive writer
   либо без proof независимого verifier: `FailedPrecondition`; exact replay
   после proof возвращает прежний authorization ID.
5. Повторить suspension с тем же key/hash и затем с изменённым invocation,
   approval, tuple или request hash: первый повтор возвращает receipt, второй
   конфликтует.
6. Одновременно approve/reject/expire/cancel и success/error: у decision и
   terminal execution ровно по одному winner; после restart authoritative read
   возвращает сохранённый state и один continuation turn.
7. Читать continuation из другого tenant/session/turn либо с устаревшими
   version/revision/input/fence: данные не раскрываются.

## Forward-only rollback

Миграция добавляется только вперёд и не имеет штатного `down`. Откат образа
допустим лишь на версию, понимающую новую схему и policy revision. Иначе
workload остаётся неготовой до выпуска совместимой версии. Удаление строк,
снижение fence/grant generation, повторное открытие lease/claim, сброс receipt
или откат authority policy запрещены; исправление выполняется новой
компенсирующей migration и новым Issue после backup/readback и owner gate.
