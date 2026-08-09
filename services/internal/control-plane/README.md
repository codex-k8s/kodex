# Control-plane

`control-plane` — авторитетный внутренний сервис конфигурации и управляющего
состояния MatterCodex. Он реализует Issue
[#187](https://github.com/codex-k8s/matter-codex/issues/187) как один
развёртываемый компонент и расширяет его специализированным runtime и
integration-continuation контуром Issue
[#221](https://github.com/codex-k8s/matter-codex/issues/221) и owner
Project/Schedule/OwnerGate/Backup/Restore path Issue
[#231](https://github.com/codex-k8s/matter-codex/issues/231) и полной
owner-конфигурацией Issue
[#234](https://github.com/codex-k8s/matter-codex/issues/234) и authoritative
owner readbacks Issue
[#263](https://github.com/codex-k8s/matter-codex/issues/263).

Сервис владеет:

- проектами, командами и чатами; legacy `ROLE`/`PROMPT_PROFILE` доступны только
  как immutable read для старых graphs и server-derived runtime projection;
- отдельными `RoleDefinition`, `Agent`, `AgentAssignment`, версионируемыми
  `InstructionSet`, masked provider refs/pools и Workspace↔Mattermost mapping;
- deterministic legacy cutover map и typed owner reconciliation для
  существующих `ROLE`/`PROMPT_PROFILE`, которые невозможно безопасно
  преобразовать без exact immutable Instruction content;
- метаданными привязок учётных данных, репозиториев, рабочих пространств и
  интеграций;
- неизменяемыми ревизиями среды исполнения;
- immutable runtime execution snapshot, lease/fence, архивной ссылкой,
  независимым restore proof и bounded cleanup authorization;
- безопасной owner backup projection и discoverable restore operation,
  связывающей полный приватный archive/source tuple с server-owned fresh
  attempt через `source_authority_sha256`, монотонное поколение и
  consume/revoke watermark;
- typed integration approval/execution continuation и её version-pinned
  authoritative read/rejoin;
- сессиями, ходами и родословной процессов;
- расписаниями, шлюзами владельца, памятью и заявками на работу;
- owner readback и закрытыми действиями Run/Incident, а также полным
  `WORKSPACE|ALL_WORKSPACES` backup/restore envelope;
- server-derived Agent runtime catalog, versioned Schedule presets/defaults,
  immutable inline prompt materialization, closed `nextActions`, safe
  Run/Incident/Restore display projections и bounded typed configuration diff;
- метаданными артефактов; immutable Instruction и Schedule prompt content записывается через
  узкий versioned S3 client, а остальные artifact bytes остаются вне сервиса.
  Readiness перед каждым canary Put получает PostgreSQL transaction-scoped
  advisory fence на выделенной connection и только затем bounded согласует все
  versions/delete markers двух выделенных readiness prefixes. Поэтому replica не удаляет live
  VersionID соседнего probe, а ambiguous S3 commit переживает replacement pod и
  не создаёт неограниченную цепочку orphan versions.

Значения секретов остаются во внешнем хранилище Vault/Kubernetes.
`control-plane` не вызывает Mattermost, MCP, Codex и Kubernetes API, не
согласует среду исполнения и не реализует внешний HTTP API.

## Сквозные границы

```text
control-api-gateway
  -> точные mTLS и первый OIDC-вызов
  -> control-plane AuthorityProofResolver
  -> серверное разрешение проекта и полномочий в PostgreSQL
  -> короткоживущее доказательство полномочий
  -> локальный для рабочей нагрузки путь issuer/verifier #186
  -> полный метод ControlPlaneService
  -> caster -> доменный сервис -> порт репозитория
  -> транзакция PostgreSQL
       агрегат + подтверждение идемпотентности + аудит + необязательный факт outbox
  -> сквозной кэш Redis по принадлежащей PostgreSQL эпохе
  -> ретранслятор outbox -> точные поток и subject NATS JetStream
```

Actor, организация, проект, полномочия, рабочая нагрузка и SPIFFE-идентичность
не принимаются в бизнес-запросе. Они выводятся из проверенного контекста
Issue #186. Для первого OIDC-вызова сервис дополнительно проверяет точного
mTLS-клиента, issuer, единственную audience, `iat`/`nbf`/`exp`, максимальный
TTL, ревизию сессии и JTI. Полномочия проекта разрешаются внутри границы
организации PostgreSQL до подписи доказательства.

## Контракты и потребители

- Proto: `contracts/proto/controlplane/v1/control_plane.proto`;
- сгенерированный публичный Go API: `libs/go/controlplaneapi/gen/controlplane/v1`;
- переиспользуемая промышленная композиция клиента: `libs/go/controlplaneclient`;
- AsyncAPI: `contracts/asyncapi/control-plane/v1/asyncapi.yaml`;
- политика полномочий: `deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json`.
- две lifecycle/authority matrix и сквозная карта:
  `services/internal/control-plane/runtime-continuation-contract.md`.
- закрытый реестр owner-конфигурации, карты сценариев, execution graph и полная
  lifecycle/authority matrix Issue #234:
  `services/internal/control-plane/owner-configuration-contract.md`.
- Typed owner materializer исторического полного графа и его authority,
  dependency, provenance, lifecycle/replay/error matrices зафиксированы в
  `services/internal/control-plane/legacy-data-materializer-contract.md`.

Внешнее отображение принадлежит будущему `control-api-gateway`; этот компонент
публикует только внутренний gRPC. Политика deny-by-default регистрирует
отдельных производителей доказательств и точные идентичности клиентов для gateway, `agent-runner`,
`automation-scheduler`, внешнего `artifact-scanner`, `interaction-gateway`,
`runtime-controller`, `integration-gateway` и локального `memory-indexer`.
Последний индексирует
локальную проекцию pgvector без внешнего сервиса embeddings, scanner владеет
сканированием байтов, а `control-plane` — метаданными и автоматом состояний.
Неизвестные производитель, назначение учётных данных, рабочая нагрузка,
SPIFFE ID, полный метод, audience или полномочие закрыто отклоняются.
Новые owner operations входят в policy revision 27. Provider reference
mutation принимает только exact `integration-gateway` provider-readback
receipt. Workspace↔Mattermost mapping и Agent bot identity принимают только
exact `interaction-gateway` provider-readback receipt; team/object refs
выводятся из проверенного proof, а OIDC-профиль имеет лишь безопасные typed
reads. Git reconcile отделён от обычного UI update точными RPC и permission.

Owner readbacks Issue #263 зарегистрированы отдельными operation IDs
`control.owner-configuration.catalog`, `control.owner-schedule.manage|get|list`
и `control.run.list`. Startup загружает их из той же exact authority policy, а
наблюдаемость строит bounded method labels из generated descriptor. Новых
workers, async consumers, broker subjects, egress либо deployable не добавлено.

Это receive-side contract, а не заявление готовности внешних producers.
После merge #234 Issue #235 обязана rebase, добавить Mattermost Team/bot
effect, signer `ProviderEffectReadbackReceipt` и generated calls
`ManageWorkspaceMattermostMapping`/`ManageAgentMattermostBotIdentity`.
Issue #236 обязана добавить Git reconciler signer/call site и provider-reference
producer. До этих merge gates control-plane закрыто принимает только proof от
зарегистрированных exact workload, но соответствующие end-to-end сценарии не
считаются готовыми. Browser/OIDC не может выпускать ни provider, ни Git proof.

`controlplaneclient` выполняет полный путь потребителя: точный mTLS к
`control-plane`, проверку прикладного разрешения конкретной рабочей нагрузки
через `AuthorityProofResolver`, локальный UDS issuer Issue #186, interceptor
полного метода и readiness через тот же защищённый RPC. Конкретный компонент
потребителя обязан смонтировать своё разрешение, сокет issuer и файлы mTLS и
вызвать один из закрытых профилей операций (`AgentRunnerOperations`,
`AutomationSchedulerOperations`,
`ArtifactScannerOperations`, `RuntimeControllerOperations`,
`RuntimeOwnerOperations`, `RuntimeRestoreVerifierOperations`,
`RuntimeRestoreEffectOperations`,
`RuntimeCleanupAuthorizerOperations`, `IntegrationGatewayOperations`,
`OwnerGateDeliveryOperations`,
`MemoryIndexerOperations`, `InteractionGatewayOperations`). Consumer
Deployments не принадлежат Issue #187 и здесь не подменяются фиктивными
развёртываемыми компонентами. Issue #231 материализует exact
`runtime-restore-verifier` и `runtime-restore-effect` profiles; последний
доступен только S3 restore exchanger и перед STS повторно сверяет durable
revoke watermark. `runtime-cleanup-authorizer` остаётся отдельным закрытым
профилем. `control-api-gateway` не входит ни в один из этих
профилей и не может тем же trust path подтвердить restore и разрешить cleanup.

Issue #193 материализует отдельный consumer scheduler profile в
`services/jobs/automation-scheduler` и `deploy/k8s/base/automation-scheduler`.
Он использует generated protected client и не получает прямой PostgreSQL,
Mattermost либо Kubernetes authority.

Публикуются только два факта с утверждёнными потребителями:

| Факт                                          | Условие                                                                                               | Потребитель            | Доставка                                  |
| --------------------------------------------- | ----------------------------------------------------------------------------------------------------- | ---------------------- | ----------------------------------------- |
| `control_plane.runtime_configuration_changed` | устойчивое изменение project/team/chat/role/prompt/binding/workspace/integration/runtime/session/turn | `runtime-controller`   | at-least-once, inbox и курсор потребителя |

Для процессов, шлюзов владельца, памяти, заявок на работу и метаданных артефактов
спекулятивные события не публикуются: авторитетные пути — `GetResource`,
`ListResources`, `SearchResources`, `ListAuditEvents` и `ListTombstones`.
Удаление, отмена, завершение и повтор каждого агрегата сохраняют tombstone,
аудит и подтверждение. Outbox фиксируется в транзакции команды; ретранслятор
не публикует из транспортного или доменного кода. После устойчивого JetStream
`PubAck` строка остаётся с потоком, последовательностью, признаком дубликата и
ограниченным сроком очистки. Потерянное подтверждение безопасно повторяет тот
же `event_id`.

Runtime execution и integration continuation не добавляют speculative facts в
AsyncAPI: до материализации будущего потребителя Issue #192 их результат
доступен через специализированные защищённые read/rejoin RPC. Первый
`GetIntegrationContinuation` не принимает неизвестные caller-selected OCC
tokens: exact row разрешается из signed authority нового server-owned Turn и
возвращает current delivery attempt/version/fence/input; ACK сверяет эти exact
значения. Retry `FAILED/EXPIRED` атомарно перепривязывает тот же immutable
outcome к свежим RuntimeRevision/input/attempt/grant и не повторяет approval
или внешний вызов. `ProcessRunSpec` различает взаимоисключающие typed bindings
`OWNER_GATE` и `INTEGRATION`; integration binding хранит exact continuation ID
и outcome digest без фиктивного owner feedback. Закрытые domain operations
целиком переключают `OWNER_GATE|INTEGRATION|NONE`, очищая поля другого arm:
после integration rejoin новый OwnerGate не наследует integration ID/digest, а
`CHANGES_REQUESTED` не смешивает arms. Semantic idempotency не зависит
от одноразового JTI/correlation ID, но сохраняет полный проверенный owner и
authority tuple. Cleanup issue и consume повторно проверяют все non-`REJOINED`
continuation exact session, включая current delivery binding.

Все команды, которые могут встретить один execution graph, используют один
code-enforced acquisition: read-only candidate, затем существующий
RuntimeExecution → ScheduleOccurrence → Schedule → ScheduledRun → Session →
Turn → ProcessRun → pinned resources → OwnerGate → IntegrationContinuation.
Unscheduled path использует подмножество без schedule rows. Current-turn
discovery охватывает `CLAIMED`, `WAITING_OWNER`, `CONTINUATION` и terminal
`SUCCEEDED/FAILED/CANCELLED`; stale ClaimTurn, scheduler recovery, Get/ACK и owner-gate decision
повторно проверяют exact tuple/deadline/version после locks. PostgreSQL retry
остаётся safety net, а не способом исправить lock inversion.

`ClaimTurn` после server-owned `QUEUED -> CLAIMED` под теми же locks переносит
новую `Turn.Version` в `ProcessRun.Current*` и применимые
`ScheduleOccurrence.Execution*`/`ScheduledRun.Current*`; новый
`ProcessRun.Version` также становится частью scheduled binding. Только после
этого одной transaction сохраняются Turn lease/attempt, receipt, audit и outbox.
Stale process/occurrence/run tuple закрыто отклоняет весь claim, а exact replay
не создаёт второго version bump. Поэтому первый `ClaimRuntimeExecution` видит
согласованный scheduled или unscheduled graph без caller-selected versions.

Scheduled producer хранит два разных server-owned digest без смешения типов.
До materialization `Schedule.EffectiveInputSHA` и queued occurrence содержат
immutable snapshot target/prompt/artifact/runtime/session policy. После
`ClaimScheduleOccurrence` только фиксирует `RESERVED` и one-time capability;
после `MaterializeScheduleOccurrence` exact execution digest совпадает в
`Turn.EffectiveInputSHA256`, `ScheduleOccurrence.EffectiveInputSHA256` и
`ScheduledRun.CurrentInputSHA256`, а исходный snapshot остаётся в
`ScheduledRun.EffectiveInputSHA256`. Та же owner transaction закрепляет
`scheduled-result.v1` в `RuntimeRevision` и `Turn`; generated read path
передаёт его runtime-controller и runner без локальной подмены. PostgreSQL `UpdateScheduleOccurrence`
явно сохраняет изменяемый digest; repository fake повторяет field-level SQL
contract и не маскирует пропущенный named argument заменой всей структуры.
Обычный `FAILED/EXPIRED` completion и watchdog, который после ожидания lock
видит уже terminal Turn/Process, проходят один
`applyScheduledTerminalDisposition`: одинаково применяют retry/dead-letter,
завершают прежний run, восстанавливают queued occurrence digest из его
immutable snapshot и очищают прежний claim/execution tuple. Более новая
Schedule snapshot не подменяет pinned значение и закрыто блокирует requeue.
Watchdog discovery и каждая exact recovery disposition коммитятся отдельно от
следующего scheduler selection; отсутствие новой due строки не откатывает уже
terminal run, retry/dead-letter, authority cleanup и audit. Overlap `SKIP`
также фиксирует самостоятельный terminal fact до нового poll. Selection SQL и
post-lock проверка после occurrence→Schedule учитывают любой historical open
`ScheduledRun`, поэтому terminal occurrence не освобождает `QUEUE` для второго
graph до закрытия прежнего run.
Под `PAUSED` queued retry ждёт `ACTIVATE`; `ARCHIVED/DELETION_PENDING/DELETED`
не принимают requeue. Retry/suspension/rebind сравнивают current digest,
сохраняя snapshot provenance.
Переходы Schedule/occurrence/run пишут audit и доступны authoritative read, но
не объявляют AsyncAPI event topology: scheduler использует только polling.
События других утверждённых Resource aggregates используют sequence ровно
новой версии действительно изменённого aggregate.

Session lifecycle и cross-session delegation используют batch-вариант того же
resolver: RuntimeExecution/occurrence/schedule/run, Session, Turn и ProcessRun
глобально сортируются для всех затронутых graphs. `ManageSession` повторяет
open-turn discovery под Session lock; ARCHIVE/CLEANUP запрещены при open Turn,
live runtime или любой non-`REJOINED` continuation этой session, а CLOSE/CANCEL
не закрывают graph в обход specialized runtime/scheduler transition.
`StartProcess` и `EnqueueTurn` повторно сверяют parent/current/delegation tuple;
target Turn с materialized runtime не перепривязывается.
`ManageSchedule(UPDATE/ARCHIVE/DELETE)` блокирует Schedule и проверяет
authoritative occurrence+run open-set до receipt; UPDATE только затем получает
pinned rows. ARCHIVE проходит лишь после terminal/no-open graph и необратим.

Каждая lifecycle-команда с receipt использует двухфазный порядок: сначала
exact owner/current graph, transport authority, attempt/revision/input,
version/fence/generation/state/expiry, затем receipt и только после отсутствия
receipt — effect. Admission replay возвращает LeaseToken лишь пока тот же
RuntimeExecution остаётся current `ADMITTED` и token digest совпадает; после
terminal/cancel/expiry/rebind старый authority-bearing result закрыт.
`ManageWorkClaim`, `CompleteProcess` и `CancelProcess` выводят current Turn из
server-owned ProcessRun и проходят тот же resolver до первого shared row lock.
Generic Turn/Process и stale scheduler paths закрыто отказываются при live
RuntimeExecution. `RequestOwnerGate` вместо этого атомарно переводит exact
active runtime в `SUSPENDED`, очищает lease/token/deadline, завершает attempt и
лишь затем фиксирует `WAITING_OWNER`; решение или retry создаёт только свежую
authority.
OwnerGate delivery claim хранит только server-side hash idempotency key:
unlocked candidate проходит тот же graph resolver, Gate блокируется последним,
и receipt читается после workload/SPIFFE/deadline/current-claim проверки.
Готовность решения появляется только после provider read-after-write receipt;
browser не передаёт Mattermost locator либо process/session/turn tuple. После
delivery, expiry или decision старый ClaimToken не возвращается, а replay
`CHANGES_REQUESTED` возвращает сохранённый receipt и после законного
продвижения созданного continuation.
`ClaimTurn`/`RenewTurn` также разрешают exact Turn и lease до receipt. Scheduler
claim сохраняет отдельный server-owned claim-key hash в occurrence: retry
сначала восстанавливает current graph по этой привязке, и только live exact
occurrence может повторно вернуть LeaseToken.
Первый `next_run_at` и каждый следующий watermark вычисляются владельцем по
PostgreSQL clock из cron/interval/timezone. `RunScheduleNow` создаёт отдельную
occurrence под owner/version/idempotency lock и не двигает этот watermark.
Organization-scoped scheduler grant не выбирает проект: durable owner cursor
разрешает project partition до project-scoped transaction. Ротация JTI и
revision короткоживущего unbound grant не меняет semantic intent уже
проверенного workload, но exact SPIFFE, full method, permission и server-owned
occurrence/lease остаются обязательными.
`AdmitRuntimeExecution` и `HeartbeatRuntimeExecution` одной transaction по
PostgreSQL clock выравнивают deadline RuntimeExecution и зависимого TurnLease;
generic `RenewTurn` не получает runtime authority. Deadline-sensitive paths
читают fresh decision time только после canonical graph и exact target row
locks. Поэтому `ManageWorkClaim(CREATE/RENEW)` replay повторно блокирует
сохранённую claim до clock/receipt, а RENEW, начавшийся до expiry и продолживший
после ожидания lock, не раскрывает старый ACTIVE result и не воскресает.
`RequestOwnerGate`, Turn/scheduler leases и pinned Integration credentials
используют тот же post-lock invariant.

## Доменные инварианты

| Область                  | Инвариант                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Все команды              | семантический ключ идемпотентности, канонический digest запроса, OCC и аудит фиксируются атомарно; receipt читается только после authoritative current eligibility, а superseded authority-bearing result никогда не возвращается                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| Проект                   | ID и владельца назначает сервер; создание в организации требует полномочия владельца; update/delete сначала разрешают tenant owner project, затем проверяют OCC/receipt; owner и `managed_by` неизменяемы; delete допускается только без live children и одной transaction фиксирует `DELETION_PENDING`→`DELETED`, audit и события; slug стабилен                                                                                                                                                                                                                                                                                                             |
| Команда, роль и prompt   | общий CRUD не управляет полномочиями; отдельная административная команда проверяет полномочие вида, назначаемое подмножество и запрещает самостоятельное включение и повышение                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| Управляемая конфигурация | target `RoleDefinition`/`Agent`/`InstructionSet`/`ProviderPool` хранит server-assigned `managed_by=UI|GIT`; UI не назначает Git ownership, Git-owned объект изменяет только exact signed reconciler RPC #236 с source/revision/digest/target/intent и one-use JTI; detach очищает source binding, copy создаёт новую UI entity; deterministic legacy map остаётся `BLOCKED` с typed action до exact reconciliation, которая атомарно создаёт весь target catalog                                                                                                                                                                                                                  |
| Привязка учётных данных  | хранится только URI метаданных; назначение и principal неизменяемы; ревизия растёт ровно на один; provider binding несёт server-verified eligibility/capabilities, лимит, usage, время и ревизию наблюдения                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Интеграция               | идентичность определения неизменяема; версия движется только вперёд                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Ревизия среды исполнения | перед каждой новой target-сессией/ходом сервер повторно разрешает active `AgentAssignment`, Agent, published InstructionSet, ProviderPool, RuntimeProfile и credential через единый selector с expiry headroom/freshness/capacity/durable weighted slot; cursor привязан к существующему authoritative Agent/Role, capacity объединяет legacy Session pin и target RuntimeRevision pin; `RuntimeRevision` хранит target tuple в top-level digest/readback, а Components содержит только принятые runtime-controller kinds и immutable derived ROLE/PROMPT; Prompt pin-ит CLEAN content Artifact, который materializes как `AGENTS.md`, без legacy mutation authority |
| Сессия                   | новый `ManageSession(CREATE)` допускает только Agent + active Workspace/Room assignment и target version-pinned dependencies; `createLegacyManagedSession` недостижим из нового path, а legacy RoleSpec/PromptProfileSpec остаются immutable read только для ранее созданных graphs; provider binding server-resolved; close/cancel работают через полный owner graph                                                                                                                                                                                                                                                                        |
| Ход                      | неизменяемый закреплённый снимок, строгий FIFO и один активный ход на сессию; claim/renew/complete связывают рабочую нагрузку, попытку, поколение полномочий, срок и fence; после runtime admission heartbeat атомарно продлевает RuntimeExecution и TurnLease до одного PostgreSQL deadline                                                                                                                                                                                                                                                                                                                                                                                         |
| Восстановление хода      | истечение срока или ручной повтор сначала закрывает прежние attempt/lease/gate/claim, затем создаёт свежие `RuntimeRevision`, effective input, attempt и grant и атомарно перепривязывает единый current execution tuple процесса и `ScheduledRun`; `SourceRef` остаётся bounded server-owned identity, номер attempt хранится только в tuple; устаревшие workload/generation/token отклоняются                                                                                                                                                                                                                                                                                  |
| Runtime execution       | control-plane материализует server-owned tuple organization/project/process/session/thread/role/turn/attempt, immutable input digest, RuntimeRevision version/digest, закрытые ResourceClass и `NONE/PROJECT_READ_ONLY/CLUSTER_ADMIN`, exact workload/generation и monotonic fence; terminal/cancel/expiry закрывают Turn/ProcessRun/occurrence/ScheduledRun вместе, retry принимает только active/`FAILED`/`EXPIRED` и создаёт свежие revision/input/attempt/grant; cleanup проходит монотонные `NONE/ACTIVE/EXPIRED/CONSUMED` и невозможна без exact archive checksum и отдельной verifier attestation; owner restore повторно проверяет latest eligible archive, source version/fence, safe digests и retention по PostgreSQL clock, затем атомарно создаёт immutable restore operation и pinned fresh Turn/RuntimeRevision/attempt; private locator/evidence/PVC/grant копируются только при exact runtime claim и никогда не входят в browser RPC |
| Integration continuation | suspension отдельно сверяет claimant `agent-runner` TurnLease/TurnAttempt и executor `runtime-controller` RuntimeExecution/SPIFFE, атомарно терминализирует runtime как `SUSPENDED`, закрывает runtime и scheduler authority, переводит Turn/Session/Process в `WAITING_EXTERNAL` и записывает в ProcessRun/occurrence/ScheduledRun полный current tuple с уже увеличенными Session/Turn versions; terminal decision/result под теми же locks переводит source Turn в terminal `CANCELLED/integration_continuation_materialized`, сохраняет его provenance через RuntimeExecution/TurnAttempt/audit и `PredecessorTurnID`, затем создаёт один fresh RuntimeRevision/input/Turn/grant и перепривязывает полный scheduled current tuple; retry delivery увеличивает attempt/version/fence для того же immutable outcome без повторного external effect, pending/повторно открытая delivery блокирует cleanup до rejoin |
| Процесс                  | дочерний процесс наследует server-owned root actor/org/project и может перейти в отдельную target session только через неизменяемое delegation edge source→target с exact turn/attempt/input/generation; enqueue и WorkClaim повторно проверяют эту родословную; terminal success/failure/cancel сверяется с авторитетным ходом, закрывает result и запрещён при активном child/work/gate                                                                                                                                                                                                                                                                                        |
| Расписание               | `CreateScheduleFromOwnerSelections` принимает stable Agent/Instruction/Pool/Room и display name Artifact и под locks назначает ID и exact Workspace/Runtime/Assignment tuple; все reuse/archive/create Session сначала получают единый project graph advisory fence и после ожидания перечитывают exact conversation boundary `FOR UPDATE`; admission partial index охватывает только live/resumable Session, поэтому immutable `ARCHIVED` history не блокирует replacement; `BindScheduleConfiguration` для `PERSISTENT`/`ROLLING` сохраняет совместимую Session либо атомарно создаёт replacement, `NEW` очищает binding; каждая materialization повторяет assignment resolution; stale/revoked fail closed, lifecycle #231 остаётся единственным источником истины |
| Owner-конфигурация       | Agent state actions специализированы; provider/Git receipts включают exact protected target/intent и one-use consume в owner transaction, а exact replay читает сохранённый immutable result snapshot, не mutable current row; bot/mapping producer ожидает #235, Git/provider producer — #236; mapping relink/unlink отклоняется при open graph; Incident fence монотонен, release закрывает runtime graph, retry принимает только current FAILED/EXPIRED либо released CANCELLED execution; Run lineage строится от Process→Turn→TurnAttempt→его immutable RuntimeRevision pin и охватывает pre-admission/terminal attempts/artifacts; backup отдельным owner query включает все non-deleted historical Session с exact archive либо целиком откатывается |
| Шлюз владельца           | запрос закрепляет корневого инициатора и единый server-owned current execution tuple process/session/turn/attempt/runtime revision/input, schedule/occurrence и точного получателя; доставка имеет неизменяемые ID, digest, Mattermost post и устойчивое подтверждение; `ExpireOwnerGate` выбирает unlocked candidate, затем блокирует полный graph и сам Gate последним, повторно сверяет PostgreSQL deadline и автономно закрывает просроченный graph; delivery query его не выдаёт; `CHANGES_REQUESTED` сохраняет terminal decision receipt и полное неизменяемое owner feedback в новом `TurnSpec`, тот же ProcessRun/root и создаёт свежие revision/input/turn; complete/gate/work-claim/schedule/retry читают одну current-связку, а решение не отображается в `FAILED` |
| Память                   | область, владелец, процесс, рабочая нагрузка и происхождение назначаются сервером; единый eligibility скрывает `DELETED` title/content в single/list/generic/FTS/vector путях и оставляет tombstone только в авторизованном audit/read path; FTS ищет title/content с ранжированием и курсором; проекция pgvector связывает точные content/resource/model version и digest                                                                                                                                                                                                                                                                                                       |
| Заявка на работу         | владелец, процесс, рабочая нагрузка, задача и попытка выводятся сервером и неизменяемы; активная заявка точного процесса или хода уникальна; RENEW и CREATE/RENEW replay получают свежий PostgreSQL clock после canonical graph и exact WorkClaim lock, поэтому ожидание блокировки не позволяет раскрыть receipt или оживить истёкшую ACTIVE строку; database eligibility обновляется отдельной forward-only migration                                                                                                                                                                                                                                                                                                                        |
| Метаданные артефакта     | только `RegisterArtifact` создаёт `PENDING`; точный scanner переводит `SCANNING`→`CLEAN`/`QUARANTINED`/`FAILED`; прикреплять и использовать разрешено только точный `CLEAN` digest                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |

Ссылки разрешаются внутри текущих настроек RLS организации и проекта;
межорганизационный и скрытый ресурсы дают одинаковый `NotFound`.

## Данные и кэш

PostgreSQL — единственный источник истины. Миграция создаёт схему
`control_plane`, отдельного владельца `NOLOGIN/NOSUPERUSER/NOBYPASSRLS`,
групповые роли среды исполнения и ретранслятора, `FORCE RLS`, ограничения и
точные разрешения. Поколения LOGIN `CURRENT`/`NEXT`/ограниченное
`PREVIOUS`/`RETIRED` материализуются принадлежащим окружению жизненным циклом
Vault. Устойчивая монотонная верхняя граница и намерение не допускают
воскрешения поколения после отката метаданных ConfigMap/Vault; повышение
требует фактического чтения через DSN principal `NEXT`.
Вывод из эксплуатации выполняет `NOLOGIN`, отзыв членства и серверное
завершение открытых соединений. Каждый statement использует одноразовый
привязанный HMAC контекст транзакции и заново связывает `session_user`,
поколение, состояние, organization/project/actor, PID соединения и ID
транзакции. GUC и `SET SESSION AUTHORIZATION` не являются источником
полномочий. Readiness проверяет схему
`20260806023400`,
membership, `LOGIN`, `NOSUPERUSER` и `NOBYPASSRLS`.

SQL хранится по одному именованному запросу в
`internal/repository/postgres/controlplane/sql`. Транзакция команды использует
`SERIALIZABLE`; путь запроса — транзакцию `READ ONLY` с локальной для
транзакции областью RLS.

Redis хранит только ограниченные снимки ресурсов:

- ключ содержит SHA-256 точного пространства имён
  `organization+project+kind+id+epoch`;
- строгая оболочка повторяет organization/project/kind/id/version, digest ключа
  и проекции; неизвестное поле или несовпадение никогда не возвращает кэш;
- TTL не более минуты, value не более 128 KiB;
- авторитетная эпоха кэша увеличивается в той же транзакции PostgreSQL;
- промах, повреждение или ошибка Redis приводит к чтению PostgreSQL;
- владение, полномочия, идемпотентность, аренды и верхние границы в Redis не
  хранятся.

## Запуск, готовность и остановка

До привязки gRPC listener сервис синхронно проверяет:

1. роли и схему PostgreSQL для среды исполнения и ретранслятора;
2. путь Redis с TLS;
3. точный поток JetStream (`CONTROL_PLANE`, subjects, replicas, файловое
   хранилище, `LimitsPolicy`, `DiscardOld`, максимальный срок 30 дней, окно
   дедупликации 2 минуты,
   `MaxMsgs=10000000`, `MaxBytes=34359738368`,
   `MaxMsgsPerSubject=5000000`, максимальный размер сообщения 262144 байта,
   запрет delete/purge, отсутствие mirror/source/republish/rollup/transform);
4. независимо доставленные закрытый ключ и доверие доказательства, ревизию
   политики;
5. тот же локальный verifier #186, который обслуживает рабочие RPC.

После барьера запускаются ретранслятор и периодическое согласование readiness.
Неожиданное завершение любого worker закрыто завершает процесс; orchestrator
не получает внешне живую реплику без циклов ретранслятора и readiness. При
остановке readiness сначала закрывается, workers отменяются и присоединяются
до закрытия PostgreSQL/Redis/NATS; gRPC и HTTP получают ограниченную остановку.
Остановка tracing и сброс Sentry используют независимые бюджеты.

Метрики не содержат ID организации или ресурса и используют закрытые labels.
Dashboard — `mattercodex-control-plane`. Alerts ведут на абсолютный HTTPS URL
runbook.
Новые protected команды учитываются в `mutations_total` только через закрытый
набор `kind/action`; отдельная панель показывает owner configuration и recovery
mutations без tenant/resource labels.

## Конфигурация

Значения ниже — имена, а не значения секретов.

| Переменная                                                                                                                                                                                                                                | Назначение                                                                                                                                      |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `CONTROL_PLANE_GRPC_LISTEN`, `CONTROL_PLANE_TECHNICAL_LISTEN`                                                                                                                                                                             | внутренние listeners                                                                                                                            |
| `CONTROL_PLANE_TLS_CERTIFICATE_FILE`, `CONTROL_PLANE_TLS_PRIVATE_KEY_FILE`, `CONTROL_PLANE_TLS_CLIENT_CA_FILE`                                                                                                                            | точный mTLS рабочей нагрузки                                                                                                                    |
| `CONTROL_PLANE_POSTGRES_DSN_FILE`, `CONTROL_PLANE_POSTGRES_RELAY_DSN_FILE`                                                                                                                                                                | файлы DSN среды исполнения и ретранслятора                                                                                                      |
| `CONTROL_PLANE_POSTGRES_RUNTIME_CURRENT_DSN_FILE`, `CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_DSN_FILE`, `CONTROL_PLANE_POSTGRES_RUNTIME_PREVIOUS_DSN_FILE`                                                                                     | точные DSN materialized LOGIN; CLI создаёт отсутствующее поколение через ограниченный controller, затем проверяет `NEXT` отдельным подключением |
| `CONTROL_PLANE_POSTGRES_TLS_SERVER_NAME`, `CONTROL_PLANE_POSTGRES_CA_FILE`, `CONTROL_PLANE_POSTGRES_MAX_CONNECTIONS`                                                                                                                      | TLS и пул PostgreSQL                                                                                                                            |
| `CONTROL_PLANE_POSTGRES_PRINCIPAL_NAME`, `CONTROL_PLANE_POSTGRES_PRINCIPAL_GENERATION`, `CONTROL_PLANE_POSTGRES_CONTEXT_KEY_ID`, `CONTROL_PLANE_POSTGRES_CONTEXT_KEY_FILE`                                                                | точное поколение среды исполнения и доказательство контекста транзакции                                                                         |
| `CONTROL_PLANE_REDIS_ADDRESS`, `CONTROL_PLANE_REDIS_TLS_SERVER_NAME`, `CONTROL_PLANE_REDIS_CA_FILE`, `CONTROL_PLANE_REDIS_USERNAME`, `CONTROL_PLANE_REDIS_PASSWORD_FILE`, `CONTROL_PLANE_REDIS_DATABASE`, `CONTROL_PLANE_REDIS_POOL_SIZE` | ограниченный кэш Redis                                                                                                                          |
| `CONTROL_PLANE_NATS_URL`, `CONTROL_PLANE_NATS_TLS_SERVER_NAME`, `CONTROL_PLANE_NATS_CA_FILE`, `CONTROL_PLANE_NATS_CREDENTIALS_FILE`, `CONTROL_PLANE_NATS_STREAM`, `CONTROL_PLANE_NATS_REPLICAS`                                           | точный издатель JetStream                                                                                                                       |
| `CONTROL_PLANE_AUTHORITY_POLICY_FILE`                                                                                                                                                                                                     | версионированная политика deny-by-default                                                                                                       |
| `CONTROL_PLANE_APPLICATION_GRANT_TRUST_DIR`                                                                                                                                                                                               | независимо доставленные публичные JWK точных разрешений производителей                                                                          |
| `CONTROL_PLANE_PROOF_PRIVATE_JWK_FILE`, `CONTROL_PLANE_PROOF_TRUST_FILE`, `CONTROL_PLANE_PROOF_SIGNER_GENERATION`                                                                                                                         | независимо проверенный signer доказательств                                                                                                     |
| `CONTROL_PLANE_LEASE_SIGNING_KEY_FILE`                                                                                                                                                                                                    | HMAC-ключ аренды хода                                                                                                                           |
| `CONTROL_PLANE_OIDC_TLS_SERVER_NAME`, `CONTROL_PLANE_OIDC_CA_FILE`                                                                                                                                                                        | закреплённый TLS discovery/JWKS OIDC                                                                                                            |
| `POD_UID`                                                                                                                                                                                                                                 | владелец аренды ретранслятора                                                                                                                   |
| `CONTROL_PLANE_*_TIMEOUT`, `CONTROL_PLANE_*_INTERVAL`, `CONTROL_PLANE_CACHE_TTL`, `CONTROL_PLANE_SCHEDULE_CLAIM_LIMIT`                                                                                                                    | ограниченные пределы жизненного цикла                                                                                                           |
| `OTEL_*`, `SENTRY_DSN_FILE`, `SENTRY_EXPECTED_HOST`                                                                                                                                                                                       | общая среда наблюдаемости                                                                                                                       |

Файлы секретов должны быть абсолютными обычными файлами без разрешений для
`other`. DSN, JWK, учётные данные, ключи и их содержимое не логируются.

## Развёртывание и миграции

База находится в `deploy/k8s/base/control-plane`, наложения окружений — в
`deploy/k8s/overlays/{staging,production}/control-plane`. Канонический render
требует три независимых digest и node-reachable FQDN pull endpoint; смешивать
digest `control-plane` и среды агента запрещено:

```bash
tools/render-control-plane.sh \
  staging \
  sha256:<control-plane-image-digest> \
  sha256:<internal-rpc-authority-image-digest> \
  sha256:<agent-runtime-image-digest> \
  registry-pull.<environment-domain> \
  <approved-admission-tools-image>@sha256:<digest> \
  <approved-image-admission-image>@sha256:<digest> \
  <approved-vulnerability-policy-revision> \
  <approved-vulnerability-policy-sha256> \
  <forward-only-pull-credential-generation> \
  <exact-node-ipv4-cidr> \
  <exact-node-ipv6-cidr> \
  sha256:<trusted-role-base-digest> \
  <frontend-sha256> \
  <role-runtime-contract-revision> \
  <role-runtime-contract-sha256> \
  > /tmp/control-plane-staging.yaml
```

Команда только рендерит; она не применяет manifest. Для production нужно
заменить `staging` на `production` и использовать отдельно утверждённые digest.

Общая база `deploy/k8s/base/image-supply-chain` материализует локальный
OCI registry четырьмя независимыми Deployment/ServiceAccount/Vault CSI
границами: публичный node-reachable read-only pull, staging push без DELETE,
admin DELETE только staging-retention и отдельный promotion writer. Pull
монтирует только promoted PVC read-only и не имеет сети к остальным endpoints;
push/admin разделяют staging PVC, а promotion единолично пишет другой promoted
PVC. Компрометация pull поэтому не открывает push/delete, приватные ключи
внутренних endpoints или изменяемое хранилище. Kubelet получает отдельный
`dockerconfigjson`; DaemonSet `mattercodex-registry-node-pull-readback` на
каждом узле доказывает pull exact digest. Все прикладные образы в итоговом
render ссылаются на node endpoint по digest. Теги обязаны иметь вид
`vYYYYMMDDHHMMSS-<git-sha>`; задача
оставляет текущую и две предыдущие версии каждого репозитория `mattercodex/*`
и закрыто отказывается удалять неизвестный формат. Три начальных образа
(`registry`, `moby/buildkit`, `regctl`) закреплены публичными OCI digest;
после начальной загрузки оператор зеркалирует их в тот же локальный registry.
BuildKit API и внутренние registry endpoints требуют mTLS с exact SNI/CA:
server/probe, BuildKit→push, `role-image-builder`, scanner, signer, admission,
promotion и cleanup получают client-only ключи из разных
SecretProviderClass/Vault roles, а label NetworkPolicy не является
полномочием. Certificate guard сравнивает обслуживаемый leaf с ротированным
CSI leaf, проверяет hostname/CA/срок, перечитывает текущий
`dockerconfigjson` и теми же pull credentials выполняет exact digest manifest
readback через node FQDN; несовпадение htpasswd/dockerconfig generation,
digest или TLS снимает readiness и закрыто перезапускает только
registry process для перечитывания ключей. Forward-only pull credential
generation входит в Pod templates registry и node-readback DaemonSet: coherent
rotation обязательно создаёт новые Pod на каждом узле, а пропущенная либо
рассинхронная generation не получает readiness.
`services/jobs/role-image-builder` получает exact fenced attempt у
`control-plane`, потоково материализует exact OCI context/package/tool в
private `emptyDir`, отправляет сборку во внешний rootless BuildKit и не имеет
staging-push credential/egress. Staging push identity принадлежит BuildKit;
builder не монтирует signer, promotion, admin или node-pull identity. Отдельный
`tools/render-image-admission-job.sh` не принимает artifact IDs/digests от
caller: первая фаза получает server-owned claim, после чего scanner и signer
проверяют exact staging digest, labels и native BuildKit provenance. Admission
фиксирует SBOM/vulnerability/signature/receipt через protected RPC, а отдельный
promotion workload расходует one-time claim только после exact registry
readback. Rejected, stale или неполное evidence не становится пригодным.
Marker/PVC задают только порядок пяти фаз и не являются owner state. Immutable
intent фиксирует закрытый состав `base64`, `cmp`, `cosign`, `date`, `grype`,
`image-admission-bridge`, `jq`, `regctl`, `sha256sum`, `syft`; signer,
admission, promotion и admin credentials различны и доставляются Vault CSI без
значений в manifest. Rootless BuildKit сохраняет process sandbox, не получает
ServiceAccount token или прикладные owner secrets, API закрыт mTLS, state
ограничен `emptyDir`, а build client не получает admin DELETE. Отключать
TLS/auth или использовать internal Service DNS как kubelet pull host запрещено.

Варианты сборщика Kubernetes сверены с официальными источниками: standalone
BuildKit, Shipwright Build/BuildRun и Tekton Tasks. В соответствии с
`ADR-MC-008` выбран прямой BuildKit как минимальный авторитетный backend;
Shipwright и Tekton остаются возможными оркестраторами поверх него, но не
создают второй источник истины. Старый Kaniko template сохранён только для
legacy-контура и не включён в новую базу.

Supply-chain и build Job имеют отдельные fail-closed render interfaces:

```bash
tools/render-image-supply-chain.sh \
  staging \
  sha256:<control-plane-image-digest> \
  sha256:<internal-rpc-authority-image-digest> \
  registry-pull.<environment-domain> \
  <approved-admission-tools-image>@sha256:<digest> \
  <approved-image-admission-image>@sha256:<digest> \
  <approved-vulnerability-policy-revision> \
  <approved-vulnerability-policy-sha256> \
  <forward-only-pull-credential-generation> \
  <exact-node-ipv4-cidr> \
  <exact-node-ipv6-cidr> \
  sha256:<trusted-role-base-digest> \
  <frontend-sha256> \
  <role-runtime-contract-revision> \
  <role-runtime-contract-sha256> \
  > /tmp/image-supply-chain-staging.yaml

tools/render-image-admission-job.sh \
  staging \
  v<UTC-YYYYMMDDHHMMSS>-<exact-git-sha> \
  > /tmp/role-image-admission.yaml
```

Команды только материализуют YAML. Apply, сборка и promotion требуют
отдельного разрешения владельца; после них обязательны exact digest readback
admission receipt/promotion endpoint и готовность node-pull DaemonSet.

Migration Job запускает `control-plane-cli migrate expand` до rollout и
атомарно согласует `CURRENT`/`NEXT`/`PREVIOUS`, активный ключ контекста и
выведенные из эксплуатации сессии. Для каждого объявленного поколения GitOps
доставляет отдельный DSN-файл: CLI создаёт exact LOGIN через owner-only
controller bootstrap и закрыто сверяет catalog membership. При наличии `NEXT`
CLI дополнительно подключается именно этим LOGIN и сохраняет readback; только
следующее идемпотентное согласование может повысить его до `CURRENT`. Миграции
`20260731000200`, `20260731000300`, `20260731000400` и
`20260731000500`, `20260731000600`, `20260801000100`,
`20260802000100`, `20260803000100` и `20260806023400` явно forward-only. Уже применённая
`20260731000500` не переписывается; `20260803000100` обновляет
`work_claim_graph_is_active` через `CREATE OR REPLACE FUNCTION`, закрепляет
database-expiry predicate и privilege/readback. Downgrade отклоняется,
потому что потерял бы RLS fences, верхнюю границу и readback principal,
попытки, подтверждения и происхождение вектора. Миграция `20260806023400` ещё
не merged и поэтому исправляется атомарно только внутри PR #239; применять её
вне owner-approved migration process запрещено. После первого применения она
так же неизменяема, а исправление выполняется новой forward migration. Откат приложения выполняется
только совместимым образом; откат схемы — новой компенсирующей forward
миграцией.

Поток JetStream и учётные данные Vault database/static принадлежат окружению.
Их точный контракт проверяется стартовым барьером; сервис не создаёт и не
ослабляет ресурсы брокера или Vault. RBAC Role/RoleBinding намеренно
отсутствуют: контейнеры приложения и миграции не обращаются к Kubernetes API;
доставку CSI выполняет драйвер окружения.

## Ручная приёмка

Без deploy можно:

1. собрать оба бинарных файла и публичные модули клиента и API;
2. выполнить `buf build` и проверить воспроизводимую генерацию кода;
3. проверить разбор YAML/JSON и канонический render с тремя тестовыми
   ненулевыми digest и тестовым node-reachable FQDN;
4. убедиться, что render содержит рабочую нагрузку non-root/read-only,
   Migration Job, deny-all и только NetworkPolicy с точными назначениями;
5. сравнить все методы Proto с политикой полномочий, а группы ошибок —
   с `contracts/errors/v1/rpc-http-mapping.yaml`;
6. для Issue #221 пройти negative/competition/replay сценарии из
   `runtime-continuation-contract.md` и сверить все перечисленные full methods,
   operation IDs, permissions и producer profiles;
7. проверить, что `Closes #187` или `Closes #221` относится только к своему PR.

Фактические проверки PostgreSQL/Redis/NATS/Vault/Kubernetes и staging rollout
требуют отдельного разрешения и окружения.

## Политика прототипа и ограничения

Активен профиль `Prototype`: полное покрытие, integration/E2E,
contract/deploy/render/lifecycle/oracle suites и полный baseline не входят в
этот PR. Поддерживаемая волна тестирования отслеживается в
[Issue #216](https://github.com/codex-k8s/matter-codex/issues/216).

Не входят в компонент: внешний OpenAPI/HTTP gateway, согласование среды
исполнения, выполнение автоматизаций, процессы Mattermost/MCP/Codex, хранение
байтов артефактов и значения секретов.

Эксплуатация и восстановление описаны в
[`docs/runbooks/control-plane.md`](../../../docs/runbooks/control-plane.md).

## Проверенные внешние источники

Context7 был вызван для PostgreSQL, pgx, goose, gRPC/Protobuf, Redis, NATS,
OpenTelemetry, Sentry, Kubernetes и Vault, но вернул quota error. Использован
резервный путь к официальной первичной документации:

- [PostgreSQL row security](https://www.postgresql.org/docs/current/ddl-rowsecurity.html),
  [transaction isolation](https://www.postgresql.org/docs/current/transaction-iso.html)
  и [full-text search](https://www.postgresql.org/docs/current/textsearch.html);
- [pgx](https://pkg.go.dev/github.com/jackc/pgx/v5) и
  [goose](https://github.com/pressly/goose);
- [gRPC Go](https://grpc.io/docs/languages/go/) и
  [Protocol Buffers](https://protobuf.dev/);
- [Redis Go client](https://redis.io/docs/latest/develop/clients/go/) и
  [NATS JetStream](https://docs.nats.io/nats-concepts/jetstream);
- [pgvector](https://github.com/pgvector/pgvector);
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/),
  [Sentry Go](https://docs.sentry.io/platforms/go/),
  [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/),
  [Kustomize](https://kubectl.docs.kubernetes.io/references/kustomize/) и
  [Secrets Store CSI Driver](https://secrets-store-csi-driver.sigs.k8s.io/);
- [BuildKit](https://github.com/moby/buildkit),
  [Distribution registry](https://distribution.github.io/distribution/),
  [regctl](https://regclient.org/usage/regctl/),
  [Vault CSI provider](https://developer.hashicorp.com/vault/docs/platform/k8s/csi/configurations),
  [Vault PKI issue API](https://developer.hashicorp.com/vault/api-docs/secret/pki#generate-certificate-and-key),
  [Shipwright Build](https://shipwright.io/docs/build/) и
  [Tekton Tasks](https://tekton.dev/docs/pipelines/tasks/).

Для Issue #221 Context7 повторно подтвердил API `/jackc/pgx` v5.10.0,
`/pressly/goose` v3.27.3, `/grpc/grpc-go` v1.82.1, `/bufbuild/buf` и
`/protocolbuffers/protobuf-go`: транзакции и `Serializable`, forward-only
migrations, gRPC full method/status/interceptor, `buf format|lint|build|generate`
и совместимость generated Go API с runtime. Использованы закреплённые
репозиторием версии инструментов.
