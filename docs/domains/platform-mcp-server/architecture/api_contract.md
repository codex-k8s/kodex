---
doc_id: API-CK8S-PLATFORM-MCP-0001
type: api-contract
title: kodex — API-обзор platform-mcp-server
status: active
owner_role: SA
created_at: 2026-05-14
updated_at: 2026-05-14
related_issues: [747]
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
- Версионирование: MCP tool catalog версионируется отдельно от внутренних gRPC-контрактов сервисов-владельцев.
- Основные операции: hooks, agent-manager tools, provider tools, project/runtime/fleet/package reads, interaction requests и diagnostics.

## Источники правды

Машинные спецификации в этом документе не создаются. Документ фиксирует верхнеуровневую карту инструментов и правила контрактов. Будущий контрактный срез должен создать:

- machine-readable MCP tool catalog;
- транспортные DTO для tool envelope;
- внутренние client interfaces к сервисам-владельцам;
- тесты совместимости маршрутизации и sanitization.

## Общий envelope вызова

Каждый tool call должен содержать:

| Поле | Назначение |
|---|---|
| `tool_name` | Устойчивое имя инструмента. |
| `tool_version` | Версия внешней MCP-поверхности. |
| `actor` | Пользователь, сервисный аккаунт, agent-manager или slot-agent. |
| `source` | Тип и экземпляр источника вызова. |
| `scope` | Организация, проект, репозиторий, provider target или package scope. |
| `run_context` | `agent_run_id`, `session_id`, `slot_id`, если применимо. |
| `command_meta` | `command_id`, idempotency key, expected version, reason, correlation id. |
| `payload` | Типизированные данные конкретного инструмента после проверки размера. |

Свободный JSON допустим только для ограниченных диагностических и hook-сценариев после нормализации. Provider write tools, agent tools и runtime commands должны быть типизированными.

## Группы инструментов MVP

### Hooks

| Инструмент | Сервис-владелец маршрута | Назначение |
|---|---|---|
| `hooks.session_start` | `agent-manager`, при необходимости `runtime-manager` | Зафиксировать старт или resume Codex-сессии в слоте. |
| `hooks.user_prompt_submit` | `agent-manager`, `interaction-hub` | Передать факт пользовательского prompt submit после очистки. |
| `hooks.pre_tool_use` | `agent-manager`, `runtime-manager` | Проверить или зафиксировать намерение вызвать tool, если policy требует deny/ask/risk decision. |
| `hooks.permission_request` | `agent-manager`, `interaction-hub` | Создать или продолжить gate/approval request. |
| `hooks.post_tool_use` | `provider-hub`, `runtime-manager`, `agent-manager` | Передать безопасный итог tool и provider/runtime signal. |
| `hooks.pre_compact` | `agent-manager`, `runtime-manager` | Зафиксировать метаданные checkpoint перед сжатием контекста. |
| `hooks.post_compact` | `agent-manager`, `runtime-manager` | Зафиксировать метаданные checkpoint после сжатия. |
| `hooks.stop` | `agent-manager`, `runtime-manager`, `provider-hub` | Передать итог хода, pending actions и безопасные ссылки на артефакты. |

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

Provider-инструменты не принимают токены и не вызывают GitHub/GitLab напрямую. `external_account_id`, `operation_policy_context` и `approval_gate_ref` передаются в `provider-hub`, который применяет свой write pipeline.

### Project, runtime, fleet и package reads

| Группа | Владелец | Примеры |
|---|---|---|
| `project.*` | `project-catalog` | Прочитать проект, репозиторий, workspace policy, release policy, placement policy. |
| `runtime.*` | `runtime-manager` | Прочитать slot, job, workspace materialization, short status. |
| `fleet.*` | `fleet-manager` | Прочитать cluster health, placement decision, fleet scope. |
| `package.*` | `package-hub` | Прочитать package, installation, manifest, guidance package. |

В MVP эти группы ориентированы на чтения и безопасные команды, необходимые агентам. Изменяющие операции добавляются отдельными срезами только после явной политики доступа и доменного контракта владельца.

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
| `diagnostics.mcp_status.read` | Readiness MCP-сервера, версии tool catalog и состояние маршрутов. |
| `diagnostics.dependency_status.read` | Ограниченный статус зависимостей без секретов и больших логов. |
| `diagnostics.run_context.read` | Безопасная сводка по run/session/slot, если actor имеет право. |
| `diagnostics.last_errors.read` | Короткий bounded tail ошибок по маршруту или группе инструментов. |

Диагностика не заменяет `operations-hub` и не становится хранилищем логов.

## Модель ошибок

| Ошибка | Когда возвращается |
|---|---|
| `mcp.invalid_context` | Нет обязательного actor/source/run/slot/scope контекста или поля несовместимы. |
| `mcp.tool_not_found` | Инструмент не зарегистрирован в текущем tool catalog. |
| `mcp.tool_not_allowed` | Policy boundary запретил инструмент для источника или роли. |
| `mcp.payload_rejected` | Данные вызова слишком большие, содержат запрещённые поля или не прошли sanitization. |
| `mcp.owner_unavailable` | Сервис-владелец недоступен или вернул retryable ошибку. |
| `mcp.owner_rejected` | Сервис-владелец отклонил команду по доменным правилам. |
| `mcp.idempotency_conflict` | Повторный command id пришёл с другим безопасным отпечатком входа. |
| `mcp.rate_limited` | Превышен лимит actor/source/tool group. |

## Совместимость

- Новые инструменты добавляются как новые имена или новые версии.
- Изменение обязательных полей envelope требует новой версии tool group.
- Внутренний gRPC контракт владельца может развиваться независимо, если MCP-caster сохраняет прежнюю внешнюю форму.
- Удаление инструмента проходит через deprecation window и явное обновление tool catalog.

## Апрув

- request_id: `owner-2026-05-14-platform-mcp-kickoff`
- Решение: approved
- Комментарий: API-обзор `platform-mcp-server` согласован как целевое состояние.
