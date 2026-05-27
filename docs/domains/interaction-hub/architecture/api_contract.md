---
doc_id: API-CK8S-INTERACTION-HUB-0001
type: api-contract
title: kodex — API-обзор interaction-hub
status: active
owner_role: SA
created_at: 2026-05-22
updated_at: 2026-05-27
related_issues: [582, 768, 781, 783, 800, 806, 821, 835, 843, 853, 867, 882]
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

- Тип API: внутренний gRPC `InteractionHubService`, доменные события `interaction.*`, MCP-инструменты через `platform-mcp-server`, callback envelope через `integration-gateway`.
- Аутентификация: gateway, MCP или сервисный токен; доменные команды дополнительно проверяются через `access-manager`.
- Версионирование: стабильное транспортное пространство имён `kodex.interactions.v1`; channel contract версионируется отдельно от конкретного gateway transport.
- Основные операции: диалоги, feedback request, approval request, Human gate, owner inbox reads, notification, subscriptions, delivery attempts, callback и чтение статусов.

## Спецификации

- gRPC proto: `proto/kodex/interactions/v1/interaction_hub.proto`;
- сгенерированный Go-контракт: `proto/gen/go/kodex/interactions/v1/**`;
- AsyncAPI: `specs/asyncapi/interaction-hub.v1.yaml`;
- Go-контракты событий: `libs/go/platformevents/interaction/events.gen.go`;
- действия доступа: `libs/go/accesscatalog/actions.go`;
- snapshot или contract tests для MCP-поверхности `interaction.*` будут добавляться при подключении через `platform-mcp-server`.

Внешний OpenAPI-каркас для callback находится в `specs/openapi/integration-gateway.v1.yaml`. Источник правды lifecycle и payload callback остаётся в доменном контракте `interaction-hub`.

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
| `ListOwnerInboxItems` | gRPC query | `interaction.request.read` | нет | Доменная read-поверхность входящих решений: pending/active feedback, approval, Human gate и callback diagnostics с safe summary/status/refs. |
| `RequestNotification` | gRPC command | `interaction.notification.request` | `command_id` | Создаёт one-way уведомление или reminder intent, не `InteractionRequest`; хранит только safe refs, safe summary/title/preview и policy refs. |
| `UpsertSubscription` | gRPC command | `interaction.subscription.manage` | `command_id` + expected version | Создаёт или меняет подписку с optimistic concurrency для существующего aggregate. |
| `DisableSubscription` | gRPC command | `interaction.subscription.manage` | `command_id` + expected version | Отключает подписку. |
| `ListSubscriptions` | gRPC query | `interaction.subscription.read` | нет | Читает подписки по scope или subscriber. |
| `PlanDelivery` | gRPC command | `interaction.delivery.plan` | `command_id` | Выбирает route и создаёт delivery attempt. |
| `RecordDeliveryResult` | gRPC command | `interaction.delivery.update` | `delivery_id` + safe result fingerprint | Фиксирует ответ package workload через согласованный runtime boundary; replay того же `delivery_id/result` возвращает сохранённое состояние без нового event. |
| `RecordChannelCallback` | gRPC command | `interaction.callback.record` | `callback_id` | Принимает безопасный callback envelope от gateway, связывает его с delivery/request и, если callback является допустимым terminal answer, фиксирует `InteractionResponse`. |
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
| `RecordDeliveryResult` | runtime boundary или channel package runtime -> `interaction-hub` | Зафиксировать safe status `accepted`, `delivered`, `failed` или `expired` без raw external payload. Значения `deferred/rejected`, если остаются в enum для совместимости, в срезе IH-6 явно отклоняются. |
| `RecordChannelCallback` | `integration-gateway` -> `interaction-hub` | Передать безопасный callback envelope после публичной проверки. |

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
| `route_id` | Выбранный route ref. |
| `channel_capability_ref` | Capability установленного channel package. |
| `package_installation_ref` | Установка package-hub без владения package state. |
| `package_version_ref` | Выбранная версия package-hub. |
| `delivery_command_ref` | Ссылка на safe runtime command envelope. |
| `callback_route_ref` | Safe ref callback route, если route настроен. |
| `runtime_ref` | Package-owned runtime boundary ref. |
| `routing_policy_ref` | Ссылка на routing policy без embedded policy JSON. |

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
| `gateway_ref` | Safe ref gateway request. |
| `correlation_id` | Сквозная связь с delivery/owner context. |

`RecordChannelCallback` всегда сначала фиксирует безопасную callback record, если envelope прошёл базовую нормализацию и correlation. Для `feedback`, `approval` и `human_gate` request сервис применяет callback к request lifecycle только когда подпись принята, request не terminal, action входит в `allowed_actions` и помечен terminal. Тогда создаётся один `InteractionResponse` с `source_kind=channel_callback`, request переводится в `answered`, а owner decision state остаётся у сервиса-владельца решения. Повтор того же `callback_id` возвращает сохранённые callback/response без нового outbox event. Callback к `cancelled`, `expired`, `answered` или `failed` request сохраняется как безопасный diagnostic no-op с `processing_status=rejected` и не создаёт новый response.

## Owner inbox read surface

`ListOwnerInboxItems` отдаёт только доменную read-поверхность `interaction-hub` по собственным сущностям. Это подготовка для `staff-gateway`, UI и `operations-hub`, но не их cross-domain read model.

Фильтры:

- `scope`;
- `request_kinds`;
- `statuses`; если список пустой, сервис возвращает active статусы `created`, `routed`, `waiting`;
- `source_owner_kind` и `source_owner_ref`;
- `assignee_ref` через `InteractionRequest.target_refs`;
- `actor_ref` по последнему response/callback actor;
- `correlation_ref` через `InteractionRequest.context_refs`;
- `correlation_id` по safe owner/ingress/callback correlation refs;
- `include_diagnostics`, чтобы добавить request с последним rejected callback diagnostic даже вне active status filter;
- `page`.

Сортировка детерминированная: active request идут первыми, затем ближайший `deadline_at`, затем `updated_at DESC`, затем `id DESC`.

Ответ содержит только safe поля: `request_id`, kind/status, title/summary из bounded `prompt_summary`, scope refs, requester/source owner refs, assignee refs, context refs, deadline/reminder policy ref, delivery summary, latest callback diagnostic refs/status/error code, latest response refs/action/actor/source, timestamps и version. Ответ не содержит raw message body, raw callback payload, headers, tokens, secret values, provider payload, prompt/transcript, stdout/stderr, logs или внешнюю PII сверх safe display summary.

## Интеграции с другими сервисами

| Сервис | Вызовы из `interaction-hub` | Правило |
|---|---|---|
| `access-manager` | Проверка действий создания запроса, отправки ответа, чтения статуса и использования маршрута. | `interaction-hub` не вычисляет права сам. |
| `package-hub` | Чтение установленных plugin packages и manifest capability внешнего канала. | Установка и manifest остаются у `package-hub`. |
| `runtime-manager` | Техническая доставка command в runtime-нагрузку channel package через согласованный контур. | Jobs и workloads остаются у runtime; `interaction-hub` не вызывает package workload в обход runtime boundary. |
| `fleet-manager` | Косвенно через runtime placement для channel package. | `interaction-hub` не выбирает Kubernetes-кластер. |
| `agent-manager` | Получает события ответа и вызывает команды feedback/approval/Human gate. | Flow/run/session остаются у `agent-manager`. |
| `provider-hub` | Получает context refs и owner decision ref от владельца решения; provider operations остаются там. | `interaction-hub` не пишет в GitHub/GitLab и не создаёт provider approval. |
| `operations-hub` | Читает request/delivery/inbox status и получает `interaction.*` события. | Cross-domain read models и очереди остаются у `operations-hub`; `interaction-hub` отдаёт только свои interaction-сущности. |
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
| `CALLBACK_ACTION_NOT_ALLOWED` | Callback выбрал action вне `allowed_actions` request. |
| `CALLBACK_ACTION_UNSUPPORTED` | Callback action не мапится на поддерживаемый response action. |
| `CALLBACK_ACTION_NOT_TERMINAL` | Callback выбрал допустимый, но не завершающий action. |
| `CALLBACK_ACTOR_REQUIRED` | Для terminal response не хватает safe actor ref. |
| `CALLBACK_RESPONSE_REQUIRED` | Для `answer/custom` не хватает safe summary или object ref. |

## События

| Event | Aggregate | Payload минимум |
|---|---|---|
| `interaction.thread.created` | thread | `thread_id`, `scope`, `source_kind`, `correlation_id` |
| `interaction.message.recorded` | message | `message_id`, `thread_id`, `message_kind`, `author_ref` |
| `interaction.feedback.requested` | request | `request_id`, `scope`, `source_owner_kind`, `deadline_at` |
| `interaction.approval.requested` | request | `request_id`, `scope`, `risk_class`, `provider_operation_ref` |
| `interaction.human_gate.requested` | request | `request_id`, `scope`, `agent_run_ref`, `deadline_at` |
| `interaction.notification.requested` | notification | `notification_id`, `scope`, `notification_kind`, `priority`, `source_owner_kind`, `status` |
| `interaction.subscription.updated` | subscription | `subscription_id`, `scope`, `subscriber_ref`, `source_owner_kind`, `status`, `version` |
| `interaction.delivery.requested` | delivery | `delivery_attempt_id`, `delivery_id`, `request_id` или `notification_id`, `route_id`, `attempt_number`, `status` |
| `interaction.delivery.accepted` | delivery | `delivery_attempt_id`, `delivery_id`, `channel_message_ref`, `status` |
| `interaction.delivery.delivered` | delivery | `delivery_attempt_id`, `delivery_id`, `channel_message_ref`, `runtime_job_ref`, `status` |
| `interaction.delivery.failed` | delivery | `delivery_attempt_id`, `delivery_id`, `error_code`, `error_class`, `next_retry_at`, `status` |
| `interaction.delivery.expired` | delivery | `delivery_attempt_id`, `delivery_id`, `error_code`, `status` |
| `interaction.callback.received` | callback | `callback_id`, `delivery_attempt_id`, `request_id`, `processing_status`, `callback_route_ref`, `gateway_ref`, `correlation_id` |
| `interaction.request.response_recorded` | request | `request_id`, `request_kind`, `scope_type`, `scope_ref`, `source_owner_kind`, `source_owner_ref`, `agent_run_ref`, `provider_operation_ref`, `response_id`, `response_action`, `actor_ref`, `source_kind`, `owner_request_ref`, `owner_decision_ref` |
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
| Go-реализация `interaction-hub` | Готовы process config, health/readiness/metrics, gRPC registration, PostgreSQL repository, service-local outbox, thread/message MVP lifecycle, feedback/approval/Human gate request lifecycle, owner inbox read surface, lifecycle уведомлений/подписок, delivery attempt lifecycle, channel contract integration и callback request resolution с safe response lifecycle. |
| MCP-инструменты | Зафиксированы как контрактный задел `platform-mcp-server`; реализация зависит от готовности доменного контракта. |
| Channel package integration | Зафиксирована и реализована на owner-side refs: delivery route/capability, safe delivery command ref, runtime/job refs и callback envelope без конкретных каналов. |
| Gateway callback OpenAPI | Generic route закреплён в `specs/openapi/integration-gateway.v1.yaml`: `integration-gateway` проверяет source/signature/limits и вызывает `RecordChannelCallback`, не владея lifecycle callback. |

## Совместимость

- `InteractionHubService v1` должен покрыть feedback, approval, Human gate, notification, delivery и callback, даже если реализация поставляется по срезам.
- `ChannelDeliveryContract` должен иметь собственную версию, чтобы `integration-gateway` и channel packages могли развиваться без изменения request lifecycle.
- События `interaction.*` проектируются так, чтобы переход с PostgreSQL event log на брокер не ломал payload.
- Внешние surface не получают собственный lifecycle: UI, voice и channel callback сходятся в одни команды и статусы request.

## Апрув

- request_id: `owner-2026-05-22-interaction-hub-kickoff`
- Решение: approved
- Комментарий: API-обзор `interaction-hub` согласован как стартовое целевое состояние; машинные контракты созданы контрактным срезом `IH-1`, сервисная реализация остаётся отдельным срезом.
