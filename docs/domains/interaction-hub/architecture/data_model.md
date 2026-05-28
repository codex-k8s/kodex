---
doc_id: DM-CK8S-INTERACTION-HUB-0001
type: data-model
title: kodex — модель данных домена центра взаимодействий
status: active
owner_role: SA
created_at: 2026-05-22
updated_at: 2026-05-28
related_issues: [582, 768, 800, 821, 835, 843, 867, 911, 928]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-22-interaction-hub-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-22
---

# Модель данных: центр взаимодействий

## TL;DR

- Ключевые сущности: `ConversationThread`, `ConversationMessage`, `InteractionRequest`, `InteractionResponse`, `Notification`, `Subscription`, `DeliveryRoute`, `DeliveryAttempt`, `ChannelCallback`.
- Технические агрегаты: `CommandResult`, `OutboxEvent`.
- Основные связи: thread содержит сообщения; request может быть feedback, approval или Human gate; request имеет delivery attempts и callbacks; response завершает interaction lifecycle и передаётся владельцу business decision; subscription создаёт notification intent.
- Риски миграций: нельзя хранить flow/run/session, provider write operation, runtime job, package installation, UI state, сырые секреты, полные внешние callback payload, голосовые и медиа-файлы в PostgreSQL.

## Правило внешних ссылок

`interaction-hub` хранит внешние ссылки как typed refs:

- `agent_session_ref`;
- `agent_run_ref`;
- `agent_stage_ref`;
- `provider_work_item_ref`;
- `provider_operation_ref`;
- `runtime_job_ref`;
- `runtime_slot_ref`;
- `package_installation_ref`;
- `channel_package_ref`;
- `operations_queue_ref`;
- `actor_ref`;
- `scope_ref`.

Эти ссылки не являются SQL-связями с БД других сервисов. Источник истины остаётся у сервиса-владельца.

## Сущности

### ConversationThread

`ConversationThread` описывает диалоговую ветку между пользователем, агентным контуром и платформой.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор ветки. |
| `scope_type` | enum | нет | `platform`, `organization`, `project`, `repository`, `service`. |
| `scope_ref` | text | нет | Внешний идентификатор области. |
| `thread_kind` | enum | нет | `user_dialog`, `owner_feedback`, `approval`, `human_gate`, `notification`, `ops`. |
| `primary_actor_ref` | text | да | Пользователь или service principal, с которого начался диалог. |
| `source_kind` | enum | нет | `web_console`, `voice`, `mcp`, `provider`, `channel_package`, `codex_hook`, `system`, `service`. |
| `source_ref` | text | да | Ссылка на внешний или соседний источник. |
| `status` | enum | нет | `open`, `waiting`, `closed`, `archived`. |
| `latest_message_id` | uuid | да | Последнее сообщение ветки. |
| `correlation_id` | text | нет | Связь с run, provider artifact, delivery или incident. |
| `retention_class` | enum | нет | Класс хранения сообщений и вложений. |
| `version` | bigint | нет | Оптимистичная конкуренция. |
| `created_at`, `updated_at`, `closed_at` | timestamptz | да | Временные метки. |

### ConversationMessage

`ConversationMessage` фиксирует сообщение или системный факт внутри ветки.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор сообщения. |
| `thread_id` | uuid | нет | Диалоговая ветка. |
| `message_kind` | enum | нет | `user_text`, `voice_transcript`, `agent_text`, `system_notice`, `response_summary`, `callback_summary`. |
| `author_ref` | text | нет | Пользователь, агент, service principal или channel package. |
| `body_summary` | text | да | Короткая безопасная сводка. |
| `body_object_ref` | text | да | Ссылка на полный текст или вложение во внешнем хранилище. |
| `body_digest` | text | да | Digest полного содержимого, если оно хранится объектом. |
| `locale` | text | да | Язык сообщения. |
| `metadata` | jsonb | нет | Небольшие audit-safe признаки без секретов и больших payload. |
| `created_at` | timestamptz | нет | Когда сообщение записано. |

Полный prompt, голос, медиа и длинные вложения не пишутся в PostgreSQL. В БД хранится ссылка, digest, размер и retention metadata.

### InteractionRequest

`InteractionRequest` является общим агрегатом для feedback, approval и Human gate delivery request. Business decision state остаётся у сервиса-владельца решения.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор запроса. |
| `request_kind` | enum | нет | `feedback`, `approval`, `human_gate`. |
| `scope_type` | enum | нет | Область запроса. |
| `scope_ref` | text | нет | Внешняя ссылка области. |
| `thread_id` | uuid | да | Диалоговая ветка, если запрос связан с диалогом. |
| `source_owner_kind` | enum | нет | `agent_manager`, `slot_agent`, `governance_manager`, `provider_hub`, `operations_hub`, `user`, `system`. |
| `source_owner_ref` | text | да | Внешняя ссылка владельца сценария: run/session/provider operation/user/system rule. |
| `ingress_kind` | enum | нет | `direct_grpc`, `mcp`, `codex_hook`, `gateway`, `system`, `service`. |
| `ingress_ref` | text | да | Ссылка на transport/ingress command, hook event или gateway request. |
| `decision_owner_kind` | enum | да | `agent_manager`, `governance_manager`, `provider_hub`, `operations_hub`, `system`; кто валидирует ответ и владеет business decision. |
| `decision_owner_ref` | text | да | Ссылка на gate/request/operation у владельца решения. |
| `target_refs` | jsonb | нет | Actor/group/role refs получателей. |
| `context_refs` | jsonb | нет | Run, session, provider, runtime, package и incident refs. |
| `prompt_summary` | text | нет | Короткая безопасная формулировка запроса. |
| `prompt_object_ref` | text | да | Ссылка на расширенное описание. |
| `allowed_actions` | jsonb | нет | Допустимые действия ответа. |
| `risk_class` | enum | да | `low`, `medium`, `high`, `critical`. |
| `status` | enum | нет | `created`, `routed`, `waiting`, `answered`, `expired`, `cancelled`, `failed`. |
| `deadline_at` | timestamptz | да | Срок ожидания ответа. |
| `reminder_policy_ref` | text | да | Ссылка на правила напоминаний и эскалации. Тело policy не хранится в request в текущем контракте. |
| `version` | bigint | нет | Оптимистичная конкуренция. |
| `created_at`, `updated_at`, `resolved_at` | timestamptz | да | Временные метки. |

`InteractionRequest` не хранит канонический статус `Run`, provider operation или runtime job. Он хранит только связь с ними. `platform-mcp-server`, `codex-hook-ingress` и gateway являются transport/ingress route, а не владельцами бизнес-источника request.

### InteractionResponse

`InteractionResponse` фиксирует ответ человека или подтверждённого внешнего субъекта, полученный через UI, MCP или внешний channel callback. Эта запись не является каноническим `GateDecision`, release decision, provider approval или состоянием `Run`; сервис-владелец решения валидирует actor/policy и фиксирует итоговое business decision у себя.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор ответа. |
| `request_id` | uuid | нет | Запрос, на который получен ответ. |
| `response_action` | enum | нет | `answer`, `approve`, `reject`, `request_changes`, `defer`, `acknowledge`, `custom`. |
| `responded_by_actor_ref` | text | нет | Проверенный actor. |
| `response_summary` | text | да | Короткая безопасная сводка. |
| `response_object_ref` | text | да | Ссылка на полный ответ или вложения. |
| `source_kind` | enum | нет | `web_console`, `mcp`, `channel_callback`, `system`, `service`. |
| `source_ref` | text | да | Callback, message или command ref. |
| `owner_decision_ref` | text | да | Ссылка на решение у сервиса-владельца, если оно уже зафиксировано. |
| `created_at` | timestamptz | нет | Время ответа. |

Для request с terminal state допускается только один итоговый ответ. Callback response использует `source_kind=channel_callback` и `source_ref` на безопасную callback record. Повторная доставка того же callback возвращает уже сохранённый ответ.

### Notification

`Notification` описывает уведомление или reminder, созданные явно или по подписке.

One-way уведомления и reminders не создают `InteractionRequest`. Если уведомление связано с feedback, approval или Human gate, оно хранит `request_id` как ссылку на request; собственный delivery status остаётся в `Notification` и `DeliveryAttempt`.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор уведомления. |
| `scope_type` | enum | нет | Область уведомления. |
| `scope_ref` | text | нет | Внешняя ссылка области. |
| `notification_kind` | enum | нет | `status`, `reminder`, `error`, `attention`, `decision_required`, `ops`. |
| `request_id` | uuid | да | Исходный запрос, если уведомление связано с ним. |
| `subscription_id` | uuid | да | Подписка, которая создала уведомление. |
| `recipient_refs` | jsonb | нет | Получатели. |
| `message_template_ref` | text | нет | Локализуемый шаблон сообщения. |
| `message_summary` | text | нет | Короткая безопасная сводка. |
| `priority` | enum | нет | `low`, `normal`, `high`, `urgent`. |
| `status` | enum | нет | `created`, `queued`, `delivered`, `acknowledged`, `expired`, `failed`. |
| `source_owner_kind` | enum | нет | Сервис или сценарий, который запросил уведомление: `agent_manager`, `slot_agent`, `governance_manager`, `provider_hub`, `operations_hub`, `user`, `system`. |
| `source_owner_ref` | text | да | Safe ref агрегата владельца: run, gate request, provider operation, system rule. |
| `ingress_kind` | enum | нет | `direct_grpc`, `mcp`, `codex_hook`, `gateway`, `system`, `service`. |
| `ingress_ref` | text | да | Safe ref команды, hook event или gateway request. |
| `context_refs` | jsonb | нет | Ссылки на соседний контекст без копирования state. |
| `channel_hint_refs` | jsonb | нет | Generic surface/package/channel hints без hardcoded vendor list. |
| `notification_policy_ref` | text | да | Ссылка на policy; тело policy не хранится локально без отдельного расширения контракта. |
| `message_title` | text | да | Короткий безопасный заголовок. |
| `body_preview` | text | да | Короткий безопасный preview; raw transcript/artifact не хранится. |
| `created_at`, `updated_at`, `expires_at` | timestamptz | да | Временные метки. |

### Subscription

`Subscription` хранит правило доставки уведомлений для actor, группы, роли или scope.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор подписки. |
| `scope_type` | enum | нет | Где действует подписка. |
| `scope_ref` | text | нет | Внешняя ссылка области. |
| `subscriber_ref_kind` | text | нет | Тип подписчика: пользователь, группа, роль или service principal. |
| `subscriber_ref` | text | нет | Идентификатор подписчика в owner domain. |
| `event_filter` | jsonb | нет | Типы событий, severity, domain refs и ограничения. |
| `delivery_preferences` | jsonb | нет | Предпочтения поверхностей без vendor-specific enum конкретных каналов. |
| `status` | enum | нет | `active`, `paused`, `disabled`. |
| `version` | bigint | нет | Оптимистичная конкуренция. |
| `source_owner_kind` | enum | нет | Сервис или сценарий, который управляет подпиской. |
| `source_owner_ref` | text | да | Safe ref owner-domain агрегата. |
| `channel_hint_refs` | jsonb | нет | Generic surface/package/channel hints без vendor-specific списка. |
| `subscription_policy_ref` | text | да | Ссылка на policy; local policy table не вводится без отдельного расширения контракта. |
| `created_at`, `updated_at` | timestamptz | нет | Временные метки. |

Подписка не является UI-настройкой конкретного клиента. UI может управлять подпиской через gateway, но каноническая подписка хранится здесь.

Если `interaction-hub` понадобится service-local таблица policy для notification или subscription rules, она вводится отдельным срезом после явного расширения контракта. Текущий контракт хранит только `notification_policy_ref` и `subscription_policy_ref`.

### DeliveryRoute

`DeliveryRoute` фиксирует выбранный маршрут доставки для запроса или уведомления.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор маршрута. |
| `scope_type` | enum | нет | Область маршрута. |
| `scope_ref` | text | нет | Внешняя ссылка области. |
| `surface_kind` | enum | нет | `web_console`, `voice`, `provider_surface`, `channel_package`, `system`. |
| `channel_capability_ref` | text | да | Ссылка на capability установленного plugin package. |
| `package_installation_ref` | text | да | Ссылка на установку пакета в `package-hub`. |
| `package_version_ref` | text | да | Ссылка на выбранную версию пакета в `package-hub`; package truth остаётся там. |
| `routing_policy_ref` | text | да | Ссылка на правила маршрутизации: приоритеты, fallback, quiet hours, retry strategy. Тело policy не хранится в route в текущем контракте. |
| `callback_route_ref` | text | да | Ссылка на gateway-owned callback route, если маршрут поддерживает callback. |
| `runtime_ref` | text | да | Ссылка на package-owned runtime boundary; Kubernetes job/workload не принадлежит `interaction-hub`. |
| `status` | enum | нет | `active`, `paused`, `disabled`. |
| `created_at`, `updated_at` | timestamptz | нет | Временные метки. |

`DeliveryRoute` не заменяет package installation. Он хранит только доменную привязку маршрута к запросам и доставке.

Если `interaction-hub` понадобится service-local таблица policy для reminder или routing rules, она вводится отдельным срезом после явного расширения контракта. `IH-3` хранит только `reminder_policy_ref` и `routing_policy_ref`.

### DeliveryAttempt

`DeliveryAttempt` описывает одну попытку доставки request или notification.

Подписочный контекст проходит через связанное уведомление: `notification_id` указывает на `Notification`, а `Notification.subscription_id` хранит safe ref подписки. Прямая delivery attempt цель по subscription не вводится в текущем proto-контракте.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор попытки. |
| `request_id` | uuid | да | Запрос, если попытка доставляет feedback, approval или Human gate request. |
| `notification_id` | uuid | да | Уведомление, если попытка доставляет one-way notification или reminder. |
| `route_id` | uuid | нет | Выбранный маршрут. |
| `delivery_id` | text | нет | Идемпотентный ключ channel contract. |
| `status` | enum | нет | `queued`, `sent`, `accepted`, `delivered`, `failed`, `cancelled`, `expired`. |
| `channel_message_ref` | text | да | Безопасная ссылка на сообщение канала. |
| `attempt_number` | int | нет | Номер попытки. |
| `next_retry_at` | timestamptz | да | Следующая попытка. |
| `error_code` | text | да | Короткий безопасный код ошибки. |
| `error_class` | enum | да | `temporary`, `permanent`, `auth`, `rate_limited`, `policy`. |
| `payload_digest` | text | нет | Digest нормализованного delivery command. |
| `result_fingerprint` | text | да | Digest нормализованного safe delivery result; используется для идемпотентного replay по `delivery_id` без повторного outbox event. |
| `channel_capability_ref` | text | да | Снимок выбранной channel capability на момент планирования. |
| `package_installation_ref` | text | да | Снимок установки channel package без владения package state. |
| `package_version_ref` | text | да | Снимок выбранной версии channel package. |
| `delivery_command_ref` | text | да | Ссылка на safe delivery command envelope для runtime boundary. |
| `callback_ref` | text | да | Непрозрачная ссылка сопоставления callback. |
| `callback_route_ref` | text | да | Ссылка на callback route, если настроена. |
| `runtime_ref` | text | да | Ссылка на package-owned runtime boundary. |
| `runtime_job_ref` | text | да | Safe ref runtime job/workload, если runtime сообщил его результатом доставки. |
| `routing_policy_ref` | text | да | Снимок routing policy ref. |
| `created_at`, `updated_at`, `sent_at` | timestamptz | да | Временные метки. |

Ровно одно из полей `request_id` или `notification_id` должно быть заполнено. Статус request не выводится из статуса one-way notification; request завершает только `InteractionResponse` или доменная команда истечения/отмены.

### ChannelCallback

`ChannelCallback` хранит безопасно нормализованный callback от внешнего канала.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор записи callback. |
| `callback_id` | text | нет | Идемпотентный ключ callback. |
| `delivery_id` | text | да | Safe delivery id из callback envelope. |
| `delivery_attempt_id` | uuid | да | Попытка доставки, если сопоставлена. |
| `request_id` | uuid | да | Запрос, если сопоставлен. |
| `source_route_id` | uuid | да | Маршрут канала. |
| `actor_ref` | text | да | Проверенный actor, если gateway смог его определить. |
| `action` | text | да | Действие callback. |
| `callback_summary` | text | да | Короткая безопасная сводка. |
| `callback_object_ref` | text | да | Ссылка на полный payload или вложения после sanitization. |
| `signature_status` | enum | нет | `verified`, `trusted_internal`, `rejected_before_domain`. |
| `processing_status` | enum | нет | `accepted`, `duplicate`, `rejected`, `failed`. |
| `error_code` | text | да | Безопасный код ошибки обработки. |
| `callback_route_ref` | text | да | Safe ref gateway-owned callback route. |
| `gateway_ref` | text | да | Safe ref gateway request. |
| `correlation_id` | text | да | Сквозная корреляция callback с delivery/owner context. |
| `callback_fingerprint` | text | да | Digest нормализованного safe callback envelope для replay/conflict. |
| `received_at`, `created_at` | timestamptz | нет | Временные метки. |

`interaction-hub` не хранит сырую подпись, токены, секреты канала и полный внешний payload. Публичная проверка выполняется gateway до вызова домена.

Если callback сопоставлен с feedback, approval или Human gate request и выбирает разрешённый terminal action, `interaction-hub` создаёт `InteractionResponse` и переводит request в `answered` в одной транзакции с callback record. Callback к terminal request, callback с недопустимым action или callback без достаточного safe actor/response context сохраняется как `processing_status=rejected` с bounded `error_code`; такая запись не создаёт новый response и не меняет owner business decision state.

### OwnerInboxItem

`OwnerInboxItem` — read-модель API, а не отдельная таблица. Она собирается из `InteractionRequest`, последней `DeliveryAttempt`, последнего `ChannelCallback` и итогового `InteractionResponse`.

Поля ответа ограничены safe summary/refs:

- request id, kind, status, scope, requester/source owner, assignee refs и context refs;
- title и summary из bounded `InteractionRequest.prompt_summary`;
- deadline, reminder policy ref, timestamps и version;
- delivery summary: attempt count, latest delivery id/status/error class/error code, retry time, route/channel message refs;
- latest callback diagnostic: callback id/ref, delivery id, signature/processing status, actor/action, error code, gateway/correlation refs;
- latest response summary: response id/ref, action, actor, source kind/source ref, owner decision ref, bounded response summary, summary digest и sanitized response object ref/digest.

Read-модель не возвращает raw prompt object, raw callback payload, raw response body, headers, tokens, provider payload, stdout/stderr, logs или расширенную PII. Полный ответ доступен только как sanitized object ref/digest после отдельной политики хранения; operator/owner read surface получает bounded safe summary и digest.

### CommandResult

`CommandResult` хранит идемпотентный след команд.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `key` | text | нет | Первичный ключ идемпотентного следа. |
| `command_id` | uuid | да | Глобальный ключ команды. |
| `idempotency_key` | text | да | Альтернативный ключ в области actor + operation. |
| `actor_ref` | text | нет | Инициатор команды. |
| `operation` | text | нет | Имя операции. |
| `aggregate_type` | text | нет | `thread`, `request`, `notification`, `subscription`, `delivery`, `callback`. |
| `aggregate_id` | uuid | нет | Затронутый агрегат. |
| `request_fingerprint` | text | нет | Digest безопасного входа для replay conflict. |
| `result_payload` | jsonb | нет | Безопасный результат повтора. |
| `created_at` | timestamptz | нет | Время первой записи. |

### OutboxEvent

`OutboxEvent` фиксируется в одной транзакции с изменением агрегата и публикуется через `platform-event-log`.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор события. |
| `aggregate_type` | text | нет | Тип агрегата. |
| `aggregate_id` | uuid | нет | Идентификатор агрегата. |
| `event_type` | text | нет | Имя события `interaction.*`. |
| `schema_version` | int | нет | Версия схемы события. |
| `payload` | jsonb | нет | Минимальная полезная нагрузка. |
| `occurred_at` | timestamptz | нет | Когда событие произошло. |

## Связи

| Связь | Кардинальность | Правило |
|---|---|---|
| `ConversationThread` -> `ConversationMessage` | 1:N | Сообщения не меняют thread lifecycle без команды владельца. |
| `ConversationThread` -> `InteractionRequest` | 1:N | Запрос может быть создан из диалога или без него. |
| `InteractionRequest` -> `InteractionResponse` | 1:0..1 | Terminal request имеет один итоговый ответ. |
| `InteractionRequest` -> `DeliveryAttempt` | 1:N | Повторы и fallback фиксируются отдельными попытками. |
| `Notification` -> `DeliveryAttempt` | 1:N | Уведомление может доставляться нескольким получателям и поверхностям. |
| `Subscription` -> `Notification` | 1:N | Подписка может создавать много уведомлений. |
| `DeliveryAttempt` -> `ChannelCallback` | 1:N | Callback может быть повторён; идемпотентность по `callback_id`. |
| `ChannelCallback` -> `InteractionResponse` | 1:0..1 | Только accepted terminal callback создаёт итоговый response через `source_kind=channel_callback`. |

## Индексы и критичные запросы

| Запрос | Индексы |
|---|---|
| Найти активные request по scope и status | `(scope_type, scope_ref, status, deadline_at)` |
| Найти request по владельцу сценария | `(source_owner_kind, source_owner_ref)` |
| Фильтровать owner inbox по assignee/correlation refs | JSONB GIN по `target_refs` и `context_refs` |
| Найти попытки доставки для retry | `(status, next_retry_at, route_id)` |
| Найти callback по idempotency key | unique `(callback_id)` |
| Найти response по callback source | partial unique `(source_kind, source_ref)` для `source_kind='channel_callback'` |
| Найти delivery attempt по channel delivery id | unique `(delivery_id)` |
| Найти подписки по scope и event filter | `(scope_type, scope_ref, status)` плюс JSONB GIN по `event_filter` при необходимости |
| Найти сообщения ветки | `(thread_id, created_at)` |

## Политика хранения данных

- Диалоговые сообщения и ответы хранятся по retention class, заданному request или scope policy.
- Голосовые записи, медиа, полные ответы и большие callback payload хранятся во внешнем объектном хранилище ссылками.
- Delivery attempts и callback records хранятся достаточно долго для аудита, расследования доставки и повторов, затем архивируются или агрегируются по политике.
- Сырые внешние подписи, токены, секреты и непроверенные payload не хранятся.
- Публичные идентификаторы внешних каналов хранятся только как safe refs, если они нужны для troubleshooting и не раскрывают секреты.

## Апрув

- request_id: `owner-2026-05-22-interaction-hub-kickoff`
- Решение: approved
- Комментарий: модель данных `interaction-hub` согласована как стартовое целевое состояние.
