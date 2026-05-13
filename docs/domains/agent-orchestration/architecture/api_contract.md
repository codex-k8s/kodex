---
doc_id: API-CK8S-AGENT-ORCHESTRATION-0001
type: api-contract
title: kodex — API-обзор agent-manager
status: active
owner_role: SA
created_at: 2026-05-12
updated_at: 2026-05-13
related_issues: [733]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-12-agent-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-12
---

# API-обзор: agent-manager

## TL;DR

- Тип API: будущий внутренний gRPC `AgentManagerService`, доменные события `agent.*`, MCP-инструменты через `platform-mcp-server`.
- Аутентификация: gateway, MCP или сервисный токен; доменные команды дополнительно проверяются через `access-manager`.
- Версионирование: транспортный `v1` будет создан отдельным контрактным срезом; этот документ фиксирует целевую карту операций без proto.
- Основные операции: flow, role, prompt template, session, run, acceptance и follow-up.

## Спецификации

- gRPC proto: будет создан как `proto/kodex/agents/v1/agent_manager.proto`.
- AsyncAPI: будет создан как `specs/asyncapi/agent-manager.v1.yaml`.
- MCP-инструменты: публикуются через `platform-mcp-server` и маршрутизируются к `agent-manager`.
- Внешний HTTP для пользовательской и операторской консоли: через профильный gateway, не напрямую из доменного сервиса.

Этот документ является обзором целевого API. Когда появятся proto и AsyncAPI, машинные спецификации станут источником правды для транспорта, а документ должен обновляться в том же PR при расхождении.

## Операции

| Операция | Вид | Доступ | Идемпотентность | Примечание |
|---|---|---|---|---|
| `CreateFlow` | gRPC command | `agent.flow.manage` | `CommandMeta.command_id` | Создаёт flow в scope. |
| `UpdateFlow` | gRPC command | `agent.flow.manage` | `command_id` + expected version | Меняет отображаемые метаданные flow, не активную immutable-версию. |
| `CreateFlowVersion` | gRPC command | `agent.flow.manage` | `command_id` | Создаёт новую версию flow из определения. |
| `ActivateFlowVersion` | gRPC command | `agent.flow.manage` | `command_id` + expected version | Делает версию активной для новых запусков. |
| `GetFlow` | gRPC query | `agent.flow.read` | нет | Читает flow и активную версию. |
| `ListFlows` | gRPC query | `agent.flow.read` | нет | Список flow по scope/status. |
| `CreateRoleProfile` | gRPC command | `agent.role.manage` | `command_id` | Создаёт роль агента. |
| `UpdateRoleProfile` | gRPC command | `agent.role.manage` | `command_id` + expected version | Меняет профиль роли и доступные MCP-инструменты. |
| `CreatePromptTemplateVersion` | gRPC command | `agent.prompt.manage` | `command_id` | Создаёт версию prompt для роли. |
| `ActivatePromptTemplateVersion` | gRPC command | `agent.prompt.manage` | `command_id` + expected version | Активирует prompt version для новых запусков. |
| `StartAgentSession` | gRPC command | `agent.session.start` | `command_id` | Создаёт или продолжает сессию по пользовательскому запросу или provider target. |
| `StartAgentRun` | gRPC command | `agent.run.start` | `command_id` | Создаёт `Run`, фиксирует `flow_version_id`, `stage_id`, `role_profile_id`, `role_profile_version`, `role_profile_digest`, `prompt_template_version_id`, `prompt_template_digest`, guidance refs и запрашивает runtime. |
| `RecordRunState` | gRPC command | `agent.run.update` | `command_id` + expected version | Фиксирует переход `Run` после сигнала от runtime, MCP или агента. |
| `RecordSessionStateSnapshot` | gRPC command | `agent.session.update` | `command_id` + expected version | Записывает метаданные Codex session JSON/JSONL в объектном хранилище и обновляет указатель на актуальный снимок сессии. |
| `RequestAcceptance` | gRPC command | `agent.acceptance.run` | `command_id` | Запускает машину приёмки по session/run/stage. |
| `RecordAcceptanceResult` | gRPC command | `agent.acceptance.update` | `command_id` + expected version | Фиксирует результат проверки. |
| `CreateFollowUpIntent` | gRPC command | `agent.follow_up.create` | `command_id` | Формирует намерение следующей provider-native задачи. |
| `RequestHumanGate` | gRPC command | `agent.human_gate.request` | `command_id` | Создаёт запрос решения через `interaction-hub`. |
| `GetAgentSession` | gRPC query | `agent.session.read` | нет | Читает сессию. |
| `ListAgentRuns` | gRPC query | `agent.run.read` | нет | Читает запуски по session/status/provider target. |

## Инструменты MCP

`platform-mcp-server` должен предоставлять типизированные инструменты, которые маршрутизируются в `agent-manager`:

| Инструмент | Назначение |
|---|---|
| `agent.start_session` | Начать или продолжить агентную сессию по пользовательскому запросу. |
| `agent.start_role_run` | Запустить роль в рамках session/stage. |
| `agent.record_run_result` | Принять результат от ролевого агента или runner. |
| `agent.record_session_snapshot` | Зафиксировать ссылку на актуальный Codex session state без передачи содержимого JSON через MCP. |
| `agent.request_acceptance` | Запустить машинную приёмку. |
| `agent.request_follow_up` | Сформировать следующий provider-native `Issue`. |
| `agent.ask_owner` | Запросить решение человека через `interaction-hub`. |

MCP-инструменты не должны принимать свободный JSON для provider-операций. Если нужно создать `Issue`, комментарий или `PR/MR`, инструмент вызывает `provider-hub` через типизированный provider-контракт.

## Интеграции с другими сервисами

| Сервис | Вызовы из `agent-manager` | Правило |
|---|---|---|
| `package-hub` | `ListPackageInstallations(package_kind=guidance)`, `GetPackageManifest` | Только чтение руководящих пакетов и manifest. |
| `runtime-manager` | `PrepareRuntime`, будущие команды запуска или продолжения slot-agent | Состояние runtime остаётся у runtime. |
| `provider-hub` | Типизированные операции `CreateIssue`, `UpdateIssue`, `CreatePullRequest`, `CreateComment`, `CreateReviewSignal`, чтение проекций и ускоряющий сигнал сверки | Provider-native состояние остаётся у provider. |
| `project-catalog` | Чтение workspace policy, release policy, project/repository refs | Проектная policy остаётся у project. |
| `access-manager` | Проверка действий, ролей, аккаунтов и scope | `agent-manager` не вычисляет права сам. |
| `interaction-hub` | Запрос Human gate, обратной связи и уведомления | Диалог и доставка остаются у interaction. |

## Модель ошибок

| Ошибка | Когда возвращается |
|---|---|
| `invalid_argument` | Невалидный flow, stage, role, prompt, transition, provider target или request context. |
| `permission_denied` | `access-manager` запретил действие или роль не имеет нужного MCP-инструмента. |
| `not_found` | Flow, роль, prompt, session, run или acceptance result не найдены. |
| `already_exists` | Дубликат slug или повтор создания активной сущности в scope. |
| `failed_precondition` | Нельзя запустить роль без prompt, workspace policy, provider target или обязательного решения. |
| `aborted` | Конфликт expected version или устаревший `Run` state. |
| `unavailable` | Временная ошибка package, runtime, provider, interaction или event log. |

## События

| Событие | Когда публикуется |
|---|---|
| `agent.session.created` | Создана новая агентная сессия. |
| `agent.session.updated` | Изменился текущий этап или статус сессии. |
| `agent.run.requested` | Запрошен ролевой запуск. |
| `agent.run.started` | Runtime подтвердил старт или подготовку. |
| `agent.run.waiting` | Запуск ожидает человека, runtime, provider или retry. |
| `agent.run.completed` | Ролевой запуск завершён. |
| `agent.run.failed` | Ролевой запуск завершился ошибкой. |
| `agent.session.snapshot_recorded` | Зафиксирован новый снимок Codex session state. |
| `agent.acceptance.requested` | Запрошена машинная приёмка. |
| `agent.acceptance.completed` | Приёмка завершилась успешно. |
| `agent.acceptance.failed` | Приёмка обнаружила блокеры или ошибку. |
| `agent.follow_up.requested` | Нужно создать или обновить follow-up `Issue`. |
| `agent.follow_up.created` | Follow-up provider-native задача создана или подтверждена. |
| `agent.human_gate.requested` | Требуется решение человека. |
| `agent.human_gate.resolved` | Решение человека получено. |
| `agent.flow.version_activated` | Активирована версия flow. |
| `agent.role.version_activated` | Активирована версия роли. |
| `agent.prompt.version_activated` | Активирована версия prompt. |

## Состояние реализации

| Область | Статус |
|---|---|
| Доменная документация | Подготовлена как стартовый срез. |
| gRPC proto | Запланирован отдельным контрактным срезом. |
| AsyncAPI `agent.*` | Запланирован отдельным контрактным срезом. |
| Go-реализация `agent-manager` | Не начата в этом срезе. |
| Интеграция с `package-hub` | Зафиксирована как чтение guidance installations и manifest. |
| Интеграция с runtime/provider/interaction | Зафиксирована как междоменная граница без реализации. |

## Совместимость

- `v1` контракт должен покрыть согласованный объём доменного API, даже если реализация поставляется по срезам.
- Если контракт опережает реализацию, delivery-документ фиксирует реализованные и отложенные операции.
- События должны проектироваться так, чтобы переход с PostgreSQL event log на брокер не ломал payload.
- `Run` должен сохранять immutable-ссылки и версии flow/stage/role/prompt/guidance, включая digest роли и prompt, чтобы новая версия конфигурации не меняла старые результаты.

## Апрув

- request_id: `owner-2026-05-12-agent-manager-kickoff`
- Решение: approved
- Комментарий: API-обзор `agent-manager` согласован как стартовое целевое состояние; proto и AsyncAPI создаются отдельным срезом.
