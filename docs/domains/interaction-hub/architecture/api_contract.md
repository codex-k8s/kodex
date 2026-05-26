---
doc_id: API-CK8S-INTERACTION-HUB-0001
type: api-contract
title: kodex — API-обзор interaction-hub
status: active
owner_role: SA
created_at: 2026-05-22
updated_at: 2026-05-25
related_issues: [582, 768, 783]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-22-interaction-hub-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-22
---

# API-обзор: interaction-hub

## TL;DR

- Тип API: внутренний gRPC `InteractionHubService`, доменные события `interaction.*`, MCP-инструменты через `platform-mcp-server`, callback envelope через future gateway.
- Аутентификация: gateway, MCP или сервисный токен; доменные команды дополнительно проверяются через `access-manager`.
- Версионирование: стабильное транспортное пространство имён `kodex.interactions.v1`; channel contract версионируется отдельно от конкретного gateway transport.
- Основные операции: диалоги, feedback request, approval request, Human gate, notification, subscriptions, delivery attempts, callback и чтение статусов.

## Спецификации

- gRPC proto: `proto/kodex/interactions/v1/interaction_hub.proto`;
- сгенерированный Go-контракт: `proto/gen/go/kodex/interactions/v1/**`;
- AsyncAPI: `specs/asyncapi/interaction-hub.v1.yaml`;
- Go-контракты событий: `libs/go/platformevents/interaction/events.gen.go`;
- действия доступа: `libs/go/accesscatalog/actions.go`;
- snapshot или contract tests для MCP-поверхности `interaction.*` будут добавляться при подключении через `platform-mcp-server`.

Внешний OpenAPI для callback не является источником правды в этом срезе. Его создаёт будущий gateway-срез после утверждения `integration-gateway` или другого профильного gateway.

## Операции `InteractionHubService`

| Операция | Вид | Доступ | Идемпотентность | Примечание |
|---|---|---|---|---|
| `CreateConversationThread` | gRPC command | `interaction.thread.create` | `CommandMeta.command_id` | Создаёт диалоговую ветку по scope и source. |
| `RecordConversationMessage` | gRPC command | `interaction.message.record` | `command_id` | Записывает сообщение или системную сводку без больших payload. |
| `GetConversationThread` | gRPC query | `interaction.thread.read` | нет | Авторитетное чтение ветки. |
| `ListConversationMessages` | gRPC query | `interaction.message.read` | нет | Чтение сообщений ветки с пагинацией. |
| `RequestFeedback` | gRPC command | `interaction.feedback.request` | `command_id` | Создаёт запрос обратной связи владельца или пользователя. |
| `RequestApproval` | gRPC command | `interaction.approval.request` | `command_id` | Создаёт approval request для рискованного действия или provider operation. |
| `RequestHumanGate` | gRPC command | `interaction.human_gate.request` | `command_id` | Создаёт Human gate request для agent, release, runtime или ops сценария. |
| `RecordInteractionResponse` | gRPC command | `interaction.request.respond` | `command_id` + expected version | Фиксирует ответ человека и передаёт результат сервису-владельцу решения; не создаёт governance/provider/agent decision. |
| `CancelInteractionRequest` | gRPC command | `interaction.request.cancel` | `command_id` + expected version | Отменяет request по команде владельца сценария. |
| `ExpireInteractionRequests` | gRPC command | `interaction.request.expire` | batch id | Переводит просроченные request в terminal state. |
| `GetInteractionRequest` | gRPC query | `interaction.request.read` | нет | Авторитетное чтение request. |
| `ListInteractionRequests` | gRPC query | `interaction.request.read` | нет | Список по scope, status, kind, source owner и deadline. |
| `RequestNotification` | gRPC command | `interaction.notification.request` | `command_id` | Создаёт one-way уведомление или reminder intent, не `InteractionRequest`. |
| `UpsertSubscription` | gRPC command | `interaction.subscription.manage` | `command_id` + expected version | Создаёт или меняет подписку. |
| `DisableSubscription` | gRPC command | `interaction.subscription.manage` | `command_id` + expected version | Отключает подписку. |
| `ListSubscriptions` | gRPC query | `interaction.subscription.read` | нет | Читает подписки по scope или subscriber. |
| `PlanDelivery` | gRPC command | `interaction.delivery.plan` | `command_id` | Выбирает route и создаёт delivery attempt. |
| `RecordDeliveryResult` | gRPC command | `interaction.delivery.update` | `delivery_id` | Фиксирует ответ package workload через согласованный runtime boundary. |
| `RecordChannelCallback` | gRPC command | `interaction.callback.record` | `callback_id` | Принимает безопасный callback envelope от gateway. |
| `GetDeliveryStatus` | gRPC query | `interaction.delivery.read` | нет | Читает состояние request/notification и delivery attempts. |

`PlanDelivery` может быть внутренней операцией сервиса или worker-командой. Внешний вызывающий контур обычно создаёт request, а delivery planning выполняет сам `interaction-hub` по политике маршрута.

## MCP-инструменты

`platform-mcp-server` должен предоставить типизированные инструменты и маршрутизировать их к `interaction-hub`.

| Инструмент | Назначение |
|---|---|
| `interaction.feedback.request` | Запросить обратную связь владельца или пользователя из agent-manager или slot-агента. |
| `interaction.approval.request` | Запросить approval для действия, которое требует решения человека. |
| `interaction.human_gate.request` | Создать Human gate request с run/session/provider/runtime context. |
| `interaction.request.status_read` | Прочитать безопасный статус request и deadline. |
| `interaction.delivery.status_read` | Прочитать delivery attempts и последний безопасный error code. |

MCP не доставляет уведомления сам. MCP проверяет actor/source/run/session/slot binding, policy и audit, затем вызывает доменный сервис.

## Channel contract

`ChannelDeliveryContract` является доменным контрактом взаимодействия с установленным channel package. Он не равен OpenAPI внешнего gateway.

| Операция контракта | Направление | Назначение |
|---|---|---|
| `DeliverInteraction` | `interaction-hub` -> runtime boundary -> channel package workload | Передать delivery command для request или notification без создания jobs внутри `interaction-hub`. |
| `RecordDeliveryResult` | runtime boundary или channel package runtime -> `interaction-hub` | Зафиксировать, что пакет принял, отложил или отклонил попытку доставки. |
| `RecordChannelCallback` | future gateway -> `interaction-hub` | Передать безопасный callback envelope после публичной проверки. |

Минимальный payload `DeliverInteraction`:

| Поле | Правило |
|---|---|
| `delivery_id` | Обязательный идемпотентный ключ. |
| `request_ref` | Обязателен для feedback, approval и Human gate. |
| `notification_ref` | Обязателен для notification без request. |
| `delivery_kind` | Закрытый enum `feedback`, `approval`, `human_gate`, `notification`. |
| `scope` | Область и внешняя ссылка области. |
| `recipient_refs` | Safe refs получателей, без секретов и лишних PII. |
| `message` | Локализуемый template ref или bounded safe summary. |
| `actions` | Набор допустимых action keys. |
| `callback_ref` | Ссылка для сопоставления callback. |
| `correlation_id` | Сквозная связь с соседним контекстом. |
| `expires_at` | Срок действия delivery attempt. |

Минимальный payload `RecordChannelCallback`:

| Поле | Правило |
|---|---|
| `callback_id` | Обязательный идемпотентный ключ callback. |
| `delivery_id` | Связь с попыткой доставки. |
| `request_ref` | Исходный request, если известен. |
| `actor_ref` | Проверенный gateway actor или внешний субъект. |
| `action` | Действие из разрешённого набора. |
| `answer_summary` | Короткая безопасная сводка. |
| `answer_object_ref` | Ссылка на полный ответ или вложение после sanitization. |
| `signature_status` | Результат проверки gateway без сырой подписи. |

## Интеграции с другими сервисами

| Сервис | Вызовы из `interaction-hub` | Правило |
|---|---|---|
| `access-manager` | Проверка действий создания запроса, отправки ответа, чтения статуса и использования маршрута. | `interaction-hub` не вычисляет права сам. |
| `package-hub` | Чтение установленных plugin packages и manifest capability внешнего канала. | Установка и manifest остаются у `package-hub`. |
| `runtime-manager` | Техническая доставка command в runtime-нагрузку channel package через согласованный контур. | Jobs и workloads остаются у runtime; `interaction-hub` не вызывает package workload в обход runtime boundary. |
| `fleet-manager` | Косвенно через runtime placement для channel package. | `interaction-hub` не выбирает Kubernetes-кластер. |
| `agent-manager` | Получает события ответа и вызывает команды feedback/approval/Human gate. | Flow/run/session остаются у `agent-manager`. |
| `provider-hub` | Получает context refs и owner decision ref от владельца решения; provider operations остаются там. | `interaction-hub` не пишет в GitHub/GitLab и не создаёт provider approval. |
| `operations-hub` | Читает request/delivery status и получает `interaction.*` события. | Read models и очереди остаются у `operations-hub`. |
| `codex-hook-ingress` | Передаёт нормализованные hook events, которые требуют вопроса или разрешения. | Hook transport и sanitization остаются у hook ingress. |

## Модель ошибок

| Ошибка | Когда возвращается |
|---|---|
| `invalid_argument` | Невалидный request kind, action, source context, deadline, scope или callback envelope. |
| `permission_denied` | `access-manager` запретил действие или actor не может отправить ответ. |
| `not_found` | Thread, request, delivery attempt, subscription или route не найдены. |
| `already_exists` | Повтор с тем же command id или callback id уже записан с совместимым payload. |
| `failed_precondition` | Request уже в terminal state, action не разрешён, route отключён или channel capability недоступен. |
| `aborted` | Конфликт expected version или replay с другим fingerprint. |
| `unavailable` | Временная ошибка БД, package, runtime, gateway или event log. |

Delivery-specific safe error codes:

| Код | Смысл |
|---|---|
| `DELIVERY_ROUTE_NOT_FOUND` | Нет допустимого маршрута для scope и получателя. |
| `DELIVERY_CHANNEL_UNAVAILABLE` | Runtime channel package временно недоступен. |
| `DELIVERY_AUTH_REQUIRED` | У channel package не заполнены или недоступны секреты. |
| `DELIVERY_RATE_LIMITED` | Канал вернул лимит или backoff. |
| `CALLBACK_REJECTED` | Callback envelope не прошёл доменную проверку. |
| `REQUEST_ALREADY_RESOLVED` | Callback пришёл после terminal state. |

## События

| Event | Aggregate | Payload минимум |
|---|---|---|
| `interaction.thread.created` | thread | `thread_id`, `scope`, `source_kind`, `correlation_id` |
| `interaction.message.recorded` | message | `message_id`, `thread_id`, `message_kind`, `author_ref` |
| `interaction.feedback.requested` | request | `request_id`, `scope`, `source_owner_kind`, `deadline_at` |
| `interaction.approval.requested` | request | `request_id`, `scope`, `risk_class`, `provider_operation_ref` |
| `interaction.human_gate.requested` | request | `request_id`, `scope`, `agent_run_ref`, `deadline_at` |
| `interaction.notification.requested` | notification | `notification_id`, `scope`, `notification_kind`, `priority` |
| `interaction.subscription.updated` | subscription | `subscription_id`, `scope`, `subscriber_ref`, `version` |
| `interaction.delivery.requested` | delivery | `delivery_attempt_id`, `request_id`, `route_id`, `attempt_number` |
| `interaction.delivery.accepted` | delivery | `delivery_attempt_id`, `channel_message_ref` |
| `interaction.delivery.failed` | delivery | `delivery_attempt_id`, `error_code`, `error_class`, `next_retry_at` |
| `interaction.callback.received` | callback | `callback_id`, `delivery_attempt_id`, `processing_status` |
| `interaction.request.response_recorded` | request | `request_id`, `response_id`, `response_action`, `actor_ref`, `owner_request_ref`, `owner_decision_ref` |
| `interaction.request.expired` | request | `request_id`, `deadline_at` |
| `interaction.request.cancelled` | request | `request_id`, `cancelled_by_ref` |

## Состояние реализации

| Область | Статус |
|---|---|
| Доменная документация | Подготовлена как стартовый срез. |
| gRPC proto | Подготовлен как контрактный срез `IH-1`. |
| AsyncAPI `interaction.*` | Подготовлен как контрактный срез `IH-1`. |
| Go-контракты transport/events | Сгенерированы из proto и AsyncAPI. |
| Действия доступа | Добавлены в общий каталог системных действий. |
| Go-реализация `interaction-hub` | Сервисный каркас готов: process config, health/readiness/metrics, gRPC registration, domain service skeleton и repository stub. Бизнес-операции возвращают `Unimplemented`. |
| MCP-инструменты | Зафиксированы как контрактный задел `platform-mcp-server`; реализация зависит от готовности доменного контракта. |
| Channel package integration | Зафиксирована как гибрид package-owned runtime + channel contract; конкретные каналы не проектируются. |
| Gateway callback OpenAPI | Отложен до среза будущего gateway. |

## Совместимость

- `InteractionHubService v1` должен покрыть feedback, approval, Human gate, notification, delivery и callback, даже если реализация поставляется по срезам.
- `ChannelDeliveryContract` должен иметь собственную версию, чтобы future gateway и channel packages могли развиваться без изменения request lifecycle.
- События `interaction.*` проектируются так, чтобы переход с PostgreSQL event log на брокер не ломал payload.
- Внешние surface не получают собственный lifecycle: UI, voice и channel callback сходятся в одни команды и статусы request.

## Апрув

- request_id: `owner-2026-05-22-interaction-hub-kickoff`
- Решение: approved
- Комментарий: API-обзор `interaction-hub` согласован как стартовое целевое состояние; машинные контракты созданы контрактным срезом `IH-1`, сервисная реализация остаётся отдельным срезом.
