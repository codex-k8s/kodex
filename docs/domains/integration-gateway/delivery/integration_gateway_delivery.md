---
doc_id: DLV-CK8S-INTEGRATION-GATEWAY
type: delivery-plan
title: kodex — поставка integration-gateway
status: active
owner_role: EM
created_at: 2026-05-25
updated_at: 2026-05-26
related_issues: [781, 792, 807, 770, 829]
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
| IGW-3 | не назначено | Callback routes для внешних каналов и пакетов после готовности owner-service contracts: `interaction-hub`, `package-hub` или другой владелец. |
| IGW-4 | #819 | Security hardening: per-source/per-route limits, backpressure policy, safe audit summary, replay/idempotency tests и compatibility tests OpenAPI без расширения бизнес-состояния gateway. |
| IGW-5 | #829 | Deploy-контур: Dockerfile, manifests, secrets refs, smoke, runbook, monitoring и rollback. |

## Зависимости и блокировки

| Домен или сервис | Связь | Статус |
|---|---|---|
| `provider-hub` | Владеет webhook inbox, дедупликацией и нормализацией provider events. | Первый внутренний контракт готов: `IngestWebhookEvent`. |
| `interaction-hub` | Владеет delivery/callback lifecycle внешних каналов. | Callback route остаётся контрактным заделом до готовности owner-service callback API. |
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
| Smoke | `scripts/smoke-integration-gateway.sh` проверяет health/readiness/metrics/OpenAPI и safe negative responses для GitHub route без реального webhook secret. |
| Ops | Runbook и monitoring docs описывают route checks, backpressure, safe errors, provider-hub connectivity и rollback. |

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
- OpenAPI, runbook и smoke покрывают публичный route без раскрытия приватных env.

## Апрув

- request_id: `owner-2026-05-25-integration-gateway-igw-0`
- Решение: approved
- Комментарий: план поставки `integration-gateway` согласован как целевое состояние IGW-0.
