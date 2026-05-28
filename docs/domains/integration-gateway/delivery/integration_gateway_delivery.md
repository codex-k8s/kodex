---
doc_id: DLV-CK8S-INTEGRATION-GATEWAY
type: delivery-plan
title: kodex — поставка integration-gateway
status: active
owner_role: EM
created_at: 2026-05-25
updated_at: 2026-05-27
related_issues: [781, 792, 807, 770, 829, 853, 895, 909]
related_prs: []
related_docsets:
  - docs/domains/integration-gateway/product/requirements.md
  - docs/domains/integration-gateway/architecture/design.md
  - docs/domains/integration-gateway/architecture/api_contract.md
  - docs/domains/integration-gateway/ops/integration_gateway_runbook.md
  - docs/domains/integration-gateway/ops/integration_gateway_monitoring.md
  - specs/openapi/integration-gateway.v1.yaml
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-25-integration-gateway-igw-0"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-25
---

# Поставка integration-gateway

## TL;DR

`integration-gateway` поставляется малыми срезами: сначала граница, OpenAPI-каркас и первый provider webhook маршрут, затем сервисный каркас, provider webhook handler, callback routes, эксплуатационный контур и расширение на новые внешние источники.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования | `docs/domains/integration-gateway/product/requirements.md` |
| Дизайн | `docs/domains/integration-gateway/architecture/design.md` |
| API-обзор | `docs/domains/integration-gateway/architecture/api_contract.md` |
| OpenAPI | `specs/openapi/integration-gateway.v1.yaml` |
| Карта Issue | `docs/delivery/issue-map/domains/integration-gateway.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---:|---|
| IGW-0 | #781 | Граница `integration-gateway`, первый MVP route provider webhook -> `provider-hub.IngestWebhookEvent`, требования security/backpressure/retry/idempotency и OpenAPI-каркас зафиксированы. Код сервиса не входит. |
| IGW-1 | #792 | Сервисный каркас: процесс, конфигурация, graceful shutdown, health/readiness/metrics, HTTP router, OpenAPI runtime validation/codegen-модели, payload guard, request id, timeout, structured safe errors, redaction-safe logging и provider-hub client interface без provider business logic. Provider route зарегистрирован как отключённый stub до проверки подписи. |
| IGW-2 | #807 | Реальный route `POST /v1/provider-webhooks/{provider_slug}` для `provider_slug=github`: проверка `X-Hub-Signature-256`, обязательных GitHub headers, лимита payload, idempotency mapping и вызов `provider-hub.IngestWebhookEvent`. |
| IGW-3 | не назначено | Расширение callback routes для внешних каналов и пакетов после готовности дополнительных owner-service contracts. |
| IGW-4 | #819 | Security hardening: per-source/per-route limits, backpressure policy, safe audit summary, replay/idempotency tests и compatibility tests OpenAPI без расширения бизнес-состояния gateway. |
| IGW-5 | #829 | Deploy-контур: Dockerfile, manifests, secrets refs, runbook, monitoring и rollback. |
| IGW-6 | #853 | Первый active callback route: generic `/v1/external-callbacks/{callback_source}` проверяет source binding, HMAC SHA-256 подпись, лимиты и вызывает `interaction-hub.RecordChannelCallback` safe envelope без gateway business state. |
| Provider merge signal checks | #895, #909 | Go checks на staged fixtures проверяют GitHub provider webhook route wiring до `provider-hub.IngestWebhookEvent` и live HTTP diagnostic mode без переноса provider business state в gateway; bootstrap и adoption fixtures проходят один и тот же thin-edge route. |

## Зависимости и блокировки

| Домен или сервис | Связь | Статус |
|---|---|---|
| `provider-hub` | Владеет webhook inbox, дедупликацией и нормализацией provider events. | Первый внутренний контракт готов: `IngestWebhookEvent`. |
| `interaction-hub` | Владеет delivery/callback lifecycle внешних каналов. | Первый owner-service callback API готов: gateway вызывает `RecordChannelCallback` и не меняет request/decision lifecycle. |
| `package-hub` | Владеет пакетами, package-owned runtime metadata и package callbacks. | Callback route добавляется только после package-owned контракта. |
| `access-manager` / secret resolver | Нужны для безопасного разрешения secret refs webhook источников. | В IGW-2 GitHub webhook secret задаётся через `secret_store_type + secret_store_ref` в deployment config; значение разрешается через `libs/go/secretresolver` только в памяти процесса. |
| `platform-event-log` | Доменные события публикуют сервисы-владельцы. | Gateway не публикует provider business events сам. |

## Реализованный каркас IGW-1

| Область | Состояние |
|---|---|
| Размещение | `services/external/integration-gateway`. |
| Процесс | `cmd/integration-gateway`, shared `servicemain`, graceful shutdown по сигналам. |
| HTTP | Echo router за outer middleware stack: request id, timeout, body size guard, OpenAPI validation и safe errors. |
| Service endpoints | `/health/livez`, `/health/readyz`, `/metrics`, `/openapi/integration-gateway.v1.yaml`. |
| Provider owner client | Зафиксирован интерфейс и gRPC adapter к `provider-hub.IngestWebhookEvent`; GitHub route активируется только при включённом route flag, provider-hub token и настроенной ссылке на webhook secret. |
| Состояние | Gateway не добавляет БД, inbox, projections, cursors, operations или raw payload storage. |

## Реализованное усиление IGW-4

| Область | Состояние |
|---|---|
| Route protection | Активный GitHub route имеет per-route/per-source in-memory guard: `max_in_flight`, `rate_limit_burst`, `rate_limit_window`, `retry_after`. |
| Backpressure | Guard возвращает `429/rate_limited` или `503/backpressure` с `Retry-After` до вызова `provider-hub`. |
| Replay | Повторный GitHub delivery id проходит edge verification и передаётся в `provider-hub`; gateway не хранит dedupe state. |
| Safe diagnostics | Request summary содержит только route, source, status, latency, payload size bucket и reject reason; raw payload, подписи, токены и секреты не логируются. |

## Реализованный deploy-контур IGW-5

| Область | Состояние |
|---|---|
| Image | `services/external/integration-gateway/Dockerfile` собирает `build`, `dev` и `prod` stages; prod содержит бинарник и OpenAPI spec без исходников и секретов. |
| Manifests | `deploy/base/integration-gateway/**` содержит kustomize base для `ServiceAccount`, `ConfigMap`, `Service`, `Deployment`, probes и metrics scrape annotations. |
| Secret refs | GitHub webhook secret и provider-hub gRPC token подключаются через `kodex-platform-runtime` keys без значений в manifests/docs/tests. |
| Config | Route guard, HTTP limits, OpenAPI path, provider-hub address/timeout и secret resolver backends задаются env/config refs. |
| Проверки | Health/readiness/metrics/OpenAPI и safe negative responses для GitHub route проверяются Go tests или будущим Go integration runner. |
| Ops | Runbook и monitoring docs описывают route checks, backpressure, safe errors, provider-hub connectivity и rollback. |

Staged fixtures GitHub `pull_request closed + merged` bootstrap/adoption используются в Go tests: на gateway-стороне проверяется только HTTP boundary, HMAC verifier, delivery/event headers, correlation metadata и передача payload в `provider-hub` client. Provider-owned merge signal, read surface, replay/conflict и outbox проверяются в `provider-hub`; `integration-gateway` не хранит provider projections, inbox, operation state или бизнес-события.

## Реализованный callback route IGW-6

| Область | Состояние |
|---|---|
| HTTP route | `POST /v1/external-callbacks/{callback_source}` активируется конфигурацией `KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_*`. |
| Source binding | Разрешённые `callback_source` задаются статическим deployment config; route не содержит Telegram/WhatsApp/Slack hardcode. |
| Signature | `X-Kodex-External-Signature` проверяется как HMAC SHA-256 по raw request body через safe secret ref; значение подписи и секрета не логируется. |
| Owner call | Gateway строит `RecordChannelCallback` envelope для `interaction-hub`: `callback_id`, `delivery_id` или `request_ref`, `contract_version`, `action`, safe refs, `gateway_ref`, `received_at`, `correlation_id` и `signature_status=VERIFIED`. |
| Idempotency | Gateway передаёт `callback_id` владельцу и не создаёт собственный cache, inbox или БД для дедупликации. |
| Guard | Для callback route используется отдельный per-route/per-source in-memory guard с env-лимитами и `Retry-After`. |

## Критерии начала реального provider route

- Принят IGW-1 service scaffold.
- Выбран формат route registry для MVP: статическая конфигурация deployment или чтение утверждённой конфигурации владельца.
- Подтверждены лимиты размера payload и timeout для первого provider webhook route.
- Подключён verifier подписи и source binding, чтобы webhook не принимался как проверенный без edge-проверки.

## Критерии завершения MVP

- Gateway принимает provider webhook только через утверждённый route.
- Подпись и source binding проверяются до gRPC-вызова.
- Payload size guard, redaction и backpressure работают до передачи владельцу.
- `provider-hub` получает проверенный `IngestWebhookEvent` и остаётся владельцем webhook inbox.
- Gateway не хранит provider projections, cursors, operations или raw secret values.
- OpenAPI, runbook и проверка готовности покрывают публичный route без раскрытия приватных env.

## Апрув

- request_id: `owner-2026-05-25-integration-gateway-igw-0`
- Решение: approved
- Комментарий: план поставки `integration-gateway` согласован как целевое состояние IGW-0.
