---
id: ARCH-MC-011
title: Целевая архитектура web-first платформы
type: architecture
status: approved
owner: architect
version: 1.2.0
updated: 2026-08-29
---

# Целевая архитектура web-first платформы

Документ фиксирует единую архитектуру owner-approved product reset. Он
заменяет Mattermost-first, repository-first и migration/cutover решения в
части, где они противоречат новому продукту. Источники UX — `UX-MC-002` и
`UX-MC-003`.

## Продуктовая граница

Kodex — web-платформа управления ИИ-сотрудниками и выполняемыми ими
Процессами. Core-платформа без внешних интеграций обеспечивает вход,
Проекты, ИИ-сотрудников, инструкции, Процессы, ручные и плановые запуски,
сессии, делегирование, Human Gates, результаты, файлы и аудит.

Пользовательские термины:

- `Проект` — единственный контейнер работы;
- `ИИ-сотрудник` — агент с назначением, инструкциями и capabilities;
- `Процесс` — versioned workflow одного или нескольких ИИ-сотрудников;
- `Запуск` — одно выполнение агента или Процесса;
- `Решение` — долговечный Human Gate;
- `Помощник Kodex` — встроенный системный ИИ-сотрудник.

Mattermost, GitHub, GitLab, Kubernetes, CRM, ERP, email, object storage и
knowledge systems являются только IntegrationDefinition. Ни одна definition,
connection или credential не входит в core readiness.

## Владение состоянием

| Компонент              | Авторитетное состояние                                                                                                                                                                                                                                       | Не владеет                                               |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------- |
| `control-plane`        | Organization, Subject, permission registry, role/version/binding, Project, Agent, Instruction, Workflow, Session, Turn, Run graph/events, Human Gate, artifact metadata, Schedule, Integration metadata/grants, audit, idempotency, outbox, system assistant | provider credentials и внешние эффекты                   |
| `control-api-gateway`  | browser session, CSRF и ограниченный connection state                                                                                                                                                                                                        | lifecycle, permissions, domain projections, event store  |
| `runtime-controller`   | materialization/claim readback конкретной execution attempt                                                                                                                                                                                                  | Проекты, агенты, root lineage и решения                  |
| `agent-runner`         | только процесс выполнения выданной immutable attempt                                                                                                                                                                                                         | domain state, orchestration authority и delivery routing |
| `integration-gateway`  | encrypted/masked credential state и provider effect receipts                                                                                                                                                                                                 | connection metadata, grants и core readiness             |
| `interaction-gateway`  | delivery attempts необязательных каналов                                                                                                                                                                                                                     | gates, artifacts, sessions и terminal core outcome       |
| `automation-scheduler` | worker lease текущего reconciliation cycle                                                                                                                                                                                                                   | Schedule и occurrence lifecycle                          |

Каждая state-changing команда `control-plane` одной PostgreSQL-транзакцией
фиксирует aggregate changes, semantic idempotency receipt, OCC version, audit
и обязательные outbox events.

## Организация и полномочия

Bootstrap создаёт одну Organization, но все агрегаты и queries сохраняют
`organization_id`. Проверенный OIDC issuer + subject разрешается в Subject, а
bounded groups синхронизируются как identity read model. Browser payload не
принимает actor, organization, owner, root lineage или permission.

Application policy является allow-only: закрытый permission registry,
immutable version системной/custom role и user/OIDC-group/service binding с
organization/project/resource-kind/resource-instance scope. Полномочия всегда
вычисляет `control-plane` из pinned role version, актуальных binding,
server-owned ownership и точного target. Скрытый или чужой объект неотличим от
отсутствующего; OIDC role/group не является policy authority.

`RESOURCE_INSTANCE` является полноценным scope: разрешение на один Agent,
Session, Artifact, Secret, RoleImage или RuntimeEnvironment не распространяется
на соседний экземпляр. Backend сначала разрешает target и его tenant/project
lineage, затем проверяет точный permission. Frontend route, видимость кнопки,
Project ref из payload и наличие объекта в локальном store не являются
authority.

Чувствительные permissions `secret.reveal`, `image.build`, `image.promote`,
`environment.privileged.manage`, `prompt.full.view`, `artifact.delete`,
`artifact.restore`, `artifact.purge` и `session.cancel` зарегистрированы
раздельно. Fresh OIDC re-auth обязательна для reveal, build, promotion,
привилегированного изменения окружения, полного prompt и необратимого purge.
Soft delete/restore и аварийная отмена Session требуют exact permission без
обязательного step-up. Action proof связан с Subject, browser session,
permission, exact target, nonce и expiry и потребляется однократно.

Старый Membership contract является только presentation adapter. Его команды
создают или изменяют canonical role/version/binding в той же транзакции, а
чтение выполняется через SQL view над binding с server-owned presentation
marker. Отдельной membership write model нет. Exact resource binding не
проецируется в project-wide permissions.

## Защищённые агрегаты и команды

| Агрегат                          | Разрешённые специализированные команды                                                                                    |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| Membership compatibility adapter | add, change role/permissions, suspend, remove через canonical role/version/binding; последний Owner защищён               |
| AccessRole, AccessBinding        | create role/version, archive custom role, create/change/revoke binding; system role immutable                             |
| Agent                            | create, update profile, create/validate/publish/rollback instructions, enable, disable, archive, grant/revoke capability  |
| RoleImage                        | create draft/revision, validate Dockerfile, build revision, promote exact digest, archive unused revision                 |
| RuntimeEnvironment               | create revision, bind promoted image, set env/secret refs/tools, preview effective policy, publish privileged revision    |
| SecretDescriptor                 | create version, rotate, reveal once, revoke reference; plaintext не является состоянием `control-plane`                   |
| System Assistant                 | update owner supplement, activate shipped prompt/runtime revision, recover warm runtime; delete/disable/archive запрещены |
| Workflow                         | create draft, update section, validate, publish, archive                                                                  |
| Session/Turn                     | create session, enqueue turn, cancel queued/active turn, continue terminal session                                        |
| Run                              | launch agent, launch workflow, delegate child, cancel graph, retry terminal attempt                                       |
| Human Gate                       | open from execution, resolve once, expire, cancel with graph                                                              |
| Artifact                         | reserve upload, complete upload, bind input/result, issue/consume download grant, quarantine, soft delete, restore, purge |
| Schedule                         | create, update, enable, pause, materialize occurrence, complete occurrence                                                |
| Integration                      | register definition, create/test/enable/disable connection, grant/revoke typed capability                                 |

Универсальный CRUD не обслуживает эти виды.

## Сквозная карта owner API

| Инициатор и endpoint                                                                                          | Gateway mapping                                                                | Control-plane command/query                         | Authority и concurrency                                                                              | Состояние и событие                                                                       | Потребитель результата                       |
| ------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ | --------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- | -------------------------------------------- |
| `POST /api/v1/projects`                                                                                       | `CreateProject`                                                                | Project service                                     | OIDC actor с canonical `project.create`, `Idempotency-Key`                                           | Project + audit + `project.created`                                                       | PWA global snapshot                          |
| `POST /api/v1/projects/{projectRef}/agents`                                                                   | `CreateAgent`                                                                  | Agent service                                       | project `AGENT_CREATE`, idempotency                                                                  | Agent draft + audit + `agent.created`                                                     | PWA project snapshot                         |
| `GET /api/v1/administration/membership-candidates`, `GET /api/v1/projects/{projectRef}/membership-candidates` | bounded catalog query                                                          | Access service                                      | OIDC actor; canonical organization/project `access.manage`                                           | read-only список пользователей по имени/email без subject/UUID                            | форма управления доступом                    |
| `POST /api/v1/agents/{agentRef}/instruction-commands`                                                         | typed instruction RPC                                                          | Instruction service                                 | owner resolve before `If-Match`                                                                      | immutable published version + `agent.instructions_published`                              | runtime revision resolver                    |
| `POST /api/v1/projects/{projectRef}/workflows`                                                                | `CreateWorkflow`                                                               | Workflow service                                    | project `WORKFLOW_MANAGE`, idempotency                                                               | Workflow draft + audit                                                                    | authoritative reads                          |
| `GET /api/v1/search`                                                                                          | `SearchPlatform`                                                               | Control-plane query service                         | OIDC actor с binding; eligibility каждого Project по canonical `project.view`, как list/detail       | bounded read-only projection без domain event                                             | глобальная панель Control Center             |
| `POST /api/v1/workflows/{workflowRef}/commands`                                                               | validate/publish/archive                                                       | Workflow service                                    | owner resolve + OCC                                                                                  | published version + `workflow.published`                                                  | run target catalog                           |
| `POST /api/v1/runs`                                                                                           | `LaunchAgent` или `LaunchWorkflow`                                             | Execution service                                   | target resolve, `RUN_LAUNCH`, idempotency                                                            | Session/Turn/Run/root node/task + `run.created`                                           | runtime-controller, WS projector             |
| `POST /api/v1/sessions/{sessionRef}/turns`                                                                    | `EnqueueTurn`                                                                  | Execution service                                   | session eligibility, FIFO, idempotency                                                               | Turn + node/event + `run.turn_queued`                                                     | runtime-controller, WS projector             |
| Session cancel command                                                                                        | `CancelSession`/`CancelTurn`                                                   | Execution service                                   | exact `session.cancel`, target resolve, lifecycle fence, OCC/idempotency                             | active claim/grant revoked, terminal transition + event                                   | runtime-controller, общий realtime transport |
| `POST /api/v1/runs/{runRef}/commands`                                                                         | cancel/retry                                                                   | Execution service                                   | run owner, typed command, OCC/idempotency                                                            | whole-graph transition + events                                                           | runtime-controller, WS projector             |
| `GET /api/v1/runs/{runRef}/graph`                                                                             | query mapping                                                                  | Execution query                                     | owner eligibility                                                                                    | graph snapshot + sequence                                                                 | PWA authoritative replace                    |
| `GET /api/v1/runs/{runRef}/events`                                                                            | bounded catch-up                                                               | Execution query                                     | owner eligibility, `afterSequence`                                                                   | ordered durable deltas                                                                    | PWA reducer                                  |
| `WSS /api/v1/session/stream`                                                                                  | authorize owner session, then multiplex platform and bounded Run subscriptions | platform cursor plus Run snapshot/read/catch-up RPC | browser session и тот же owner rule, что HTTP Run detail                                             | platform invalidations, Run snapshots и ordered NATS-backed deltas с независимыми cursors | один browser socket                          |
| `POST /api/v1/owner-gates/{gateRef}/resolution`                                                               | `ResolveOwnerGate`                                                             | Gate service                                        | recipient permission, OCC/idempotency                                                                | one winner + graph continuation + `owner_gate.resolved`                                   | runtime-controller, WS, optional adapter     |
| artifact upload/download endpoints                                                                            | reserve/complete/grant RPC                                                     | Artifact service                                    | project/run lineage, exact instance permission, one-time grant                                       | metadata/scan/result + `artifact.available`                                               | object boundary, PWA                         |
| artifact delete/restore/purge commands                                                                        | typed lifecycle RPC                                                            | Artifact service                                    | exact permission на resolved Artifact; purge требует fresh action proof                              | retention transition или irreversible object deletion + audit/event                       | object boundary, общий realtime transport    |
| RoleImage build/promotion commands                                                                            | typed image RPC                                                                | Image service                                       | exact image revision, отдельные `image.build`/`image.promote`, fresh action proof, admission         | Build/Promotion revision, digest/provenance + audit/events                                | builder, registry, environment resolver      |
| privileged RuntimeEnvironment command                                                                         | typed environment RPC                                                          | Environment service                                 | exact `environment.privileged.manage`, fresh action proof, actor ceiling + admission                 | immutable effective revision + audit/event                                                | RuntimeRevision resolver                     |
| Secret reveal command                                                                                         | one-time reveal grant RPC                                                      | Access service + `secret-broker`                    | exact `secret.reveal`, fresh action proof, exact Secret version                                      | audit metadata без значения; plaintext не сохраняется                                     | одноразовый `no-store` browser response      |
| full prompt read                                                                                              | protected prompt query                                                         | Execution query                                     | exact `prompt.full.view`, fresh action proof, Session/Turn eligibility                               | bounded read без secret/provider credentials                                              | detail modal без preload/store cache         |
| assistant conversation endpoints                                                                              | enqueue system turn/apply typed plan                                           | Assistant + same domain services                    | user authority preserved per tool; context refs разрешаются сервером                                 | Session/Turn, typed receipts, double attribution                                          | warm assistant runtime, PWA                  |
| schedule endpoints                                                                                            | typed preset/time/timezone commands                                            | Schedule service                                    | target resolved server-side, OCC/idempotency; claim pin-ит schedule version + immutable input digest | Schedule/Occurrence + `run.created`                                                       | scheduler/runtime-controller                 |
| integration endpoints                                                                                         | metadata RPC + typed gateway client                                            | Integration service                                 | grants and secret boundary separated                                                                 | connection metadata/audit; credential receipt only                                        | integration-gateway, PWA                     |
| `GET /api/v1/audit-events`                                                                                    | `ListAuditEvents` с bounded поиском                                            | Audit query service                                 | OIDC actor; platform Auditor/Administrator/Owner либо project `VIEW_AUDIT`                           | read-only события с двойной attribution и server-resolved resource name; без event        | экран аудита                                 |

Глобальный поиск не является отдельным владельцем данных и не индексирует
скрытые ресурсы. Gateway передаёт только ограниченные `query` и `limit` через
generated client; control-plane повторно разрешает actor и organization,
применяет единое project eligibility правило к Проектам, агентам, Процессам и
запускам и возвращает только opaque refs с безопасными display metadata.
Поскольку операция read-only, idempotency, OCC и domain event ей не нужны.

Поиск аудита не фильтрует уже загруженный случайный срез в браузере. Gateway
передаёт строку control-plane, а тот применяет её к разрешённому
человекочитаемому имени ресурса после tenant/project eligibility. Исполнитель
системной операции возвращается по имени помощника, а opaque ref остаётся
только ссылкой из авторитетного readback и не показывается как UI-copy.

## Модель выполнения

`Run` — root execution. Агентский `RunNode` представляет одну логическую
`Session`, а не отдельный Turn или Attempt. Три независимые Session одного
ИИ-сотрудника отображаются тремя nodes; continuation, retry и новые Turns той
же Session остаются в timeline одной node. Root Process, Human Gate и bounded
external action являются отдельными typed nodes. `RunEdge` задаёт
`DELEGATED_TO`, `CALLBACK_TO`, `RETRY_OF`, `CONTINUES` или `WAITING_FOR`.

Timeline Session объединяет сообщения инициатора или parent Session, ответы
агента, Turn/Attempt transitions и нормализованные tool-call
`started/progress/completed/failed`. Источник tool activity — проверенный
`codex exec --json` JSONL; hooks могут только добавлять безопасные метаданные.
Raw stdout/stderr и provider payload не становятся timeline event. Detail node
показывает launch context, immutable RuntimeRevision, lineage и безопасные
rendered prompt metadata. Полный материализованный prompt загружается отдельным
защищённым запросом только с `prompt.full.view` и fresh re-auth.

Каждый root Run хранит `graph_revision` и непрерывный `next_event_sequence`.
Добавление/изменение node, edge, turn, gate, artifact и incident резервирует
следующий sequence в той же транзакции. `RunEvent` неизменяем; duplicate event
ID с тем же digest безопасно игнорируется, иной digest является конфликтом.

### Состояния Run

```text
QUEUED -> RUNNING -> WAITING_HUMAN -> RUNNING -> SUCCEEDED
                    |                       |
                    +-> CANCELLED           +-> FAILED
QUEUED/RUNNING/WAITING_HUMAN -> CANCELLING -> CANCELLED
FAILED/CANCELLED -> retry -> новая Attempt в том же lineage
```

Terminal состояния не переоткрываются. Retry создаёт новую attempt,
RuntimeRevision, claim/grant и `RETRY_OF`; прежняя attempt остаётся read-only.

### Матрица полного графа

| Переход      | Блокировка и проверки                                   | Атомарный результат                                                |
| ------------ | ------------------------------------------------------- | ------------------------------------------------------------------ |
| launch       | target/version, permissions, idempotency, input scan    | Session, Turn, root Run/node, RuntimeRevision, task, audit, outbox |
| claim/start  | FIFO turn, exact attempt, workload grant/fence          | claim/lease и `RUNNING` nodes                                      |
| delegate     | parent active attempt, policy, allowed agent/capability | child run/node, edge, inherited root actor/policy, fresh revision  |
| callback     | terminal child, matching edge/fence, not delivered      | callback exactly once, parent event/turn continuation              |
| complete     | all required children/gates terminal, exact claim       | nodes, result, artifacts, leases/grants and root terminal          |
| cancel       | root owner and non-terminal graph                       | all active leases/grants/gates revoked, nodes terminal             |
| retry        | terminal predecessor, retry allowed                     | new attempt/revision/grant and lineage edge                        |
| lease expiry | exact lease/fence and non-terminal graph                | retryable queue or bounded failed/dead-letter outcome              |

## Human Gate

Gate сохраняет server-owned recipient policy, root/run/node/turn/attempt,
canonical safe context digest и version. `APPROVE`, `REJECT`,
`CHANGES_REQUESTED` и `CANCEL` — отдельные transitions. Web и optional
Mattermost adapter конкурируют за одну строку; `SELECT FOR UPDATE` + OCC дают
одного winner. Exact retry возвращает receipt, stale surface получает
`409 Conflict` и winner readback без повторного continuation.

## Системный помощник

Bootstrap с stable key `system-assistant` создаёт ровно одного системного
Agent, protected core prompt version, owner supplement, durable system Session
и WarmRuntimeDesiredState. Database constraints и domain methods запрещают
delete, archive, disable и смену system purpose.

Warm runtime — отдельный long-lived materialization с resource limits,
revision и heartbeat. Readiness положительна только если desired revision
фактически обслуживается, system session доступна и runtime способен принять
следующий FIFO turn. Idle не является active Turn. После restart reconciler
восстанавливает materialization до открытия assistant readiness.

Помощник не имеет database, Kubernetes или secret credentials. Его tools —
закрытый registry специализированных owner commands. Каждая tool invocation
повторно использует authority проверенного пользователя и фиксирует двойную
атрибуцию `initiator_user + system_assistant`.

Каждый assistant Turn получает server-owned `AssistantContextDescriptor`:
Organization, текущий Project, тип и ref открытого ресурса, route purpose,
locale, доступные read-модели и закрытый набор допустимых tool operations.
Browser передаёт только route locators; `control-plane` повторно разрешает
каждый ref и отбрасывает недоступный контекст. При смене экрана следующий Turn
получает новую immutable context revision, поэтому старый диалог не наследует
полномочия нового экрана и наоборот.

План помощника содержит полный список create/change/delete operations и
значения несекретных параметров. Пользователь может отредактировать план, но
apply не доверяет plan payload: каждая tool-команда заново разрешает exact
target, проверяет permission, fresh re-auth policy, OCC и idempotency. Контекст
может сузить registry tools для удобства, но не выдаёт новых permissions.

Registry включает создание Project, Agent, Workflow, metadata integration
connection, постановку connection test, изменение capability/grant, создание
Schedule и запуск. Secret value не входит ни в prompt, ни в MCP payload:
помощник может только подготовить переход в защищённую форму Control Center.

## Интеграции и секреты

`control-plane` владеет definitions, connections metadata, capabilities,
grants и audit. `integration-gateway` владеет credential material и выполняет
только типизированные adapter operations. Browser получает только masked
credential state. Пустой definition catalog и отсутствие credentials — Ready.

Runtime Secret пишет и раскрывает отдельный `secret-broker` с минимальным
namespace-scoped ServiceAccount. PostgreSQL содержит только descriptor,
Kubernetes ref, version, rotation state и `display_hint` не длиннее 15% значения
и 12 символов; короткие и binary значения не показываются. Reveal требует
exact `secret.reveal` и fresh action proof, возвращается один раз с `no-store` и
не проходит через assistant, prompt, PostgreSQL, cache, event или frontend
store.

RoleImage build и promotion являются разными операциями. RuntimeEnvironment
pin-ит exact promoted digest и хранит только типизированные resources, network
и Kubernetes RBAC profiles. Effective policy вычисляется backend и не может
превысить application permissions actor или installation admission policy.
Raw Kubernetes YAML из browser не является исполняемой policy.

Mattermost definition имеет независимые capabilities `INBOUND_MESSAGES`,
`NOTIFICATIONS`, `RESULT_MIRROR`, `HUMAN_GATE_DECISIONS`. Ошибка delivery
создаёт отдельный retryable DeliveryAttempt/incident и не меняет core Run.

## Защищённые web-представления

Control Center использует доступную ширину страницы и семантические размеры
modal: подтверждения остаются `sm/md`, detail/editor/preview используют
`xl/full`. Размер presentation не меняет состав выданных данных. Secret value,
полный prompt и необратимый purge никогда не prefetch-ятся и не сохраняются в
общем frontend store; они открываются только отдельной командой после exact
backend check и, где требуется, fresh re-auth.

`Решения` представлены как decision inbox и detail panel/modal. List содержит
только gates, доступные actor, а detail и каждая `APPROVE`, `REJECT`,
`CHANGES_REQUESTED` или `CANCEL` повторно проверяют exact Gate и recipient
policy. Client-side grouping, primary action и disabled state не являются
authorization decision.

Avatar ИИ-сотрудника хранится как immutable Artifact revision в S3. Upload,
crop и remove используют Agent и Artifact permissions; внешний URL не
становится доверенным источником. Generated fallback не расширяет доступ к
исходному Artifact и не раскрывает удалённую revision.

## Realtime

Control-plane transaction сохраняет `RunEvent` и outbox envelope. Relay
публикует события в NATS JetStream at least once. Stateless gateway использует
событие как bounded wake-сигнал и не фиксирует локальный бизнес-эффект:
авторитетными источниками восстановления остаются `RunEvent` store и platform
cursor control-plane.

В одной browser session работает один общий WebSocket transport. Он
мультиплексирует разрешённые subscriptions главной, Проекта, решений,
помощника, Session graph и timeline; route component не создаёт собственное
соединение. Для каждой подписки gateway выполняет exact backend eligibility
check, связывает subscription с Subject, organization, resource instance и
policy revision и прекращает её после invalidation binding/Subject.

Для каждой stream subscription:

1. gateway авторизует Run через control-plane;
2. получает snapshot и sequence;
3. подписывает соединение на уже проверенный root Run;
4. отправляет deltas строго по sequence;
5. при gap запрашивает catch-up;
6. при недоступном диапазоне заменяет state новым snapshot.
7. на heartbeat сверяет авторитетный sequence/cursor и автоматически
   восстанавливает сигнал, потерянный при reconnect NATS без разрыва WebSocket.

Reconnect выполняется в фоне с bounded backoff. Он не вызывает route reload,
повторный mount страницы, сброс draft form или закрытие modal/drawer. После
восстановления transport frontend передаёт последний cursor каждой подписки;
gateway выполняет HTTP catch-up либо присылает новый snapshot при недоступном
диапазоне. Смена маршрута меняет набор subscriptions, но не пересоздаёт
transport.

Frontend reducer нормализует nodes/edges/events/gates/artifacts, игнорирует
duplicate, обнаруживает gap и никогда не выводит terminal/nextActions локально.
Progress coalesced и bounded. Raw stdout, stderr, JSONL, provider responses,
secrets и files по WebSocket запрещены.

## Fresh database и bootstrap

Fresh install использует одну baseline migration `control_plane_baseline`.
Legacy aliases, backfill, cutover, dual read/write и migration jobs отсутствуют.
Bootstrap идемпотентно создаёт organization, initial owner claim contract,
permission registry, system roles/bindings, system assistant, core prompt,
platform capabilities, built-in integration definitions, default runtime
policy и system policies. Повтор после завершённого
bootstrap сверяет защищённые revision, digest и content core prompt: новая
поставляемая revision применяется forward-only, а rollback либо конфликт одной
revision закрывают startup. Остальные baseline-записи принадлежат installation
schema/release и повторно не создаются и не перезаписываются.

## Профили готовности

`web-only` требует PostgreSQL, NATS, control-plane,
control-api-gateway, runtime-controller, agent-runner capacity, scheduler,
integration-gateway с пустым catalog и реальный warm assistant. Interaction
gateway и Mattermost не входят в профиль.

`web-with-mattermost` добавляет interaction gateway и выбранные capabilities.
Каждая capability имеет самостоятельную readiness/delivery policy.

## Удаляемый контур

В том же reset удаляются historical `apps/control-center`, legacy bot-service,
legacy data migration/cutover jobs и contracts, старые migration chains,
Mattermost Team/Room/bot authority из core, compatibility APIs, generic
protected-resource CRUD, dark-deploy manifests, старые Mattermost-first E2E,
runbooks и roadmaps. Git history остаётся архивом.
