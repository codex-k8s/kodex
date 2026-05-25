---
doc_id: DLV-CK8S-AGENT-MANAGER
type: delivery-plan
title: kodex — поставка agent-manager
status: active
owner_role: EM
created_at: 2026-05-12
updated_at: 2026-05-22
related_issues: [733, 739, 744, 749, 755, 759, 772, 322]
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

`agent-manager` поставляется малыми срезами: сначала доменный пакет документации, затем транспортные контракты, сервисный каркас, модель flow/role/prompt, сессии и `Run`, машина приёмки, follow-up задачи и интеграции с package/runtime/provider/governance/interaction контурами.

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
| AGO-6 | не назначено | Интеграция с `runtime-manager`: подготовка workspace и запуск роли через runtime-контур готовы. |
| AGO-7 | не назначено | Машина приёмки: проверка provider-native артефактов, watermark, ролей и policy готова. |
| AGO-8 | не назначено | Follow-up задачи через `provider-hub`; ожидание Human gate идёт через `governance-manager`, delivery — через `interaction-hub`. |
| AGO-9 | не назначено | Эксплуатационный контур `agent-manager`: deploy manifests, migration job, smoke-проверки и runbook готовы. |

## Статус операций `AgentManagerService`

| Операция | Текущий статус | Плановый срез |
|---|---|---|
| `CreateFlow` / `UpdateFlow` / `CreateFlowVersion` / `ActivateFlowVersion` | storage/use-case слой и gRPC handlers готовы | AGO-3, AGO-3b |
| `GetFlow` / `ListFlows` | storage/use-case слой и gRPC handlers готовы; `GetFlow` возвращает активную версию при наличии `active_version_id` | AGO-3, AGO-3b |
| `CreateRoleProfile` / `UpdateRoleProfile` / `GetRoleProfile` / `ListRoleProfiles` | storage/use-case слой и gRPC handlers готовы | AGO-3, AGO-3b |
| `GetPromptTemplate` / `ListPromptTemplates` | storage/use-case слой и gRPC handlers готовы; `GetPromptTemplate` возвращает активную версию при наличии `active_version_id` | AGO-3, AGO-3b |
| `CreatePromptTemplateVersion` / `ActivatePromptTemplateVersion` / `GetPromptTemplateVersion` / `ListPromptTemplateVersions` | storage/use-case слой и gRPC handlers готовы | AGO-3, AGO-3b |
| `StartAgentSession` | Слой хранения, use-case и gRPC handlers готовы; создаёт авторитетную сессию, а при непустом provider target продолжает активную `open`/`waiting` session без нового события создания | AGO-4 |
| `StartAgentRun` | Слой хранения, use-case и gRPC handlers готовы; создаёт `requested` `Run`, фиксирует версии роли и prompt, проверяет stage-bound связку flow/stage/role, разрешает guidance hints через `package-hub` и сохраняет безопасные refs; runtime подключается следующим срезом | AGO-4, AGO-5, AGO-6 |
| `RecordRunState` | Слой хранения, use-case и gRPC handlers готовы; требует ожидаемую версию, проверяет state machine, пишет результат команды и публикует lifecycle event только с обязательными полями AsyncAPI | AGO-4, AGO-6 |
| `RecordSessionStateSnapshot` | Слой хранения, use-case и gRPC handlers готовы; пишет метаданные снимка и обновляет указатель сессии через ожидаемую версию | AGO-4, AGO-6 |
| `RequestAcceptance` / `RecordAcceptanceResult` / `GetAcceptanceResult` / `ListAcceptanceResults` | зарегистрировано в gRPC-каркасе; бизнес-реализация запланирована | AGO-7 |
| `CreateFollowUpIntent` | зарегистрировано в gRPC-каркасе; бизнес-реализация запланирована | AGO-8 |
| `RequestHumanGate` | зарегистрировано в gRPC-каркасе как flow-level ожидание; бизнес-реализация должна делегировать gate request/decision в `governance-manager` | AGO-8 |
| `GetAgentSession` / `ListAgentRuns` | Слой хранения, use-case и gRPC handlers готовы; `GetAgentSession` возвращает последний снимок при наличии указателя | AGO-4 |

## Синхронизация с параллельными доменами

| Домен | Когда синхронизироваться | Причина |
|---|---|---|
| `package-hub` | Готово для AGO-5 | Используются чтения установок, package/version metadata и manifest validation state; `agent-manager` не хранит manifest payload и не меняет установки. |
| `runtime-manager` | Перед AGO-6 | Нужен контракт подготовки workspace, запуска слота и передачи `agent_run_id`. |
| `provider-hub` | Перед AGO-7 и AGO-8 | Нужны проекции `Issue/PR/MR`, ускоряющие сигналы сверки и типизированные provider-операции. |
| `risk-and-release-governance` | Перед AGO-7 и AGO-8 | Нужны контракты risk assessment, review signals, gate request и gate decision refs. |
| `interaction-hub` | Перед AGO-8 | Нужен delivery/callback контракт для Human gate и запросов обратной связи без владения decision state. |
| `project-catalog` | Перед AGO-5 и AGO-6 | Нужны workspace policy, project/repository refs и release/risk policy. |
| `access-manager` | Перед открытием команд через gateway/MCP и перед AGO-7/AGO-8 | Действия доступа заведены в AGO-1; AGO-4 не обходит будущие сервисные проверки, но не реализует полноценный контур авторизации команд. |
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
