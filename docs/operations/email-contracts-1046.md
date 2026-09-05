---
id: OPS-EMAIL-CONTRACT-1046
title: Контрактная передача email authority и reconciliation
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Граница передачи

Источник: Issues #1037/#1046, MVP-UI-42. Этот checkpoint передаёт исходные
Proto, generated Go и operation policy для параллельной реализации consumer.
SQL/domain handlers, выдача worker credential и deploy trust для нового
producer ещё не реализованы. Наличие RPC в generated не означает доступный
рабочий путь: до подключения handler сервер возвращает Unimplemented.

## Исполняемая авторизация

`RuntimeWorkService.ResolveEmailAuthorization` вызывается только email-bridge
через generated gRPC client, mTLS, application worker grant и authority proof.
Operation: `platform.email.authorization.resolve`; профиль клиента:
`controlplaneclient.EmailBridgeOperations()`. CP HTTP endpoint не создаётся.

`EmailExecutionBinding` содержит oneof `invocation_ref|connection_test_ref` и
`WorkLease`. Integration consumer передаёт exact owner claim, включая fence и
generation. Статический credential connection не доказывает право invocation.
Содержимое binding не логируется. CP проверяет workload исходного claim,
tenant, connection, execution route, состояние и срок lease по owner state.

Request содержит binding, mailbox_ref, configuration_revision, operation,
semantic_input_digest, effect_key, sender, folder, destination_folder. Эти поля
сверяются с immutable owner input; caller не назначает scopes или grant.
Digest вычисляется по типизированной email command, а не по сырому MCP JSON.

Response содержит allowed, actor_ref, agent_ref, organization_ref, project_ref,
connection_ref, mailbox_ref, grant_ref, operation, semantic_input_digest,
effect_key, configuration_revision, credential_generation, policy,
gate_approved, user_scope, agent_scope, connection_scope, resource_scope,
expires_at и binding. Scope содержит mailbox_ref, sender, operations, folders,
recipients. Секретов и тела письма в response нет.

Для invocation обязательна точная user/agent/connection/resource intersection.
Connection test допускает только HEALTH; agent_ref и agent_scope отсутствуют,
поскольку проверка соединения не создаёт фиктивного агента. Consumer обязан
различать эти два source. HUMAN_GATE нельзя ослабить настройкой mailbox.

## Receipt и решение владельца

| Инициатор / RPC | Authority и переход | Результат / чтение |
| --- | --- | --- |
| email-bridge / ReportEmailEffectReceipt | Exact binding и semantic digest; mutation/idempotency; durable UNKNOWN до первого внешнего write | EmailEffectReceipt с owner ref/version, invocation, external receipt ref/digest, outcome, configuration revision и project |
| HTTP gateway / GetEmailEffectReceipt | Видимость exact invocation и его project выводится сервером | receipt и optional decision; запрос содержит invocation_ref |
| Пользователь / ReconcileEmailEffect | Свежая owner permission; mutation.expected_version, receipt_ref и expected_receipt_digest; только подтверждённый EFFECT_CONFIRMED либо NO_EFFECT_CONFIRMED | Отдельный EmailReconciliationDecision; прежний UNKNOWN receipt не удаляется |
| email-bridge / ResolveEmailReconciliation | Exact receipt_ref/decision_ref/external_receipt_ref/external_receipt_digest; актуальный server grant и срок | decision и receipt, без произвольного разрешения повторной отправки |

Report request содержит mutation, binding, external_receipt_ref,
external_receipt_digest, outcome, semantic_input_digest. Ответ: receipt.
Decision содержит ref/version, receipt_ref/receipt_version/receipt_digest,
invocation_ref, outcome, grant_ref, actor_ref, created_at, expires_at.
Reconcile request дополнительно содержит note. Ответ: decision.

Operation IDs: `platform.email.effect-receipts.report`,
`platform.email.reconciliation.resolve`,
`platform.query.email-effect-receipts.get`,
`platform.command.email-effects.reconcile`. Последние два принадлежат
ControlAPIGatewayOperations. Report требует idempotency metadata; Reconcile
требует resource/version/idempotency metadata. Lease/generation привязаны
к полному Proto digest, а не принимаются как самостоятельная authority.

Автоматический retry UNKNOWN запрещён. Обычный status read не выдаёт grant.
Подтверждение NO_EFFECT создаёт отдельное серверное разрешение; старое решение
и старый effect receipt остаются доступными для аудита. Изменение generation
само по себе не разрешает повторный SMTP effect.

Для этих команд авторитетный read path указан в таблице; новый domain event
этим контрактом не вводится. Реализация должна атомарно сохранять owner state,
audit и idempotency receipt и закрывать связанные claims при terminal.

## Оставшаяся реализация

- Owner mailbox configuration с immutable revision и credential generation.
- Авторизация по текущему claim/grant/gate и проверенному semantic command.
- Durable receipts, owner reconciliation и повторная проверка grant.
- Worker credential issuance, trust registration и deploy key delivery.
- PostgreSQL positive/negative matrix и consumer gRPC проверки: NOT RUN.

Policy revision 52 сохраняет scheduler/interaction/STT operations и добавляет
только отдельный email-bridge producer с тремя закрытыми operations.
