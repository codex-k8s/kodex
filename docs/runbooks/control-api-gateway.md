---
id: RUN-MC-013
title: Диагностика control-api-gateway
type: runbook
status: approved
owner: sre
version: 2.1.0
updated: 2026-08-23
---

# Диагностика control-api-gateway

## Граница

Gateway проверяет browser session/OIDC, Origin, CSRF, rate limits,
`Idempotency-Key` и `If-Match`, затем вызывает generated control-plane clients.
Он не читает PostgreSQL, не вычисляет permissions/lifecycle/next actions и не
владеет event store.

Public host, Origin и OIDC endpoints задаются deployment parameters. В коде нет
фиксированного пользовательского домена.

## Probes

- `/healthz` — текущий HTTP process;
- `/readyz` — уже рассчитанный snapshot локального issuer sidecar и прямой
  инфраструктуры NATS consumer;
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

| Симптом | Проверить |
|---|---|
| `401` | OIDC issuer/audience/session expiry и bounded JWKS LKG |
| `403` | exact Origin/CSRF и server-owned permission |
| `409` | `If-Match`, idempotency intent либо stale Human Gate winner |
| `503` | JWKS/control-plane working path; это не причина делать gateway Pod unready |
| WS reconnect loop | NATS client material, subject policy, sequence/catch-up |
| старая локаль | trusted user locale и наличие key в RU/EN YAML |

LKG JWKS не продлевается повторными ошибками и ограничен двумя минутами;
signature/revision rollback/expiry fail closed немедленно.
