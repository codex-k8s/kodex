---
doc_id: API-CK8S-PLATFORM-MCP-0001
type: api-contract
title: kodex — API-обзор platform-mcp-server
status: active
owner_role: SA
created_at: 2026-05-14
updated_at: 2026-05-22
related_issues: [747, 753, 760]
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
- Основные поверхности: agent-manager tools, provider tools, project/runtime/fleet/package reads, interaction requests и diagnostics.

## Источники правды

Канонический контракт MCP-инструментов строится из Go-регистрации tools через официальный MCP Go SDK, JSON Schema входов и snapshot-проверок ответа `tools/list`. Отдельный YAML-каталог не является источником правды для MCP или hooks.

Этот документ фиксирует верхнеуровневую карту и смысл контрактов. Кодовые срезы должны создать:

- Go-описания MCP tools и typed handlers;
- JSON Schema для входов инструментов;
- snapshot-тесты `tools/list`;
- внутренние client interfaces к сервисам-владельцам;
- тесты совместимости маршрутизации, очистки данных и обратной совместимости MCP-поверхности.

В начальном сервисном каркасе активен только диагностический инструмент `diagnostics.mcp_status.read`: он показывает версию регистрации, список зарегистрированных tools и безопасную сводку маршрутов к сервисам-владельцам без секретов и бизнес-данных. Остальные инструменты из этого документа остаются целевой картой и добавляются отдельными срезами после готовности контрактов сервисов-владельцев.

При детализации контрактов использовать внешние источники:

- OpenAI Codex hooks: command-обработчики Codex из `hooks.json` или `config.toml`, JSON-вход события и stdout/stderr ответ обработчика.
- Model Context Protocol Go SDK: `mcp.Server`, `mcp.Tool`, typed tool handlers, `CallToolRequest`, `CallToolResult` и поддерживаемые transport.

## Разделение MCP tools и Codex hooks

Codex hooks не являются прямыми MCP tool calls. Codex настраивает hooks как command-обработчики в `hooks.json`, `config.toml` или управляемых requirements; обработчик запускается в рабочей директории сессии, получает JSON-вход события и возвращает обычный или JSON-ответ через stdout/stderr.

Поэтому hook-события принимает отдельный `codex-hook-ingress`. Hook emitter или локальный sidecar нормализует вход Codex hook, очищает данные и отправляет безопасный envelope в этот ingress. `platform-mcp-server` остаётся только MCP-сервером.

MCP tools реализуются отдельно через MCP Go SDK: сервер регистрирует `mcp.Tool`, typed handler принимает `CallToolRequest` и возвращает `CallToolResult`; transport может быть streamable HTTP или другой поддержанный SDK транспорт. Эти детали должны учитываться в контрактном срезе, но API-обзор не фиксирует конкретную Go-реализацию.

## Общий envelope MCP-вызова

Каждый tool call должен содержать:

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

Свободный JSON допустим только для ограниченных диагностических сценариев после нормализации. Provider write tools, agent tools и runtime commands должны быть типизированными.

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
| `agent.gate.request` | `agent-manager` | Запросить gate или approval через agent-домен. |
| `agent.gate.submit_decision` | `agent-manager` | Передать решение, полученное из UI или внешнего канала. |

MCP не хранит состояние этих операций. Если нужна доставка решения человеку, `agent-manager` обращается к `interaction-hub`.

### Provider tools

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
| `provider.artifact.signal` | `provider-hub` | Ускорить сверку после работы slot-агента. |
| `provider.projection.read` | `provider-hub` | Прочитать локальную проекцию `Issue`/`PR/MR`/comment/relationship. |
| `provider.bootstrap.request` | `provider-hub` | Зарезервированный маршрут bootstrap/adoption через `provider-hub`; активируется только после утверждения отдельного bootstrap/adoption контракта. |

Provider-инструменты не принимают токены и не вызывают GitHub/GitLab напрямую. `external_account_id`, `operation_policy_context` и `approval_gate_ref` передаются в `provider-hub`, который применяет свой write pipeline.

Каждый provider tool call должен нести actor/source/correlation context и route metadata для учёта количества запросов к внешнему провайдеру. Если позже появится управляемый proxy для `gh` или другой CLI провайдера, он должен передавать те же метаданные в `provider-hub`, а не вести отдельный учёт.

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

### Interaction tools

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

- Новые инструменты добавляются через Go-регистрацию MCP tool и snapshot-проверку `tools/list`.
- Изменение обязательных полей envelope требует новой версии группы инструментов.
- Внутренний gRPC контракт владельца может развиваться независимо, если MCP-caster сохраняет прежнюю внешнюю форму.
- Удаление инструмента проходит через период устаревания и явное изменение MCP-регистрации.
- Каждый новый инструмент должен получить JSON Schema входа, тест валидного вызова, тест отказа политики или тест обратной совместимости, если меняется существующая группа.

## Апрув

- request_id: `owner-2026-05-14-platform-mcp-kickoff`
- Решение: approved
- Комментарий: API-обзор `platform-mcp-server` согласован как целевое состояние.
