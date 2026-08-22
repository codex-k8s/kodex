---
id: ARCH-MC-011
title: Целевая архитектура web-first платформы
type: architecture
status: approved
owner: architect
version: 1.0.0
updated: 2026-08-22
---

# Целевая архитектура web-first платформы

Документ фиксирует единую архитектуру owner-approved product reset. Он
заменяет Mattermost-first, repository-first и migration/cutover решения в
части, где они противоречат новому продукту. Источники UX — `UX-MC-002` и
`UX-MC-003`.

## Продуктовая граница

MatterCodex — web-платформа управления ИИ-сотрудниками и выполняемыми ими
Процессами. Core-платформа без внешних интеграций обеспечивает вход,
Проекты, ИИ-сотрудников, инструкции, Процессы, ручные и плановые запуски,
сессии, делегирование, Human Gates, результаты, файлы и аудит.

Пользовательские термины:

- `Проект` — единственный контейнер работы;
- `ИИ-сотрудник` — агент с назначением, инструкциями и capabilities;
- `Процесс` — versioned workflow одного или нескольких ИИ-сотрудников;
- `Запуск` — одно выполнение агента или Процесса;
- `Решение` — долговечный Human Gate;
- `Помощник MatterCodex` — встроенный системный ИИ-сотрудник.

Mattermost, GitHub, GitLab, Kubernetes, CRM, ERP, email, object storage и
knowledge systems являются только IntegrationDefinition. Ни одна definition,
connection или credential не входит в core readiness.

## Владение состоянием

| Компонент | Авторитетное состояние | Не владеет |
|---|---|---|
| `control-plane` | Organization, Membership, Project, Agent, Instruction, Workflow, Session, Turn, Run graph/events, Human Gate, artifact metadata, Schedule, Integration metadata/grants, audit, idempotency, outbox, system assistant | provider credentials и внешние эффекты |
| `control-api-gateway` | browser session, CSRF и ограниченный connection state | lifecycle, permissions, domain projections, event store |
| `runtime-controller` | materialization/claim readback конкретной execution attempt | Проекты, агенты, root lineage и решения |
| `agent-runner` | только процесс выполнения выданной immutable attempt | domain state, orchestration authority и delivery routing |
| `integration-gateway` | encrypted/masked credential state и provider effect receipts | connection metadata, grants и core readiness |
| `interaction-gateway` | delivery attempts необязательных каналов | gates, artifacts, sessions и terminal core outcome |
| `automation-scheduler` | worker lease текущего reconciliation cycle | Schedule и occurrence lifecycle |

Каждая state-changing команда `control-plane` одной PostgreSQL-транзакцией
фиксирует aggregate changes, semantic idempotency receipt, OCC version, audit
и обязательные outbox events.

## Организация и полномочия

Bootstrap создаёт одну Organization, но все агрегаты и queries сохраняют
`organization_id`. Проверенный OIDC issuer + subject разрешается в User и
активную Membership. Browser payload не принимает actor, organization, owner,
root lineage или permission.

Platform roles: `OWNER`, `ADMINISTRATOR`, `OPERATOR`, `MEMBER`, `AUDITOR`.
Project membership отдельно задаёт typed permissions. Полномочия всегда
вычисляет `control-plane` из активных memberships и server-owned ownership.
Скрытый или чужой объект неотличим от отсутствующего.

## Защищённые агрегаты и команды

| Агрегат | Разрешённые специализированные команды |
|---|---|
| Membership | add, change role/permissions, suspend, remove; последний Owner защищён |
| Agent | create, update profile, create/validate/publish/rollback instructions, enable, disable, archive, grant/revoke capability |
| System Assistant | update owner supplement, activate shipped prompt/runtime revision, recover warm runtime; delete/disable/archive запрещены |
| Workflow | create draft, update section, validate, publish, archive |
| Session/Turn | create session, enqueue turn, cancel queued/active turn, continue terminal session |
| Run | launch agent, launch workflow, delegate child, cancel graph, retry terminal attempt |
| Human Gate | open from execution, resolve once, expire, cancel with graph |
| Artifact | reserve upload, complete upload, bind input/result, issue/consume download grant, quarantine |
| Schedule | create, update, enable, pause, materialize occurrence, complete occurrence |
| Integration | register definition, create/test/enable/disable connection, grant/revoke typed capability |

Универсальный CRUD не обслуживает эти виды.

## Сквозная карта owner API

| Инициатор и endpoint | Gateway mapping | Control-plane command/query | Authority и concurrency | Состояние и событие | Потребитель результата |
|---|---|---|---|---|---|
| `POST /api/v1/projects` | `CreateProject` | Project service | OIDC membership, `Idempotency-Key` | Project + audit + `project.created` | PWA global snapshot |
| `POST /api/v1/projects/{projectRef}/agents` | `CreateAgent` | Agent service | project `AGENT_CREATE`, idempotency | Agent draft + audit + `agent.created` | PWA project snapshot |
| `POST /api/v1/agents/{agentRef}/instruction-commands` | typed instruction RPC | Instruction service | owner resolve before `If-Match` | immutable published version + `agent.instructions_published` | runtime revision resolver |
| `POST /api/v1/projects/{projectRef}/workflows` | `CreateWorkflow` | Workflow service | project `WORKFLOW_MANAGE`, idempotency | Workflow draft + audit | authoritative reads |
| `POST /api/v1/workflows/{workflowRef}/commands` | validate/publish/archive | Workflow service | owner resolve + OCC | published version + `workflow.published` | run target catalog |
| `POST /api/v1/runs` | `LaunchAgent` или `LaunchWorkflow` | Execution service | target resolve, `RUN_LAUNCH`, idempotency | Session/Turn/Run/root node/task + `run.created` | runtime-controller, WS projector |
| `POST /api/v1/sessions/{sessionRef}/turns` | `EnqueueTurn` | Execution service | session eligibility, FIFO, idempotency | Turn + node/event + `run.turn_queued` | runtime-controller, WS projector |
| `POST /api/v1/runs/{runRef}/commands` | cancel/retry | Execution service | run owner, typed command, OCC/idempotency | whole-graph transition + events | runtime-controller, WS projector |
| `GET /api/v1/runs/{runRef}/graph` | query mapping | Execution query | owner eligibility | graph snapshot + sequence | PWA authoritative replace |
| `GET /api/v1/runs/{runRef}/events` | bounded catch-up | Execution query | owner eligibility, `afterSequence` | ordered durable deltas | PWA reducer |
| `WSS /api/v1/runs/{runRef}/stream` | authorize then subscribe | snapshot/read/catch-up RPC | same owner rule as HTTP | snapshot then ordered NATS-backed deltas | one browser connection |
| `POST /api/v1/owner-gates/{gateRef}/resolution` | `ResolveOwnerGate` | Gate service | recipient permission, OCC/idempotency | one winner + graph continuation + `owner_gate.resolved` | runtime-controller, WS, optional adapter |
| artifact upload/download endpoints | reserve/complete/grant RPC | Artifact service | project/run lineage, one-time grant | metadata/scan/result + `artifact.available` | object boundary, PWA |
| assistant conversation endpoints | enqueue system turn/apply typed plan | Assistant + same domain services | user authority preserved per tool | Session/Turn, typed receipts, double attribution | warm assistant runtime, PWA |
| schedule endpoints | typed schedule commands | Schedule service | target/grants resolved server-side | Schedule/Occurrence + `run.created` | scheduler/runtime-controller |
| integration endpoints | metadata RPC + typed gateway client | Integration service | grants and secret boundary separated | connection metadata/audit; credential receipt only | integration-gateway, PWA |

## Модель выполнения

`Run` — root execution. `RunNode` представляет root process, agent execution,
Human Gate или bounded external action. `RunEdge` задаёт `DELEGATED_TO`,
`CALLBACK_TO`, `RETRY_OF`, `CONTINUES` или `WAITING_FOR`. Tool activity живёт
в event/detail выбранного node и не засоряет основной graph.

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

| Переход | Блокировка и проверки | Атомарный результат |
|---|---|---|
| launch | target/version, permissions, idempotency, input scan | Session, Turn, root Run/node, RuntimeRevision, task, audit, outbox |
| claim/start | FIFO turn, exact attempt, workload grant/fence | claim/lease и `RUNNING` nodes |
| delegate | parent active attempt, policy, allowed agent/capability | child run/node, edge, inherited root actor/policy, fresh revision |
| callback | terminal child, matching edge/fence, not delivered | callback exactly once, parent event/turn continuation |
| complete | all required children/gates terminal, exact claim | nodes, result, artifacts, leases/grants and root terminal |
| cancel | root owner and non-terminal graph | all active leases/grants/gates revoked, nodes terminal |
| retry | terminal predecessor, retry allowed | new attempt/revision/grant and lineage edge |
| lease expiry | exact lease/fence and non-terminal graph | retryable queue or bounded failed/dead-letter outcome |

## Human Gate

Gate сохраняет server-owned recipient policy, root/run/node/turn/attempt,
canonical safe context digest и version. `APPROVE`, `REJECT`,
`CHANGES_REQUESTED` и `CANCEL` — отдельные transitions. Web и optional
Mattermost adapter конкурируют за одну строку; `SELECT FOR UPDATE` + OCC дают
одного winner. Exact retry возвращает receipt, stale surface получает
`409 Conflict` и winner readback без повторного continuation.

## Системный помощник

Bootstrap с stable key `system.assistant` создаёт ровно одного системного
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

## Интеграции и секреты

`control-plane` владеет definitions, connections metadata, capabilities,
grants и audit. `integration-gateway` владеет credential material и выполняет
только типизированные adapter operations. Browser получает только masked
credential state. Пустой definition catalog и отсутствие credentials — Ready.

Mattermost definition имеет независимые capabilities `INBOUND_MESSAGES`,
`NOTIFICATIONS`, `RESULT_MIRROR`, `HUMAN_GATE_DECISIONS`. Ошибка delivery
создаёт отдельный retryable DeliveryAttempt/incident и не меняет core Run.

## Realtime

Control-plane transaction сохраняет `RunEvent` и outbox envelope. Relay
публикует события в NATS JetStream at least once. Gateway durable consumer
фиксирует inbox/cursor до fan-out. Для browser stream:

1. gateway авторизует Run через control-plane;
2. получает snapshot и sequence;
3. подписывает соединение на уже проверенный root Run;
4. отправляет deltas строго по sequence;
5. при gap запрашивает catch-up;
6. при недоступном диапазоне заменяет state новым snapshot.

Frontend reducer нормализует nodes/edges/events/gates/artifacts, игнорирует
duplicate, обнаруживает gap и никогда не выводит terminal/nextActions локально.
Progress coalesced и bounded. Raw stdout, stderr, JSONL, provider responses,
secrets и files по WebSocket запрещены.

## Fresh database и bootstrap

Fresh install использует одну baseline migration `control_plane_baseline`.
Legacy aliases, backfill, cutover, dual read/write и migration jobs отсутствуют.
Bootstrap идемпотентно создаёт organization, initial owner membership contract,
system assistant, core prompt, platform capabilities, built-in integration
definitions, default runtime policy и system policies. Повтор с теми же stable
keys сверяет immutable content; расхождение требует controlled revision, а не
перезапись.

## Профили готовности

`web-only` требует PostgreSQL, object storage, NATS, control-plane,
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
