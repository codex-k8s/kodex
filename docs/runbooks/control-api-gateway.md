---
id: RUN-MC-013
title: Диагностика control-api-gateway
type: runbook
status: approved
owner: sre
version: 2.2.0
updated: 2026-08-26
---

# Диагностика control-api-gateway

## Граница

Gateway проверяет browser session/OIDC, Origin, CSRF, rate limits,
`Idempotency-Key` и `If-Match`, затем вызывает generated control-plane clients.
Он не читает PostgreSQL, не вычисляет permissions/lifecycle/next actions и не
владеет event store.

Public host, Origin и OIDC endpoints задаются deployment parameters. В коде нет
фиксированного пользовательского домена.

## Browser session

OAuth2 Proxy cookie пропускает browser к UI, Keycloak SSO подтверждает login, а
`__Host-kodex-session` является отдельной прикладной API session. Realm token
lifespan равен 300 секундам; только client `kodex-control-center` имеет
readback-проверяемый override `access.token.lifespan=3600`.

API session имеет sliding idle TTL 15 минут. В последней трети TTL gateway
перевыпускает session и CSRF cookies по `PUT /api/v1/session` после успешной
проверки OIDC binding, exact Origin, CSRF header/cookie и application/project
context. Subject, organization, OIDC `sid`, revision, session ID, bearer и CSRF binding не
меняются; новый expiry ограничен исходным bearer. При `401`, rejected Origin,
CSRF failure или binding mismatch отсутствие `Set-Cookie` является ожидаемым
fail-closed результатом.

WebSocket handshake и обычные GET/HEAD не продлевают API session. Control
Center вызывает bounded renewal mutation каждые пять минут; reconnect после
idle expiry создаёт новую session через warm SSO.

При logout Control Center останавливает renewal loop, отменяет и дожидается
текущего renewal, после чего вызывает `DELETE /api/v1/session`. Gateway
синхронно фиксирует browser session ID в авторитетном JetStream-потоке
`CONTROL_API_SESSION_REVOCATIONS` и только после broker ack очищает
session/CSRF cookies. Каждая реплика gateway проверяет durable revocation до
OIDC/application context и при недоступном либо повреждённом потоке закрыто
отклоняет запрос. Поэтому запоздавший renewal или копия старой session cookie
в другом browser context не восстанавливают доступ; новый login получает новый
browser session ID.

Runtime NATS identity gateway имеет только publish на
`kodex.control_api.session_revocation.*`, exact `STREAM.INFO`/`STREAM.MSG.GET`
для этого потока, realtime subscriptions и response inbox. Права
`STREAM.CREATE|UPDATE|DELETE|PURGE` принадлежат только одноразовой broker
bootstrap identity; wildcard `$JS.API.>` runtime workload запрещён.
Upgrade со старого permission contract выполняет installer: cutoff отзывает
все ранее выпущенные runtime user JWT, после него выпускаются exact-permission
credentials, обновляется account JWT и Kubernetes Secrets, затем по очереди
перезапускаются NATS, control-plane и gateway. Повторный запуск сверяет
versioned policy и не ротирует уже актуальные credentials. Applied revision
записывается только после точного byte-level readback всех Secrets, проверки,
что прежние JWT находятся до revocation cutoff, новые JWT выпущены после него,
и успешного rollout. Прерывание после локальной подготовки оставляет pending
evidence и на следующем запуске повторяет cluster materialization.

## Probes

- `/healthz` — текущий HTTP process;
- `/readyz` — уже рассчитанный snapshot локального issuer sidecar, realtime
  consumer и точного контракта durable session-revocation stream;
- control-plane, NATS producer, runtime и Mattermost не опрашиваются на каждую
  Kubernetes probe.

Недоступный OIDC/JWKS либо control-plane рабочий request нормализуется в
`502/503/504` со stable error/message key. Initial JWKS outage не блокирует
startup: verifier закрыт до успешного refresh, а запрос получает локализованный
`503`. Пользовательский текст выбирается из YAML i18n по locale; raw
gRPC/provider diagnostics не возвращаются.

## Realtime Run

Для `/api/v1/runs/{runRef}/stream` проверить:

1. authorization использует тот же owner rule, что HTTP Run detail;
2. первое сообщение содержит authoritative snapshot и sequence;
3. последующие deltas возрастают по sequence;
4. reconnect передаёт `afterSequence` и восстанавливает gap;
5. недоступный диапазон приводит к новому snapshot, не phantom node;
6. duplicate event не применяется дважды;
7. slow client получает bounded backpressure/close и может reconnect.

WebSocket не передаёт raw stdout/stderr, Codex JSONL, provider response,
arbitrary tool payload, secret или file body. Artifact скачивается отдельным
HTTP path после owner authorization.

## Частые причины

| Симптом           | Проверить                                                                  |
| ----------------- | -------------------------------------------------------------------------- |
| `401`             | OIDC issuer/audience, bearer ceiling, API idle expiry и bounded JWKS LKG   |
| `403`             | exact Origin/CSRF и server-owned permission                                |
| `409`             | `If-Match`, idempotency intent либо stale Human Gate winner                |
| `503`             | JWKS/control-plane working path; это не причина делать gateway Pod unready |
| WS reconnect loop | NATS client material, subject policy, sequence/catch-up                    |
| старая локаль     | trusted user locale и наличие key в RU/EN YAML                             |

LKG JWKS не продлевается повторными ошибками и ограничен двумя минутами;
signature/revision rollback/expiry fail closed немедленно.
