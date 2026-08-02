---
id: SVC-MC-004
title: Контракт исполнения и продолжения control-plane
type: service-contract
status: approved
owner: developer
version: 1.3.0
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
| Immutable snapshot | Тот же `runtime-controller`; идентификатор исполнения выдаёт сервер | Одна `SERIALIZABLE` transaction блокирует turn, session, process и RuntimeRevision, проверяет единый current execution tuple и сохраняет organization/project/process/session/thread/role/turn/attempt, RuntimeRevision version+digest, immutable input digest, закрытые `ResourceClass=STANDARD/HIGH_MEMORY/ACCELERATED` и `ClusterAccessProfile=NONE/PROJECT_READ_ONLY/CLUSTER_ADMIN`, exact workload/grant generation | Создаётся `PENDING`, `version=1`, monotonic `fence=1`; snapshot-поля после вставки не изменяются | Полный snapshot возвращается из сохранённой строки; повтор key+hash возвращает тот же результат | Caller не выбирает профиль. `CLUSTER_ADMIN` выводится только из server-owned `runtime.cluster.admin`; revoked/frozen/неактивная role или несовпавший tuple закрыто отклоняются |
| Admission/reconcile | `runtime-controller`, server-issued execution ID не является authority; owner заново разрешает tenant/project/workload | Row lock, expected version/fence/grant generation и exact snapshot; выдаётся server-generated lease ID и новый fence | `PENDING -> ADMITTED`, fence возрастает; lease связан с workload, SPIFFE, attempt и grant generation | Authoritative `GetRuntimeExecution` | Конкуренты получают `Aborted`; прежняя lease отсутствует и не может быть восстановлена payload |
| Heartbeat/renew | Владелец той же admitted lease и exact workload/SPIFFE | Row lock, lease digest, version, fence, attempt и generation; bounded server duration | `ADMITTED/RUNNING -> RUNNING`, version/fence возрастают, deadline движется только вперёд | Authoritative read | Stale lease/fence/attempt/generation получает `FailedPrecondition`; новый grant не оживляет прежнюю lease |
| Watchdog/incident | `runtime-controller` с отдельным permission; incident ID и evidence digest являются данными, не authority | Row lock и semantic idempotency receipt; один incident на exact execution/fence/digest | Сохраняется bounded incident; state не открывает дополнительного исполнителя | Audit + authoritative read; событие неприменимо без фактического consumer | Повтор exact intent идемпотентен, другой digest конфликтует; incident не меняет terminal winner |
| Terminal success/error | Текущий владелец lease с exact fence | Одна owner transaction блокирует execution, lease, turn, attempt, exact current `ProcessRun`, проверяет отсутствие открытых children/work, отзывает claims/grants и через единый `completeProcessFromTurn` закрывает применимые occurrence/ScheduledRun | `RUNNING/ADMITTED -> SUCCEEDED/FAILED`; Turn и ProcessRun получают совместимый terminal state, lease закрыта, fence возрастает | Audit + authoritative read; новый speculative event отсутствует | Transaction не коммитит terminal RuntimeExecution/Turn при живом ProcessRun. Только первый terminal CAS побеждает; replay возвращает receipt |
| Cancel | Полномочие `runtime_execution.cancel`; actor/tenant/project берутся из verified context | Та же полная owner transaction | Любое допустимое nonterminal -> `CANCELLED`; старые lease/claims/grants закрыты | Audit + authoritative read | Race с complete/expiry/retry имеет одного победителя |
| Retry | Полномочие `runtime_execution.retry`; request не задаёт новый tuple | Owner transaction принимает только active predecessor либо сохранённый immutable `FAILED/EXPIRED`; полностью закрывает его authority, сохраняет outcome и создаёт новый queued Turn со свежими attempt, RuntimeRevision, input и будущим grant. Если Turn доставляет integration outcome, та же transaction перепривязывает неизменяемый outcome к новой delivery attempt | Старый execution -> `RETRIED`, сохраняя terminal outcome `FAILED/EXPIRED`; новый Turn -> `QUEUED`; delivery version/fence возрастают и `READY` открывается только для новой attempt, новый execution появится лишь через свежий `ClaimRuntimeExecution` | Authoritative read прежней попытки и нового Turn; crash до Get/между Get и ACK повторяет сохранённый outcome без нового approval/external execution | `SUCCEEDED/CANCELLED/SUSPENDED/RETRIED` fail-closed; complete/cancel/expiry/retry/ACK конкурируют одним row/OCC winner; старые grant/attempt/input не читают новую binding, replay не создаёт второй Turn |
| Lease expiry/stale attempt | Watchdog `runtime-controller`, database time authoritative | Row lock, `lease_expires_at <= database clock`; закрывает lease/claim/grant/attempt и через тот же процессный terminal invariant закрывает exact ProcessRun/occurrence/ScheduledRun | `ADMITTED/RUNNING -> EXPIRED`; fence возрастает | Audit + authoritative read | Caller timestamp не принимается; renew и expiry имеют одного CAS winner; живой open work откатывает весь terminal transition |
| Archive reference/checksum | После terminal, exact archive writer workload; reference bounded, checksum exact SHA-256 | Row lock; immutable archive reference/checksum сохраняются один раз | Terminal state сохраняется, archive state `RECORDED`, version/fence возрастают | Authoritative read | Mutated checksum/reference и archive до terminal отклоняются |
| Independent restore proof | Только `runtime-restore-verifier`, exact SPIFFE, отдельные audience, `RUNTIME_RESTORE_VERIFIER_GRANT`, readiness profile и `controlplane.runtime_execution.restore.verify`; archive writer, OIDC и cleanup authorizer не подходят | Row lock; exact execution/revision/input/attempt/generation и archive checksum; workload, SPIFFE, grant generation, proof reference/checksum сохраняются append-only | Restore state `VERIFIED`, version/fence возрастают | Authoritative read contract существует, но внешний verifier deployable/readback не материализован в #221 | До отдельного deployable и trust material destructive path остаётся fail-closed; control-api-gateway не может подписать proof |
| Cleanup issue/reissue | Только `runtime-cleanup-authorizer`, отдельные SPIFFE/audience/`RUNTIME_CLEANUP_AUTHORIZER_GRANT`, readiness и permission | Transaction требует terminal execution, exact archive checksum, сохранённую независимую verifier attestation и под row lock exact owner Session проверяет все её integration continuation. Любая строка с `continuation_state != REJOINED`, включая pending source и current delivery binding, блокирует cleanup; PostgreSQL clock решает expiry | `NONE/EXPIRED -> ACTIVE`, generation монотонно возрастает, ID и TTL 15 минут новые; истёкший `ACTIVE` атомарно становится `EXPIRED` перед reissue | Exact key+стабильный semantic hash возвращает сохранённый receipt; новый JTI того же доказанного intent не меняет hash, новый authority/business tuple конфликтует | Живой `ACTIVE`, любой `CONSUMED` и non-`REJOINED` continuation той же session блокируют reissue; строки другой session не влияют; concurrent continuation/reissue имеет одного owner/OCC winner |
| Cleanup consume/expire | `runtime-controller` потребляет exact authorization; только cleanup authorizer явно закрывает истёкшую | Consume заново в своей transaction блокирует exact Session и сверяет execution/revision/input/attempt/grant, ID, generation, checksums, expiry и session-scoped continuation blocker; ранее выданная authorization не заменяет eligibility. Expire использует PostgreSQL clock | `ACTIVE -> CONSUMED` либо `ACTIVE -> EXPIRED`; generation не уменьшается, `consumed_at` append-only | Authoritative RuntimeExecution read; replay exact intent возвращает прежний receipt | Crash после issue не блокирует reissue после expiry; concurrent continuation/retry/consume не допускает TOCTOU; consumed cleanup никогда не получает вторую authorization |

## Матрица B: integration approval и continuation

| Переход | Инициатор и authority | Неизменяемая привязка | State и competition | Durable continuation/read-rejoin | Отказ |
| --- | --- | --- | --- | --- | --- |
| Invocation | `integration-gateway`, exact workload/SPIFFE/audience/full method/permission; `AGENT_SESSION_GRANT` разрешается owner по активной session/turn authority | Server-owned organization/project/process/session/thread/role/turn/attempt, RuntimeRevision version+digest, immutable input digest/fence, invocation/approval/request hash; выбранные Integration и credential bindings повторно разрешаются owner как exact ID+version+projection digest | RuntimeExecution прежней attempt становится `SUSPENDED` с terminal receipt; lease/attempt/claims/grants закрываются, Turn/Session/Process переходят в `WAITING_EXTERNAL`. Для scheduled process occurrence/run атомарно переходят `CLAIMED -> CONTINUATION`, scheduler lease очищается и current tuple фиксирует suspended versions; создаётся одна `PENDING` continuation | Сохранённая typed suspension является источником истины после restart и блокирует cleanup/retention до `REJOINED`; `WAITING_OWNER/CONTINUATION` считаются открытым schedule graph и не допускают новый claim/overlap/delete | Caller IDs — только данные проверки. Произвольный account, stale binding или изменение любого tuple/binding/hash создают conflict/new intent; прежняя runtime и scheduler attempt не оживают |
| Exact suspension retry | Тот же verified context | Idempotency scope включает tenant/project, actor, workload/SPIFFE, permission, authority source/reference/revision/digest/generation, operation, key и бизнес-поля. Одноразовые JTI/correlation ID, nonce и transport time не входят в semantic hash | Без нового effect возвращается сохранённая suspension даже с новым валидным transport proof | Ответ хранится в receipt | Exact key+semantic hash возвращает исходный результат; изменение authority или бизнес-поля даёт `AlreadyExists`, stale owner tuple закрыто отклоняется |
| Approved | Отдельный `ApproveIntegrationInvocation`; decision payload не выбирает owner/tuple | Row lock, expected version/fence, exact approval/invocation/request hash и повторная проверка активных pinned Integration/credential bindings | Первый decision CAS: `PENDING -> APPROVED`; reject/expiry/cancel конкурируют | До execution continuation не готова | Stale/mismatched approval, hash, revision, binding или fence закрыто отклоняется; смена connection/account требует нового intent |
| Rejected | `RejectIntegrationInvocation` | Та же immutable binding и bounded reason digest | `PENDING -> REJECTED`, одновременно материализуется ровно один continuation turn; scheduled occurrence/run перепривязываются к его exact current tuple | Новый server-owned turn получает fresh RuntimeRevision, input digest и source reference; future #192 читает structured row по authority нового turn | Execution после rejection запрещено; replay не создаёт второй turn или partial scheduled graph |
| Expired | `ExpireIntegrationInvocation`; database clock authoritative | Exact approval deadline, tuple и fence | `PENDING -> EXPIRED`, один continuation turn | Тот же version-pinned rejoin | Caller timestamp и ранняя expiry запрещены |
| Cancelled | Специализированный `CancelIntegrationInvocation`; verified integration workload | Exact tuple/fence и immutable pinned bindings | `PENDING+NOT_STARTED -> CANCELLED+NOT_APPLICABLE` либо `APPROVED+NOT_STARTED -> CANCELLED+NOT_APPLICABLE`; один continuation turn | Тот же version-pinned rejoin | Approved cancel конкурирует с Begin: победивший cancel создаёт один continuation; если begin уже перевёл строку в `EXECUTING`, cancel получает closed conflict и не отменяет внешний effect |
| Execution begin | `integration-gateway`, `BeginIntegrationExecution` | Exact approved row, request hash, version/fence и всё ещё активные pinned Integration/credential bindings | `APPROVED+NOT_STARTED -> EXECUTING`, fence возрастает | Состояние переживает restart | Начало до approval, после cancel, со stale binding/fence или повтор с mutation запрещено |
| Execution success | `CompleteIntegrationExecution`; structured result reference+digest | Exact invocation/request/tuple и immutable pinned binding; terminal result фиксируется один раз. ProcessRun хранит закрытый discriminated union `continuation_kind=INTEGRATION`, exact continuation ID и outcome digest, взаимно исключающий owner-gate tuple | `EXECUTING -> SUCCEEDED`, одновременно ровно один continuation turn | `GetIntegrationContinuation` разрешает future #192 только по signed authority нового server-owned turn и возвращает текущие version/fence/OCC tokens вместе с typed binding/result | Opposite error/result, decision/cancel, смешанный `OWNER_GATE|INTEGRATION` binding или второй terminal CAS проигрывает |
| Execution error | `FailIntegrationExecution`; structured error code/reference/digest | Та же binding | `EXECUTING -> FAILED`, один continuation turn | Тот же read/rejoin | Result/error mutation и second terminal отклоняются |
| Consumer rejoin | Будущий `agent-runner` #192, exact `AGENT_SESSION_GRANT` текущей continuation attempt | Первый `GetIntegrationContinuation` имеет пустой request: owner находит строку по verified current Turn/attempt/RuntimeRevision/input/grant tuple и возвращает текущие version/fence. Последующий ACK обязан передать эти exact tokens и input digest; owner/current binding и pinned resources разрешаются до receipt replay | Read не меняет state; `AcknowledgeIntegrationContinuation` даёт один `READY -> REJOINED` CAS для текущей delivery binding. Retry после `FAILED/EXPIRED`, в том числе после прежнего ACK, увеличивает attempt/version/fence и снова открывает один ACK новой binding | Structured immutable outcome version-pinned; crash до Get/между Get и ACK и exact ACK replay с новым JTI безопасны. Новая delivery attempt не повторяет external invocation и не создаёт approval | Request ID/JTI не authority и не semantic intent; прежний grant/attempt/revision/input, чужой tenant/session/turn, stale version/input/fence или повтор ACK с mutation закрыто отклоняется |

## Сквозная contract/authority map

| Сценарий | Requirement | Actor и источник authority | Transport identity и полный RPC | Authoritative owner/scope | Idempotency, OCC, fence и transaction | Result/fact/consumer/readiness | Ошибки |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Claim runtime snapshot | #221, `ARCH-MC-004`, `GUIDE-DOC-006` | Workload actor из signed `RUNTIME_REVISION_GRANT`; IDs запроса отсутствуют | `runtime-controller`, exact SPIFFE, `/controlplane.v1.ControlPlaneService/ClaimRuntimeExecution` | `control-plane`; org/project из proof, execution tuple из PostgreSQL | UUID key + canonical empty semantic intent; `SERIALIZABLE`, row locks, unique tuple, version/fence | Typed immutable snapshot; polling/read-rejoin; readiness через protected `CheckReadiness` той же operation profile | `NotFound`, `FailedPrecondition`, `Aborted`, `AlreadyExists`, `Unavailable` |
| Runtime mutations | #221, `GUIDE-DOC-003/006` | Exact workload/grant from verified context plus server-owned execution row | Отдельные full methods для admission, heartbeat, incident, complete, cancel, retry, expiry, archive; generic protected-kind registry их не обслуживает | `control-plane`; tenant/project resolved before ID lookup, current ProcessRun/occurrence/ScheduledRun сверяются и закрываются вместе | Key+stable semantic hash receipt: transport JTI/correlation/nonce/time исключены, exact actor/org/project/workload/SPIFFE/permission/authority/attempt/revision/input/fence/generation сохранены. Expected version/fence и одна owner transaction закрывают leases/claims/grants; retry разрешён только active/`FAILED`/`EXPIRED` | Audit and authoritative Get; AsyncAPI неприменим: #188 не реализует consumer inbox/effect в #221 | Hidden/cross-tenant `NotFound`; stale `FailedPrecondition`; race `Aborted`; изменённый intent `AlreadyExists` |
| Restore/cleanup boundary | #221, `GUIDE-DOC-003/006` | Отдельные signed grants `RUNTIME_RESTORE_VERIFIER_GRANT` и `RUNTIME_CLEANUP_AUTHORIZER_GRANT`; runtime-controller только archive/consume | `/VerifyRuntimeRestore`, `/AuthorizeRuntimeCleanup`, `/ExpireRuntimeCleanupAuthorization`, `/ConsumeRuntimeCleanupAuthorization` и protected readiness каждого профиля | RuntimeExecution owner row; attestation pin-ит archive checksum/execution/revision/input/attempt/generation; exact organization/project/session row сериализует eligibility, все non-`REJOINED` continuation этой session блокируют issue и consume | OCC version/fence + cleanup generation; consume повторно проверяет session blocker в своей transaction; state, receipt и audit одной transaction; PostgreSQL clock управляет expiry/reissue | Authoritative read; внешний verifier/authorizer deployable и issuer profile не входят в #221, поэтому destructive path до их materialization fail-closed | OIDC/control-api-gateway, archive writer, чужой SPIFFE, stale checksum/proof/generation, foreign/mixed owner rows и consumed reissue получают закрытый отказ |
| Resolve integration session | #220 contract need, #221 | `integration-gateway` session grant; request carries no business IDs | `/controlplane.v1.ControlPlaneService/ResolveIntegrationSession` | Session/turn/process/role/integration/credential state in owner DB | Read-only exact current tuple; RuntimeRevision component version+projection digest и credential expiry сверяются до ответа; no receipt | Typed bounded context с integration definition/capabilities/endpoint и credential metadata reference без secret values; protected readiness | `NotFound`, `PermissionDenied`, `FailedPrecondition` |
| Suspend approval | #221, `ADR-MC-006`, `ARCH-MC-005` | Session/turn/grant authority from signed context; invocation/approval/request hash и selected bindings — данные проверки | `/controlplane.v1.ControlPlaneService/SuspendForIntegrationApproval` | `control-plane` tenant + current RuntimeExecution/Turn/Session/Process + RuntimeRevision Integration/credential components + применимые ScheduleOccurrence/ScheduledRun | Stable semantic receipt; scheduled path использует единый lock order RuntimeExecution→occurrence→schedule→scheduled run→session→turn→ProcessRun→pinned resources→continuation, unscheduled — его подмножество RuntimeExecution→session→turn→ProcessRun→pinned resources→continuation. Одна transaction переводит RuntimeExecution в `SUSPENDED`, закрывает runtime и scheduler leases/attempt/claims/grants, graph `WAITING_EXTERNAL/CONTINUATION` и вставляет pinned continuation | Typed stored suspension; cleanup и повторный scheduler claim/overlap/delete blocked; no speculative event | Произвольный integration/account, stale digest/version/generation/token и повтор с другим semantic hash закрыто отклоняются без partial graph |
| Approval/execute/terminal | #221 | Exact integration workload; caller IDs re-resolved against immutable pinned binding | Specialized approve/reject/expire/cancel/begin/complete/fail RPCs | Owner graph блокируется раньше continuation: RuntimeExecution→occurrence→schedule→scheduled run→session→turn→ProcessRun→pinned resources→continuation; обычный path использует совместимое подмножество | version/fence CAS; approved cancel и begin имеют одного winner; terminal transaction creates one continuation turn, сохраняет `ProcessContinuationKind=INTEGRATION` и перепривязывает полный scheduled current tuple | Durable row + fresh RuntimeRevision/input/turn/grant; прежняя runtime/scheduler authority остаётся закрытой | Same bounded errors; смешанный owner-gate/integration binding и смена binding требуют закрытого отказа/нового approval intent; unavailable persistence is retryable without partial effect |
| Structured consumer rejoin | #221, future #192 | Future `agent-runner` session grant bound to server-created current continuation attempt | Пустой `GetIntegrationContinuation` и token-bearing `AcknowledgeIntegrationContinuation` | Owner resolves by verified current turn/attempt/revision/input/grant, затем pinned resources и continuation row; первый request не принимает continuation/tenant/OCC IDs | Get возвращает текущие delivery attempt/version/fence/input; ACK сверяет exact tokens до receipt lookup и фиксирует receipt/CAS. Stable hash допускает повтор с новым валидным JTI. Runtime retry атомарно rebind-ит тот же outcome, typed ProcessRun и scheduled tuple к свежей attempt | Version-pinned structured read. #192 consumer/deploy/inbox intentionally not implemented or declared active; external execution не повторяется | Hidden/mismatch `NotFound`; stale grant/attempt/revision/input или ACK `FailedPrecondition`; semantic mutation `AlreadyExists` |

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
- Deployable, issuer/readback и runtime-доставка ключей для
  `runtime-restore-verifier` и `runtime-cleanup-authorizer` не материализованы
  в разрешённом scope #221. Control-plane фиксирует exact deny-by-default
  profiles и ожидаемые отдельные trust files; до появления этих deployable
  startup/readiness либо destructive RPC остаётся закрытым. Это не заявление о
  готовом внешнем verifier/cleanup worker.

## Ручные негативные и конкурентные сценарии

1. Попытаться передать tenant/owner/process/session/turn/attempt в claim/read
   request: таких полей в Proto нет.
2. Повторить claim/heartbeat/terminal с чужим SPIFFE, project, stale attempt,
   RuntimeRevision version/digest, input digest, grant generation или fence:
   ни state, ни receipt не меняются.
3. Одновременно выполнить complete/cancel/retry/expiry: успешен один CAS,
   прежние lease/claim/grant закрыты, а Turn и ProcessRun не расходятся.
   Добавить open child/work: terminal transaction должна полностью откатиться.
4. Повторить retry для сохранённых `FAILED` и `EXPIRED`: старая строка сохраняет
   outcome и становится `RETRIED`, новая attempt получает свежие
   RuntimeRevision/input/grant. Для `SUCCEEDED`, `CANCELLED` и `SUSPENDED`
   ожидать закрытый отказ.
5. При suspension проверить одним readback, что RuntimeExecution стал
   `SUSPENDED`, lease/attempt/claims закрыты, Turn/Session/Process находятся в
   `WAITING_EXTERNAL`, а heartbeat/complete/retry/expiry старого fence не
   проходят; cleanup до `REJOINED` запрещён.
6. Вызвать restore proof и cleanup через `control-api-gateway`, archive writer
   и общий OIDC credential: policy должна отказать. Без отдельно
   материализованного `runtime-restore-verifier` destructive authorization
   остаётся недостижимой.
7. После exact proof выдать cleanup, повторить тот же key/hash и получить тот же
   receipt. До PostgreSQL expiry новая выдача запрещена; после expiry новый
   intent получает большую generation. Одновременно consume/expire/reissue:
   успешен один winner; после `CONSUMED` повторная выдача невозможна.
8. Повторить suspension с тем же key/hash и затем изменить invocation,
   approval, RuntimeRevision, integration ID/version/digest либо любой
   credential binding ID/version/digest: первый повтор возвращает receipt,
   второй конфликтует; произвольный account не принимается.
9. Одновременно approve/reject/expire/cancel; отдельно после `APPROVED` запустить
   cancel и begin. В каждом race один winner; если begin победил, cancel не
   меняет `EXECUTING` и не отменяет внешний effect. Terminal success/error даёт
   ровно один continuation turn и переживает restart.
10. Выполнить первый `GetIntegrationContinuation` пустым request под exact grant
    нового continuation Turn, получить current version/fence/input, затем
    передать их в ACK. Чужой tenant/session/turn и stale ACK данные не раскрывают.
11. Завершить/просрочить delivery RuntimeExecution до первого Get, повторить
    `FAILED/EXPIRED` и проверить fresh attempt/revision/input/grant, увеличенные
    delivery version/fence и доступность того же outcome. Повторить crash между
    Get/ACK и retry после ACK: на каждую текущую binding один ACK winner, старый
    grant не проходит, новый approval/external invocation не создаётся.
12. Для scheduled source при suspension проверить `CLAIMED -> CONTINUATION`,
    очистку scheduler token/generation/lease и сохранение suspended current tuple.
    Stale scheduler expiry/claim, overlap и delete не проходят. Reject/expiry/
    pending cancel/approved cancel/success/error перепривязывают occurrence/run к
    continuation tuple; terminal/retry/ACK закрывают либо продолжают граф один раз.
13. Материализовать role с `runtime.cluster.admin` и без него: snapshot обязан
    вернуть соответственно `CLUSTER_ADMIN` или закрытый более слабый профиль;
    caller-provided profile на результат не влияет.
14. Потерять ответ state-changing runtime/integration команды и повторить тот же
    idempotency key и semantic intent с новым валидным JTI: получить сохранённый
    result/error. Изменить actor/workload/SPIFFE/permission, authority tuple,
    attempt/revision/input/fence/generation или бизнес-поле: replay не проходит.
15. Проверить `ProcessRunSpec` отдельно для полного `OWNER_GATE` и полного
    `INTEGRATION` binding. Пустой discriminator, неполный tuple и смесь gate с
    integration ID/outcome digest закрыто отклоняются. Reject/expiry/cancel/
    success/error и delivery retry сохраняют валидный integration union.
16. Создать две continuation одной session (`SUSPENDED`/`READY`) и terminal
    execution другой attempt: cleanup issue и consume заблокированы до `REJOINED`
    всех строк. Non-`REJOINED` строка другой session не блокирует. В гонке
    continuation retry/reissue/consume нет проверки по устаревшему допуску.
17. Одновременно запустить runtime terminal/retry, integration materialization,
    `CancelScheduleOccurrence` и scheduler expiry для одного scheduled tuple.
    Проверить единый порядок RuntimeExecution→occurrence→schedule→scheduled run→
    session→turn→ProcessRun→pinned resources→continuation, одного OCC winner,
    отсутствие `40P01`-инверсии и partial graph commit. Unscheduled path не
    создаёт schedule rows.

## Forward-only rollback

Миграция добавляется только вперёд и не имеет штатного `down`. Откат образа
допустим лишь на версию, понимающую новую схему и authority policy revision 8. Иначе
workload остаётся неготовой до выпуска совместимой версии. Удаление строк,
снижение fence/grant generation, повторное открытие lease/claim, сброс receipt
или откат authority policy запрещены; исправление выполняется новой
компенсирующей migration и новым Issue после backup/readback и owner gate.
