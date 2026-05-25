---
doc_id: DLV-CK8S-INTERACTION-HUB
type: delivery-plan
title: kodex — поставка interaction-hub
status: active
owner_role: EM
created_at: 2026-05-22
updated_at: 2026-05-25
related_issues: [582, 768, 783]
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

`interaction-hub` поставляется малыми PR-срезами: сначала доменная документация, затем транспортные и событийные контракты, сервисный каркас, PostgreSQL-модель, lifecycle запросов, delivery attempts, channel contract integration, MCP-связка и операционный контур.

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
| IH-3 | не назначено | PostgreSQL-модель thread, message, request, response, notification, subscription, route, delivery attempt, callback, command result и service-local outbox готова. |
| IH-4 | не назначено | Lifecycle feedback, approval и Human gate requests готов: создать, прочитать, записать ответ, отменить, истечь, идемпотентность и события. |
| IH-5 | не назначено | Notifications, subscriptions, delivery attempts, retry/reminder policy и безопасные статусы доставки готовы без конкретных внешних каналов. |
| IH-6 | не назначено | Channel contract integration готова: чтение channel package capability из `package-hub`, delivery command в package-owned runtime boundary, callback envelope и delivery result без vendor-specific канала. |
| IH-7 | не назначено | MCP-интеграция готова: `platform-mcp-server` маршрутизирует `interaction.feedback.request`, `interaction.approval.request`, `interaction.human_gate.request`, status reads. |
| IH-8 | не назначено | Связка с `agent-manager`, `codex-hook-ingress`, `governance-manager` и `provider-hub` готова для PermissionRequest, owner feedback, owner decision refs и событий ответа. |
| IH-9 | не назначено | Проекции для `operations-hub`, operator visibility, dual-surface inbox status и диагностика delivery failures готовы. |
| IH-10 | не назначено | Эксплуатационный контур: deploy manifests, migration job, smoke-проверка, runbook и monitoring docs готовы. |

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
| `CreateConversationThread` / `RecordConversationMessage` | scaffold готов, возвращает `Unimplemented` | IH-3, IH-4 |
| `GetConversationThread` / `ListConversationMessages` | scaffold готов, возвращает `Unimplemented` | IH-3, IH-4 |
| `RequestFeedback` | scaffold готов, возвращает `Unimplemented` | IH-4 |
| `RequestApproval` | scaffold готов, возвращает `Unimplemented` | IH-4 |
| `RequestHumanGate` | scaffold готов, возвращает `Unimplemented` | IH-4 |
| `RecordInteractionResponse` | scaffold готов, возвращает `Unimplemented` | IH-4 |
| `CancelInteractionRequest` | scaffold готов, возвращает `Unimplemented` | IH-4 |
| `ExpireInteractionRequests` | scaffold готов, возвращает `Unimplemented` | IH-4 |
| `GetInteractionRequest` / `ListInteractionRequests` | scaffold готов, возвращает `Unimplemented` | IH-3, IH-4 |
| `RequestNotification` | scaffold готов, возвращает `Unimplemented` | IH-5 |
| `UpsertSubscription` / `DisableSubscription` / `ListSubscriptions` | scaffold готов, возвращает `Unimplemented` | IH-5 |
| `PlanDelivery` | scaffold готов, возвращает `Unimplemented` | IH-5 |
| `RecordDeliveryResult` | scaffold готов, возвращает `Unimplemented` | IH-5, IH-6 |
| `RecordChannelCallback` | scaffold готов, возвращает `Unimplemented` | IH-6 |
| `GetDeliveryStatus` | scaffold готов, возвращает `Unimplemented` | IH-5 |

## Синхронизация с параллельными доменами

| Домен или сервис | Когда синхронизироваться | Причина |
|---|---|---|
| `agent-manager` | Перед IH-4 и IH-8 | Нужен общий lifecycle Human gate, feedback request и события ответа без владения `Run` в `interaction-hub`. |
| `platform-mcp-server` | Перед IH-7 | Нужна MCP-поверхность `interaction.*` и route к `interaction-hub` без реализации доставки в MCP. |
| `codex-hook-ingress` | Перед IH-8 | PermissionRequest и другие hook events могут создавать Human gate или feedback request. |
| `provider-hub` | Перед IH-4 и IH-8 | Owner decision refs нужны provider write pipeline, но provider write и provider approval остаются вне `interaction-hub`. |
| `package-hub` | Перед IH-6 | Channel package capability, installation refs и manifest requirements читаются из пакетного домена. |
| `runtime-manager` и `fleet-manager` | Перед IH-6 | Runtime-нагрузку channel package запускает runtime/fleet контур. |
| `operations-hub` | Перед IH-9 | Операторские очереди и dual-surface inbox читают проекции и события `interaction.*`. |
| Future gateway | После IH-6 | Публичный callback transport и OpenAPI проектируются отдельным gateway-срезом. |

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
- `agent-manager`, `platform-mcp-server`, `codex-hook-ingress`, `provider-hub`, `package-hub`, `runtime-manager`, `operations-hub` и future gateway связаны через согласованные контракты.
- UI и внешние каналы используют один request lifecycle и не становятся отдельными источниками правды.
- Документы и карты Issue обновлены, хвосты перенесены в следующие срезы явно.

## Апрув

- request_id: `owner-2026-05-22-interaction-hub-kickoff`
- Решение: approved
- Комментарий: план поставки `interaction-hub` согласован как стартовое целевое состояние; первый кодовый PR должен быть контрактным.
