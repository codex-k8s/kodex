---
doc_id: API-CK8S-INTEGRATION-GATEWAY-0001
type: api-contract
title: kodex — API-обзор integration-gateway
status: active
owner_role: SA
created_at: 2026-05-25
updated_at: 2026-05-26
related_issues: [781, 792, 770]
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
- Первый MVP route: `POST /v1/provider-webhooks/{provider_slug}` -> `provider-hub.IngestWebhookEvent`.
- Версионирование: внешняя gateway-поверхность описывается в `specs/openapi/integration-gateway.v1.yaml`.
- Принцип: gateway проверяет и маршрутизирует, сервис-владелец хранит бизнес-состояние и выполняет доменную обработку.

## Спецификации

| Вид | Путь | Статус |
|---|---|---|
| OpenAPI | `specs/openapi/integration-gateway.v1.yaml` | HTTP-каркас gateway для MVP provider webhook и резервной callback-поверхности. |
| gRPC к `provider-hub` | `proto/kodex/providers/v1/provider_hub.proto` | Используется операция `IngestWebhookEvent`. |

`integration-gateway` не создаёт отдельный proto в IGW-0, потому что он не владеет доменными командами. Внутренние вызовы идут в сервисы-владельцы по их gRPC-контрактам.

## Внешние операции

| HTTP endpoint | Назначение | Внутренний владелец | Статус |
|---|---|---|---|
| `POST /v1/provider-webhooks/{provider_slug}` | Принять provider webhook от GitHub/GitLab или другого provider source. | `provider-hub.IngestWebhookEvent` | Сервисный stub IGW-1, реальная активация после проверки подписи. |
| `POST /v1/external-callbacks/{callback_source}` | Принять callback внешнего канала, пакета или интеграции. | `interaction-hub`, `package-hub` или другой владелец по route registry | Контрактный задел, активируется отдельным owner-срезом. |

## Provider webhook envelope

После активации provider route gateway передаёт в `provider-hub.IngestWebhookEvent`:

| Поле | Источник | Правило |
|---|---|---|
| `provider_slug` | path parameter | Проверяется по route registry. |
| `delivery_id` | Provider header или согласованный idempotency header | Обязателен для MVP provider webhook. |
| `event_name` | Provider event header | Gateway не интерпретирует бизнес-смысл события. |
| `repository_provider_id` | Не заполняется в базовом MVP | Может появиться только если provider даёт безопасный edge metadata без разбора payload. |
| `payload_json` | HTTP body | Передаётся в `provider-hub` после size guard; не пишется в gateway logs. |
| `received_at` | Время приёма gateway | UTC timestamp. |
| `meta` | Внутренний command context | `source=integration-gateway`, correlation id, request id и безопасная idempotency-связь. |

## Валидация и отказы

| HTTP status | Причина |
|---:|---|
| `202` | Событие проверено на границе и передано сервису-владельцу. |
| `400` | Некорректный route, отсутствует обязательный delivery id/event name, неподдерживаемый content type или malformed JSON. |
| `401` | Подпись, токен или source binding не прошли проверку. |
| `413` | Payload превышает лимит route. |
| `429` | Сработал rate limit по source/route/downstream. |
| `503` | Downstream owner service недоступен или backpressure guard временно закрыт маршрут. |

Тело ошибки содержит только безопасный код, короткое сообщение, `request_id`, `correlation_id` и retryable flag. Секреты, подписи и полный payload не возвращаются.

## Идемпотентность и retry

- Provider webhook дедуплицируется доменным владельцем `provider-hub` по `provider_slug + delivery_id`.
- Gateway не создаёт скрытую очередь side effects в IGW-0. Если downstream недоступен, внешний sender получает retryable ответ.
- Для callback routes idempotency key является обязательной частью route contract владельца перед активацией endpoint.
- Повтор с тем же delivery id должен возвращать безопасный ответ без создания второго бизнес-события у владельца.

## Безопасность

- Подписи проверяются до передачи payload владельцу.
- Значения секретов разрешаются только по ссылке и удерживаются в памяти процесса на время проверки.
- Gateway не хранит raw payload в собственной БД.
- Логи, ошибки и метрики проходят redaction до записи.
- Внутренний gRPC-вызов содержит только безопасный edge context и payload, предназначенный сервису-владельцу.
- В IGW-1 provider route отключён по умолчанию, потому что проверка подписи и source binding ещё не реализованы.

## Совместимость

- Новые HTTP endpoints добавляются только после фиксации owner-service и внутренней операции владельца.
- Изменение обязательных полей ответа или ошибки требует новой версии OpenAPI-поверхности.
- Callback endpoint нельзя активировать для внешнего канала, пока `interaction-hub` или другой владелец не зафиксировал свой idempotency и callback lifecycle.

## Апрув

- request_id: `owner-2026-05-25-integration-gateway-igw-0`
- Решение: approved
- Комментарий: API-обзор `integration-gateway` согласован как целевое состояние IGW-0.
