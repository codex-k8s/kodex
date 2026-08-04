---
id: RUN-MC-013
title: Диагностика и восстановление control-api-gateway
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-04
---

# Диагностика и восстановление control-api-gateway

## Назначение и запреты

Runbook применяется при отказе startup/readiness, OIDC/session/CSRF/CORS,
protected control-plane RPC, WebSocket snapshots, rate limits, TLS или
observability. Он не разрешает deploy, production change, чтение Secret,
сброс authority watermark/replay state или ручное изменение control-plane.

Не выводить OIDC bearer/JWT claims, encrypted cookie, CSRF token, session key,
JWS/JWK, DSN, TLS private key, Sentry DSN или содержимое Secret. Допустимы
только имена ресурсов, immutable image digest, bounded error code, readiness и
агрегированные метрики.

## Read-only preflight

1. Зафиксировать Git SHA и immutable digest `control-api-gateway` и
   `internal-rpc-authority`.
2. Получить canonical render без apply:

```bash
kubectl kustomize deploy/k8s/overlays/staging/control-api-gateway \
  > /tmp/control-api-gateway-staging.yaml
```

3. Сверить Deployment, Service, passthrough Ingress, PDB, ServiceMonitor,
   PrometheusRule, VaultConnection/VaultAuth/VaultPKISecret/VaultStaticSecret,
   issuer-owned SecretProviderClass, deny-all и exact-path NetworkPolicy.
4. В Deployment должна быть ровно одна application container и один issuer,
   `/readyz` application container, TLS-only `8443`, технический `9090`,
   non-root/read-only filesystem, отсутствующий Kubernetes API token.
5. Egress destination обязаны быть точными: DNS, control-plane, identity SSO,
   Vault, issuer persistence/readback, OTel и Sentry. Правило только по порту,
   wildcard CIDR, plaintext или `skipTLSVerify` запрещены.

Доступ к среде и любая команда `kubectl get` выполняются только после
отдельного подтверждения владельца. Secret data не читать.

## Startup и readiness

Startup fail-closed до обслуживания `8443`. Проверять по порядку:

- public certificate/key — regular files безопасного mode, соответствуют друг
  другу и обслуживают exact external hostname;
- OIDC issuer — HTTPS exact hostname/SNI/CA, audience
  `mattercodex-control-api`, discovery/JWKS без redirect/proxy;
- current и previous session keys — два обязательных 32-byte hex файла в
  deploy-профиле; значения не сравнивать в выводе;
- control-plane client certificate/key и CA доступны отдельно, target имеет
  exact SNI;
- readiness application grant доступен как файл безопасного mode;
- local issuer UDS проверил peer UID/GID, snapshot, proof trust, durable replay
  и served-state readback;
- `controlplaneclient.Check` последовательно проходит resolver, local issuer и
  защищённый `CheckReadiness` тем же путём, что рабочие RPC;
- OTLP TLS и Sentry file/expected host настроены.

Нельзя заменять `/readyz` проверкой TCP, наличием файла или отдельным health
RPC. Liveness `/livez` подтверждает только живой процесс и не разрешает traffic.

## Owner session, CSRF и CORS

- `401 UNAUTHENTICATED`: проверить только issuer/audience/time и наличие
  обязательных session claims; token/claims не копировать;
- `403 ORIGIN_REJECTED`: сверить exact HTTPS allowlist окружения; wildcard и
  suffix matching запрещены;
- `403 CSRF_REJECTED`: client должен отправить double-submit CSRF cookie/header,
  а WebSocket — cookie и `csrf.<token>` subprotocol вместе с
  `mattercodex.control.v1`; значения не логировать;
- session expiry или OIDC revocation требует нового `POST /session`; gateway не
  продлевает bearer и не создаёт refresh token;
- rotation выполняется forward-only в три фазы: сначала доставить новый key в
  `previous`, сохранив старый `current`, и дождаться restart/readiness всех
  реплик; затем атомарно поменять их местами (`current=new`, `previous=old`) и
  снова дождаться всех реплик; только после максимального session TTL заменить
  `previous` текущим key. Так old/new pods на rolling overlap принимают обе
  стороны перехода. Rollback current generation запрещён.

Cookie `__Host-mattercodex-session` обязана быть `Secure`, `HttpOnly`,
`SameSite=Strict`, без `Domain`, с `Path=/`. CSRF cookie отличается только
`HttpOnly=false`. Ослаблять атрибуты для диагностики запрещено.

## Protected RPC и единый error contract

Для `500 INTERNAL` проверить, что downstream status содержит ровно один
`controlplane.v1.ErrorDetail`, а reason/code/retryable и gRPC code совпадают с
`contracts/errors/v1/rpc-http-mapping.yaml`. Unknown/missing/mismatched detail
закрыто преобразуется в `INTERNAL`; внутренний текст не возвращается.

Для `404` не пытаться различать отсутствующий и cross-tenant hidden ресурс.
Для `412 VERSION_MISMATCH` перечитать resource/ETag и повторить с новым
idempotency key только после owner decision. Для `409 IDEMPOTENCY_CONFLICT`
не менять semantic request под прежним key. `mTLS` не заменяет bearer,
resolver, permission и domain owner check.

## WebSocket snapshots

WebSocket хранит только connection-local sequence. При reconnect client
повторяет `SUBSCRIBE`; gateway заново читает authoritative state:

- `RUNS` — `ListResources(PROCESS_RUN)`;
- `RESOURCES` — `ListResources` для каждого из ≤8 закрытых kind;
- `INCIDENTS` — `ListAuditEvents(record_runtime_incident)`;
- `CONFIGURATION_CHANGES` — `ListAuditEvents` с закрытым action filter.

Нет NATS subscription, durable cursor или локальной БД. Gap/duplicate лечится
полной заменой channel snapshot, а не воспроизведением промежуточного состояния.
Frame >16 KiB, неизвестный channel/kind, повтор enum или отсутствие CSRF
закрывает connection policy violation. Не увеличивать предел без отдельного
Issue и threat review.

## Rate limits и observability

`429 RATE_LIMITED` означает fixed-window limit проверенного subject либо общий
concurrency bound. Реестр subject keys ограничен и удаляет самый давно видимый
key; actor/session/resource ID не является Prometheus label.

Проверять только закрытые labels:

- `mattercodex_control_api_gateway_http_requests_total{route,status}`;
- `mattercodex_control_api_gateway_http_request_duration_seconds{route}`;
- `mattercodex_control_api_gateway_websocket_connections{state="open"}`;
- `mattercodex_control_api_gateway_websocket_snapshots_total{channel,outcome}`;
- `mattercodex_control_api_gateway_readiness{ready}`.

Alerts имеют абсолютный `runbook_url` на этот документ. При telemetry failure
не отключать TLS/CA и не печатать exporter или Sentry credential.

## Rollback

Rollback разрешён только на ранее проверенный image digest, совместимый с уже
опубликованными OpenAPI/AsyncAPI/control-plane contracts и текущими authority
revision/key generations. Session/authority keys, replay watermarks, CA и
credential generations движутся только вперёд и не откатываются вместе с
образом.

Если предыдущий образ не понимает current contract или key overlap, оставить
workload неготовой и выпустить совместимую исправленную версию. Apply, rollout
и production rollback требуют отдельного owner OK.

Полные staging/integration/E2E/lifecycle проверки отложены в
[Issue #216](https://github.com/codex-k8s/matter-codex/issues/216).
