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
Owner read, ReconcileEmailEffect и ResolveEmailReconciliation подключены к
SQL/domain handlers. ResolveEmailAuthorization, ReportEmailEffectReceipt,
выдача worker credential и deploy trust ещё не завершены. Наличие остальных
RPC в generated не означает доступный рабочий путь: до подключения handler
сервер возвращает Unimplemented.

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

Domain policy в `internal/domain/service/emailpolicy` фиксирует boundary для
подключаемого owner path: `integration.manage` на exact connection и `run.view`
на связанный owner run; свежая интерактивная аутентификация не старше пяти минут
с проверенными ACR/AMR. Отказ freshness — `FRESH_AUTHENTICATION_REQUIRED`.
Browser elevation purpose не является полем CP request; secret reveal purpose
не переиспользуется. Gateway связывает свою elevation/session с exact receipt,
а CP повторно проверяет owner state, права и доверенный principal.

Note ограничен 2000 Unicode code points, UTF-8, без NUL. Digest — ровно 64
lowercase hex без `sha256:`. External receipt ref соответствует генерируемому
bridge ID: ровно 32 lowercase hex. Outcome reconciliation допускает только
EFFECT_CONFIRMED и NO_EFFECT_CONFIRMED, не UNKNOWN_OUTCOME или RETRY.
EffectKey остаётся opaque UTF-8 строкой длиной 1..128 bytes без NUL, без
ограничения на hex или prefix. CP сейчас назначает `eff_` и 32 hex из digest
намерения, но это не меняет общий bridge-контракт.

`ExternalReceiptDigest` соответствует immutable identity
`kodex.email.receipt.v1` из bridge, а не semantic command digest. Он не меняется
при подтверждении outcome. CP сохраняет исходный UNKNOWN и последующие
observations; запрещены изменение identity и замена terminal outcome.

## Реализованный owner path

Миграция `20260904000615` добавляет email receipts, append-only observations и
immutable reconciliation decisions. Create начинается с UNKNOWN; подтверждение
добавляет observation с новой версией, сохраняя прежний факт. Decision не меняет
receipt, invocation, run, claims и не создаёт retry либо нового SMTP effect.
Новый domain event отсутствует: авторитетны GetEmailEffectReceipt и защищённый
ResolveEmailReconciliation. Command transaction сохраняет decision, audit и
idempotency receipt атомарно.

Reconcile проверяет integration.manage на exact connection и run.view до OCC
и idempotency replay; freshness повторяется и при replay. Project выводится
из invocation/run; несовпадающий project в проверенном transport отклоняется.
Решение допускается только для UNKNOWN invocation и exact receipt version/digest.
Другой outcome уже принятого решения запрещён; повторная свежая авторизация
того же outcome может создать новое решение и grant не более чем на две минуты.
Resolve принимает только email-bridge и последнее решение, сверяет exact refs,
digest, version, expiry и актуальные права actor. Предыдущий grant после нового
решения, revoked actor permission и несовпадающая source identity отклоняются.

Для ограниченного bridge reconciler `DecisionRef` может быть пустым: owner
выбирает последнее действующее решение по exact receipt/ref/digest. Нет решения
либо последнее истекло — NOT_FOUND без grant. Непустой DecisionRef остаётся
строгим: нельзя получить другое решение вместо запрошенного. Это read, не consume
и не отправка письма; bridge атомарно сохраняет своё решение/аудит и снимает
локальную блокировку без автоматического retry, исходный UNKNOWN сохраняется.

Canonical commitment принят из bridge checkpoint
`c07e66b20762c843995c94c68b5486ab3cf1116f`; golden
`6dfdb1521d14b99bec6fac759edeb2a11ce30120cbeb1489ab7baa0d5150e41e`.
CP не пересчитывает его из ограниченного HTTP safe view.

`CONTROL_PLANE_EMAIL_GRANT_TRUST_FILE` подключает отдельный email-bridge public
worker key к WorkerGrantTrustFiles. Путь по умолчанию пуст: без activation
credential не принимается; непустой путь должен быть абсолютным и нормализованным.
Состав остальных worker keys сохраняется. Issuer, application credential и
доставка ключа принадлежат root; эта регистрация не доказывает их readiness.

Локальные проверки:
- Go/race domain emailpolicy, platform service и gRPC transport: PASS.
- Disposable PostgreSQL `^TestBootstrapComponent$/email_receipt`: PASS;
  собственные project/agent/run/receipt fixtures, freshness/OCC/digest/replay,
  run-only denial, permission intersection, revoke перед replay/resolve,
  exact worker/decision/source, отсутствие retry и immutable observations.
- Source authorization/report через реальный protected bridge RPC: NOT RUN.

## Оставшийся producer

- Owner mailbox configuration с immutable revision и credential generation.
- Авторизация по текущему claim/grant/gate и проверенному semantic command.
- Привязка Report к durable receipts и source authorization перед первым write.
- Worker credential issuance, trust registration и deploy key delivery.
- PostgreSQL positive/negative matrix и consumer gRPC проверки: NOT RUN.

Policy revision 52 сохраняет scheduler/interaction/STT operations и добавляет
только отдельный email-bridge producer с тремя закрытыми operations.
