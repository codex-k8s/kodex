---
doc_id: API-CK8S-AGENT-ORCHESTRATION-0001
type: api-contract
title: kodex — API-обзор agent-manager
status: active
owner_role: SA
created_at: 2026-05-12
updated_at: 2026-05-15
related_issues: [733, 739, 744, 753, 755, 698]
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

- Тип API: внутренний gRPC `AgentManagerService`, доменные события `agent.*`, MCP-инструменты через `platform-mcp-server`, Codex hook events через `codex-hook-ingress`.
- Аутентификация: gateway, MCP или сервисный токен; доменные команды дополнительно проверяются через `access-manager`.
- Версионирование: стабильное транспортное пространство имён `kodex.agents.v1`.
- Основные операции: flow, role, prompt template, session, run, acceptance и follow-up.

## Спецификации

- gRPC proto: `proto/kodex/agents/v1/agent_manager.proto`.
- Сгенерированный Go-контракт: `proto/gen/go/kodex/agents/v1/**`.
- AsyncAPI: `specs/asyncapi/agent-manager.v1.yaml`.
- Сгенерированные Go-контракты событий: `libs/go/platformevents/agent/events.gen.go`.
- MCP-инструменты: публикуются через `platform-mcp-server` и маршрутизируются к `agent-manager`.
- Codex hook events: приходят через `codex-hook-ingress`, а не через MCP tools.
- Внешний HTTP для пользовательской и операторской консоли: через профильный gateway, не напрямую из доменного сервиса.

Этот документ является обзором целевого API. Машинные спецификации являются источником правды для транспорта, а документ должен обновляться в том же PR при расхождении.

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
| `GetRoleProfile` | gRPC query | `agent.role.read` | нет | Читает профиль роли. |
| `ListRoleProfiles` | gRPC query | `agent.role.read` | нет | Список ролей по scope/kind/status. |
| `GetPromptTemplate` | gRPC query | `agent.prompt.read` | нет | Читает метаданные prompt template и активную версию без обхода роли. |
| `ListPromptTemplates` | gRPC query | `agent.prompt.read` | нет | Список prompt template по роли и назначению. |
| `CreatePromptTemplateVersion` | gRPC command | `agent.prompt.manage` | `command_id` | Создаёт версию prompt для роли по `source_ref`, объектной ссылке и digest без передачи свободного текста prompt в события. |
| `ActivatePromptTemplateVersion` | gRPC command | `agent.prompt.manage` | `command_id` + expected version | Активирует prompt version для новых запусков. |
| `GetPromptTemplateVersion` | gRPC query | `agent.prompt.read` | нет | Читает одну версию prompt. |
| `ListPromptTemplateVersions` | gRPC query | `agent.prompt.read` | нет | Список версий prompt по роли, назначению и статусу. |
| `StartAgentSession` | gRPC command | `agent.session.start` | `command_id` | Создаёт или продолжает сессию по пользовательскому запросу или provider target. |
| `StartAgentRun` | gRPC command | `agent.run.start` | `command_id` | Создаёт `Run`, фиксирует версии flow/stage/role/prompt, разрешает guidance hints через `package-hub` и запрашивает runtime; вызывающая сторона не передаёт готовые runtime refs или сырые guidance refs. |
| `RecordRunState` | gRPC command | `agent.run.update` | `command_id` + expected version | Фиксирует переход `Run` после сигнала от runtime, MCP-инструмента или `codex-hook-ingress`. |
| `RecordSessionStateSnapshot` | gRPC command | `agent.session.update` | `command_id` + expected version | Записывает метаданные Codex session JSON/JSONL в объектном хранилище и обновляет указатель на актуальный снимок сессии. |
| `RequestAcceptance` | gRPC command | `agent.acceptance.run` | `command_id` | Запускает машину приёмки по session/run/stage. |
| `RecordAcceptanceResult` | gRPC command | `agent.acceptance.update` | `command_id` + expected version | Фиксирует результат проверки. |
| `GetAcceptanceResult` | gRPC query | `agent.acceptance.read` | нет | Читает один результат приёмки. |
| `ListAcceptanceResults` | gRPC query | `agent.acceptance.read` | нет | Список результатов приёмки по session/run/stage/status. |
| `CreateFollowUpIntent` | gRPC command | `agent.follow_up.create` | `command_id` | Формирует намерение следующей provider-native задачи. |
| `RequestHumanGate` | gRPC command | `agent.human_gate.request` | `command_id` | Создаёт запрос решения через `interaction-hub`. |
| `GetAgentSession` | gRPC query | `agent.session.read` | нет | Читает сессию. |
| `ListAgentRuns` | gRPC query | `agent.run.read` | нет | Читает запуски по session/status/provider target. |

## Инструменты MCP

`platform-mcp-server` должен предоставлять типизированные инструменты, которые маршрутизируются в `agent-manager`:

| Инструмент | Назначение |
|---|---|
| `agent.session.start` | Начать или продолжить агентную сессию по пользовательскому запросу. |
| `agent.run.start` | Запустить роль в рамках session/stage. |
| `agent.run.record_state` | Принять результат от ролевого агента или runner. |
| `agent.session.record_snapshot` | Зафиксировать ссылку на актуальный Codex session state без передачи содержимого JSON через MCP. |
| `agent.acceptance.request` | Запустить машинную приёмку. |
| `agent.follow_up.request` | Сформировать следующий provider-native `Issue`. |
| `agent.gate.request` | Запросить gate или решение человека через `interaction-hub`. |
| `agent.gate.submit_decision` | Передать решение, полученное из UI или внешнего канала. |

MCP-инструменты не должны принимать свободный JSON для provider-операций. Если нужно создать `Issue`, комментарий или `PR/MR`, инструмент вызывает `provider-hub` через типизированный provider-контракт.

## Codex hook events

Codex hooks не являются MCP-инструментами. `agent-manager` получает их только после нормализации во входном контуре `codex-hook-ingress`.

| Hook event | Как влияет на `agent-manager` |
|---|---|
| `SessionStart` | Создаёт или связывает Codex-сессию с существующим `AgentSession` и `Run`. |
| `UserPromptSubmit` | Фиксирует безопасный факт нового пользовательского ввода и связывает его с session/run context. |
| `PreToolUse` | Даёт сигнал намерения вызвать инструмент; может привести к gate или realtime-событию, но не заменяет MCP tool call. |
| `PermissionRequest` | Преобразуется в human gate или запрос решения через `interaction-hub`. |
| `PostToolUse` | Передаёт безопасный итог инструмента, provider artifact signal или bounded error. |
| `Stop` | Фиксирует контрольную точку хода, pending actions и безопасную итоговую сводку. |

Контрольные точки сжатия контекста и session snapshot остаются внутренними событиями `agent-manager`/`runtime-manager`. Они не описываются как Codex hooks и не проходят через `platform-mcp-server`.

## Интеграции с другими сервисами

| Сервис | Вызовы из `agent-manager` | Правило |
|---|---|---|
| `package-hub` | `ListPackageInstallations(package_kind=guidance)`, `GetPackageManifest` | Только чтение руководящих пакетов и manifest. |
| `runtime-manager` | `PrepareRuntime`, будущие команды запуска или продолжения slot-agent | Состояние runtime остаётся у runtime. |
| `provider-hub` | Типизированные операции `CreateIssue`, `UpdateIssue`, `CreatePullRequest`, `CreateComment`, `CreateReviewSignal`, чтение проекций и ускоряющий сигнал сверки | Provider-native состояние остаётся у provider. |
| `project-catalog` | Чтение workspace policy, release policy, project/repository refs | Проектная policy остаётся у project. |
| `access-manager` | Проверка действий, ролей, аккаунтов и scope | `agent-manager` не вычисляет права сам. |
| `interaction-hub` | Запрос Human gate, обратной связи и уведомления | Диалог и доставка остаются у interaction. |
| `codex-hook-ingress` | Нормализованные Codex hook events: lifecycle, permission, tool result и stop summary | Hook transport и очистка входа остаются у hook ingress; `agent-manager` хранит только своё состояние. |

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
| gRPC proto | Подготовлен как контрактный срез `AGO-1`. |
| AsyncAPI `agent.*` | Подготовлен как контрактный срез `AGO-1`. |
| Go-реализация `agent-manager` | Сервисный каркас готов. Операции flow, role и prompt подключены к storage/use-case слою через gRPC handlers; session, run, acceptance, follow-up и human gate пока остаются зарегистрированными контрактами без бизнес-реализации. |
| Интеграция с `package-hub` | Зафиксирована как чтение guidance installations и manifest. |
| Интеграция с runtime/provider/interaction/hooks | Зафиксирована как междоменная граница без реализации. |

## Совместимость

- `v1` контракт должен покрыть согласованный объём доменного API, даже если реализация поставляется по срезам.
- Если контракт опережает реализацию, delivery-документ фиксирует реализованные и отложенные операции.
- События должны проектироваться так, чтобы переход с PostgreSQL event log на брокер не ломал payload.
- `Run` должен сохранять immutable-ссылки и версии flow/stage/role/prompt/guidance, включая digest роли и prompt, чтобы новая версия конфигурации не меняла старые результаты.

## Апрув

- request_id: `owner-2026-05-12-agent-manager-kickoff`
- Решение: approved
- Комментарий: API-обзор `agent-manager` согласован как стартовое целевое состояние; proto и AsyncAPI зафиксированы контрактным срезом.
