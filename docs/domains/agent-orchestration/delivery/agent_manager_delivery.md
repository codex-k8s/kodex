---
doc_id: DLV-CK8S-AGENT-MANAGER
type: delivery-plan
title: kodex — поставка agent-manager
status: active
owner_role: EM
created_at: 2026-05-12
updated_at: 2026-05-27
related_issues: [733, 739, 744, 749, 755, 759, 772, 322, 782, 795, 809, 820, 834, 842, 862]
related_prs: []
related_docsets:
  - docs/domains/agent-orchestration/product/requirements.md
  - docs/domains/agent-orchestration/architecture/design.md
  - docs/domains/agent-orchestration/architecture/data_model.md
  - docs/domains/agent-orchestration/architecture/api_contract.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-12-agent-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-12
---

# Поставка agent-manager

## TL;DR

`agent-manager` поставляется малыми срезами: сначала доменный пакет документации, затем транспортные контракты, сервисный каркас, модель flow/role/prompt, сессии и `Run`, интеграции с package/runtime, машина приёмки, follow-up задачи и связи с provider/governance/interaction контурами.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования домена | `docs/domains/agent-orchestration/product/requirements.md` |
| Дизайн домена | `docs/domains/agent-orchestration/architecture/design.md` |
| Модель данных | `docs/domains/agent-orchestration/architecture/data_model.md` |
| API-обзор | `docs/domains/agent-orchestration/architecture/api_contract.md` |
| Карта Issue | `docs/delivery/issue-map/domains/agent-orchestration.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---|---|
| AGO-0 | #733 | Доменная документация, границы `agent-manager`, модель данных, API-обзор, план поставки и карты связей готовы. |
| AGO-1 | #739 | gRPC и AsyncAPI контракты `agent-manager`, события `agent.*` и действия доступа готовы; сервисная реализация не входит в срез. |
| AGO-2 | #744 | Сервисный процесс, env-конфигурация, health, readiness, metrics, регистрация gRPC `AgentManagerService` и outbox-каркас готовы; бизнес-операции возвращают `Unimplemented`, outbox не имитирует успешную доставку. |
| AGO-3 | #749 | PostgreSQL-модель flow, stage, role, prompt template, версий, command result и service-local outbox готова; storage/use-case слой подключён к process readiness. |
| AGO-3b | #755 | gRPC handlers, casters и безопасное отображение ошибок для flow, role и prompt подключены к готовому storage/use-case слою; session/run не входят в срез. |
| AGO-4 | #759 | Сессии и agent `Run`: создание, чтение, статусы, снимки состояния, идемпотентность, защита активной session от дублей, stage-bound проверка роли и AsyncAPI-совместимые события готовы. |
| AGO-5 | #772 | Интеграция с `package-hub` для guidance packages готова: `agent-manager` выбирает установки, проверяет manifest/version metadata и фиксирует refs/summary в `Run` без checkout/mount. |
| AGO-6 | #782 | Контекст руководящих пакетов в workspace готов: зафиксирован MVP-путь передачи замороженных `guidance_refs` в `runtime-manager` как источников `guidance_package`, локальные пути `.kodex/guidance/<safe_local_name>`, проверка идентичности источника через `package-hub` и граница без checkout из `agent-manager`. |
| AGO-7 | #795 | Интеграция `StartAgentRun` с `project-catalog.GetWorkspacePolicy` и `runtime-manager.PrepareRuntime` готова: workspace request собирается из project/source refs, role/run context и `guidance_refs`; `Run` фиксирует только runtime refs, fingerprint/summary и безопасную классификацию ошибок. До появления deploy wiring для `agent-manager` подготовка runtime включается явно через `KODEX_AGENT_MANAGER_RUNTIME_PREPARATION_ENABLED=true`. |
| AGO-8 | #809 | Базовая machine acceptance готова: `agent-manager` создаёт pending acceptance result по session/run/stage, записывает `passed`/`failed`/`waiting`/`skipped` через ожидаемую версию, ограничивает `target_ref` safe-ref форматом, хранит только safe refs/status/bounded details и публикует service-local outbox events без QA runner, Human gate decision, provider write pipeline и хранения raw payload. Для `human_gate` фиксируется только `waiting` с gate/risk/governance ref. |
| AGO-9a | #820 | Follow-up intent lifecycle готов в границах `agent-manager`: команда `CreateFollowUpIntent` сохраняет session/run/stage/acceptance refs, provider target refs, тип следующей provider-native задачи, safe title/summary/hints, idempotency trace и статус, публикует `agent.follow_up.requested` без provider write. |
| AGO-9b | #834 | Safe activity timeline готова: `agent-manager` хранит canonical persistent историю действий по session/run, операции `RecordAgentActivity` и `ListAgentActivities`, PostgreSQL-модель, idempotency, safe guards и gRPC handler без raw tool payload, stdout/stderr, prompt, transcript, provider payload или workspace paths. |
| AGO-9c | #842 | Интеграция follow-up intent с create-path provider-командой готова: `DispatchFollowUpIntent` переводит `planned/requested` intent через expected version, перед provider write атомарно резервирует dispatch локальным bump версии и deterministic provider command ref, вызывает только `provider-hub.CreateIssue`, сохраняет `provider_operation_ref`, safe result refs и статус `created`/`failed`, публикует `agent.follow_up.created`/`agent.follow_up.failed` без прямого GitHub/GitLab доступа из `agent-manager`. |
| AGO-9c.1 | #862 | Follow-up dispatch приведён к целевой typed-модели: `DispatchFollowUpIntent` принимает явный `FollowUpDispatchKind` и typed `oneof` для `create_issue`, `update_issue`, `create_comment` или `update_comment`, резервирует dispatch до provider write, вызывает только соответствующие typed команды `provider-hub`, сохраняет `provider_operation_ref`, safe result refs и статус `created`/`updated`/`commented`/`failed`, публикует `agent.follow_up.created`/`agent.follow_up.updated`/`agent.follow_up.commented`/`agent.follow_up.failed`. `UpdatePullRequest` и `CreateReviewSignal` вынесены в следующий явный срез. |
| AGO-9d | не назначено | Ожидание Human gate через `governance-manager`, delivery — через `interaction-hub`; `agent-manager` хранит только ожидание flow и refs. |
| AGO-10 | не назначено | Эксплуатационный контур `agent-manager`: deploy manifests, migration job, smoke-проверки и runbook готовы. |

## Статус операций `AgentManagerService`

| Операция | Текущий статус | Плановый срез |
|---|---|---|
| `CreateFlow` / `UpdateFlow` / `CreateFlowVersion` / `ActivateFlowVersion` | storage/use-case слой и gRPC handlers готовы | AGO-3, AGO-3b |
| `GetFlow` / `ListFlows` | storage/use-case слой и gRPC handlers готовы; `GetFlow` возвращает активную версию при наличии `active_version_id` | AGO-3, AGO-3b |
| `CreateRoleProfile` / `UpdateRoleProfile` / `GetRoleProfile` / `ListRoleProfiles` | storage/use-case слой и gRPC handlers готовы | AGO-3, AGO-3b |
| `GetPromptTemplate` / `ListPromptTemplates` | storage/use-case слой и gRPC handlers готовы; `GetPromptTemplate` возвращает активную версию при наличии `active_version_id` | AGO-3, AGO-3b |
| `CreatePromptTemplateVersion` / `ActivatePromptTemplateVersion` / `GetPromptTemplateVersion` / `ListPromptTemplateVersions` | storage/use-case слой и gRPC handlers готовы | AGO-3, AGO-3b |
| `StartAgentSession` | Слой хранения, use-case и gRPC handlers готовы; создаёт авторитетную сессию, а при непустом provider target продолжает активную `open`/`waiting` session без нового события создания | AGO-4 |
| `StartAgentRun` | Слой хранения, use-case и gRPC handlers готовы; создаёт `Run`, фиксирует версии роли и prompt, проверяет stage-bound связку flow/stage/role, разрешает guidance hints через `package-hub`; при включённом `KODEX_AGENT_MANAGER_RUNTIME_PREPARATION_ENABLED` читает workspace policy у `project-catalog`, вызывает `runtime-manager.PrepareRuntime` и сохраняет только runtime refs, fingerprint/diagnostic summary и безопасный статус подготовки | AGO-4, AGO-5, AGO-6, AGO-7 |
| `RecordRunState` | Слой хранения, use-case и gRPC handlers готовы; требует ожидаемую версию, проверяет state machine, пишет результат команды и публикует lifecycle event только с обязательными полями AsyncAPI | AGO-4, AGO-6 |
| `RecordSessionStateSnapshot` | Слой хранения, use-case и gRPC handlers готовы; пишет метаданные снимка и обновляет указатель сессии через ожидаемую версию | AGO-4, AGO-6 |
| `RequestAcceptance` / `RecordAcceptanceResult` / `GetAcceptanceResult` / `ListAcceptanceResults` | Слой хранения, use-case и gRPC handlers готовы; request создаёт один pending check за команду, record требует expected version и принимает только bounded safe `target_ref`/`details_json`; `human_gate` переводится только в `waiting` с gate/risk/governance ref; outbox публикует requested/completed/failed события | AGO-8 |
| `CreateFollowUpIntent` | Слой хранения, use-case и gRPC handler готовы; команда валидирует session/run/stage/acceptance связи, поддерживает idempotency replay и conflict для отличающегося payload, хранит только safe provider refs/title/summary/hints/digest/status и публикует `agent.follow_up.requested` без provider write | AGO-9a |
| `DispatchFollowUpIntent` | Слой хранения, use-case, typed provider-hub client и gRPC handler готовы; команда требует expected version, поддерживает idempotency replay/conflict, резервирует dispatch до provider write, принимает `FollowUpDispatchKind` + typed `oneof`, вызывает `provider-hub.CreateIssue`/`UpdateIssue`/`CreateComment`/`UpdateComment`, обновляет intent до `created`/`updated`/`commented`/`failed` и хранит только safe operation/result refs | AGO-9c, AGO-9c.1 |
| `RecordAgentActivity` / `ListAgentActivities` | Слой хранения, use-case и gRPC handlers готовы; record поддерживает idempotency replay/conflict и safe-storage guards для activity kind/status/tool metadata/timestamps/summary/digest/refs/details; list читает timeline по session/run с фильтрами и cursor pagination | AGO-9b |
| `RequestHumanGate` | зарегистрировано в gRPC-каркасе как flow-level ожидание; бизнес-реализация должна делегировать gate request/decision в `governance-manager` | AGO-9d |
| `GetAgentSession` / `ListAgentRuns` | Слой хранения, use-case и gRPC handlers готовы; `GetAgentSession` возвращает последний снимок при наличии указателя | AGO-4 |

## Синхронизация с параллельными доменами

| Домен | Когда синхронизироваться | Причина |
|---|---|---|
| `package-hub` | Готово для AGO-5 | Используются чтения установок, package/version metadata и manifest validation state; `agent-manager` не хранит manifest payload и не меняет установки. |
| `runtime-manager` | Готово для AGO-7 | `agent-manager` вызывает `PrepareRuntime(agent_run_id, workspace policy, runtime profile, placement constraints)` при явно включённой runtime preparation и не материализует workspace сам. |
| `provider-hub` | Готово для AGO-9c.1 | `agent-manager` вызывает typed `CreateIssue`, `UpdateIssue`, `CreateComment` и `UpdateComment`, хранит только safe operation/result refs; provider write pipeline и адаптеры остаются у `provider-hub`. `UpdatePullRequest` и `CreateReviewSignal` требуют отдельного typed dispatch-среза. |
| `risk-and-release-governance` | Перед расширением AGO-8 и AGO-9 | Базовый AGO-8 хранит только risk/gate refs и статусы ожидания; для решений нужны контракты risk assessment, review signals, gate request и gate decision refs. |
| `interaction-hub` | Перед AGO-9d | Нужен delivery/callback контракт для Human gate и запросов обратной связи без владения decision state. |
| `codex-hook-ingress` | После AGO-9b | Persistent timeline готова в `agent-manager`; следующий CHI-срез должен маршрутизировать sanitized `PreToolUse`/`PostToolUse` в `RecordAgentActivity`, сохраняя `codex-hook-ingress` как sanitizer/router/realtime ops feed без долгого хранения tool calls. |
| `project-catalog` | Готово для AGO-7 | `agent-manager` читает проверенную workspace policy и использует project/repository refs без владения проектной политикой. |
| `access-manager` | Перед открытием команд через gateway/MCP | Действия доступа заведены в AGO-1; текущие gRPC use-case не обходят будущие сервисные проверки, но не реализуют полноценный контур авторизации команд. |
| `platform-mcp-server` | Перед AGO-5 и далее | MCP-0 зафиксировал границы и MVP-группы инструментов. Для реальных вызовов session, `Run` и gate нужен следующий контрактный и сервисный срез MCP, но `agent-manager` остаётся владельцем состояния. |

## Критерии начала кода

- Принят пакет доменной документации `agent-orchestration`.
- Для каждого кодового PR есть отдельный GitHub Issue.
- Контрактный PR создаёт proto и AsyncAPI до реализации бизнес-операций.
- Старый код из `deprecated/**` не используется как основа реализации.
- Соседние доменные контракты не обходятся локальными заглушками без отдельного согласования.

## Критерии завершения домена

- `agent-manager` имеет свой контур данных, миграций, контрактов и событий.
- Flow, stage, role, prompt, session, run, acceptance и follow-up имеют авторитетные команды и чтения.
- Сервис публикует `agent.*` события через outbox и `platform-event-log`.
- `package-hub`, `runtime-manager`, `provider-hub`, `governance-manager`, `interaction-hub`, `project-catalog`, `access-manager` и `platform-mcp-server` связаны через согласованные контракты.
- Документы и карты Issue обновлены, хвосты перенесены в следующие срезы явно.

## Апрув

- request_id: `owner-2026-05-12-agent-manager-kickoff`
- Решение: approved
- Комментарий: план поставки `agent-manager` согласован как стартовое целевое состояние.
