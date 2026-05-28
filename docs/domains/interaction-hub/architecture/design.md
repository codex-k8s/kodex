---
doc_id: DSG-CK8S-INTERACTION-HUB-0001
type: design-doc
title: kodex — дизайн домена центра взаимодействий
status: active
owner_role: SA
created_at: 2026-05-22
updated_at: 2026-05-28
related_issues: [582, 768, 781, 800, 821, 835, 843, 853, 867, 882, 911, 921, 928]
related_prs: []
related_adrs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-22-interaction-hub-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-22
---

# Детальный дизайн: центр взаимодействий

## TL;DR

- Что меняем: выделяем `interaction-hub` как владельца диалогов, запросов к человеку, approval request, Human gate, уведомлений, подписок, delivery attempts и callback внешних каналов.
- Почему: owner feedback loop и dual-surface inbox должны иметь один lifecycle независимо от UI, голоса, MCP или внешнего канала.
- Основные компоненты: БД `interaction-hub`, lifecycle engine запросов, delivery planner, channel contract boundary, callback resolver, subscription engine и outbox `interaction.*`.
- Риски: превратить `interaction-hub` в UI, gateway, runtime плагинов, package-hub или agent-manager. Эти контуры остаются у соседних владельцев.

## Цели

- Зафиксировать границы `interaction-hub` до контрактов и кода.
- Подготовить первый кодовый PR с proto/AsyncAPI контрактами и следующий PR с сервисным каркасом.
- Описать единый lifecycle feedback, approval, Human gate и notification delivery.
- Закрепить гибридную модель внешних каналов: package-owned runtime плюс stable channel delivery/callback contract.
- Развести `interaction-hub`, `agent-manager`, `platform-mcp-server`, `codex-hook-ingress`, `provider-hub`, `operations-hub`, `integration-gateway` и plugin packages.

## Не-цели

- Не реализовывать код, proto, AsyncAPI, OpenAPI или gateway в стартовом документационном срезе.
- Не проектировать конкретный внешний канал.
- Не выбирать vendor-specific список каналов.
- Не владеть flow, `Run`, session, acceptance, runtime job, provider write operation, package installation или UI state.
- Не заменять пакетную модель новым магазином, каталогом или отдельным plugin registry.

## Граница сервиса

| Владеет `interaction-hub` | Не владеет |
|---|---|
| Диалоговые ветки, сообщения, feedback request, approval request, Human gate request, уведомления, подписки, delivery route choice, delivery attempts, reminders, callback records, ответы человека, события `interaction.*`. | Flow, stage, role, prompt, agent session, agent `Run`, acceptance result, provider write pipeline, provider projections, risk/gate/release/provider business decision state, package catalog, package installation, runtime job, Kubernetes workload, UI, внешний HTTP gateway, операции биллинга. |

Правило: `interaction-hub` отвечает за вопрос "какой запрос к человеку существует, как он доставляется и какой ответ получен". Соседний сервис-владелец отвечает за проверку ответа и изменение своего business decision state.

## Выбранная модель внешних каналов

Выбрана гибридная модель:

1. Канал поставляется как `plugin` package через `package-hub`.
2. `package-hub` владеет package entry, version, manifest, installation, secret schema и verification.
3. `runtime-manager` и `fleet-manager` исполняют runtime-нагрузку установленного пакета.
4. `interaction-hub` хранит только channel binding refs, delivery attempts, callback records и lifecycle запроса.
5. Стабильный `ChannelDeliveryContract` описывает delivery command, delivery result, callback envelope и error model.
6. `integration-gateway` принимает внешний HTTP callback, проверяет публичную подпись и маршрутизирует безопасный внутренний вызов в `interaction-hub`.

Эта модель фиксирует lifecycle channel contract как доменную истину. Внешний callback-вход находится в `integration-gateway`, но lifecycle, дедупликация и callback record остаются в `interaction-hub`.

## Компоненты

| Компонент | Назначение |
|---|---|
| `interaction-hub` | Сервис-владелец домена взаимодействий. |
| БД `interaction-hub` | Диалоги, запросы, уведомления, подписки, попытки доставки, callback, command result и outbox. |
| Lifecycle engine | Переходы feedback, approval, Human gate и one-way notification/reminder. |
| Delivery planner | Выбор допустимого delivery route по scope, policy, подпискам и channel capability. |
| Channel contract boundary | Стабильный контракт доставки в установленный channel package и обратного callback. |
| Callback resolver | Идемпотентная привязка callback к delivery attempt/request и безопасное применение terminal callback к request response lifecycle. |
| Owner inbox reader | Авторитетное чтение pending/active request, detail item и callback diagnostics по собственным сущностям `interaction-hub`. |
| Subscription engine | Правила подписки на события и области, создание notification intent и reminders. |
| Outbox-доставщик | Публикация `interaction.*` событий через `platform-event-log`. |

Текущая сервисная основа реализует authoritative lifecycle `Notification`, `Subscription`, delivery attempts и safe callback records: создание notification intent, создание/изменение/отключение/чтение подписок, `PlanDelivery`, `RecordDeliveryResult`, `RecordChannelCallback`, `GetDeliveryStatus`, `ListOwnerInboxItems`, `GetOwnerInboxItem`, command idempotency, optimistic concurrency для subscription и safe `interaction.*` outbox events. Внешний `integration-gateway` callback route передаёт generic safe envelope в `RecordChannelCallback`; `RecordChannelCallback` применяет допустимый terminal callback к feedback/approval/Human gate request как `InteractionResponse`, но не фиксирует owner business decision. Событие `interaction.request.response_recorded` содержит safe request/response refs, request kind, scope, source owner, decision owner, agent/provider/governance context refs, normalized outcome, digest/object refs, timestamps и correlation/idempotency digest, чтобы owner service мог возобновить свой lifecycle через собственную границу без чтения raw response text. Owner inbox read surface отдаёт только собственные request/delivery/callback/response summaries `interaction-hub`, включая bounded response summary, digest, allowed actions и version для безопасной команды ответа; `request_changes` отделяет запрос доработки от отказа `reject` и передаётся как normalized outcome для соседнего владельца решения. Первый `staff-gateway` контур публикует этот flow наружу через OpenAPI и остаётся тонким маршрутизатором к `interaction-hub`; междоменная агрегация остаётся у следующих срезов `staff-gateway`/`operations-hub`. Конкретные channel packages и runtime worker остаются отдельным контуром.

## Основные потоки

### Slot-агент просит обратную связь

```mermaid
sequenceDiagram
  participant Runner as slot-agent
  participant MCP as platform-mcp-server
  participant IH as interaction-hub
  participant PKG as package-hub
  participant R as runtime-manager
  participant CH as channel package workload
  participant GW as integration-gateway
  participant AM as agent-manager
  Runner->>MCP: interaction.feedback.request
  MCP->>IH: RequestFeedback(command)
  IH->>PKG: read installed channel capability
  IH->>IH: create request + delivery attempt + outbox
  IH->>R: deliver to package workload endpoint
  R->>CH: DeliverInteraction(channel contract)
  CH-->>R: accepted or failed delivery result
  R-->>IH: RecordDeliveryResult(delivery_id)
  IH-->>MCP: request ref + status
  CH-->>GW: callback with answer
  GW->>IH: RecordChannelCallback(safe envelope)
  IH->>IH: resolve request
  IH-->>AM: interaction.request.response_recorded event
```

Slot-агент не выбирает внешний канал и не получает секреты канала. MCP проверяет actor/source/run/session/slot binding и маршрутизирует команду к владельцу. `interaction-hub` не создаёт runtime job сам: delivery command передаётся в уже согласованный runtime boundary для package workload, а публичный callback проходит через профильный gateway.

### Approval request для provider write

```mermaid
sequenceDiagram
  participant AM as agent-manager
  participant GOV as governance-manager
  participant IH as interaction-hub
  participant PH as provider-hub
  AM->>GOV: RequestGate(provider operation context)
  GOV->>IH: RequestApproval(delivery request)
  IH->>IH: create approval request
  IH->>IH: deliver via UI or channel route
  IH-->>GOV: approval request ref
  IH->>IH: record interaction response
  GOV->>GOV: validate actor and record owner decision
  GOV-->>AM: owner decision ref
  AM->>PH: provider write command with owner decision ref
  PH->>PH: execute provider command by its own pipeline
```

`interaction-hub` не выполняет provider write и не принимает provider approval как бизнес-решение. Он возвращает callback/response result владельцу decision state, а `provider-hub` применяет свой typed write pipeline только с decision ref от владельца.

### Human gate в agent flow

```mermaid
sequenceDiagram
  participant AM as agent-manager
  participant GOV as governance-manager
  participant IH as interaction-hub
  participant Ops as operations-hub
  AM->>GOV: RequestGate(run/stage/risk context)
  GOV->>IH: RequestHumanGate(delivery request)
  IH->>IH: create gate request + reminders policy
  IH-->>Ops: interaction.human_gate.requested event
  IH->>IH: record interaction response
  IH-->>GOV: interaction.request.response_recorded event
  GOV-->>AM: governance decision ref
  AM->>AM: update Run or acceptance state
```

`agent-manager` остаётся владельцем `Run`, stage и acceptance. `governance-manager` владеет gate decision, а `interaction-hub` хранит запрос доставки и ответ человека.

### Callback внешнего канала

```mermaid
sequenceDiagram
  participant External as external channel
  participant GW as integration-gateway
  participant IH as interaction-hub
  participant Ops as operations-hub
  External->>GW: callback
  GW->>GW: authenticate, verify signature, sanitize payload
  GW->>IH: RecordChannelCallback(safe envelope)
  IH->>IH: idempotency + callback record + terminal response transition
  IH-->>Ops: interaction.callback.received / interaction.request.response_recorded
```

Публичная проверка подписи и rate limit живут в `integration-gateway`. `interaction-hub` принимает только безопасный внутренний envelope, сопоставляет его с delivery/request, сохраняет callback record и создаёт `InteractionResponse`, если request активен и action является разрешённым завершающим действием. Для завершённого request повторный или поздний callback сохраняется как diagnostic no-op без повторного response.

### Входящие решения владельца

```mermaid
sequenceDiagram
  participant UI as web-console
  participant SG as staff-gateway
  participant IH as interaction-hub
  participant Ops as operations-hub
  UI->>SG: list pending decisions
  SG->>IH: ListOwnerInboxItems(scope/filter/page)
  IH->>IH: read requests + delivery/callback/response summaries
  IH-->>SG: safe owner inbox items
  UI->>SG: open one decision
  SG->>IH: GetOwnerInboxItem(request_id, scope, assignee_ref)
  IH-->>SG: safe detail + allowed actions + version
  UI->>SG: choose action
  SG->>IH: RecordInteractionResponse(command_id, expected_version, safe action/refs)
  IH-->>SG: safe response refs/status
  Ops-->>SG: later cross-domain projections
```

`ListOwnerInboxItems` и `GetOwnerInboxItem` — доменное авторитетное чтение только по interaction-сущностям. List возвращает pending/active feedback, approval, Human gate request и callback diagnostics с фильтрами по scope, kind/status, assignee, actor и correlation refs. Detail открывает один request по `request_id + scope`, опционально ограничивает чтение `assignee_ref`, возвращает allowed actions, safe owner/source/context refs, latest delivery/callback/response summaries, timestamps и version. Основные действия для UI: утвердить — `approve`, отклонить — `reject`, запросить доработку — `request_changes`, ответить — `answer`. `request_changes` требует safe summary или object ref с описанием доработки и не должен смешиваться с `reject`. Ответ владельца проходит через существующий `RecordInteractionResponse` с expected version и idempotency; `staff-gateway` передаёт эту команду из OpenAPI в gRPC без собственной модели решений, а `operations-hub` позже объединяет результат с provider/agent/runtime контекстом.

## Channel delivery contract

Контракт канала описывает смысл данных, которые передаются установленному channel package. Транспорт и конкретный gateway не фиксируются этим документом.

### Delivery command

| Поле | Назначение |
|---|---|
| `delivery_id` | Идемпотентный идентификатор попытки доставки. |
| `request_ref` | Ссылка на feedback, approval или Human gate request. |
| `notification_ref` | Ссылка на one-way notification или reminder, если delivery не связан с request. |
| `delivery_kind` | `feedback`, `approval`, `human_gate`, `notification`. |
| `scope` | Platform, организация, проект, репозиторий или сервис. |
| `recipient_refs` | Пользователи, группы или роли получателей без раскрытия лишних PII. |
| `message_template_ref` | Ссылка на локализуемый шаблон или безопасный текст сообщения. |
| `actions` | Допустимые действия ответа: `answer`, `approve`, `reject`, `request_changes`, `defer`, `acknowledge` или `custom` action key. |
| `callback_ref` | Внутренняя ссылка, по которой gateway сможет сопоставить callback. |
| `correlation_id` | Связь с run, provider operation, runtime job, issue или инцидентом. |
| `expires_at` | Срок действия запроса или попытки доставки. |
| `retention_class` | Политика хранения callback и вложений. |
| `package_installation_ref` / `package_version_ref` | Safe refs установленного channel package; package truth остаётся у `package-hub`. |
| `channel_capability_ref` | Capability ref, выбранная route planner. |
| `delivery_command_ref` | Safe ref подготовленного runtime command envelope. |
| `callback_route_ref` | Safe ref gateway-owned callback route. |
| `runtime_ref` | Safe ref package-owned runtime boundary. |

### Delivery result

| Поле | Назначение |
|---|---|
| `delivery_id` | Та же попытка доставки. |
| `accepted` | Пакет принял попытку к доставке. |
| `delivered` | Пакет сообщил доставку на внешнюю поверхность. |
| `expired` | Пакет или runtime сообщил истечение попытки. |
| `channel_message_ref` | Безопасная ссылка на сообщение канала, если она есть. |
| `retry_after` | Когда можно повторить, если ошибка временная. |
| `error_code` | Короткий безопасный код ошибки. |
| `error_class` | `temporary`, `permanent`, `auth`, `rate_limited`, `policy`. |

### Callback envelope

| Поле | Назначение |
|---|---|
| `callback_id` | Идемпотентный идентификатор callback от gateway или channel package. |
| `delivery_id` | Связь с попыткой доставки. |
| `request_ref` | Исходный запрос. |
| `actor_ref` | Проверенный пользователь, внешний субъект или service principal. |
| `action` | Выбранное действие. |
| `answer_summary` | Короткая безопасная сводка ответа. |
| `answer_object_ref` | Ссылка на полный ответ или вложения, если они хранятся вне PostgreSQL. |
| `received_at` | Время получения callback. |
| `signature_status` | Результат проверки gateway, без сырой подписи. |
| `gateway_ref`, `correlation_id` | Safe refs для диагностики и трассировки без заголовков и raw payload. |

## Междоменные связи

| Домен или сервис | Связь |
|---|---|
| `agent-manager` | Создаёт feedback и получает события ответа; для Human gate ждёт governance decision ref и сам меняет `Run`, session и acceptance. |
| `platform-mcp-server` | Публикует MCP tools `interaction.*`, проверяет source/run/session/slot binding и маршрутизирует вызовы к `interaction-hub`. |
| `codex-hook-ingress` | Передаёт нормализованные hook events, которые требуют разрешения, вопроса или уведомления человеку. |
| `provider-hub` | Использует owner decision ref и provider refs; provider write pipeline остаётся у `provider-hub`. |
| `package-hub` | Даёт сведения об установленном channel package, manifest capability и required platform APIs; установка и секреты пакета остаются там. |
| `runtime-manager` и `fleet-manager` | Исполняют runtime-нагрузку channel package; `interaction-hub` не создаёт jobs сам. |
| `access-manager` | Проверяет права создания запроса, отправки ответа, чтения статуса и использования channel package. |
| `operations-hub` | Получает события и читает авторитетные статусы для операторской очереди и dual-surface inbox; cross-domain aggregation остаётся у него, а `interaction-hub` отдаёт только собственные inbox items. |
| `integration-gateway` | Выполняет внешнюю аутентификацию, public rate limit, signature verification и маршрутизацию callback. |

## События

Минимальные события:

- `interaction.thread.created`;
- `interaction.message.recorded`;
- `interaction.feedback.requested`;
- `interaction.approval.requested`;
- `interaction.human_gate.requested`;
- `interaction.notification.requested`;
- `interaction.subscription.updated`;
- `interaction.delivery.requested` с safe refs попытки, route и request/notification target;
- `interaction.delivery.accepted` с safe channel message ref;
- `interaction.delivery.delivered`;
- `interaction.delivery.failed` с bounded diagnostics и retry metadata;
- `interaction.delivery.expired`;
- `interaction.callback.received`;
- `interaction.request.response_recorded`;
- `interaction.request.expired`;
- `interaction.request.cancelled`.

События публикуются через service-local outbox и общий `platform-event-log`. Payload не содержит сырой внешний payload, токены, secret refs, полные медиа, полный prompt или большие вложения.

## Конкурентные изменения

- Каждый изменяемый request имеет `version`.
- Команда ответа передаёт expected version или идемпотентный `command_id`.
- Повтор callback с тем же `callback_id` возвращает уже сохранённый безопасный callback/response результат.
- Две разные попытки решить один request не создают второй response; поздний callback к завершённому request фиксируется как безопасный diagnostic no-op.
- Долгие ожидания человека не держат SQL-блокировку; срок ожидания хранится в request, а правила напоминаний передаются как `reminder_policy_ref`.
- Повтор delivery command не создаёт новую попытку, если `delivery_id` уже принят с тем же безопасным отпечатком.

## Наблюдаемость

- Логи: request id, request kind, scope, source service, delivery id, channel package ref, correlation id, outcome и safe error code.
- Метрики: количество активных запросов, время до первого delivery attempt, время до ответа, retry count, callback duplicates, expired requests и delivery failures.
- Трейсы: входящий gRPC/MCP, проверка доступа, выбор delivery route, вызов channel package, запись callback, outbox publish.
- Алерты: рост застрявших Human gate, массовые ошибки delivery, истёкшие approval requests, недоступность channel package и всплеск callback conflicts.

## Риски

| Риск | Митигирующее решение |
|---|---|
| `interaction-hub` начнёт владеть flow/run/session. | В request хранить только refs; переходы `Run` выполняет `agent-manager`. |
| Внешний канал станет источником правды. | Channel package только доставляет и возвращает callback; lifecycle request хранится в `interaction-hub`. |
| Домен превратится в gateway. | Публичная аутентификация, подпись и HTTP находятся в `integration-gateway`; домен принимает безопасный internal envelope. |
| Домен начнёт управлять пакетами. | Использовать `package-hub` readings и refs, не менять installation и manifest. |
| Provider write смешается с approval. | `interaction-hub` хранит response/callback result; final approval decision остаётся у владельца decision state, а `provider-hub` выполняет write по своей политике и pipeline. |

## Апрув

- request_id: `owner-2026-05-22-interaction-hub-kickoff`
- Решение: approved
- Комментарий: дизайн `interaction-hub` согласован как стартовое целевое состояние; внешний канал подключается через package-owned runtime и стабильный channel contract.
