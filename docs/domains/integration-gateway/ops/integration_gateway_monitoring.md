---
doc_id: MON-CK8S-INTEGRATION-GATEWAY-0001
type: monitoring
title: "integration-gateway — наблюдаемость"
status: active
owner_role: SRE
created_at: 2026-05-26
updated_at: 2026-05-26
related_issues: [829]
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-26-integration-gateway-deploy"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-26
---

# Наблюдаемость: integration-gateway

## TL;DR

- Дашборды: HTTP edge overview, GitHub webhook route, provider-hub downstream, guard/backpressure и Kubernetes rollout.
- Метрики: route/source/status, latency, payload size bucket, reject reason, `Retry-After` классы и readiness.
- Логи: один redaction-safe request summary без raw payload, подписи, токенов, webhook secret, DSN и приватных адресов.
- Алерты: readiness down, рост `signature_invalid`, `payload_too_large`, `rate_limited`, `backpressure`, `downstream_unavailable`, отсутствие OpenAPI endpoint или частые restarts.

## Источники данных

- HTTP health: `/health/livez`, `/health/readyz`.
- HTTP metrics: `/metrics`.
- OpenAPI static endpoint: `/openapi/integration-gateway.v1.yaml`.
- Prometheus metrics:
  - `kodex_integration_gateway_http_requests_total`;
  - `kodex_integration_gateway_http_request_duration_seconds`.
- Kubernetes: `Deployment/integration-gateway`, `Service/integration-gateway`, pod status, restarts и events.
- Логи приложения: structured logs без секретов, raw provider payload, подписей и token-like headers.

## Дашборды

| Название | Ссылка | Для чего | Owner |
|---|---|---|---|
| Integration gateway overview | TBD | Readiness, traffic, latency, status classes и restarts. | SRE |
| Provider webhook edge | TBD | GitHub route, reject reasons, payload buckets, rate limit и backpressure. | SRE |
| Provider-hub downstream | TBD | `downstream_unavailable`, latency owner call и retryable classes. | SRE |
| Public ingress safety | TBD | Safe error mix, signature failures, oversized payloads и unsupported sources. | SRE |

## Golden signals

- Latency: HTTP request duration by `route`, `source`, `status`, `reject_reason`.
- Traffic: request count by route/source/status and payload size bucket.
- Errors: `signature_invalid`, `source_not_allowed`, `payload_too_large`, `rate_limited`, `backpressure`, `downstream_unavailable`.
- Saturation: доля `429/rate_limited`, `503/backpressure`, active rollout restarts, p95/p99 latency и количество replicas.

## Route metrics

`integration-gateway` пишет только безопасные labels:

- `route`: например `provider_webhook` или `external_callback`;
- `source`: provider/channel slug, например `github`;
- `status`: HTTP status code;
- `payload_size_bucket`: bucket размера, а не payload;
- `reject_reason`: короткий безопасный код ошибки или `none`.

В labels запрещены delivery id, signature, webhook secret, provider token, raw body, repository names, private domains и внутренние адреса.

## Логи

Один request summary должен содержать:

- `request_id`;
- `route_id`;
- `source`;
- `method`;
- `path`;
- `status`;
- `duration_ms`;
- `payload_size_bucket`;
- `reject_reason`.

В логи не попадают:

- `Authorization`, `X-Hub-Signature-256`, webhook secret, Vault token, provider-hub token;
- raw provider payload и большие body;
- `secret_store_ref`, если он раскрывает путь к чувствительному хранилищу;
- DSN, приватные домены и адреса серверов из bootstrap-профиля.

## Проверки и routine health

- Liveness: `/health/livez` возвращает успешный ответ.
- Readiness: `/health/readyz` подтверждает HTTP router, OpenAPI validator и route registry.
- OpenAPI: `/openapi/integration-gateway.v1.yaml` доступен и содержит активный provider webhook route.
- Negative synthetic: неподписанный GitHub webhook получает `401/signature_invalid`; неподдержанный provider slug получает `400/source_not_allowed`.
- Downstream: retryable `downstream_unavailable` не должен оставаться высоким после восстановления `provider-hub`.

## Алерты

- Readiness недоступен дольше установленного окна.
- Pod restarts или rollout не завершается.
- Доля `5xx` выше baseline.
- `downstream_unavailable` растёт после восстановления `provider-hub`.
- `backpressure` или `rate_limited` держатся выше ожидаемого webhook traffic baseline.
- `payload_too_large` резко вырос: проверить route limit и тип provider event.
- `signature_invalid` резко вырос: проверить source configuration и GitHub webhook secret ref без вывода значений.
- OpenAPI endpoint недоступен после rollout.

## Открытые вопросы

- Конкретные Prometheus recording rules и alert rules будут закреплены после появления штатного observability stack.
- GitLab-специфичные route dashboards добавляются вместе с GitLab verifier/source policy.
- Callback route dashboards добавляются вместе с owner-service callback contracts.

## Апрув

- request_id: `owner-2026-05-26-integration-gateway-deploy`
- Решение: approved
- Комментарий: monitoring-документ входит в эксплуатационный контур IGW-5.
