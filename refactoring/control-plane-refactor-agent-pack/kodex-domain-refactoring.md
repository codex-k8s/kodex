# Аналитический отчёт по сервису Control Plane в репозитории codex-k8s/kodex

## Executive summary

Сервис **Control Plane** в репозитории `codex-k8s/kodex` является центральным «узлом управления» платформы: один Go‑процесс поднимает **gRPC API** (единый `ControlPlaneService`) и **HTTP endpoint** для `/mcp`, health‑проб и `/metrics`, а также содержит фоновые циклы (reconciler) для части доменов. Это видно по композиционному корню `internal/app/app.go`, где в одном месте создаются клиенты к Kubernetes/GitHub/Postgres/Registry, поднимаются воркеры, метрики и регистрируется весь gRPC‑сервер. fileciteturn11file0L1-L1

Control Plane аккумулирует множество доменов в одном бинаре и одном API-контракте: ingestion GitHub webhook’ов, управление пользователями/проектами/репозиториями и токенами, выдача MCP‑токенов и approvals, runtime deploy orchestration (с lease/очередью задач), пользовательские взаимодействия (включая Telegram‑адаптер), устойчивость к GitHub rate limit, Mission Control (dashboard/workspace + командная модель), Quality/Change governance, системные настройки (включая reload loop) и учёт runtime errors. Ширина и разнородность gRPC контракта подтверждается `proto/kodex/controlplane/v1/controlplane.proto`. fileciteturn17file0L1-L1

Данные по большинству доменов сосредоточены в **одной PostgreSQL базе** (таблицы `agent_runs`, `flow_events`, `users/projects/project_members`, `agent_sessions`, `runtime_deploy_tasks`, `interaction_*`, `mission_control_*`, `github_rate_limit_*`, `system_settings`, `runtime_errors`, `change_governance_*` и др.), управляемой миграциями Control Plane. fileciteturn34file0L1-L1 fileciteturn40file0L1-L1 fileciteturn41file0L1-L1 fileciteturn48file0L1-L1 fileciteturn56file0L1-L1 fileciteturn55file0L1-L1 fileciteturn60file0L1-L1 fileciteturn66file0L1-L1 fileciteturn63file0L1-L1 fileciteturn58file0L1-L1

Ключевые архитектурные риски текущего состояния: «god‑service» + «shared DB», где независимые функции (auth/management vs runtime deploy vs mission control vs interactions) связаны общим процессом, конфигурацией и релизным циклом; обширный gRPC контракт усложняет эволюцию и ownership; рост доменов ведёт к усилению связности и повышению blast radius при сбоях (например, проблемы runtime deploy могут влиять на доступность staff‑операций). Эти риски напрямую следуют из способа wiring’а доменов и зависимостей в `internal/app/app.go` и единого `ControlPlaneService`. fileciteturn11file0L1-L1 fileciteturn17file0L1-L1

Рекомендуемая траектория: **инкрементальная декомпозиция по Strangler Fig** (постепенное «оборачивание» монолита фасадом и вынос функциональности наружу), начав с наиболее «тяжёлых» и относительно изолируемых доменов (Runtime Deploy, Interactions, Mission Control), с последующим выносом GitHub rate-limit и Governance. Такой подход снижает риск модернизации за счёт поэтапной замены функций без «big bang rewrite». citeturn4search0 Для разделения данных и надёжной межсервисной интеграции целесообразно применять **Transactional Outbox** citeturn3search2 и **Saga** для распределённых/длительных транзакций (orchestration или choreography). citeturn3search3

**Предположения и ограничения отчёта:** в репозитории не обнаружено единой актуальной высокоуровневой диаграммы системы (поэтому диаграммы ниже — реконструкция из кода/миграций). Также ссылки на строки даны как GitHub line‑anchors по ключевым файлам; в некоторых случаях якоря могут незначительно смещаться при изменениях ветки `main`.

## Текущая структура Control Plane

Control Plane расположен в `services/internal/control-plane/` и запускается как отдельный сервис. Точка сборки/композиции — `internal/app/app.go`, где читается конфигурация из env, открывается соединение к Postgres, создаются репозитории/доменные сервисы, поднимаются фоновые циклы, регистрируется gRPC сервер и HTTP mux (MCP + health + metrics). fileciteturn11file0L1-L1

### Ключевые точки входа и «горячие» места (с line‑anchors)

| Артефакт | Роль | Перма‑ссылка (file/lines) |
|---|---|---|
| `internal/app/app.go` | Composition root: wiring доменов, запуск gRPC+HTTP, фоновые loop’ы | `https://github.com/codex-k8s/kodex/blob/main/services/internal/control-plane/internal/app/app.go#L72-L571` |
| `internal/app/app.go` | Инициализация DB + репозиториев | `https://github.com/codex-k8s/kodex/blob/main/services/internal/control-plane/internal/app/app.go#L92-L118` |
| `internal/app/app.go` | Инициализация MCP service с зависимостями (GitHub/K8s/PostgresAdmin/DB repos) | `https://github.com/codex-k8s/kodex/blob/main/services/internal/control-plane/internal/app/app.go#L159-L198` |
| `internal/app/app.go` | Регистрация `ControlPlaneService` и проброс всех доменных зависимостей в gRPC transport | `https://github.com/codex-k8s/kodex/blob/main/services/internal/control-plane/internal/app/app.go#L496-L511` |
| `internal/app/config.go` | Env‑конфигурация Control Plane (DB/GitHub/tokens/runtime deploy/registry/labels) | `https://github.com/codex-k8s/kodex/blob/main/services/internal/control-plane/internal/app/config.go#L11-L177` |
| `proto/.../controlplane.proto` | Единый gRPC контракт (очень широкий) | `https://github.com/codex-k8s/kodex/blob/main/proto/kodex/controlplane/v1/controlplane.proto` |
| `internal/transport/grpc/server.go` | gRPC façade: тип `Server` агрегирует домены и маппит RPC → use-cases | `https://github.com/codex-k8s/kodex/blob/main/services/internal/control-plane/internal/transport/grpc/server.go` |
| Миграции Control Plane | Создание/эволюция shared DB | `https://github.com/codex-k8s/kodex/blob/main/services/internal/control-plane/cmd/cli/migrations/` |

Факт «сборки всего в одном месте» хорошо виден по импорту и созданию множества доменных сервисов в `app.go` (MCP, RunStatus, Webhook, Staff, Mission Control, GitHub Rate Limit, Runtime Deploy, Change Governance, System Settings и пр.). fileciteturn11file0L1-L1

### Модули/пакеты Control Plane (по фактическому wiring)

Ниже — практическая «карта» по тому, что реально живёт в Control Plane (по зависимостям, которые создаются и передаются в transport):

- **Конфигурация и bootstrap**: `internal/app/config.go` — env‑контракт включает адреса gRPC/HTTP, GitHub PAT и bot token, webhook secret/events, множество labels для триггеров/стадий, настройки runtime deploy (timeouts, worker id, lease ttl, reconcile interval), параметры registry, настройки шифрования токенов, параметры основной БД и «admin DB» для lifecycle project DB. fileciteturn15file0L1-L1
- **Транспортный слой**: `internal/transport/grpc/server.go` реализует `ControlPlaneServiceServer` и содержит агрегированный набор зависимостей на доменные сервисы, а также преобразование ошибок в gRPC коды и утилиты валидации principal/bearer token. fileciteturn18file0L1-L1
- **MCP HTTP endpoint**: в `app.go` явно регистрируется handler на `/mcp` и `/mcp/`, который оборачивает `mcpService`. fileciteturn11file0L1-L1
- **Доменные сервисы (сейчас в одном процессе)**:
  - `mcpdomain.NewService(...)` получает зависимости на runs/flow events/repos/platform tokens/action requests/interactions/sessions/project databases + криптосервис + GitHub/K8s/PostgresAdmin. fileciteturn11file0L1-L1
  - `runtimedeploydomain.NewService(...)` получает Kubernetes adapter + `runtime_deploy_tasks` repo + runs repo + flow events + registry + runtime error recorder. fileciteturn11file0L1-L1
  - `runstatusdomain.NewService(...)` зависит от runs/sessions/platform tokens/tokencrypt/GitHub/K8s/flow events + staff runs + GitHub rate-limit waits + runtime deploy. fileciteturn11file0L1-L1
  - `webhook.NewService(...)` получает доступ к runs/agents/flow events/repos/projects/users/members + RunStatus + RuntimeErrors + GitHubMgmt и т.д. fileciteturn11file0L1-L1
  - `staff.NewService(...)` агрегирует CRUD по users/projects/members/repos/tokens/feedback/runs/tasks/errors + GitHubMgmt + RunStatus + RuntimeDeploy + SystemSettings. fileciteturn11file0L1-L1
  - `missioncontroldomain.NewService(...)` и `missioncontrolworkerdomain.NewService(...)` разделяют domain/workers и используют projection repo + flow events + staff runs + agent runs. fileciteturn11file0L1-L1
  - `githubratelimitdomain.NewService(...)` связывает waits/runs/flow events и даже использует `staffService` как `PlatformReplay`, что является важным признаком тесной связности доменов. fileciteturn11file0L1-L1
  - `changegovernancedomain.NewService(...)` работает поверх projection repo и rollout state из system settings. fileciteturn11file0L1-L1
- **Клиенты внешних систем** (внутри Control Plane):
  - GitHub client (операции над issue/PR/labels/comments) — `internal/clients/github/client.go`. fileciteturn23file0L1-L1
  - GitHub management / preflight — `internal/clients/githubmgmt/client.go`. fileciteturn25file0L1-L1
  - Kubernetes client (очень широкий: apply, wait, logs, exec, namespace/secret/configmap и т.д.) — `internal/clients/kubernetes/client.go`. fileciteturn27file0L1-L1
  - Postgres admin client (create/drop/check DB) — `internal/clients/postgresadmin/client.go`. fileciteturn29file0L1-L1

### Взаимодействия с другими сервисами kodex

Из кода клиентов/использования RPC следует, что:

- **Worker** (`services/jobs/worker`) использует gRPC Control Plane для: выдачи MCP токена, подготовки runtime окружения, оценки runtime reuse, жизненного цикла interaction dispatch, обработки GitHub rate limit waits, warmup Mission Control, leasing и обновления состояния команд, выполнения next-step действий, обновления run status comments. fileciteturn19file0L1-L1
- **Agent-runner** (`services/jobs/agent-runner`) использует Control Plane как «центр состояния» (agent session upsert, events, resume payload, run status comment). fileciteturn20file0L1-L1
- В proto контракте явно указано, что некоторые RPC используются **api-gateway** для staff auth/OAuth callback (внутренний сервис/компонент платформы), что показывает зависимость сторонних компонентов от единого `ControlPlaneService`. fileciteturn17file0L1-L1

### Архитектурная схема текущего состояния (реконструкция)

```mermaid
flowchart LR
  subgraph CP[Control Plane (один сервис)]
    GRPC[gRPC: ControlPlaneService]
    HTTP[HTTP: /mcp, /metrics, /health*]
    DOM[Домены: staff, webhook, mcp, runtime-deploy, mission-control, github-rate-limit, governance, ...]
    DBL[(PostgreSQL: shared DB)]
  end

  subgraph EXT[Внешние системы]
    GH[GitHub API + Webhooks]
    K8S[Kubernetes API]
    REG[Internal Container Registry]
    PGADM[(Postgres Admin / lifecycle DB)]
  end

  subgraph JOBS[Jobs/Services]
    WRK[worker (dispatch, mission control, rate-limit, runtime)]
    AR[agent-runner (agent sessions, callbacks)]
    AGW[api-gateway (staff auth, OAuth callback)]
  end

  GH -->|webhook events| GRPC
  AGW -->|ResolveStaffByEmail / AuthorizeOAuthUser| GRPC
  WRK -->|IssueRunMCPToken / PrepareRunEnvironment / interactions / mission-control / rate-limit| GRPC
  AR -->|UpsertAgentSession / events / resume payload| GRPC

  DOM --> DBL
  DOM -->|GitHub ops| GH
  DOM -->|runtime deploy ops| K8S
  DOM -->|image ops| REG
  DOM -->|db lifecycle| PGADM

  HTTP --> WRK
```

Диаграмма опирается на фактическое наличие RPC для worker/agent-runner и на wiring сервисов/клиентов в `app.go`. fileciteturn11file0L1-L1 fileciteturn19file0L1-L1 fileciteturn20file0L1-L1 fileciteturn17file0L1-L1

## Домены, зависимости и shared DB

Ниже — срез по данным (таблицы) и тому, как домены «живут» вместе. Он важен, потому что именно модель данных и cross-domain зависимости обычно являются главным ограничителем при декомпозиции.

### Базовые домены «Runs» и событийность

- `agent_runs` и `flow_events` заложены как фундаментальные сущности: runs имеют `correlation_id`, статус, payload, learning_mode, timestamps; flow events ведут историю по correlation и типу события. fileciteturn34file0L1-L1  
  Перма‑ссылка: `https://github.com/codex-k8s/kodex/blob/main/services/internal/control-plane/cmd/cli/migrations/20260206191000_day1_webhook_ingest.sql#L1-L42`

- `agent_sessions` хранит «тяжёлые» JSON (session_json, codex_cli_session_json), а также связку run/project/repo + параметры запуска (branch/pr/template/model/reasoning_effort). fileciteturn41file0L1-L1  
  Перма‑ссылка: `https://github.com/codex-k8s/kodex/blob/main/services/internal/control-plane/cmd/cli/migrations/20260212113000_day9_agent_sessions.sql#L1-L42`

Эти сущности используются сразу несколькими доменами (agent callbacks, run status, interactions, rate limit resilience), что создаёт естественный «узел связности» вокруг `run_id`/`correlation_id`. fileciteturn11file0L1-L1

### Identity/Access и управление проектами

- Таблицы `users`, `projects`, `project_members` создают базовый RBAC и allowlist/ролей (read/read_write/admin) на уровень проекта. fileciteturn40file0L1-L1  
  Перма‑ссылка: `https://github.com/codex-k8s/kodex/blob/main/services/internal/control-plane/cmd/cli/migrations/20260209120000_day3_auth_rbac_users_projects.sql#L1-L41`

### Управление репозиториями, конфигами и токенами

- Хранение токенов: `platform_github_tokens` (singleton id=1) и `project_github_tokens` (по project_id), плюс репозиторные токены (через `repositories` и repository cfg repo). fileciteturn42file0L1-L1 fileciteturn67file0L1-L1
- `config_entries` — единый механизм секретов/переменных с scope (platform/project/repository), mutability (startup_required/runtime_mutable), sync_targets и флагом dangerous. fileciteturn44file0L1-L1
- Мульти-репо топология (alias, role, default_ref, docs_root_path) дополняет `repositories` и вводит ограничения уникальности alias в проекте. fileciteturn45file0L1-L1

Это домен «управления конфигурацией и доступом», который одновременно касается security (токены/секреты) и operational (preflight, webhook setup).

### MCP approvals и ожидания

- `mcp_action_requests` хранит запросы на действия инструментов с approval_mode/state и payload; он же расширяет `agent_sessions` wait_state/heartbeat/timeout_guard. fileciteturn43file0L1-L1

### Runtime deploy orchestration

- `runtime_deploy_tasks` — очередь/состояние задач подготовки окружения: lease_owner/lease_until, attempts, last_error, результат, а позже — cancel/stop семантика и terminal_status_source/event_seq. fileciteturn48file0L1-L1 fileciteturn49file0L1-L1  
  Перма‑ссылка: `https://github.com/codex-k8s/kodex/blob/main/services/internal/control-plane/cmd/cli/migrations/20260214193000_day14_runtime_deploy_tasks.sql#L1-L31`

### User interactions (dispatch/callback) и Telegram‑контур

- Базовые таблицы взаимодействий: `interaction_requests`, `interaction_delivery_attempts`, `interaction_callback_events`, `interaction_response_records` + ограничения «один open decision_request на run» и дополнительные wait‑поля в `agent_runs`. fileciteturn56file0L1-L1
- Telegram‑контур добавляет `interaction_channel_bindings` и `interaction_callback_handles`, расширяет state‑машины и хранит provider message refs. fileciteturn57file0L1-L1

### GitHub rate limit resilience

- `github_rate_limit_waits` и `github_rate_limit_wait_evidence` формализуют ожидания по лимитам, auto‑resume, manual action и аудит/доказательства (headers, retry_after, request_id и т.п.). fileciteturn60file0L1-L1  
  Эта модель напрямую влияет на статусы `agent_runs` (добавляется `waiting_backpressure`) и `wait_reason` (добавляется `github_rate_limit`). fileciteturn60file0L1-L1

### Mission Control (dashboard/workspace/commands)

- Основа: `mission_control_entities`, `mission_control_relations`, `mission_control_timeline_entries`, `mission_control_commands` с бизнес‑ключом intent, статусами и возможными approval‑состояниями. fileciteturn55file0L1-L1
- Lease‑механика команд: добавление `lease_owner/lease_until` и индекс «claimable». fileciteturn50file0L1-L1
- Graph extensions: continuity gaps и workspace watermarks (freshness/coverage/projection/launch_policy). fileciteturn52file0L1-L1

### System settings и runtime errors

- `system_settings` + `system_setting_changes`, versioning и seeded setting `github_rate_limit_wait_enabled`. fileciteturn66file0L1-L1
- `runtime_errors` фиксирует ошибки по source/level/run/project/namespace/job и «viewed» статус. fileciteturn63file0L1-L1

### Quality/Change governance

- Набор `change_governance_*` таблиц формирует отдельный крупный домен (packages, waves, evidence blocks, decisions, feedback, projection snapshots, artifact links). fileciteturn58file0L1-L1

**Вывод по данным:** большинство доменов имеют собственные таблицы и state‑машины, но связаны через общие ключи (`run_id`, `project_id`, `repository_full_name`, `correlation_id`) и обслуживаются одними и теми же процессами/миграциями, что делает «shared DB» главным фактором связности. fileciteturn11file0L1-L1

## Выявленные проблемы текущей архитектуры

Текущие проблемы сформулированы не как «общие слова про монолиты», а как конкретные наблюдения из кода/данных.

### Сверхширокая ответственность одного сервиса и одного API

Единый `ControlPlaneService` в proto объединяет несвязанные по жизненному циклу функции: от staff auth и CRUD пользователей до runtime deploy, mission control и governance. Это затрудняет независимую эволюцию API и приводит к тому, что любые изменения в одном домене потенциально требуют релиза всего Control Plane. fileciteturn17file0L1-L1

### «God composition root» и затруднённое разделение ownership

`internal/app/app.go` создаёт десятки репозиториев и доменных сервисов и соединяет их напрямую — включая кросс-доменные зависимости (например, GitHub rate-limit сервис использует `staffService` как `PlatformReplay`). Это прямой сигнал того, что ответственность «перетекает» между доменами, и ownership становится неочевидным. fileciteturn11file0L1-L1

### Shared DB как источник скрытых связей

Миграции показывают большое количество таблиц с пересекающимися ключами и общими статусными полями; при текущем подходе любой домен может начать выполнять запросы в «чужие» таблицы (даже если сейчас стараются держать репозитории отдельно). Долгосрочно это ведёт к tight coupling на уровне схемы и усложняет split‑brain/откат миграций. fileciteturn34file0L1-L1 fileciteturn55file0L1-L1 fileciteturn60file0L1-L1

### Разный профиль нагрузки и разные SLO в одном процессе

Домены имеют разный характер:
- runtime deploy — тяжёлые операции (Kubernetes apply/wait/logs, Registry API, lease workers), длинные таймауты, фоновые reconciler‑циклы; fileciteturn48file0L1-L1 fileciteturn11file0L1-L1
- staff/auth — «короткие» CRUD/авторизация и чувствительность к latency; fileciteturn40file0L1-L1 fileciteturn17file0L1-L1
- mission control/workspace — потенциально тяжёлые вычисления/проекции и запросы к большим наборам данных. fileciteturn55file0L1-L1 fileciteturn52file0L1-L1

В одном процессе эти профили конфликтуют (CPU/memory/io), а масштабирование «точечно» невозможно — приходится масштабировать весь Control Plane.

### Сложность корректного rollout’а и обратной совместимости

Так как worker и agent-runner активно вызывают Control Plane RPC, любое изменение контракта/семантики влияет на runtime контуры. fileciteturn19file0L1-L1 fileciteturn20file0L1-L1

## Рекомендации по рефакторингу в микросервисы

Ниже предложена **целевая декомпозиция** и «минимально жизнеспособные инкременты» для поэтапного выполнения. Ключевой принцип: **не дробить “по таблицам”**, а выделять сервисы по **bounded context** и по **схеме владения данными и бизнес-инвариантами**.

### Диаграмма границ контекстов (предлагаемая целевая модель)

```mermaid
flowchart TB
  subgraph Gateway[Control Plane Gateway (совместимость)]
    CPAPI[gRPC: ControlPlaneService v1]
  end

  subgraph Identity[Identity & Access Service]
    IAM[Users / RBAC / OAuth allowlist]
    IAMDB[(iam_db)]
  end

  subgraph Projects[Projects & Repo Mgmt Service]
    PRJ[Projects / Repositories / Config Entries / GitHub tokens]
    PRJDB[(projects_db)]
  end

  subgraph Runs[Run Orchestrator Service]
    RUNS[Webhook ingest / Run lifecycle / Flow events / Agent sessions]
    RUNSDB[(runs_db)]
  end

  subgraph Runtime[Runtime Deploy Orchestrator]
    RTD[Deploy tasks / reuse evaluation / registry maintenance]
    RTDDB[(runtime_db)]
  end

  subgraph Interactions[Interactions Service]
    INT[Dispatch queue / callbacks / adapters (Telegram)]
    INTDB[(interactions_db)]
  end

  subgraph Mission[Mission Control Service]
    MC[Graph/workspace, commands, projection]
    MCDB[(mission_control_db)]
  end

  subgraph Governance[Governance Service]
    GOV[Change/Quality governance projections]
    GOVDB[(governance_db)]
  end

  CPAPI --> IAM
  CPAPI --> PRJ
  CPAPI --> RUNS
  CPAPI --> RTD
  CPAPI --> INT
  CPAPI --> MC
  CPAPI --> GOV
```

Фасад `Control Plane Gateway` нужен, чтобы не ломать существующие клиентов worker/agent-runner/api-gateway на первом этапе: gateway сохраняет `ControlPlaneService`, но внутри начинает маршрутизировать RPC в новые сервисы. Такой «strangler» подход является стандартной стратегией снижения риска модернизации. citeturn4search0

### Кандидаты на микросервисы (конкретно)

| Микросервис | Ответственность (что «внутри») | Основные зависимости | Данные/схемы БД (владение) | Приоритет |
|---|---|---|---|---|
| Control Plane Gateway (compat) | Сохранить текущий `ControlPlaneService`; проксировать часть RPC в новые сервисы; централизовать authN/authZ и корреляцию | gRPC to internal services | Без собственной бизнес‑БД (только конфиг/route rules) | P0 |
| Runtime Deploy Orchestrator | `runtime_deploy_tasks`, lease/reconcile, build+deploy, registry cleanup, namespace lifecycle | Kubernetes API, Registry API, Runs service (read) | `runtime_deploy_tasks` (+ logs) как свои таблицы | P0 |
| Interactions Service | `interaction_requests/attempts/callbacks/responses`, channel bindings/handles (telegram), SLA/expiry | Worker (как dispatcher), Runs service (link), возможно внешние adapters | Все `interaction_*` таблицы — свои | P0 |
| Mission Control Service | `mission_control_*` entities/relations/timeline/commands + leases + continuity gaps + watermarks; API для workspace/snapshot/commands | GitHub provider (read), Runs/Projects (read), Worker (executor) | Все `mission_control_*` таблицы — свои | P1 |
| GitHub Rate Limit Service | `github_rate_limit_*` waits/evidence, auto-resume, policy, projection для run waits | GitHub API (частично), Runs, возможно RunStatus | Все `github_rate_limit_*` — свои | P1 |
| Run Orchestrator Service | Ingest GitHub webhooks, создание run, `flow_events`, `agent_sessions`, базовый run state machine, resume payload | GitHub webhooks, Projects/Repo service, Runtime deploy, Interactions | `agent_runs`, `flow_events`, `agent_sessions` — свои | P1 |
| Projects & Repo Mgmt Service | Projects/users membership (или связка с IAM), repos, config entries, tokens, preflight, webhook setup | GitHub API (mgmt), IAM service | `projects`, `repositories`, `config_entries`, `*_github_tokens` — свои | P2 |
| Governance Service | `change_governance_*` projections, evidence, decisions; API для staff/mission control | Runs, Projects/Repo | `change_governance_*` — свои | P2 |

Обоснование приоритетов опирается на текущие «тяжёлые» домены с очередями/lease/state machines (runtime deploy и interactions) и на то, что они уже имеют явные таблицы и процессы, что облегчает выделение. fileciteturn48file0L1-L1 fileciteturn56file0L1-L1

### Предлагаемые API/контракты и минимальные инкременты

**Инкремент 1 (P0): выделить Runtime Deploy Orchestrator**  
Почему: это отдельная очередь (`runtime_deploy_tasks`) с lease’ами и длительными операциями, а также явные RPC `PrepareRunEnvironment`, `EvaluateRuntimeReuse`, `List/Get/Cancel/StopRuntimeDeployTask`. fileciteturn48file0L1-L1 fileciteturn17file0L1-L1  
Минимальный контракт:
- gRPC `RuntimeDeployService` (новый): `Prepare`, `EvaluateReuse`, `GetTask`, `ListTasks`, `RequestAction`.
- События (event bus): `runtime.task.updated`, `runtime.task.completed`, `runtime.task.failed`.
Минимальная миграция:
- Перенести владение таблицей `runtime_deploy_tasks` и её миграциями в новый сервис (на первом шаге — хотя бы в отдельную схему/роль в том же Postgres).

**Инкремент 2 (P0): выделить Interactions Service**  
Почему: есть отдельный набор таблиц и сложная state machine delivery/callback/expiry; worker уже дергает `ClaimNextInteractionDispatch`, `CompleteInteractionDispatch`, `ExpireNextInteraction`, а также callback endpoints завязаны на токены и provider payload. fileciteturn56file0L1-L1 fileciteturn57file0L1-L1 fileciteturn19file0L1-L1  
Минимальный контракт:
- gRPC `InteractionsService`: `ClaimDispatch`, `CompleteDispatch`, `SubmitCallback`, `ExpireDue`.
- Webhook/HTTP для adapter callbacks (Telegram) — либо в этом сервисе, либо в отдельном «telegram-adapter».
События:
- `interaction.created`, `interaction.delivery.attempted`, `interaction.resolved`, `interaction.expired` (для Runs/agent-runner resume).

**Инкремент 3 (P1): Mission Control Service**  
Почему: отдельные таблицы + команда/lease модель + граф/workspace. Сильная польза от независимого масштабирования (read-heavy). fileciteturn55file0L1-L1 fileciteturn52file0L1-L1  
Контракт:
- Read API: snapshot/workspace/node/activity.
- Command API: submit/claim/queue/reconcile/fail.
- Интеграция с worker через идемпотентные команды и события.

## Стратегии разделения данных, миграции и согласованность

С точки зрения данных у вас есть два реальных «поля боя»: (1) как физически разделить БД, (2) как сохранять согласованность между сервисами без распределённых транзакций (2PC).

### Сравнение вариантов разделения данных

| Вариант | Суть | Плюсы | Минусы | Когда выбирать |
|---|---|---|---|---|
| Shared DB (как сейчас) | Один набор таблиц, доступны всем доменам | Быстро, простые join’ы, ACID внутри одной БД | Максимальная связность, сложно выделять ownership, риск «ползучих» зависимостей, тяжёлый откат миграций | Только как временный этап |
| Shared Postgres, schema-per-service | Один Postgres кластер, но отдельные схемы и отдельные DB роли; запрет cross-schema writes | Дешевле, чем раздельные DB; проще миграции и permission boundaries | Всё ещё общий blast radius на уровне кластера; cross-service join’ы запрещены и требуют read models | Хороший прагматичный шаг P0–P1 |
| Database-per-service | Отдельная БД (или отдельный database в кластере) на сервис | Лучшее разделение ownership, независимые миграции и tuning, снижение blast radius | Сложнее интеграция (только события/API), нужна стратегия согласованности, выше операционная нагрузка | Целевая модель для зрелых доменов |
| Hybrid | Критичные сервисы — отдельные DB, остальные — schema-per-service | Баланс стоимости/риска | Усложнение ландшафта данных | Если нужно «быстро снизить риск», но не перегрузить DevOps |

Для надёжной межсервисной согласованности предпочтительны **event-driven** подходы, где каждое изменение данных сопровождается публикацией события через outbox. Это помогает избежать проблемы «dual write» (БД + брокер/события) и гарантировать «commit → event». citeturn3search2 citeturn3search1

### Транзакции и согласованность между сервисами

Рекомендованный базовый набор паттернов:

- **Transactional Outbox** в каждом сервисе, где изменения бизнес‑данных должны порождать события для других сервисов. citeturn3search2  
  Практически: таблица `outbox_events` (id, aggregate_type, aggregate_id, event_type, payload_json, created_at, published_at, attempt_count…), отдельный publisher (sidecar/job) публикует в брокер и отмечает `published_at`.

- **Saga** для длительных процессов, где нужно связать несколько локальных транзакций (например: webhook → run created → runtime deploy → issue status comment → interaction request → resume). В случае ошибок — компенсирующие действия (cancel deploy, mark run failed, release leases). citeturn3search3

- **Идемпотентность потребителей**: события и callbacks могут приходить повторно; это уже отражено в некоторых таблицах (уникальные ключи на signal_id, correlation_id, business_intent_key и т.п.), и это стоит сделать системно для всех событий. fileciteturn60file0L1-L1 fileciteturn55file0L1-L1

### Поток данных текущего жизненного цикла run (реконструкция)

```mermaid
sequenceDiagram
  autonumber
  participant GH as GitHub (webhook)
  participant CP as Control Plane
  participant DB as Postgres (shared)
  participant RT as Runtime deploy loop
  participant WRK as worker
  participant AR as agent-runner
  participant INT as Interactions
  GH->>CP: IngestGitHubWebhook(event)
  CP->>DB: create agent_run + flow_event
  CP->>CP: RunStatus init / labels / comment scheduling
  WRK->>CP: IssueRunMCPToken
  WRK->>CP: PrepareRunEnvironment (enqueue runtime_deploy_task)
  CP->>DB: runtime_deploy_tasks(status=pending, lease=...)
  RT->>DB: claim task lease; build/deploy via Kubernetes
  RT->>DB: update task status + logs
  AR->>CP: UpsertAgentSession / InsertRunFlowEvent
  CP->>DB: agent_sessions + flow_events
  CP->>GH: update run status comment / labels
  CP->>DB: interaction_requests (если нужен ответ)
  WRK->>CP: ClaimNextInteractionDispatch / CompleteInteractionDispatch
  CP->>DB: interaction_delivery_attempts / callbacks / effective_response
  CP->>AR: provide resume payload (interaction/rate-limit)
```

Фактическое наличие этих вызовов подтверждается proto RPC и worker/agent-runner клиентами, а также таблицами runtime deploy и interactions. fileciteturn17file0L1-L1 fileciteturn19file0L1-L1 fileciteturn20file0L1-L1 fileciteturn48file0L1-L1 fileciteturn56file0L1-L1

## DevOps/CI/CD и наблюдаемость для новой архитектуры

Чтобы декомпозиция не создала «зоопарк сервисов без операционной дисциплины», нужно заранее задать стандарт «как выглядит production-ready сервис».

### Деплоймент и конфигурация

Сейчас Control Plane ожидает, что readiness DB обеспечивается initContainer, и стартует fail-fast при проблемах соединения. fileciteturn11file0L1-L1  
Для микросервисов рекомендовано:

- **Единый шаблон Helm/Kustomize**: liveness/readiness, HPA, PodDisruptionBudget, network policies, service accounts, resource limits.
- **Секреты**: разделить секреты по доменам (GitHub tokens, encryption keys, MCP signing keys). В текущем Control Plane токены и ключи находятся в одном env‑контракте, что усложняет least privilege. fileciteturn15file0L1-L1
- **Отдельные DB роли** даже при schema-per-service: запрет `CREATE/ALTER` на runtime роли; миграции выполняются job’ом с elevated правами.

### Наблюдаемость (логирование/метрики/трассировка)

В Control Plane присутствует Prometheus `/metrics` и регистрация кастомного collector’а для interactions. fileciteturn11file0L1-L1  
Для распределённой архитектуры добавить:

- **Корреляция**: стандартизировать `correlation_id` как trace attribute и как поле structured log во всех сервисах (она уже является ключом run/flow_events). fileciteturn34file0L1-L1
- **Distributed tracing (OpenTelemetry)**: обязательное распространение tracecontext через gRPC/HTTP, чтобы видеть end-to-end жизненный цикл run.
- **Метрики очередей/lease**: depth очереди (`runtime_deploy_tasks` pending/running), lease contention, retry rates, latency по ключевым RPC.
- **SLO по доменам**: например, «webhook ingest p95», «prepare runtime p95», «interaction dispatch success rate», «mission control snapshot latency».

### CI/CD

- Разнести пайплайны сборки образов и релизы по сервисам.
- Ввести «contract tests» для gateway↔service и для внешних клиентов. Для gRPC это удобно через golden-файлы/прото‑совместимость.

## План работ, оценка рисков и откат

Этот план сознательно «поэтапный» и совместим с Strangler Fig: старая система остаётся рабочей, новые сервисы постепенно берут responsibility. citeturn4search0

### Риски и стратегии смягчения

- **Риск расхождения данных при split DB**: смягчается через outbox + idempotent consumers; это стандартный способ избежать dual write проблемы. citeturn3search2
- **Риск усложнения транзакций** (например, run wait states зависят от approvals/interactions/rate limit): использовать Saga и компенсирующие операции вместо 2PC. citeturn3search3
- **Риск деградации latency из-за сетевых вызовов**: начинать со schema-per-service (в одном Postgres) до полной database-per-service; применять кэширование read-models для mission control.
- **Риск ломки клиентов worker/agent-runner**: удерживать стабильный `ControlPlaneService` через gateway на первых этапах. fileciteturn17file0L1-L1 fileciteturn19file0L1-L1

### План этапов (примерная оценка сложности и критерии готовности)

| Этап | Содержание | Сложность | Критерии готовности (DoD) |
|---|---|---|---|
| Foundation | Ввести gateway‑слой маршрутизации RPC внутри Control Plane (пока в том же репо), единые middleware: auth, correlation, tracing hooks | M | Gateway проксирует хотя бы 1 группу RPC; есть smoke‑тесты и метрики |
| Runtime Deploy extraction | Вынести runtime deploy в отдельный сервис (сначала schema-per-service), перевести worker→gateway→runtime | L | `Prepare/Evaluate/Cancel/Stop` работают, lease/очередь стабильны, есть runbook отката |
| Interactions extraction | Вынести interactions + Telegram контур в отдельный сервис, перевести worker dispatch на него | L | Доставка/коллбеки идемпотентны, expiry работает, метрики по успеху доставок |
| Mission Control extraction | Вынести mission control read API + command leasing в отдельный сервис; worker остаётся executor’ом действий | L | Workspace/snapshot p95 в пределах SLO, команды не теряются, leases корректны |
| GitHub rate-limit extraction | Вынести waits/evidence/auto-resume; согласовать wait_projection для run view | M | Auto-resume стабилен, ручные сценарии документированы |
| Runs & Webhook orchestration extraction | Вынести ingress webhooks + run lifecycle + sessions/events | XL | Нет потери run/event, backfill инструменты и миграции, SLO ingest |
| Governance extraction | Вынести change governance | M | Проекции консистентны, API стабилен, нет cross-service joins |
| Projects/Repo/IAM extraction | Развести IAM и project/repo mgmt | XL | Least privilege по токенам, независимые миграции, совместимость с api-gateway |

### План отката (rollback) — практический

- Для каждого вынесенного сервиса сохранять **двухконтурную маршрутизацию** в gateway: флагом переключать RPC «на старую реализацию» или «на новую».
- На уровне данных:  
  - при schema-per-service — rollback проще (отключить новый сервис, вернуть gateway route);  
  - при отдельной БД — держать «dual read»/«shadow read» на период стабилизации и иметь backfill‑скрипты.
- Для событий (outbox): потребители должны быть идемпотентны и выдерживать повторную доставку.

---

**Резюме ключевого вывода:** Control Plane уже содержит несколько зрелых bounded contexts с собственными моделями данных и state machines (runtime deploy, interactions, mission control, rate-limit, governance). Самая безопасная стратегия — не «переписывать всё», а последовательно выносить эти контуры в отдельные сервисы через gateway‑фасад, одновременно вводя outbox+events и saga‑подход для согласованности. citeturn4search0 citeturn3search2 citeturn3search3