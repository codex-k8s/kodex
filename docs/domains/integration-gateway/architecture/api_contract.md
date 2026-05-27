---
doc_id: API-CK8S-INTEGRATION-GATEWAY-0001
type: api-contract
title: kodex — API-обзор integration-gateway
status: active
owner_role: SA
created_at: 2026-05-25
updated_at: 2026-05-27
related_issues: [781, 792, 807, 770, 853]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-25-integration-gateway-igw-0"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-25
---

# API-обзор: integration-gateway

## TL;DR

- Тип API: внешний HTTP/OpenAPI для webhook и callback событий, внутренние gRPC-вызовы к сервисам-владельцам.
- Активные MVP routes: `POST /v1/provider-webhooks/{provider_slug}` -> `provider-hub.IngestWebhookEvent` и `POST /v1/external-callbacks/{callback_source}` -> `interaction-hub.RecordChannelCallback`.
- Версионирование: внешняя gateway-поверхность описывается в `specs/openapi/integration-gateway.v1.yaml`.
- Принцип: gateway проверяет и маршрутизирует, сервис-владелец хранит бизнес-состояние и выполняет доменную обработку.

## Спецификации

| Вид | Путь | Статус |
|---|---|---|
| OpenAPI | `specs/openapi/integration-gateway.v1.yaml` | HTTP-каркас gateway для MVP provider webhook и generic callback route. |
| gRPC к `provider-hub` | `proto/kodex/providers/v1/provider_hub.proto` | Используется операция `IngestWebhookEvent`. |
| gRPC к `interaction-hub` | `proto/kodex/interactions/v1/interaction_hub.proto` | Используется операция `RecordChannelCallback`. |

`integration-gateway` не создаёт отдельный proto в IGW-0, потому что он не владеет доменными командами. Внутренние вызовы идут в сервисы-владельцы по их gRPC-контрактам.

## Внешние операции

| HTTP endpoint | Назначение | Внутренний владелец | Статус |
|---|---|---|---|
| `POST /v1/provider-webhooks/{provider_slug}` | Принять GitHub provider webhook по `provider_slug=github`. | `provider-hub.IngestWebhookEvent` | Активный route с source binding, проверкой `X-Hub-Signature-256`, per-source limits и backpressure guard; другие providers добавляются отдельными срезами. |
| `POST /v1/external-callbacks/{callback_source}` | Принять generic callback внешнего канала или пакета. | `interaction-hub.RecordChannelCallback` | Активный generic route с source binding, HMAC SHA-256 проверкой `X-Kodex-External-Signature`, payload limit, per-source guard и safe envelope без vendor-specific semantics. |

## Provider webhook envelope

После активации provider route gateway передаёт в `provider-hub.IngestWebhookEvent`:

| Поле | Источник | Правило |
|---|---|---|
| `provider_slug` | path parameter | Проверяется по route registry. |
| `delivery_id` | `X-GitHub-Delivery` | Обязателен для активного GitHub provider webhook и используется для идемпотентности. |
| `event_name` | `X-GitHub-Event` | Gateway не интерпретирует бизнес-смысл события. |
| `repository_provider_id` | Не заполняется в базовом MVP | Может появиться только если provider даёт безопасный edge metadata без разбора payload. |
| `payload_json` | HTTP body | Передаётся в `provider-hub` после size guard; не пишется в gateway logs. |
| `received_at` | Время приёма gateway | UTC timestamp. |
| `meta` | Внутренний command context | `source=integration-gateway`, correlation id, request id и безопасная idempotency-связь. |

## External channel callback envelope

Активный callback route принимает только generic safe envelope, уже очищенный channel package или внешним каналом от vendor-specific raw payload:

| Поле | Источник | Правило |
|---|---|---|
| `callback_source` | path parameter | Проверяется по route registry и используется только как source binding/diagnostic label. |
| `callback_id` | JSON body, optional `X-Kodex-External-Delivery` match | Обязательный ключ идемпотентности для `interaction-hub`; gateway сам не хранит dedupe state. |
| `delivery_id` / `request_ref` | JSON body | Должен быть передан хотя бы один safe ref для сопоставления у владельца. |
| `contract_version` | JSON body | Версия generic channel callback contract. |
| `actor_ref` | JSON body | Safe ref внешнего субъекта без PII. |
| `action` | JSON body | Owner-defined action key; gateway не интерпретирует бизнес-смысл действия. |
| `answer_summary` / `answer_object` | JSON body | Только bounded summary или safe object ref; raw transcript/provider payload не принимается как contract field. |
| `signature_status` | Gateway | Во внутренний вызов передаётся `VERIFIED` только после HMAC SHA-256 проверки. |
| `gateway_ref`, `received_at`, `correlation_id` | Gateway / JSON body | Safe gateway request ref, UTC timestamp и correlation id. |

## Валидация и отказы

| HTTP status | Причина |
|---:|---|
| `202` | Событие проверено на границе и передано сервису-владельцу. |
| `400` | Некорректный route, отсутствует обязательный delivery id/event name, неподдерживаемый content type или malformed JSON. |
| `401` | Подпись, токен или source binding не прошли проверку. |
| `413` | Payload превышает лимит route. |
| `429` | Сработал rate limit по source/route/downstream. |
| `503` | Downstream owner service недоступен или backpressure guard временно закрыт маршрут. |

Тело ошибки содержит только безопасный код, короткое сообщение, `request_id`, `correlation_id` и retryable flag. Для `429` и edge backpressure `503` gateway добавляет `Retry-After`. Секреты, подписи и полный payload не возвращаются.

## Идемпотентность и retry

- Provider webhook дедуплицируется доменным владельцем `provider-hub` по `provider_slug + delivery_id`.
- Повтор с тем же GitHub delivery id проходит тот же edge-контур и передаётся владельцу; gateway не заводит собственный cache, inbox или бизнес-состояние для дедупликации.
- Gateway не создаёт скрытую очередь side effects. Если downstream недоступен, внешний sender получает retryable ответ.
- Для callback route `callback_id` является обязательным idempotency key владельца; gateway передаёт повтор в `interaction-hub`, не создавая cache, inbox или собственное бизнес-состояние.
- Повтор с тем же callback id должен возвращать безопасный ответ без создания второго бизнес-события у владельца.

## Безопасность

- Подписи проверяются до передачи payload владельцу.
- Значения секретов разрешаются только по ссылке и удерживаются в памяти процесса на время проверки.
- Gateway не хранит raw payload в собственной БД.
- Логи, ошибки и метрики проходят redaction до записи.
- Safe audit summary ограничен route/source/status/latency/payload size bucket/reject reason и не содержит signature, token, secret или полный payload.
- Внутренний gRPC-вызов содержит только безопасный edge context и payload, предназначенный сервису-владельцу.
- Активный provider route принимает только GitHub webhook с валидной HMAC SHA-256 подписью и настроенной ссылкой на webhook secret.
- Активный callback route принимает только включённый `callback_source` с валидной HMAC SHA-256 подписью `X-Kodex-External-Signature` и настроенной ссылкой на callback secret.

## Совместимость

- Новые HTTP endpoints добавляются только после фиксации owner-service и внутренней операции владельца.
- Изменение обязательных полей ответа или ошибки требует новой версии OpenAPI-поверхности.
- Новые callback owners кроме `interaction-hub.RecordChannelCallback` добавляются только после фиксации owner-service lifecycle, idempotency и error mapping.

## Апрув

- request_id: `owner-2026-05-25-integration-gateway-igw-0`
- Решение: approved
- Комментарий: API-обзор `integration-gateway` согласован как целевое состояние IGW-0.
