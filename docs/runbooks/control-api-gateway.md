---
id: RUN-MC-013
title: Диагностика и восстановление control-api-gateway
type: runbook
status: approved
owner: sre
version: 1.4.0
updated: 2026-08-09
---

# Диагностика и восстановление control-api-gateway

## Назначение и запреты

Runbook применяется при отказе startup/readiness, OIDC/session/CSRF/CORS,
protected control-plane/interaction/integration RPC, WebSocket snapshots, rate
limits, TLS или observability. Он не разрешает deploy, production change, чтение Secret,
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
   PrometheusRule, VaultConnection/VaultAuth/VaultStaticSecret,
   issuer-owned SecretProviderClass, deny-all и exact-path NetworkPolicy.
4. В Deployment должна быть ровно одна application container и один issuer,
   а каждый `startupProbe`/`readinessProbe` — ровно один handler `httpGet` на
   `/readyz`; TLS-only `8443`, технический `9090`,
   non-root/read-only filesystem, отсутствующий Kubernetes API token.
5. Egress destination обязаны быть точными: DNS, control-plane,
   interaction-gateway, integration-gateway, identity SSO, Vault, issuer
   persistence/readback, OTel и Sentry. Правило только по порту, wildcard CIDR,
   plaintext или `skipTLSVerify` запрещены.
6. Ingress `8443` должен допускать точные browser-facing peers активного
   профиля, включая `staff-control-center/staff-frontend`. Односторонний egress
   frontend без встречного ingress gateway приводит к `502` и не считается
   рабочим owner path.

Доступ к среде и любая команда `kubectl get` выполняются только после
отдельного подтверждения владельца. Secret data не читать.

## Startup и readiness

Startup fail-closed до обслуживания `8443`. Проверять по порядку:

- public certificate/key/CA и versioned state — regular files безопасного
  mode; candidate соответствует exact hostname и имеет остаток TTL >5 минут;
- OIDC issuer — HTTPS exact hostname/SNI/CA, audience
  `mattercodex-control-api`, discovery/JWKS без redirect/proxy;
- current и previous session keys — два обязательных 32-byte hex файла в
  deploy-профиле; значения не сравнивать в выводе;
- общая workload client certificate/key и internal CA доступны отдельно;
  control-plane, interaction-gateway и integration-gateway targets имеют exact
  DNS target и exact SNI;
- readiness application grant доступен как файл безопасного mode;
- local issuer UDS проверил peer UID/GID, snapshot, proof trust, durable replay
  и served-state readback;
- `PrepareGatewayPublicTLS` сохраняет candidate как durable PENDING, local
  listener проходит loopback exact SNI/CA/DER SHA-256 readback и только затем
  `ConfirmGatewayPublicTLS` продвигает APPLIED; readiness использует read-only
  `CheckGatewayPublicTLS`;
- `controlplaneclient.Check` последовательно проходит resolver, local issuer и
  защищённый `CheckReadiness`, затем owner clients тем же local issuer и
  application proof проходят interaction `CheckReadiness` и integration
  `GetManagementDiagnostics`; любой отказ снимает readiness всего gateway;
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
- session create сначала выполняет durable `AdmitOwnerSession`; каждый REST/WS
  RPC сверяет current sid/revision/bearer digest и revocation в control-plane;
- logout выполняет `RevokeOwnerSession` с exact revision до очистки cookies;
  restart, stale bearer и пропущенное уведомление не обходят fence;
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

## Forward-only public TLS rotation

1. Read-only получить текущие generation и certificate SHA-256 из
   control-plane served-state readback, не читая private key.
2. Owner rotation controller атомарно записывает один versioned Vault KV
   envelope: `tls-crt`, `tls-key`, `ca-crt`, `generation=current+1`, exact
   `certificate-sha256`, `predecessor-generation=current` и exact predecessor
   SHA-256. Private key не попадает в RPC/БД/логи/readback.
3. Один `VaultStaticSecret` материализует весь envelope в один Kubernetes
   Secret и выполняет rolling restart. Два независимых VSO destination для
   certificate и generation запрещены: mixed N/N+1 должен быть невозможен.
4. Новый pod вызывает `PrepareGatewayPublicTLS`. Control-plane сохраняет
   exact candidate как idempotent PENDING только для `current+1` и exact
   predecessor, не меняя APPLIED; старые N replicas остаются ready.
5. Gateway загружает candidate, запускает TLS listener, выполняет loopback peer
   readback exact SNI/CA/leaf digest/expiry и затем вызывает
   `ConfirmGatewayPublicTLS`. Confirm одной transaction переводит N+1 в
   APPLIED, N в PREVIOUS и фиксирует 15-минутный overlap.
6. `/readyz` использует только read-only `CheckGatewayPublicTLS`, protected
   `CheckReadiness` и loopback served readback. APPLIED и неистёкший PREVIOUS
   могут одновременно быть ready; после overlap старые N replicas обязаны
   стать not ready и замениться.

Crash после Prepare оставляет N APPLIED и N+1 PENDING; exact replay продолжает
rollout. Crash после local load до Confirm также не продвигает state. Crash
после Confirm безопасен, потому что N+1 уже доказан served, а N принят на
bounded overlap. Unknown/skipped/rollback/mismatched digest закрыто
отклоняются; rollback metadata запрещён — выпускается следующая generation от
текущего APPLIED predecessor.

## WebSocket snapshots

WebSocket хранит только connection-local sequence. При reconnect client
повторяет `SUBSCRIBE`; один запрос содержит не более восьми из десяти закрытых
channels, gateway заново читает authoritative state:

- `RUNS` — `ListResources(PROCESS_RUN)`;
- `RESOURCES` — `ListResources` для каждого из ≤8 закрытых kind;
- `INCIDENTS` — все страницы typed `ListRuntimeIncidents`;
- `CONFIGURATION_CHANGES` — все страницы `ListAuditEvents` с закрытыми
  external action/outcome enums и общим scan cap.
- `WORKSPACE_TEAMS` — полный `ListMattermostTeams` catalog;
- `PROVIDERS` — полный `ListProviderConnections` masked readback;
- `INTEGRATIONS` — полный `ListIntegrationConfigurations` readback;
- `APPROVALS` — полный `ListIntegrationApprovals` redacted readback;
- `BACKUPS` — полный `ListWorkspaceBackups` readback;
- `HEALTH` — текущие exact control-plane diagnostics, interaction readiness и
  integration dependency observations без синтетической истории.

Нет NATS subscription, durable cursor или локальной БД. Gap/duplicate лечится
одним `complete=true` snapshot до 500 items. При следующей странице сверх cap
gateway отправляет problem и закрывает connection без client replace; частичный
snapshot и синтетические удаления запрещены.
Frame >16 KiB, неизвестный channel/kind, повтор enum или отсутствие CSRF
закрывает connection policy violation. Не увеличивать предел без отдельного
Issue и threat review.

## Восстановление WebSocket codegen

Source of truth — named components
`contracts/asyncapi/control-api-gateway/v1/asyncapi.yaml`; external projection
schemas принадлежат OpenAPI. Воспроизводимое восстановление выполняется только
командой:

```bash
make gen-control-api-gateway-asyncapi
```

Target сам удаляет ровно gateway-owned generated directory, запускает
AsyncAPI CLI и затем проверяет отсутствие anonymous files/symbols и generated
JSON codecs. В generated directory запрещены ручные изменения, сохранённые
копии моделей и любые `grep`/numeric-order/`sed`/`awk`/`cp`/`mv`
postprocessors. При structural check failure исправить source component
`title`/`$id`/`$ref`, удалить generated directory и повторить прямую
generation; подбирать anonymous type по номеру запрещено.

Runtime JSON обслуживает только strict adapter вне generated directory. Если
unknown/empty/null closed enum или out-of-range outgoing projection дошли до
boundary, frame не отправляется, connection получает bounded problem/close, а
внутреннее enum/type name не раскрывается. Не включать `--goIncludeTags` без
отдельной проверки fail-closed decoder и утверждённого contract change.

## Rate limits и observability

`429 RATE_LIMITED` означает fixed-window limit проверенного
organization+subject либо отдельный pre-auth/global/per-subject HTTP/WS bound.
Долгий WS не занимает HTTP quota; close/reconnect освобождает slot. Реестр keys
удаляет только неактивные истёкшие записи детерминированно; при заполнении
активными/current entries новый key закрыто отклоняется.

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
