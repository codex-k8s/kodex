---
doc_id: API-CK8S-PLATFORM-MCP-0001
type: api-contract
title: kodex — API-обзор platform-mcp-server
status: active
owner_role: SA
created_at: 2026-05-14
updated_at: 2026-05-25
related_issues: [747, 753, 760, 771, 780, 322]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-14-platform-mcp-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-14
---

# API-обзор platform-mcp-server

## TL;DR

- Тип API: MCP-поверхность для инструментов и будущий внутренний gRPC для служебного управления, если он понадобится.
- Аутентификация: проверенный actor/source/run/session/slot binding и сервисная идентичность внутри платформы.
- Версионирование: MCP-инструменты версионируются через Go-регистрацию, JSON Schema входов и snapshot-проверку `tools/list`, отдельно от внутренних gRPC-контрактов сервисов-владельцев.
- Основные поверхности: инструменты agent-manager, инструменты governance, инструменты provider, чтения project/runtime/fleet/package, запросы interaction delivery и диагностика.

## Источники правды

Канонический контракт MCP-инструментов строится из Go-регистрации инструментов через официальный MCP Go SDK, JSON Schema входов и snapshot-проверок ответа `tools/list`. Отдельный YAML-каталог не является источником правды для MCP или hooks.

Этот документ фиксирует верхнеуровневую карту и смысл контрактов. Кодовые срезы должны создать:

- Go-описания MCP-инструментов и типизированные обработчики;
- JSON Schema для входов инструментов;
- snapshot-тесты `tools/list`;
- внутренние client interfaces к сервисам-владельцам;
- тесты совместимости маршрутизации, очистки данных и обратной совместимости MCP-поверхности.

В сервисном каркасе активны диагностический инструмент `diagnostics.mcp_status.read`, первый набор инструментов `agent-manager` и provider-инструменты для уже реализованной поверхности `provider-hub`. Остальные инструменты из этого документа остаются целевой картой и добавляются отдельными срезами после готовности контрактов и бизнес-реализации сервисов-владельцев.

При детализации контрактов использовать внешние источники:

- OpenAI Codex hooks: command-обработчики Codex из `hooks.json` или `config.toml`, JSON-вход события и stdout/stderr ответ обработчика.
- Model Context Protocol Go SDK: `mcp.Server`, `mcp.Tool`, typed tool handlers, `CallToolRequest`, `CallToolResult` и поддерживаемые transport.

## Разделение MCP-инструментов и Codex hooks

Codex hooks не являются прямыми MCP-вызовами инструментов. Codex настраивает hooks как command-обработчики в `hooks.json`, `config.toml` или управляемых requirements; обработчик запускается в рабочей директории сессии, получает JSON-вход события и возвращает обычный или JSON-ответ через stdout/stderr.

Поэтому hook-события принимает отдельный `codex-hook-ingress`. Hook emitter или локальный sidecar нормализует вход Codex hook, очищает данные и отправляет безопасный envelope в этот ingress. `platform-mcp-server` остаётся только MCP-сервером.

MCP-инструменты реализуются отдельно через MCP Go SDK: сервер регистрирует `mcp.Tool`, типизированный обработчик принимает `CallToolRequest` и возвращает `CallToolResult`; транспорт может быть streamable HTTP или другой поддержанный транспорт SDK. Эти детали должны учитываться в контрактном срезе, но API-обзор не фиксирует конкретную Go-реализацию.

## Общий envelope MCP-вызова

Каждый вызов инструмента должен содержать:

| Поле | Назначение |
|---|---|
| `tool_name` | Устойчивое имя инструмента. |
| `tool_version` | Версия внешней MCP-поверхности. |
| `request_id` | Уникальный идентификатор конкретного вызова. |
| `correlation_id` | Сквозная связь с run, slot, provider artifact или approval. |
| `actor` | Пользователь, сервисный аккаунт, agent-manager или slot-agent. |
| `source` | Тип и экземпляр источника вызова. |
| `scope` | Организация, проект, репозиторий, provider target или package scope. |
| `run_context` | `agent_run_id`, `session_id`, `slot_id`, если применимо. |
| `idempotency_key` | Ключ повторяемости для изменяющих вызовов. |
| `policy_context` | Риск-класс и режим решения: allow, deny, ask или record-only. |
| `approval_gate_ref` | Ссылка на gate, если вызов требует решения человека. |
| `retention_class` | Класс хранения результата и диагностической сводки. |
| `payload` | Типизированные данные конкретного инструмента после проверки размера. |

Свободный JSON допустим только для ограниченных диагностических сценариев после нормализации. Инструменты записи провайдера, инструменты агентов и runtime-команды должны быть типизированными.

## MVP: инструменты

### Инструменты agent-manager

| Инструмент | Владелец | Назначение |
|---|---|---|
| `agent.session.start` | `agent-manager` | Начать или продолжить агентную сессию. |
| `agent.run.start` | `agent-manager` | Запустить роль или stage-run через доменный сервис. |
| `agent.run.record_state` | `agent-manager` | Зафиксировать изменение состояния run. |
| `agent.session.record_snapshot` | `agent-manager` | Записать ссылку на session snapshot без передачи большого файла через MCP. |
| `agent.acceptance.request` | `agent-manager` | Запросить машинную приёмку. |
| `agent.follow_up.request` | `agent-manager` | Создать намерение follow-up задачи. |
| `agent.gate.request` | `agent-manager` | Зафиксировать ожидание flow и запросить governance gate; gate request/decision хранится у `governance-manager`. |
| `agent.gate.submit_decision` | `agent-manager` | Передать ссылку на resolved governance decision для продолжения flow; само решение не создаётся в `agent-manager`. |

MCP не хранит состояние этих операций. Если нужна оценка риска или решение gate/release, `agent-manager` обращается к `governance-manager`; доставка человеку и callback выполняются через `interaction-hub`.

### Состояние реализации инструментов agent-manager

| Инструмент | Статус реализации | Правило |
|---|---|---|
| `agent.session.start` | готово | Маршрутизируется в `AgentManagerService.StartAgentSession`; MCP не хранит session state. |
| `agent.run.start` | готово | Маршрутизируется в `StartAgentRun`; MCP передаёт только типизированные ссылки и подсказки. |
| `agent.run.record_state` | готово | Маршрутизируется в `RecordRunState`; состояние проверяет и меняет только `agent-manager`. |
| `agent.session.record_snapshot` | готово | Маршрутизируется в `RecordSessionStateSnapshot`; MCP передаёт только `object ref`, не содержимое session snapshot. |
| `diagnostics.run_context.read` | готово | Безопасно читает сессию и агентные запуски через `GetAgentSession`/`ListAgentRuns`; используется для диагностики ожиданий flow без хранения данных в MCP. |
| `agent.acceptance.request` | запланировано | Не регистрируется в MCP, пока `agent-manager` не реализует машину приёмки. |
| `agent.follow_up.request` | запланировано | Не регистрируется в MCP, пока `agent-manager` не реализует follow-up intent. |
| `agent.gate.request` / `agent.gate.submit_decision` | запланировано | Не регистрируется в MCP, пока не готовы связка `agent-manager` + `governance-manager` + `interaction-hub`. |

### Инструменты governance

| Инструмент | Владелец | Назначение |
|---|---|---|
| `governance.risk.evaluate` | `governance-manager` | Запросить оценку риска для transition, PR/MR, release candidate, job или policy change. |
| `governance.signal.record_review` | `governance-manager` | Передать role-driven review signal от reviewer, QA, lexical gatekeeper, SRE, security или custom role. |
| `governance.gate.request` | `governance-manager` | Создать gate request с evidence package и ссылкой на delivery request при необходимости. |
| `governance.gate.submit_decision` | `governance-manager` | Зафиксировать human или policy decision после проверки actor policy. |
| `governance.release.prepare_decision_package` | `governance-manager` | Собрать пакет релизного решения из project/provider/runtime/agent refs. |
| `governance.release.submit_decision` | `governance-manager` | Зафиксировать release go/no-go/hold/rollback/follow-up decision. |

Инструменты governance не принимают свободный provider diff или секреты. Для project/release policy используются refs из `project-catalog`, для provider refs — `provider-hub`, для delivery callback — `interaction-hub`.

### Инструменты provider

| Инструмент | Владелец | Назначение |
|---|---|---|
| `provider.issue.create` | `provider-hub` | Создать provider-native `Issue`. |
| `provider.issue.update` | `provider-hub` | Обновить provider-native `Issue`. |
| `provider.pull_request.create` | `provider-hub` | Создать `PR/MR`. |
| `provider.pull_request.update` | `provider-hub` | Обновить `PR/MR`. |
| `provider.comment.create` | `provider-hub` | Создать комментарий. |
| `provider.comment.update` | `provider-hub` | Обновить комментарий. |
| `provider.review_signal.create` | `provider-hub` | Оставить review-сигнал. |
| `provider.relationship.update` | `provider-hub` | Обновить provider-native связь. |
| `provider.artifact_signal.register` | `provider-hub` | Ускорить сверку после работы slot-агента или agent-manager. |
| `provider.projection.get` | `provider-hub` | Прочитать локальную проекцию `Issue`/`PR/MR`. |
| `provider.projection.find` | `provider-hub` | Найти локальную проекцию по provider-native ссылке. |
| `provider.projections.list` | `provider-hub` | Получить список локальных проекций по фильтрам. |
| `provider.comments.list` | `provider-hub` | Прочитать безопасные сводки комментариев, упоминаний и review-сигналов. |
| `provider.relationships.list` | `provider-hub` | Прочитать provider-native связи. |
| `provider.repository.create` | `provider-hub` | Создать provider-native репозиторий через выбранный внешний аккаунт. |
| `provider.repository.bootstrap_pull_request.create` | `provider-hub` | Создать или обновить bootstrap branch/PR по готовым файлам и refs. |
| `provider.repository.adoption_pull_request.create` | `provider-hub` | Создать или обновить adoption branch/PR по готовым файлам и refs. |

Provider-инструменты не принимают токены и не вызывают GitHub/GitLab напрямую. `external_account_id`, `operation_policy_context` и `approval_gate_ref` передаются в `provider-hub`, который применяет свой write pipeline.

`provider.artifact_signal.register` в MCP не принимает `payload_json` или другой свободный JSON. MCP-поверхность передаёт в `provider-hub` только типизированные поля сигнала, а payload оставляет пустым. Если позже для этого инструмента потребуется дополнительный signal payload, он добавляется отдельной типизированной структурой входа с JSON Schema и лимитами размера.

Каждый вызов provider-инструмента должен нести actor/source/correlation context и route metadata для учёта количества запросов к внешнему провайдеру. Если позже появится управляемый proxy для `gh` или другой CLI провайдера, он должен передавать те же метаданные в `provider-hub`, а не вести отдельный учёт.

### Состояние реализации инструментов provider

| Инструмент | Статус реализации | Правило |
|---|---|---|
| `provider.projection.get` | готово | Маршрутизируется в `ProviderHubService.GetWorkItemProjection`; MCP возвращает только безопасную сводку проекции. |
| `provider.projection.find` | готово | Маршрутизируется в `FindWorkItemByProviderRef`; MCP не ходит в провайдера напрямую. |
| `provider.projections.list` | готово | Маршрутизируется в `ListWorkItemProjections`; тело work item и сырой provider payload не возвращаются. |
| `provider.comments.list` | готово | Маршрутизируется в `ListComments`; возвращаются digest и короткая безопасная сводка, не полный комментарий. |
| `provider.relationships.list` | готово | Маршрутизируется в `ListRelationships`; связи остаются состоянием `provider-hub`. |
| `provider.artifact_signal.register` | готово | Маршрутизируется в `RegisterProviderArtifactSignal`; MCP-вход не принимает raw JSON payload и передаёт только типизированные поля сигнала. |
| `provider.issue.create` / `provider.issue.update` | готово | Маршрутизируются в `CreateIssue`/`UpdateIssue`; write pipeline, идемпотентность, внешний аккаунт и ссылки на policy проверяет `provider-hub`. |
| `provider.comment.create` / `provider.comment.update` | готово | Маршрутизируются в `CreateComment`/`UpdateComment`; MCP не хранит body после вызова и не возвращает его в ответе. |
| `provider.pull_request.create` / `provider.pull_request.update` | готово | Маршрутизируются в `CreatePullRequest`/`UpdatePullRequest`; рискованные операции должны приходить с `approval_gate_ref`, если policy context требует approval. |
| `provider.review_signal.create` | готово | Маршрутизируется в `CreateReviewSignal`; inline comments передаются типизированно. |
| `provider.relationship.update` | готово | Маршрутизируется в `UpdateRelationship`; MCP не становится владельцем связей. |
| `provider.repository.create` | готово | Маршрутизируется в `CreateRepository`; base/default branch и результат на стороне провайдера возвращаются как безопасные ссылки. |
| `provider.repository.bootstrap_pull_request.create` | готово | Маршрутизируется в `CreateBootstrapPullRequest`; файлы должны быть подготовлены вне MCP. |
| `provider.repository.adoption_pull_request.create` | готово | Маршрутизируется в `CreateAdoptionPullRequest`; сканирование существующего репозитория не входит в MCP. |
| webhook/reconciliation/limit инструменты | запланировано | Не регистрируются в MCP-4: это отдельная операционная поверхность и не инструменты чтения или записи provider-данных для work items. |

### Project, runtime, fleet и package reads

| Группа | Владелец | Примеры |
|---|---|---|
| `project.*` | `project-catalog` | Прочитать проект, репозиторий, workspace policy, release policy, placement policy. |
| `runtime.*` | `runtime-manager` | Прочитать slot, job, workspace materialization, short status. |
| `fleet.*` | `fleet-manager` | Прочитать cluster health, placement decision, fleet scope. |
| `package.*` | `package-hub` | Прочитать package, installation, manifest, guidance package. |

В MVP эти группы ориентированы на чтения и безопасные команды, необходимые агентам. Изменяющие операции добавляются отдельными срезами только после явной политики доступа и доменного контракта владельца.

Минимальный набор точных имён чтения:

| Инструмент | Владелец |
|---|---|
| `project.policy.read` | `project-catalog` |
| `project.repository.read` | `project-catalog` |
| `runtime.slot.read` | `runtime-manager` |
| `runtime.job.read` | `runtime-manager` |
| `fleet.cluster_health.read` | `fleet-manager` |
| `package.installation.read` | `package-hub` |
| `package.manifest.read` | `package-hub` |

### Инструменты interaction

| Инструмент | Владелец | Назначение |
|---|---|---|
| `interaction.feedback.request` | `interaction-hub` | Запросить обратную связь владельца. |
| `interaction.approval.request` | `interaction-hub` | Запросить доставку approval/gate. |
| `interaction.delivery.status_read` | `interaction-hub` | Прочитать статус доставки запроса. |

До готовности `interaction-hub` эти инструменты остаются контрактным заделом. MCP не реализует доставку уведомлений сам.

### Diagnostics

| Инструмент | Назначение |
|---|---|
| `diagnostics.mcp_status.read` | Readiness MCP-сервера, версия MCP-регистрации инструментов и состояние маршрутов. |
| `diagnostics.dependency_status.read` | Ограниченный статус зависимостей без секретов и больших логов. |
| `diagnostics.run_context.read` | Безопасная сводка по run/session/slot, если actor имеет право. |
| `diagnostics.last_errors.read` | Короткий bounded tail ошибок по маршруту или группе инструментов. |

Диагностика не заменяет `operations-hub` и не становится хранилищем логов.

## Модель ошибок

| Ошибка | Когда возвращается |
|---|---|
| `mcp.invalid_context` | Нет обязательного actor/source/run/slot/scope контекста или поля несовместимы. |
| `mcp.tool_not_found` | Инструмент не зарегистрирован в текущей MCP-регистрации. |
| `mcp.tool_not_allowed` | Policy boundary запретил инструмент для источника или роли. |
| `mcp.payload_rejected` | Данные вызова слишком большие, содержат запрещённые поля или не прошли sanitization. |
| `mcp.owner_unavailable` | Сервис-владелец недоступен или вернул retryable ошибку. |
| `mcp.owner_rejected` | Сервис-владелец отклонил команду по доменным правилам. |
| `mcp.idempotency_conflict` | Повторный command id пришёл с другим безопасным отпечатком входа. |
| `mcp.rate_limited` | Превышен лимит actor/source/tool group. |

## Совместимость

- Новые инструменты добавляются через Go-регистрацию MCP-инструмента и snapshot-проверку `tools/list`.
- Изменение обязательных полей envelope требует новой версии группы инструментов.
- Внутренний gRPC контракт владельца может развиваться независимо, если MCP-caster сохраняет прежнюю внешнюю форму.
- Удаление инструмента проходит через период устаревания и явное изменение MCP-регистрации.
- Каждый новый инструмент должен получить JSON Schema входа, тест валидного вызова, тест отказа политики или тест обратной совместимости, если меняется существующая группа.

## Апрув

- request_id: `owner-2026-05-14-platform-mcp-kickoff`
- Решение: approved
- Комментарий: API-обзор `platform-mcp-server` согласован как целевое состояние.
