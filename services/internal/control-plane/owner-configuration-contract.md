---
id: SVC-MC-014
title: Контракт owner-конфигурации и полного жизненного цикла control-plane
type: service-contract
status: approved
owner: developer
version: 1.3.1
updated: 2026-08-07
---

# Контракт owner-конфигурации и полного жизненного цикла control-plane

Документ фиксирует реализацию Issue
[#234](https://github.com/codex-k8s/matter-codex/issues/234) и дополняет
`SVC-MC-004`. Он не меняет уже исполняемые Project, Schedule, OwnerGate и
SESSION restore paths Issues #187 и #231. Внешний owner HTTP/PWA mapping
принадлежит Issues #237/#194. Этот unit материализует только принимающий
control-plane contract для Mattermost effect/readback; provider Team/bot
effect, signer receipt и generated call site остаются обязательным producer
scope Issue #235 после rebase на #234. Аналогично Git receipt verifier и exact
RPC profile здесь готовы как принимающая граница, но реальный Git producer,
signer и call site принадлежат Issue #236. До принятия соответствующего unit
ни один из этих двух end-to-end путей не считается готовым.

## Закрытый реестр защищённых видов

Новые authority-bearing виды изменяются только специализированными командами:

| Вид | Разрешённые команды | Авторитетный read path |
| --- | --- | --- |
| `ROLE_DEFINITION` | UI create/update, Git reconcile, archive, delete | typed get/list/history |
| `AGENT` | UI create/update, Git reconcile, pause/resume, enable/disable, bot bind/rebind/revoke, archive, delete | typed get/list/history с masked bot identity |
| `AGENT_ASSIGNMENT` | assign, unassign | typed get/list/history |
| `INSTRUCTION_SET` | UI create/update, Git reconcile, validate, publish, rollback, detach, copy, archive, delete | typed get/list/history/compare с typed validation result |
| `PROVIDER_CONNECTION_REFERENCE` | register, refresh, archive | typed get/list/history с masked metadata |
| `PROVIDER_POOL` | UI create/update, Git reconcile, archive, delete | typed get/list/history |
| `SCHEDULE` binding | bind/rebind owner-friendly selection | существующие typed Schedule read/occurrence paths |
| `PROCESS_RUN` graph | cancel, retry | run detail/timeline/lineage/artifact read |
| `RUNTIME_INCIDENT` | acknowledge, retry, release, close | typed incident list/get/history |
| `WORKSPACE_BACKUP` | owner create/cancel/retry; internal complete/fail/expire | typed backup list/get |
| `WORKSPACE_RESTORE` | owner create/cancel/retry; internal complete/fail/expire | typed restore list/get |
| `WORKSPACE_MATTERMOST_MAPPING` | bind, relink, unlink | typed get/list |

`CreateResource`, `UpdateResource`, `TransitionResource`, `DeleteResource` и
`ManageAccessResource`, включая общие detach/copy, закрыто отклоняют эти виды.
Legacy `ROLE` и
`PROMPT_PROFILE` не принимают новые записи и изменения. Forward-only migration
атомарно создаёт deterministic per-Role cutover mapping со stable target IDs,
source versions/digests и typed `BLOCKED` action. Поскольку legacy хранит лишь
digest Instruction, owner вызывает `ResolveLegacyConfigurationCutover` с exact
immutable content; сервер сверяет digest и под одними locks материализует
RoleDefinition/Agent/InstructionSet/ProviderReference/ProviderPool/
AgentAssignment либо не создаёт ничего. Неоднозначный или неполный source
остаётся `BLOCKED` с точным readback/manual action. Старые строки остаются
version-pinned входами уже существующего графа сессий #187. Новая
owner-конфигурация не пишет legacy-виды. Для существующего `runtime-controller` control-plane создаёт
отдельную immutable derived `ROLE`/`PROMPT_PROFILE` projection, которую может
прочитать только exact versioned runtime-controller path; она не находится в
authoritative resource table и не возвращает legacy mutation authority.
Активный `AgentAssignment` является обязательным owner gate при
создании новой сессии, а создаваемый `RuntimeRevision` закрепляет `Agent`,
`RoleDefinition`, опубликованную версию `InstructionSet` и `ProviderPool`.
Двух изменяемых источников истины нет.

## Сквозные карты сценариев

Во всех строках actor, organization, project, owner и root lineage поступают
из проверенного transport/application proof. Идентификаторы из payload нужны
только для повторного разрешения объекта внутри уже доказанной owner boundary.
OCC и receipt проверяются после блокировки owner/current graph.

| Requirement и actor/authority | Будущий endpoint consumer | Generated RPC/command | Owner resolver и OCC/idempotency | Одна PostgreSQL transaction и результат | Consumer/readiness/deploy ownership |
| --- | --- | --- | --- | --- | --- |
| `DOM-MC-003`, owner UI или exact Git reconciler | control-api-gateway #237 только для UI; integration-gateway #236 для Git | `ManageRoleDefinition` либо `ReconcileGitRoleDefinition` | project из authority; UI всегда назначает `managed_by=UI`; Git source/revision/digest/target/intent берутся из подписанного exact receipt, existing row `FOR UPDATE`, OCC и receipt JTI consume | state + protected history snapshot + command receipt + audit; нового события нет | owner может читать drift/detach/copy, но не выпускать Git provenance; Git producer readiness ожидает #236 |
| `DOM-MC-003`, owner UI или exact Git reconciler | control-api-gateway #237 либо integration-gateway #236 | `ManageAgent`, `ReconcileGitAgent`; отдельные state actions | Role/Instruction/Pool и active server-owned `RoleImageRecipe` runtime profile разрешаются по stable key, закрепляются ID/version/digest; Git target/intent доказывает receipt | Agent + pins + state/history + receipt + audit; pause/disable запрещают новый runtime, resume/enable повторно проверяют eligibility | runtime получает только version-pinned `RuntimeRevision`; Git producer readiness ожидает #236 |
| `DOM-MC-003/011`, interaction-gateway provider readback profile | pending producer #235 после rebase | `ManageAgentMattermostBotIdentity` bind/rebind/revoke | exact signed receipt связывает issuer/purpose/workload/SPIFFE/full method/actor/org/project/workspace/team/Agent target/action/effect/version/generation/digest/expiry/JTI; team и bot ref выводятся из receipt | owner transaction one-use consume issuer+purpose+JTI+target+intent вместе с masked state/history/command receipt/audit и bounded immutable result snapshot; exact replay возвращает прежнюю версию даже после последующей mutation | #234 предоставляет receive contract/profile/client operation; bot effect, signer и call site не готовы до #235 |
| `DOM-MC-003/004`, owner | control-api-gateway #237 | `ManageAgentAssignment` assign/unassign | product Workspace — server-resolved Project без Git checkout; Agent и optional Room разрешаются внутри project; owner/root назначает сервер | assignment state + exact Agent/Workspace versions/digests + history + receipt + audit | новая Session и каждая `NEW`/`PERSISTENT`/`ROLLING` Schedule materialization повторно lock/resolve active assignment; stale/revoked fail closed |
| `DOM-MC-002/003`, owner UI или exact Git reconciler | control-api-gateway #237 либо integration-gateway #236 | `ManageInstructionSet`, `ReconcileGitInstructionSet`, `CompareInstructionSetVersions` | UI update не меняет Git-owned set; Git reconcile сверяет signed source/revision/digest/target/intent; detach очищает source binding, copy создаёт новый UI set; validate вычисляет digest/verdict/errors из locked immutable content | exact content сначала получает immutable versioned S3 object, затем owner transaction создаёт CLEAN Artifact + set version + validation/history/receipt/audit; publish только после successful validation той же version/digest | derived Prompt pin-ит Artifact ID/version/digest; `ClaimRuntimeExecution` читает тот же projection и материализует `AGENTS.md`; Git producer ожидает #236 |
| `DOM-MC-003`, integration-gateway provider readback profile | integration-gateway #236 | `ManageProviderConnectionReference` | exact typed receipt связывает protected target stable key и command intent; object/binding/version/generation/digest выводятся из proof | one-use issuer+purpose+JTI consume вместе с metadata/history/receipt/audit; credential values отсутствуют | provider effect/catalog/readback producer и signer принадлежат #236; #234 readiness проверяет только registered receive trust |
| `DOM-MC-003`, owner UI или exact Git reconciler | control-api-gateway #237 либо integration-gateway #236 | `ManageProviderPool` или `ReconcileGitProviderPool` | refs разрешаются под lock; signed Git receipt назначает Git ownership; eligibility, weights, observation revision/time и digest копируются в immutable snapshot | pool + snapshot digest + history + receipt + audit; нового события нет | runtime получает только server-resolved pinned pool и exact credential binding; Git producer ожидает #236 |
| `DOM-MC-005`, owner | control-api-gateway #237 | `CreateScheduleFromOwnerSelections` или update-only `BindScheduleConfiguration` | create принимает stable Agent/Instruction/Pool/Room и display name Artifact; сервер lock-resolve Workspace, RuntimeProfile и active assignment, назначает Schedule ID/version и exact tuple; каждый Session reuse/archive/create сначала получает общий с `Insert` project graph fence и перечитывает conversation candidates `FOR UPDATE` | Schedule + exact assignment/config versions/digests + effective input + receipt+audit одной transaction; partial unique index допускает immutable `ARCHIVED` history, но не более одной live/resumable Session на conversation; `NEW` очищает binding, `PERSISTENT/ROLLING` сохраняет единственную совместимую Session либо создаёт replacement; T1→T2→T1 не оставляет два admission-active tuple | automation-scheduler при каждой materialization повторно проверяет тот же assignment tuple |
| `DOM-MC-002/004/005`, owner | control-api-gateway #237 | `GetRunDetail`, `ListRunTimeline`, `GetRunLineage`, `ListRunArtifacts`, `ManageRun` cancel/retry | requested Process разрешается до graph query; recursive root/descendants и все attempts читаются в одной owner/RLS boundary; stable cursor `(occurred_at,id)` | cancel закрывает весь graph; retry из failed/expired/cancelled создаёт fresh Turn/RuntimeRevision/grants и сохраняет predecessor | lineage возвращает root, parent/child и predecessor/successor edges; timeline/artifacts охватывают все attempts без UUID pagination |
| `GUIDE-DOC-006`, owner или project operator с exact permission | control-api-gateway #237 | `ManageRuntimeIncident` | authoritative execution→project eligibility общая для get/list/history/actions; hidden cross-tenant; incident и полный graph lock | retry использует graph helper; release атомарно переводит execution и весь runtime graph в `CANCELLED`, отзывает leases/grants/claims и возвращает released readback | watchdog только создаёт evidence; broad project grant и creator-only gate отсутствуют |
| `DOM-MC-009`, owner | control-api-gateway #237 | owner `ManageWorkspaceBackup` create/cancel/retry; internal reconciliation без RPC | Workspace — Project aggregate; snapshot перечисляет все исторические owner Session независимо от current assignment/Workspace version и требует exact terminal archive каждой; ALL_WORKSPACES фиксируется под sorted locks | отсутствующий archive откатывает весь create; owner action либо internal complete/fail/expire коммитит один immutable envelope, membership, generation/revoke watermark, receipt+audit | bounded in-process recovery reconciler использует тот же membership на retry/restore; partial AVAILABLE невозможен |
| `DOM-MC-009`, owner | control-api-gateway #237 | owner `ManageWorkspaceRestore` create/cancel/retry; internal reconciliation без RPC | exact backup/membership locks; retry только terminal predecessor; each member переключает RLS project scope только через server transaction primitive | fresh attempt/generation, RuntimeRevision и grants для каждого member либо rollback; internal complete/fail/expire запрещает partial terminal | runtime-controller materializes существующий restore effect #231; in-process reconciler владеет terminal decision/readiness |
| `DOM-MC-011`, interaction-gateway provider readback profile | pending producer #235 после rebase | `ManageWorkspaceMattermostMapping` bind/relink/unlink | Project является Workspace; signed receipt связывает exact mapping target/stable key/intent, team ref и effect; mapping generation monotonic; graph lock проверяет Workspace→Chat→Session→Turn/delivery | one-use JTI consume + mapping/revoke watermark/receipt/audit одной transaction; open graph relink/unlink отклоняется | #234 владеет receive contract/state/readback; #235 обязан добавить Team effect/readback signer и generated call site до end-to-end readiness |

Для update-path Agent bot, Workspace mapping, ProviderConnectionReference и
четырёх `ReconcileGit*` завершённый semantic receipt проверяется после
owner-locked source, но до generic command-key receipt и до object-store effect.
Exact issuer/purpose/JTI/target/stable key/action/effect/generation/digests/
intent/authority возвращает сохранённый immutable result snapshot. Любое
расхождение или незавершённая reservation закрыто конфликтует; отсутствие
semantic consumption только разрешает обычной owner transaction выполнить
первую mutation.

## Граф исполнения

```text
Organization authority
  -> Project
     -> Workspace
        -> AgentAssignment
           -> Agent
              -> RoleDefinition
              -> published InstructionSetVersion
              -> ProviderPool snapshot
              -> Runtime selection
        -> Schedule binding
           -> ScheduleOccurrence
              -> ScheduledRun
                 -> Session
                    -> Turn + TurnAttempt + TurnLease
                       -> ProcessRun
                          -> RuntimeRevision
                             -> RuntimeExecution + grant/claim/lease
                                -> Artifact / RuntimeIncident
     -> WorkspaceBackup immutable membership
        -> bounded control-plane recovery reconciler
        -> WorkspaceRestoreAttempt generation
           -> fresh RuntimeRevision/Turn/attempt/grant per member
     -> WorkspaceMattermostMapping generation
        <- signed interaction-gateway provider readback receipt pending #235
```

Любой cancel, retry, terminal или expiry получает весь существующий граф в
этом порядке (несколько графов — по отсортированным ID). Одна transaction
закрывает predecessor leases, grants, claims, attempts и связанные агрегаты.
Нельзя сохранить terminal backup/restore envelope, если хотя бы один member
остался nonterminal или сохранил authority.

## Межunit readiness gates

Для #235 обязательна последовательность: interaction-gateway разрешает или
создаёт Team/bot provider object → выполняет exact effect → подписывает receipt
с `iss/purpose/workload_id/caller_spiffe_id/full_method/actor_id/
organization_id/project_id/workspace_id/team_ref/target_kind/
target_resource_id|target_stable_key/action/effect/effect_version/
effect_generation/effect_sha256/command_intent_sha256/iat/nbf/exp/jti` →
получает authority proof → вызывает generated bot либо mapping RPC → читает
exact returned version/generation. Rebase #235 на принятый #234 и readiness
этого рабочего call path — merge gate; текущий #238 этого producer contract
не содержит.

Для #236 обязательна аналогичная последовательность Git fetch/readback →
exact immutable source/revision/digest и target intent → signed
`GitReconciliationReceipt` → authority proof → один из четырёх generated
`ReconcileGit*` RPC → version-pinned readback. Owner OIDC может читать drift,
detach/copy, но не подписывать Git provenance. Receive-side JWK/profile в #234
не подменяет producer readiness #236.

## Lifecycle и authority matrix

| Вид/переход | Допустимый predecessor | Authority и дополнительные проверки | Atomic successor / revoke | Event либо authoritative read |
| --- | --- | --- | --- | --- |
| Legacy configuration cutover | deterministic `BLOCKED` map | owner exact source versions/digests; immutable Instruction content SHA; active provider/runtime/workspace eligibility | весь target catalog + history/receipt/audit и `MIGRATED` одной transaction либо полный rollback | typed get/list cutover read с block code/manual action |
| RoleDefinition create | отсутствует | owner manage; stable key unique | ACTIVE v1 + history/receipt/audit | typed version-pinned read |
| RoleDefinition update | ACTIVE/PAUSED | row lock, OCC; referenced active agents revalidated | v+1 + immutable history | typed version-pinned read |
| RoleDefinition Git reconcile | absent/GIT-owned current | exact Git command profile; immutable source/revision/digest | ACTIVE v1 либо v+1; caller не выбирает ownership | typed version-pinned read |
| RoleDefinition archive | ACTIVE/PAUSED | нет active assignment/runtime pin | ARCHIVED; новые grants невозможны | typed version-pinned read |
| RoleDefinition delete | ARCHIVED | нет live reference; owner lock | DELETED+tombstone | typed version-pinned read |
| Agent create | отсутствует | server-resolved exact Role/Instruction/Pool/Runtime profile | ACTIVE v1 + pins | typed version-pinned read |
| Agent update | ACTIVE/PAUSED | lock Agent then sorted pinned resources, OCC; UI cannot set Git ownership or bot fields | v+1; future revision only | typed version-pinned read |
| Agent Git reconcile | absent/GIT-owned current | exact Git command profile and source/revision/digest | ACTIVE v1 либо v+1 with server-resolved pins | typed version-pinned read |
| Agent pause/resume | ACTIVE/PAUSED | exact owner permission; existing pinned graph не переписывается | PAUSED/ACTIVE; paused запрещает новый runtime, resume заставляет следующий create/materialization заново разрешить dependencies | typed state read |
| Agent disable/enable | ACTIVE/PAUSED | exact owner permission; existing pinned graph не переписывается | disabled/enabled revision; disable отзывает eligibility нового runtime, enable не обходит повторный dependency resolution | typed state read |
| Agent bot bind/rebind | ACTIVE/PAUSED | exact interaction-gateway typed receipt; receipt team/action/effect/generation match | server-owned ref/username/revision/masked status; old generation revoked | typed masked read |
| Agent bot revoke | ACTIVE/PAUSED | exact revoke receipt and current generation | server ref сохраняется только как masked `REVOKED` readback, receipt watermark advanced | typed masked read |
| Agent archive | ACTIVE/PAUSED | no active assignment/schedule/open run | ARCHIVED; live authority отсутствует | typed version-pinned read |
| Agent delete | ARCHIVED | no live references | DELETED+tombstone | typed version-pinned read |
| AgentAssignment assign | отсутствует | server-resolved workspace+Agent; actor authority не назначается из payload | ACTIVE v1; server owner/root | typed version-pinned read |
| AgentAssignment unassign | ACTIVE | exact assignment owner lock/OCC | ARCHIVED; новые сессии отклоняются, уже созданный version-pinned graph не переписывается | typed version-pinned read |
| Instruction create | absent | UI command; caller не может назначить Git ownership | DRAFT set + version 1 | typed version-pinned read; нового события нет |
| Instruction update | DRAFT current, UI-owned | Git-owned ordinary update forbidden | fresh immutable DRAFT version | typed history; no event |
| Instruction Git reconcile | absent/GIT-owned current | exact Git command and source/revision/digest | fresh immutable DRAFT version | typed history; no event |
| Instruction validate | DRAFT | server читает exact immutable content; caller verdict/digest/errors отсутствуют | server-computed digest, verdict и typed errors | typed history; no event |
| Instruction publish | VALIDATED | set+version lock/OCC | prior published retained, effective version pinned | typed version-pinned read |
| Instruction rollback | prior PUBLISHED | exact target version/digest | fresh published rollback version; no mutation of old row | typed version-pinned read |
| Instruction detach | Git-owned | exact source revision/digest; no content in request | same content fresh UI-owned version | typed version-pinned read |
| Instruction copy | Git-owned | exact source lock; server copies content | new UI-owned set/version | typed version-pinned read |
| Instruction archive/delete | no active pin / ARCHIVED | full reference check | ARCHIVED / DELETED+tombstone | typed version-pinned read |
| Provider reference register/refresh | absent / ACTIVE | exact #236 integration workload, signed target/intent receipt и one-use JTI; no secret value | server ref, monotonic receipt/version/digest | typed version-pinned read; нового события нет |
| Provider reference archive | ACTIVE/INELIGIBLE | no active pool/runtime pin | ARCHIVED | typed masked read |
| ProviderPool create/update | absent / ACTIVE | refs locked, observations fresh, weights bounded | immutable eligibility snapshot + digest | typed version-pinned read |
| ProviderPool Git reconcile | absent/GIT-owned current | exact Git profile/source/revision/digest; refs server-resolved | immutable eligibility snapshot + digest | typed version-pinned read |
| ProviderPool archive/delete | no active Agent/runtime pin / ARCHIVED | full reference check | ARCHIVED / DELETED | typed version-pinned read |
| Schedule bind/rebind | active Schedule | existing graph closed; exact stable selections, active Workspace/Room assignment и прежняя Session locked; unsafe cleanup отклоняется | aggregate receives pinned assignment/Agent/Instruction/Runtime tuple; `NEW` detaches, persistent/rolling reuses only one exact-compatible Session or archives the prior Session before replacement | existing Schedule read/polling, no event |
| Schedule materialization | active binding | `NEW`, `PERSISTENT` и `ROLLING` path повторно разрешают active assignment/version/digest | stale/revoked assignment fail closed before run creation | existing polling/readback |
| Run cancel | current nonterminal | owner permission; full graph/fence | CANCELLED everywhere; leases/grants/claims revoked | typed run read |
| Run retry | FAILED/EXPIRED/CANCELLED eligible | exact predecessor/version and policy | RETRIED predecessor + fresh attempt с immutable RuntimeRevision ID/version + grants | typed lineage/read от TurnAttempt, включая pre-admission predecessor |
| Run terminal complete | current live attempt | exact executor lease/fence | entire graph terminal | existing runtime read |
| Run partial failure | любой member transition failed | неприменимо как successor | transaction rollback; no partial envelope | previous version read |
| Incident acknowledge | OPEN | owner/operator permission, exact version | ACKNOWLEDGED | typed incident history |
| Incident retry | OPEN/ACKNOWLEDGED при FAILED/EXPIRED execution; RELEASED при CANCELLED execution | exact current execution и monotonic fence lower bound; future/stale incident и не-current graph отклоняются | RETRYING и fresh run attempt atomically; post-effect `ExecutionFence` сохраняется в incident, history и receipt той же версии; иной runtime state даёт conflict без success | run+incident read |
| Incident release | ACKNOWLEDGED | owner/operator permission; exact execution and full graph lock | execution/Turn/Process/Schedule graph `CANCELLED`, grants/leases/claims revoked; incident `RELEASED` | incident history + released execution readback |
| Incident close | ACKNOWLEDGED/RELEASED/RETRYING | incident execution terminal; successor не скрывает predecessor lineage | CLOSED | typed incident history |
| Backup create | absent | WORKSPACE/ALL derived owner scope; отдельный locked non-deleted historical Session query, all members archive-verified | RUNNING/VERIFYING immutable membership/version/digest либо полный rollback | typed backup read |
| Backup internal complete/fail | current candidate | exact in-process reconciler principal, immutable membership/version/digest | full AVAILABLE/UNAVAILABLE envelope; partial=false | typed backup read |
| Backup cancel | VERIFYING/RETENTION_PENDING | exact version | CANCELLED full envelope | typed backup read |
| Backup retry | UNAVAILABLE/FAILED/EXPIRED | exact version/digest | new attempt+generation, predecessor revoked | typed backup read |
| Backup expiry/dead-letter | current candidate / retry exhausted | in-process reconciler, PostgreSQL time, exact generation | EXPIRED/UNAVAILABLE full envelope; authority revoked | typed backup read |
| Restore create | AVAILABLE backup | exact membership/version/digest | RUNNING/QUEUED fresh attempt/RuntimeRevision/grants for all members | typed restore read |
| Restore cancel | nonterminal | full member graph | CANCELLED all members; watermark advanced | typed restore read |
| Restore retry | FAILED/EXPIRED/CANCELLED | predecessor fully terminal | fresh attempt/grants; generation+1 | typed restore read |
| Restore internal complete/fail | current attempt | exact in-process reconciler; every member terminal | SUCCEEDED/FAILED whole envelope; partial=false | typed restore read |
| Restore expiry/dead-letter | deadline/retry exhausted | in-process reconciler, PostgreSQL time | EXPIRED/FAILED, all authority revoked | typed restore read |
| Mapping bind | absent | interaction-gateway exact typed receipt; Project owner | BOUND mapping generation 1 + exact provider effect version/generation | typed mapping read; no event |
| Mapping relink | BOUND | provider effect version/generation строго монотонны; no open Chat/Session/Turn/delivery graph | BOUND mapping generation+1, old receipt/effect generation revoked | typed mapping read; no event |
| Mapping unlink | BOUND | expected mapping version/generation и monotonic provider effect receipt; no open graph | UNLINKED mapping generation+1, revoke watermark advanced | typed mapping read; no event |

## События, порядок и read/rejoin

Новые protected aggregates не публикуются в
`control_plane.runtime_configuration_changed`: существующий закрытый consumer
`runtime-controller` не принимает эти виды, а ложный consumer и dead-letter
запрещены. Авторитетный результат mutation доступен через typed protected
get/list/history/compare с exact version. При запуске control-plane атомарно
создаёт `RuntimeRevision`, закрепляющую exact Agent/RoleDefinition/
InstructionSetVersion/ProviderPool snapshot и server-resolved credential
binding. В той же transaction он записывает отдельные immutable derived
`ROLE`/`PROMPT_PROFILE` rows для существующего exact runtime-controller read;
общие create/update/detach/copy этих legacy kinds запрещены. Уже поддерживаемое событие
`RUNTIME_REVISION` сохраняет прежние `origin`, condition, cardinality,
ordering и durable consumer path. Ни content, ни provider receipt, ни secret
metadata в событие не входят.

Schedule binding, run, incident, backup/restore и mapping не получают новых
AsyncAPI consumers. Их результат доступен через перечисленные typed RPC;
readiness control-plane использует тот же protected gRPC listener и тот же
authority resolver, что рабочие вызовы. Backup/restore terminal decision не
является browser RPC: bounded in-process reconciler читает кандидата из
PostgreSQL, создаёт server-owned principal и выполняет одну owner transaction.

## Ownership развёртывания

`services/internal/control-plane` и `deploy/k8s/base/control-plane` владеют
RPC, migration, PostgreSQL/RLS, cache invalidation, immutable Instruction S3
writer/readiness с exact TLS SNI/CA/mTLS/application credential и bucket destination;
readiness до каждого нового Put удерживает PostgreSQL transaction-scoped
cross-replica fence на выделенной connection, через `ListObjectVersions` bounded удаляет и
повторно проверяет все versions/delete markers выделенного deterministic prefix,
затем выполняет versioned write, version-pinned stat/read и exact version cleanup;
ambiguous commit и превышение bounds закрывают readiness на любой replica; crash
освобождает fence вместе с connection, а следующий probe сначала продолжает
bounded orphan recovery. Unit также владеет outbox relay,
in-process recovery reconciler, business metrics, dashboard и alerts.
`runbook_url` каждого alert — абсолютный
HTTPS URL. NetworkPolicy не открывает Mattermost/provider egress: mapping
команда принимает только проверенный receipt, а provider Team/bot effect,
signer и вызов control-plane остаются pending scope #235. Git signer/call site
аналогично остаётся pending scope #236. Отдельный deployable worker не добавлен: reconciler принадлежит
cancel/join lifecycle самого control-plane и его отказ закрывает readiness.
