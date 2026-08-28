---
id: RUN-MC-012
title: Диагностика integration-gateway
type: runbook
status: approved
owner: sre
version: 3.1.0
updated: 2026-08-28
---

# Диагностика integration-gateway

## Штатное пустое состояние

Gateway обязан стартовать и быть Ready при нуле connections и credentials.
Shipped definitions загружаются локально и сверяются по версии/digest с
control-plane. GitHub, synthetic fixture и Mattermost не являются startup
dependency. Отсутствующая или disabled integration отображается как capability
unavailable, но не как отказ платформы.

## MCP admission

Runtime вызывает только зарегистрированный typed MCP tool. Для invocation
проверяются exact:

- organization/project/session/turn/attempt и immutable input digest;
- IntegrationDefinition version/origin/digest и capability operation;
- active Connection metadata и credential revision без secret value;
- server-owned Agent/Workflow grant;
- risk и наличие явного grant;
- application grant, mTLS peer, method, fence и replay watermark.

Gateway не предоставляет universal HTTP/API proxy. Provider credentials
остаются в Kubernetes Secret, смонтированном только в `integration-gateway`;
role Pod получает только session-scoped MCP binding. Raw provider response не
выходит в audit/event/PWA.

## Probes

`/healthz` проверяет процесс, `/readyz` читает локальный снимок issuer sidecar.
Control-plane, credential конкретного Connection, egress gateway и external
providers не вызываются на Kubernetes probe. Их фактическая доступность
проверяется рабочим invocation либо отдельной пользовательской операцией test.

## Effect и grant

Один provider effect связывается с exact invocation/attempt/fence, definition
digest, resource scope, input digest и effect key. READ может перейти к claim
без согласования. WRITE, SENSITIVE и DESTRUCTIVE создают отдельный Human Gate
до claim; credential не выдаётся worker до `APPROVED`. После durable completion
receipt тот же invocation не исполняется повторно. Exact повтор завершения
возвращает readback одной receipt, несовпадающий повтор закрыто отклоняется.

## Credential revision

Connection хранит только `secret_ref`, Secret UID, `resourceVersion`, revision
и SHA-256 содержимого. Control Center сначала создаёт Connection без credential,
а затем отдельной OCC-командой передаёт значение в `control-plane`.
`control-plane` имеет `get`/`update` только на Secret
`kodex-integration-credentials`, создаёт детерминированный data key и немедленно
очищает копию значения в памяти. Browser не назначает Secret UID,
`resourceVersion`, ref или digest. Для shipped GitHub package допустим только
ref вида `kodex-system/kodex-integration-credentials#integration-<digest>`.
Gateway сверяет все metadata, читает ровно указанный key из root-mounted Secret
и проверяет digest перед созданием provider client. В connection config,
публичном readback, PostgreSQL, логах и документации token value отсутствует.

## Exact egress

GitHub adapter имеет фиксированный endpoint `https://api.github.com/` и идёт
только через `egress-gateway.kodex-system.svc.cluster.local:8080`. Shared policy
разрешает exact FQDN `api.github.com:443`; runtime NetworkPolicy разрешает
только pod `egress-gateway` с component `platform-egress` на `8080/TCP`.
Synthetic adapter принимает только
`integration-synthetic.kodex-system.svc.cluster.local:8080`, а NetworkPolicy —
только pod labels `integration-synthetic`/`integration-fixture`. Redirect
запрещён.

## Изолированный GitHub fixture

Live fixture не входит в локальный baseline и запускается parent после
integration. Его безопасная конфигурация передаётся только окружением тестовой
оснастки:

```text
KODEX_INTEGRATION_E2E_GITHUB_OWNER=codex-k8s
KODEX_INTEGRATION_E2E_GITHUB_REPOSITORY=kodex-integration-e2e
KODEX_GITHUB_BOT_PAT=<Kubernetes Secret source only>
```

Repository private; `kodex-agent` имеет pull/push без admin. Значение
`KODEX_GITHUB_BOT_PAT` не включается в package, Connection, manifest, command
line или отчёт. Двухфазная команда Control Center создаёт/обновляет data key в
`kodex-integration-credentials`, после чего Connection получает только
authoritative Secret metadata и content digest. В production fixture owner и
repository не имеют default и всегда задаются Connection config.

## Диагностика

| Safe code | Действие |
|---|---|
| `INTEGRATION_CONFIGURATION_INVALID` | сверить package fields, exact scope и input digest |
| `INTEGRATION_CREDENTIAL_UNAVAILABLE` | сверить Secret ref/UID/resourceVersion/digest, не читать value в диагностике |
| `INTEGRATION_AUTH_REJECTED` | проверить права token на exact repository |
| `INTEGRATION_RATE_LIMITED` | дождаться provider retry window, не обходить receipt |
| `INTEGRATION_UNAVAILABLE` | проверить exact egress host/SNI и provider status |
| `INTEGRATION_REQUEST_REJECTED` | проверить typed input и provider resource state |
| `INTEGRATION_RESPONSE_INVALID` | считать effect неизвестным и сверить authoritative provider state/receipt |
| replay или fence conflict | не повторять вручную; сверить authoritative receipt |

Secret values, provider bodies и MCP bearer не печатаются. Ошибка возвращает
stable key; локализованный пользовательский текст находится в YAML i18n.

Frontend и browser E2E проверяются parent-волной #992. Локальная проверка
использует отдельный private fixture repository и bot token с минимальными
правами; production repository и credentials в этот контур не входят.
