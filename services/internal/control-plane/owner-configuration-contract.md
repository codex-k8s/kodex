---
id: SVC-MC-005
title: Контракт owner-конфигурации и полного жизненного цикла control-plane
type: service-contract
status: approved
owner: developer
version: 1.0.0
updated: 2026-08-06
---

# Контракт owner-конфигурации и полного жизненного цикла control-plane

Документ фиксирует реализацию Issue
[#234](https://github.com/codex-k8s/matter-codex/issues/234) и дополняет
`SVC-MC-004`. Он не меняет уже исполняемые Project, Schedule, OwnerGate и
SESSION restore paths Issues #187 и #231. Внешний owner HTTP/PWA mapping
принадлежит Issues #237/#194, а provider Team effect, catalog и подписанный
readback receipt — Issue #235.

## Закрытый реестр защищённых видов

Новые authority-bearing виды изменяются только специализированными командами:

| Вид | Разрешённые команды | Авторитетный read path |
| --- | --- | --- |
| `ROLE_DEFINITION` | create, update, archive, delete | typed get/list/history |
| `AGENT` | create, update, archive, delete | typed get/list/history |
| `AGENT_ASSIGNMENT` | assign, unassign | typed get/list/history |
| `INSTRUCTION_SET` | create, update, validate, publish, rollback, detach, copy, archive, delete | typed get/list/history/compare |
| `PROVIDER_CONNECTION_REFERENCE` | register, refresh, archive | typed get/list/history с masked metadata |
| `PROVIDER_POOL` | create, update, archive, delete | typed get/list/history |
| `SCHEDULE` binding | bind/rebind owner-friendly selection | существующие typed Schedule read/occurrence paths |
| `PROCESS_RUN` graph | cancel, retry | run detail/timeline/lineage/artifact read |
| `RUNTIME_INCIDENT` | acknowledge, retry, release, close | typed incident list/get/history |
| `WORKSPACE_BACKUP` | create, cancel, retry, terminal, expire | typed backup list/get |
| `WORKSPACE_RESTORE` | create, cancel, retry, terminal, expire | typed restore list/get |
| `WORKSPACE_MATTERMOST_MAPPING` | bind, relink, unlink | typed get/list |

`CreateResource`, `UpdateResource`, `TransitionResource`, `DeleteResource` и
`ManageAccessResource` закрыто отклоняют эти виды. Legacy `ROLE` и
`PROMPT_PROFILE` не принимают новые записи и изменения: forward-only migration
создаёт целевые агрегаты и оставляет старые строки неизменяемыми, version-pinned
входами уже существующего графа сессий #187. Новая owner-конфигурация не пишет
legacy-виды. Активный `AgentAssignment` является обязательным owner gate при
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
| `DOM-MC-003`, owner с `controlplane.role_definition.manage` | control-api-gateway #237 | `ManageRoleDefinition` с закрытым action | project из authority; existing row `FOR UPDATE`; expected version; key+semantic hash | state + protected history snapshot + receipt + audit; нового события нет | version-pinned typed readback; следующий `RuntimeRevision` материализует выбранную версию |
| `DOM-MC-003`, owner с `controlplane.agent.manage` | control-api-gateway #237 | `ManageAgent` | RoleDefinition/InstructionSet/ProviderPool разрешаются сервером по stable key и закрепляются ID/version/digest; затем Agent lock/OCC | Agent + pinned references + history + receipt + audit; нового события нет | control-plane владеет RPC; runtime получает только version-pinned RuntimeRevision |
| `DOM-MC-003/004`, owner с `controlplane.agent_assignment.manage` | control-api-gateway #237 | `ManageAgentAssignment` assign/unassign | workspace и Agent разрешаются внутри project; сервер назначает owner/root; exact assignment lock/OCC | assignment state + history + receipt + audit; нового события нет | создание новой сессии повторно разрешает active assignment; уже созданный version-pinned graph не переписывается |
| `DOM-MC-002/003`, owner или Git reconciler с точным profile | control-api-gateway #237 / Git reconciler | `ManageInstructionSet`; `CompareInstructionSetVersions` | UI/Git source проверяется до mutation; Git-owned update запрещён; detach/copy не принимает новый content; publish/rollback блокирует set и immutable versions | set + immutable content version/digest/validation + receipt + audit; нового события нет | content доступен только через exact protected history/readback; следующий `RuntimeRevision` закрепляет опубликованную версию |
| `DOM-MC-003`, integration-gateway с exact workload/SPIFFE/permission | #235 provider readback producer | `ManageProviderConnectionReference` | caller не задаёт secret/provider payload; принимается только authority-registered receipt ID/version/digest и masked status; connection ref назначается сервером | metadata ref/version/digest/status + history + receipt + audit; secret отсутствует | provider effect/catalog/readback принадлежат #235; control-plane readiness проверяет только защищённый owner RPC |
| `DOM-MC-003`, owner | control-api-gateway #237 | `ManageProviderPool` | provider refs разрешаются под lock; eligibility, weights, observation revision/time и digest копируются в immutable snapshot | pool + snapshot digest + history + receipt + audit; нового события нет | runtime получает только pinned pool snapshot через RuntimeRevision |
| `DOM-MC-005`, owner | control-api-gateway #237 | `BindScheduleConfiguration` | Schedule сначала блокируется существующим graph resolver; stable keys Agent/Instruction/Runtime разрешаются сервером; request не принимает внутренние UUID | существующий Schedule получает exact pinned IDs/version/digest и новый effective input digest; receipt+audit, без нового Schedule event | automation-scheduler продолжает существующий polling/readiness path #231 |
| `DOM-MC-002/004/005`, owner | control-api-gateway #237 | `GetRunDetail`, `ListRunTimeline`, `GetRunLineage`, `ListRunArtifacts`, `ManageRun` cancel/retry | run разрешается в project; mutation получает полный execution graph в каноническом lock order и сверяет current version/fence/attempt | cancel закрывает весь graph и отзывает leases/grants/claims; retry сохраняет predecessor и создаёт fresh Turn/RuntimeRevision/grants; receipt+audit | agent-runner/runtime-controller используют существующие exact protected claims; новый event не нужен, typed readback authoritative |
| `GUIDE-DOC-006`, owner/operator | control-api-gateway #237 | `ManageRuntimeIncident` | incident разрешается по execution→project, затем incident и полный graph lock; action registry закрыт | state/version/action history + receipt+audit; retry использует тот же graph retry helper | watchdog только создаёт evidence; owner actions принадлежат control-plane |
| `DOM-MC-009`, owner | control-api-gateway #237 | `ManageWorkspaceBackup`, existing `ListBackups/GetBackup` | WORKSPACE разрешается сервером; ALL_WORKSPACES выводится из organization authority; membership snapshot формируется под отсортированными locks и immutable | backup/version/digest/members + receipt+audit; terminal/cancel/expiry меняют весь envelope, partial запрещён | logical backup собирает только существующие verified immutable archives; storage secrets не читает |
| `DOM-MC-009`, owner | control-api-gateway #237 | `ManageWorkspaceRestore`, existing restore typed readback | backup и весь membership блокируются; retry только FAILED/EXPIRED/CANCELLED; stale generation/revoke watermark отклоняется | fresh operation attempt/generation, RuntimeRevision и grants для каждого member либо rollback всей transaction; predecessor revoked | runtime-controller materializes уже существующий restore effect profile #231; control-plane владеет operation/readiness |
| `DOM-MC-011`, interaction-gateway #235 | #235 signed provider readback producer | `ManageWorkspaceMattermostMapping` bind/relink/unlink | workload/SPIFFE/permission exact; Project/Workspace server-resolved; JWS/readback receipt проверен зарегистрированным issuer; mapping version/generation monotonic | mapping + receipt digest + receipt+audit; provider locator/effect не хранится | #235 владеет Team effect/catalog; control-plane владеет mapping state/readback и не вызывает Mattermost |

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
        -> WorkspaceRestoreAttempt generation
           -> fresh RuntimeRevision/Turn/attempt/grant per member
     -> WorkspaceMattermostMapping generation
        <- signed provider readback receipt from #235
```

Любой cancel, retry, terminal или expiry получает весь существующий граф в
этом порядке (несколько графов — по отсортированным ID). Одна transaction
закрывает predecessor leases, grants, claims, attempts и связанные агрегаты.
Нельзя сохранить terminal backup/restore envelope, если хотя бы один member
остался nonterminal или сохранил authority.

## Lifecycle и authority matrix

| Вид/переход | Допустимый predecessor | Authority и дополнительные проверки | Atomic successor / revoke | Event либо authoritative read |
| --- | --- | --- | --- | --- |
| RoleDefinition create | отсутствует | owner manage; stable key unique | ACTIVE v1 + history/receipt/audit | typed version-pinned read |
| RoleDefinition update | ACTIVE/PAUSED | row lock, OCC; referenced active agents revalidated | v+1 + immutable history | typed version-pinned read |
| RoleDefinition archive | ACTIVE/PAUSED | нет active assignment/runtime pin | ARCHIVED; новые grants невозможны | typed version-pinned read |
| RoleDefinition delete | ARCHIVED | нет live reference; owner lock | DELETED+tombstone | typed version-pinned read |
| Agent create | отсутствует | server-resolved exact Role/Instruction/Pool | ACTIVE v1 + pins | typed version-pinned read |
| Agent update | ACTIVE/PAUSED | lock Agent then sorted pinned resources, OCC | v+1; future revision only | typed version-pinned read |
| Agent archive | ACTIVE/PAUSED | no active assignment/schedule/open run | ARCHIVED; live authority отсутствует | typed version-pinned read |
| Agent delete | ARCHIVED | no live references | DELETED+tombstone | typed version-pinned read |
| AgentAssignment assign | отсутствует | server-resolved workspace+Agent; actor authority не назначается из payload | ACTIVE v1; server owner/root | typed version-pinned read |
| AgentAssignment unassign | ACTIVE | exact assignment owner lock/OCC | ARCHIVED; новые сессии отклоняются, уже созданный version-pinned graph не переписывается | typed version-pinned read |
| Instruction create | absent | owner/Git profile; source immutable | DRAFT set + version 1 | typed version-pinned read; нового события нет |
| Instruction update | DRAFT current, UI-owned | Git-owned ordinary update forbidden | fresh immutable DRAFT version | typed history; no event |
| Instruction validate | DRAFT | exact version/digest; bounded validator result | VALIDATED version state | typed history; no event |
| Instruction publish | VALIDATED | set+version lock/OCC | prior published retained, effective version pinned | typed version-pinned read |
| Instruction rollback | prior PUBLISHED | exact target version/digest | fresh published rollback version; no mutation of old row | typed version-pinned read |
| Instruction detach | Git-owned | exact source revision/digest; no content in request | same content fresh UI-owned version | typed version-pinned read |
| Instruction copy | Git-owned | exact source lock; server copies content | new UI-owned set/version | typed version-pinned read |
| Instruction archive/delete | no active pin / ARCHIVED | full reference check | ARCHIVED / DELETED+tombstone | typed version-pinned read |
| Provider reference register/refresh | absent / ACTIVE | exact #235 workload and registered receipt; no secret value | server ref, monotonic receipt/version/digest | typed version-pinned read; нового события нет |
| Provider reference archive | ACTIVE/INELIGIBLE | no active pool/runtime pin | ARCHIVED | typed masked read |
| ProviderPool create/update | absent / ACTIVE | refs locked, observations fresh, weights bounded | immutable eligibility snapshot + digest | typed version-pinned read |
| ProviderPool archive/delete | no active Agent/runtime pin / ARCHIVED | full reference check | ARCHIVED / DELETED | typed version-pinned read |
| Schedule bind/rebind | active Schedule | existing Schedule graph must be closed; exact stable selections | same aggregate receives pinned Agent/Instruction/Runtime tuple | existing Schedule read/polling, no event |
| Run cancel | current nonterminal | owner permission; full graph/fence | CANCELLED everywhere; leases/grants/claims revoked | typed run read |
| Run retry | FAILED/EXPIRED/CANCELLED eligible | exact predecessor/version and policy | RETRIED predecessor + fresh attempt/RuntimeRevision/grants | typed lineage/read |
| Run terminal complete | current live attempt | exact executor lease/fence | entire graph terminal | existing runtime read |
| Run partial failure | любой member transition failed | неприменимо как successor | transaction rollback; no partial envelope | previous version read |
| Incident acknowledge | OPEN | owner/operator permission, exact version | ACKNOWLEDGED | typed incident history |
| Incident retry | OPEN/ACKNOWLEDGED/RELEASED | exact current execution/fence и full graph retry eligibility | RETRYING и fresh run attempt atomically; close отдельным OCC после terminal predecessor | run+incident read |
| Incident release | ACKNOWLEDGED | evidence resolved, graph not unsafe | RELEASED | typed incident history |
| Incident close | ACKNOWLEDGED/RELEASED/RETRYING | incident execution terminal; successor не скрывает predecessor lineage | CLOSED | typed incident history |
| Backup create | absent | WORKSPACE/ALL derived owner scope; all members archive-verified | AVAILABLE immutable membership/version/digest | typed backup read |
| Backup claim/renew/complete | неприменимо | aggregation synchronous; no external worker grant | no hidden task | typed backup read |
| Backup cancel | VERIFYING/RETENTION_PENDING | exact version | CANCELLED full envelope | typed backup read |
| Backup retry | UNAVAILABLE/FAILED/EXPIRED | exact version/digest | new attempt+generation, predecessor revoked | typed backup read |
| Backup terminal | current attempt only | all members terminal and checksums exact | AVAILABLE/UNAVAILABLE full envelope | typed backup read |
| Backup expiry/dead-letter | retention clock / retry exhausted | PostgreSQL time, exact generation | EXPIRED/UNAVAILABLE; grants revoked | typed backup read |
| Restore create | AVAILABLE backup | exact membership/version/digest | fresh attempt/RuntimeRevision/grants for all members | typed restore read |
| Restore cancel | nonterminal | full member graph | CANCELLED all members; watermark advanced | typed restore read |
| Restore retry | FAILED/EXPIRED/CANCELLED | predecessor fully terminal | fresh attempt/grants; generation+1 | typed restore read |
| Restore terminal | current attempt | every member terminal | SUCCEEDED/FAILED whole envelope; partial=false | typed restore read |
| Restore expiry/dead-letter | deadline/retry exhausted | PostgreSQL time | EXPIRED/FAILED, all authority revoked | typed restore read |
| Mapping bind | absent | #235 exact workload, signed receipt, workspace owner | BOUND generation 1 | typed mapping read; no event |
| Mapping relink | BOUND | expected mapping/provider generation; new signed receipt | BOUND generation+1, old receipt revoked | typed mapping read; no event |
| Mapping unlink | BOUND | expected version/generation | UNLINKED generation+1 | typed mapping read; no event |

## События, порядок и read/rejoin

Новые protected aggregates не публикуются в
`control_plane.runtime_configuration_changed`: существующий закрытый consumer
`runtime-controller` не принимает эти виды, а ложный consumer и dead-letter
запрещены. Авторитетный результат mutation доступен через typed protected
get/list/history/compare с exact version. При запуске control-plane атомарно
создаёт `RuntimeRevision`, закрепляющую exact Agent/RoleDefinition/
InstructionSetVersion/ProviderPool snapshot; уже поддерживаемое событие
`RUNTIME_REVISION` сохраняет прежние `origin`, condition, cardinality,
ordering и durable consumer path. Ни content, ни provider receipt, ни secret
metadata в событие не входят.

Schedule binding, run, incident, backup/restore и mapping не получают новых
AsyncAPI consumers. Их результат доступен через перечисленные typed RPC;
readiness control-plane использует тот же protected gRPC listener и тот же
authority resolver, что рабочие вызовы.

## Ownership развёртывания

`services/internal/control-plane` и `deploy/k8s/base/control-plane` владеют
RPC, migration, PostgreSQL/RLS, cache invalidation, outbox relay, readiness,
business metrics, dashboard и alerts. `runbook_url` каждого alert — абсолютный
HTTPS URL. NetworkPolicy не открывает Mattermost/provider egress: mapping
команда принимает только проверенный receipt, а provider Team effect остаётся
в #235. Изменение не добавляет второй gateway, worker или background job.
