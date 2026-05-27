---
doc_id: RB-CK8S-INTEGRATION-GATEWAY-0001
type: runbook
title: "integration-gateway — runbook: deploy, smoke и rollback"
status: active
owner_role: SRE
created_at: 2026-05-26
updated_at: 2026-05-27
related_issues: [829, 853, 895]
related_alerts: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-26-integration-gateway-deploy"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-26
---

# Runbook: integration-gateway — deploy, smoke и rollback

## TL;DR

- Симптом: `integration-gateway` не стартует, не проходит readiness, не отдаёт OpenAPI, не отклоняет неподписанный GitHub webhook/callback или возвращает `503` при маршрутизации в `provider-hub` или `interaction-hub`.
- Быстрая диагностика: проверить образ, `Deployment`, `/health/readyz`, `/metrics`, OpenAPI endpoint, runtime secret refs и доступность сервисов-владельцев.
- Быстрое восстановление: исправить image/env/secret refs, перезапустить `Deployment/integration-gateway`, выполнить smoke, при необходимости откатить image tag.

## Когда использовать

- После сборки и публикации образа `integration-gateway`.
- После изменения OpenAPI, webhook route guard, secret resolver config, provider-hub client config или Kubernetes manifests.
- При сбоях публичного provider webhook входа, росте `signature_invalid`, `rate_limited`, `backpressure` или `downstream_unavailable`.

## Предпосылки и доступы

- Доступ к Kubernetes namespace платформы.
- Доступ к логам `integration-gateway`, `provider-hub` и `interaction-hub`.
- Нормализованный bootstrap env для локального render/smoke.
- Локально для smoke нужны `kubectl`, `curl`, `grep` и `go`.
- Значения секретов, подписи, токены, DSN, приватные домены, адреса серверов и raw provider payload не выводить в Issue, PR, логи диагностики и сообщения.

## Сборка образов

```bash
KODEX_BUILD_ENV_FILE=/path/to/bootstrap.env \
  scripts/build-integration-gateway-images.sh
```

Скрипт собирает `integration-gateway` и минимальный набор образов, нужных для smoke-зависимостей: `access-manager`, `provider-hub`, их migration images и `platform-event-log` migrations image.

## Smoke-проверка

```bash
KODEX_SMOKE_ENV_FILE=/path/to/bootstrap.env \
  scripts/smoke-integration-gateway.sh
```

Путь проверки:

- рендерит `deploy/base/**` во временный каталог;
- применяет PostgreSQL stack, `platform-event-log` migrations, `access-manager` и `provider-hub`;
- применяет `integration-gateway`;
- проверяет `/health/livez`, `/health/readyz`, `/metrics` и `/openapi/integration-gateway.v1.yaml`;
- выполняет safe negative checks для GitHub webhook route и disabled callback route без реальных секретов: неподписанный GitHub webhook должен получить `401/signature_invalid`, неподдержанный provider slug и выключенный callback route — `400/source_not_allowed`.

Smoke не выполняет реальные GitHub/GitLab операции, не требует валидного webhook secret для positive route и не сохраняет provider payload в gateway.

### Staged smoke для GitHub merge signal

```bash
scripts/smoke-provider-merge-signal.sh
```

Скрипт использует синтетический safe fixture `pull_request closed + merged` и по умолчанию запускает hermetic checks без live webhook secret:

- проверяет, что route `POST /v1/provider-webhooks/github` принимает корректно подписанный тестовым секретом fixture и передаёт `event_name=pull_request`, delivery id, request id и payload в `provider-hub` client boundary;
- проверяет provider-side продолжение через `provider-hub`: safe merge signal, read surface и producer outbox event.

Live HTTP режим `KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_MODE=live-http` отправляет тот же fixture в запущенный `integration-gateway`, а затем читает `RepositoryMergeSignal` через gRPC `provider-hub`. Для него нужны настроенный webhook secret текущего контура и заранее существующая bootstrap/adoption PR-проекция с provider relationship; gateway сам не создаёт эту precondition и не хранит state.

## Диагностика rollout и health

```bash
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get deployment/integration-gateway service/integration-gateway
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" rollout status deployment/integration-gateway
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" describe deployment/integration-gateway
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" logs deploy/integration-gateway
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" port-forward svc/integration-gateway 18086:8080
curl -fsS http://127.0.0.1:18086/health/livez
curl -fsS http://127.0.0.1:18086/health/readyz
curl -fsS http://127.0.0.1:18086/metrics
curl -fsS http://127.0.0.1:18086/openapi/integration-gateway.v1.yaml
```

Readiness подтверждает, что HTTP router, OpenAPI validator и route registry собраны. Доступность `provider-hub` и `interaction-hub` проверяется через route smoke, negative checks и логи `downstream_unavailable`, потому что gateway не хранит собственное состояние и не открывает БД.

## Диагностика secret refs

Проверить только наличие refs, не значения:

- `KODEX_GITHUB_WEBHOOK_SECRET` существует в Kubernetes Secret `kodex-platform-runtime`;
- `KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_TYPE` соответствует настроенному backend;
- `KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_REF` указывает на безопасную ссылку, а не содержит значение секрета;
- `KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN` существует в `kodex-platform-runtime`;
- если callback route включён, `KODEX_EXTERNAL_CALLBACK_SECRET` и `KODEX_INTERACTION_HUB_GRPC_AUTH_TOKEN` существуют в `kodex-platform-runtime`, а `KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_SECRET_STORE_REF` содержит только safe ref;
- Vault token или mounted Kubernetes root настроены только если выбран соответствующий resolver backend.

Нельзя печатать webhook/callback secret, подписи `X-Hub-Signature-256` / `X-Kodex-External-Signature`, provider/callback payload или gRPC tokens.

## Диагностика provider-hub connectivity

- Проверить `provider-hub` readiness и gRPC service port.
- Проверить, что `KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_GRPC_ADDR` указывает на service DNS внутри namespace.
- Проверить, что token ref совпадает с `provider-hub` gRPC auth token.
- При `503/downstream_unavailable` смотреть краткий код ошибки gateway и логи `provider-hub` по `request_id`/`correlation_id`.

## Диагностика interaction-hub callback route

- Проверить, что `KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_ENABLED=true` задан только после готовности `interaction-hub`.
- Проверить, что `KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_ALLOWED_SOURCES` содержит ожидаемый generic source, без vendor-specific hardcode.
- Проверить, что `KODEX_INTEGRATION_GATEWAY_INTERACTION_HUB_GRPC_ADDR` указывает на service DNS внутри namespace.
- При `400/invalid_request` проверить наличие `callback_id`, `contract_version`, `action` и одного из `delivery_id` или `request_ref`; raw callback body не прикладывать.
- При `503/downstream_unavailable` смотреть readiness и логи `interaction-hub` по `request_id`/`correlation_id`.

## Backpressure и safe errors

| Симптом | Вероятная причина | Что проверить |
|---|---|---|
| `429/rate_limited` | Fixed-window burst исчерпан для route/source | `KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_RATE_LIMIT_*`, частоту webhook deliveries, `Retry-After`. |
| `503/backpressure` | `max_in_flight` исчерпан до вызова владельца | `KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_MAX_IN_FLIGHT`, latency `provider-hub`, число replicas. |
| `401/signature_invalid` | Нет подписи или HMAC не совпал | источник webhook, secret ref, delivery headers; значения секрета и подписи не выводить. |
| `413/payload_too_large` | Payload превышает route limit | `KODEX_INTEGRATION_GATEWAY_HTTP_MAX_BODY_BYTES`, provider event size. |
| `503/downstream_unavailable` | `provider-hub` недоступен или отказал retryable ошибкой | provider-hub rollout, gRPC token ref, service DNS, timeout. |

Для callback route применяются аналогичные safe codes по `route=external_callback`; owner-side duplicates, conflicts и lifecycle смотреть в `interaction-hub`. Gateway не создаёт очередь и не хранит retry state. Повтор delivery id или callback id проходит в сервис-владелец, где находится дедупликация.

## План отката

- Вернуть предыдущий image tag `integration-gateway` через rendered manifest или `kubectl rollout undo deployment/integration-gateway`.
- Если отказ связан только с webhook route config, временно выключить route через `KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ENABLED=false` и повторно применить manifest. Это оставляет health/OpenAPI доступными, но provider webhook route будет возвращать безопасный отказ.
- Не откатывать `provider-hub` БД, inbox или provider projections ради gateway-инцидента.
- Не удалять runtime secret и не подставлять значения секретов вручную в manifest.

## Проверка результата

- `Deployment/integration-gateway` доступен.
- `/health/readyz` возвращает успешный ответ.
- `/metrics` и OpenAPI endpoint доступны.
- Неподписанный GitHub webhook получает `401/signature_invalid`.
- Неподдержанный provider slug получает `400/source_not_allowed`.
- Выключенный callback route получает `400/source_not_allowed`; включённый route без валидной `X-Kodex-External-Signature` получает `401/signature_invalid`.
- `scripts/smoke-integration-gateway.sh` завершается сообщением `HTTP edge boundary OK`.

## Пост-действия

- Если была авария, создать Issue с причиной, безопасными symptoms и корректирующими действиями.
- Если обнаружен пробел в secret refs, guard config, manifests или smoke, обновить этот runbook вместе с исправлением.
- В Issue/PR не прикладывать значения env, DSN, токенов, webhook secret, подписи, приватные домены или raw provider payload.

## Апрув

- request_id: `owner-2026-05-26-integration-gateway-deploy`
- Решение: approved
- Комментарий: runbook фиксирует эксплуатационный контур `integration-gateway` после IGW-5.
