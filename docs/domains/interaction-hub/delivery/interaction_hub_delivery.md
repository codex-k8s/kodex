---
doc_id: DLV-CK8S-INTERACTION-HUB
type: delivery-plan
title: kodex — поставка interaction-hub
status: active
owner_role: EM
created_at: 2026-05-22
updated_at: 2026-05-28
related_issues: [582, 768, 781, 783, 800, 806, 821, 835, 843, 853, 855, 867, 882, 894, 911, 921, 928]
related_prs: []
related_docsets:
  - docs/domains/interaction-hub/product/requirements.md
  - docs/domains/interaction-hub/architecture/design.md
  - docs/domains/interaction-hub/architecture/data_model.md
  - docs/domains/interaction-hub/architecture/api_contract.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-22-interaction-hub-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-22
---

# Поставка interaction-hub

## TL;DR

`interaction-hub` поставляется малыми PR-срезами: сначала доменная документация, затем транспортные и событийные контракты, сервисный каркас, PostgreSQL-модель, lifecycle запросов, lifecycle уведомлений/подписок, delivery attempts, channel contract integration, MCP-связка и операционный контур.

Первый кодовый срез IH-1 делает только контракты: proto, AsyncAPI, события и действия доступа. Сервисный каркас идёт следующим отдельным срезом.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования домена | `docs/domains/interaction-hub/product/requirements.md` |
| Дизайн домена | `docs/domains/interaction-hub/architecture/design.md` |
| Модель данных | `docs/domains/interaction-hub/architecture/data_model.md` |
| API-обзор | `docs/domains/interaction-hub/architecture/api_contract.md` |
| Карта Issue | `docs/delivery/issue-map/domains/interaction-hub.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---|---|
| IH-0 | #582 | Доменная документация, границы, модель данных, API-обзор, delivery-план и карта связей готовы. Код, proto, AsyncAPI и OpenAPI не входят. |
| IH-1 | #768 | gRPC и AsyncAPI контракты `interaction-hub`, события `interaction.*`, действия доступа и channel contract DTO готовы; сервисная реализация не входит. |
| IH-2 | #783 | Сервисный процесс, env-конфигурация, health, readiness, metrics, регистрация `InteractionHubService`, domain service skeleton и repository stub готовы; бизнес-операции возвращают `Unimplemented`. |
| IH-3 | #800 | PostgreSQL-модель thread, message, request, response, notification, subscription, route, delivery attempt, callback, command result и service-local outbox готова; thread/message MVP lifecycle работает через repository. |
| IH-4 | #806 | Lifecycle feedback, approval и Human gate requests готов: создать, прочитать, записать ответ, отменить, истечь, идемпотентность и безопасные события без внешних channel adapters. |
| IH-5a | #821 | Notification и subscription lifecycle готовы без delivery attempts и без конкретных внешних каналов: `RequestNotification`, `UpsertSubscription`, `DisableSubscription`, `ListSubscriptions`, idempotency, optimistic concurrency, safe refs/status/policy refs и outbox events. |
| IH-5b | #835 | Delivery attempts и безопасные статусы доставки готовы без конкретных внешних каналов: `PlanDelivery`, `RecordDeliveryResult`, `GetDeliveryStatus`, retry/reminder refs и delivery attempt state machine. |
| IH-6 | #843 | Channel contract integration готова: owner-side route/capability refs, safe delivery command refs, callback envelope lifecycle и package runtime boundary refs без vendor-specific канала. |
| IH-6b | #855 | Callback request resolution готов: `RecordChannelCallback` связывает safe callback с delivery/request, идемпотентно создаёт `InteractionResponse` для terminal action и сохраняет diagnostic no-op для terminal/invalid callback без изменения owner decision state. |
| IH-7 | #867 | Owner inbox read surface готова: `ListOwnerInboxItems` отдаёт pending/active feedback, approval, Human gate и callback diagnostics по собственным interaction-сущностям с safe summary/refs, фильтрами и пагинацией. |
| IH-7b | #882 | Response boundary refs готовы: `interaction.request.response_recorded` несёт safe request kind, scope, source owner, decision owner и context refs для downstream owner resume без raw response text и без изменения чужого decision state. |
| IH-8 | #911 | Human gate response producer/read surface готова: response event и owner inbox отдают safe request/response refs, owner/context refs, normalized outcome, digest/object refs, timestamps, correlation и idempotency digest для owner resume без raw response/callback payload. |
| IH-9a | #921 | Owner inbox UI-readiness готова: `GetOwnerInboxItem` открывает safe detail по request id/scope/assignee, `OwnerInboxItem` содержит allowed actions и version, а owner action использует существующий `RecordInteractionResponse` с idempotency и expected version. |
| IH-9b | #928 | Таксономия действий владельца готова: `request_changes` закреплён как отдельный `InteractionResponseAction`, owner inbox list/detail показывает его только для активных request, а response lifecycle и callback resolution отличают запрос доработки от `reject` без raw payload. |
| IH-9c | не назначено | MCP-интеграция готова: `platform-mcp-server` маршрутизирует `interaction.feedback.request`, `interaction.approval.request`, `interaction.human_gate.request`, status reads. |
| IH-10 | не назначено | Связка с `agent-manager`, `codex-hook-ingress`, `governance-manager` и `provider-hub` готова для PermissionRequest, owner feedback, owner decision refs и событий ответа. |
| IH-11 | #894 | Эксплуатационный контур готов: Dockerfile, deploy manifests, migration job, runtime env inventory, smoke-проверка, runbook и monitoring docs доступны для первого backend deploy. |
| IH-12 | не назначено | Проекции для `operations-hub`, operator visibility, dual-surface inbox status и диагностика delivery failures готовы. |

## Минимальный первый кодовый срез IH-1

IH-1 должен создать только контрактные артефакты:

- `proto/kodex/interactions/v1/interaction_hub.proto`;
- `specs/asyncapi/interaction-hub.v1.yaml`;
- Go-сгенерированные transport contracts;
- Go-сгенерированные event contracts `interaction.*`;
- ключи действий доступа для request, response, delivery, callback и subscription;
- документированную таблицу операций и событий, синхронизированную с API-обзором.

IH-1 не должен:

- создавать БД и миграции;
- запускать сервисный процесс;
- реализовывать channel package;
- добавлять внешний HTTP gateway или OpenAPI;
- подключать конкретный внешний канал;
- менять код соседних сервисов.

## Минимальный сервисный срез IH-2

IH-2 создаёт только runnable scaffold:

- `services/internal/interaction-hub/cmd/interaction-hub/main.go`;
- `internal/app` с env config, graceful shutdown, `/health/livez`, `/health/readyz`, `/metrics` и gRPC server;
- `internal/transport/grpc` с регистрацией `InteractionHubService`;
- `internal/domain/service` с use-case skeleton для всех операций контракта;
- `internal/domain/repository/interaction` и stub-реализацию без PostgreSQL;
- запись scaffold в `services.yaml`.

IH-2 не должен:

- создавать PostgreSQL-схему, миграции или service-local outbox table;
- реализовывать lifecycle feedback, approval, Human gate или notification;
- подключать channel package runtime или любой конкретный внешний канал;
- добавлять `staff-gateway`, `integration-gateway`, UI или внешний OpenAPI;
- менять код соседних сервисов.

## Статус будущего `InteractionHubService`

| Операция | Текущий статус | Плановый срез |
|---|---|---|
| `CreateConversationThread` / `RecordConversationMessage` | MVP lifecycle реализован через PostgreSQL repository и service-local outbox | IH-3 |
| `GetConversationThread` / `ListConversationMessages` | MVP чтения реализованы через PostgreSQL repository | IH-3 |
| `RequestFeedback` | Реализовано через PostgreSQL repository, command idempotency и `interaction.feedback.requested` outbox event | IH-4 |
| `RequestApproval` | Реализовано через PostgreSQL repository, command idempotency и `interaction.approval.requested` outbox event | IH-4 |
| `RequestHumanGate` | Реализовано через PostgreSQL repository, command idempotency и `interaction.human_gate.requested` outbox event | IH-4 |
| `RecordInteractionResponse` | Реализовано: безопасная сводка/refs, завершающее действие, expected version, idempotency и `interaction.request.response_recorded` event с safe request/response/source/owner/context refs, normalized outcome, digest/object refs, timestamps и correlation/idempotency digest; `request_changes` отличается от `reject`; business decision state остаётся у owner service | IH-4/IH-7b/IH-8/IH-9b |
| `CancelInteractionRequest` | Реализовано через expected version, terminal status и `interaction.request.cancelled` event | IH-4 |
| `ExpireInteractionRequests` | Реализовано batch-истечение по scope/deadline с идемпотентным результатом и `interaction.request.expired` events | IH-4 |
| `GetInteractionRequest` / `ListInteractionRequests` | Реализованы PostgreSQL-чтения по request id и scope/status/kind/source owner/deadline | IH-4 |
| `ListOwnerInboxItems` / `GetOwnerInboxItem` | Реализовано: domain read surface для pending/active feedback, approval, Human gate и callback diagnostics; list-фильтры по scope/kind/status/source owner/assignee/actor/correlation refs, detail по request id/scope/assignee, safe delivery/callback/response summaries, allowed actions включая `request_changes` для активных request, response summary digest/object refs, request version и deterministic pagination | IH-7/IH-8/IH-9a/IH-9b |
| `RequestNotification` | Реализовано: one-way notification/reminder intent, safe title/summary/body preview, source owner refs, channel hints, policy ref, idempotency и `interaction.notification.requested` event | IH-5a |
| `UpsertSubscription` / `DisableSubscription` / `ListSubscriptions` | Реализовано: create/update/disable/list, optimistic concurrency, command idempotency, source owner/channel hints/policy refs и `interaction.subscription.updated` event | IH-5a |
| `PlanDelivery` | Реализовано: создаёт delivery attempt для request/notification target, выбирает active route по scope или принимает route ref, пишет safe `interaction.delivery.requested` event и command idempotency | IH-5b |
| `RecordDeliveryResult` | Реализовано: фиксирует safe channel/runtime result по `delivery_id`, переводит attempt в `accepted`, `delivered`, `failed` или `expired`, сохраняет bounded diagnostics/retry/runtime metadata и публикует safe delivery event | IH-5b/IH-6 |
| `RecordChannelCallback` | Реализовано: принимает sanitized callback envelope, связывает его с delivery attempt/request, хранит safe refs/status/fingerprint, создаёт `InteractionResponse` для разрешённого terminal action и публикует safe callback/response events без raw payload | IH-6/IH-6b |
| `GetDeliveryStatus` | Реализовано: возвращает request/notification context и текущие delivery attempts/status по target или `delivery_id` без raw channel payload | IH-5b |

## Синхронизация с параллельными доменами

| Домен или сервис | Когда синхронизироваться | Причина |
|---|---|---|
| `agent-manager` | Перед IH-4, IH-8 и cross-domain resume срезами | Нужен общий lifecycle Human gate, feedback request и события ответа без владения `Run` в `interaction-hub`. |
| `platform-mcp-server` | Перед IH-7 | Нужна MCP-поверхность `interaction.*` и route к `interaction-hub` без реализации доставки в MCP. |
| `codex-hook-ingress` | Перед IH-10 | PermissionRequest и другие hook events могут создавать Human gate или feedback request. |
| `provider-hub` | Перед IH-4 и IH-8 | Owner decision refs нужны provider write pipeline, но provider write и provider approval остаются вне `interaction-hub`. |
| `package-hub` | Согласовано для IH-6 | Channel package capability, installation refs и manifest requirements хранятся в пакетном домене; `interaction-hub` держит только refs. |
| `runtime-manager` и `fleet-manager` | Согласовано для IH-6 | Runtime-нагрузку channel package запускает runtime/fleet контур; `interaction-hub` хранит только safe runtime/job refs. |
| `operations-hub` | Перед IH-9 | Операторские очереди и dual-surface inbox читают проекции и события `interaction.*`. |
| `integration-gateway` | После IH-6/IH-6b | Публичный callback transport активирован как generic route: gateway проверяет source/signature/limits и вызывает `RecordChannelCallback`; lifecycle, storage, дедупликация и request resolution остаются в `interaction-hub`. |

## Критерии начала кода

- Принят пакет доменной документации `interaction-hub`.
- Выбран внешний channel model: package-owned runtime плюс stable channel contract.
- Для каждого кодового PR есть отдельный GitHub Issue.
- Контрактный PR создаёт proto и AsyncAPI до реализации бизнес-операций.
- Старый код из `deprecated/**` не используется как основа реализации.
- Конкретные внешние каналы не добавляются до утверждения channel contract и package capability.

## Критерии завершения домена

- `interaction-hub` имеет свой контур данных, миграций, контрактов и событий.
- Feedback, approval, Human gate, notification, subscriptions, delivery attempts и callback имеют авторитетные команды и чтения.
- Сервис публикует `interaction.*` события через outbox и `platform-event-log`.
- `agent-manager`, `platform-mcp-server`, `codex-hook-ingress`, `provider-hub`, `package-hub`, `runtime-manager`, `operations-hub` и `integration-gateway` связаны через согласованные контракты.
- UI и внешние каналы используют один request lifecycle и не становятся отдельными источниками правды.
- Документы и карты Issue обновлены, хвосты перенесены в следующие срезы явно.

## Апрув

- request_id: `owner-2026-05-22-interaction-hub-kickoff`
- Решение: approved
- Комментарий: план поставки `interaction-hub` согласован как стартовое целевое состояние; первый кодовый PR должен быть контрактным.
