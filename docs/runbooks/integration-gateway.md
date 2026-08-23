---
id: RUN-MC-012
title: Диагностика integration-gateway
type: runbook
status: approved
owner: sre
version: 2.0.0
updated: 2026-08-23
---

# Диагностика integration-gateway

## Штатное пустое состояние

Gateway обязан стартовать и быть Ready при нуле definitions, connections и
credentials. GitHub, GitLab, Kubernetes, Mattermost и model provider не являются
startup dependency. Отсутствующая или disabled integration отображается как
capability unavailable, но не как отказ платформы.

## MCP admission

Runtime вызывает только зарегистрированный typed MCP tool. Для invocation
проверяются exact:

- organization/project/session/turn/attempt и immutable input digest;
- IntegrationDefinition/capability schema revision;
- active Connection metadata и credential revision;
- server-owned Agent/Workflow grant;
- risk и наличие явного grant;
- application grant, mTLS peer, method, fence и replay watermark.

Gateway не предоставляет universal HTTP/API proxy. Provider credentials
остаются в credential filesystem/secret storage; role Pod получает только
session-scoped MCP binding. Raw provider response не выходит в audit/event/PWA.

## Probes

`/healthz` проверяет процесс, `/readyz` читает локальный снимок issuer sidecar.
Control-plane, credential конкретного Connection, egress gateway и external
providers не вызываются на Kubernetes probe. Их фактическая доступность
проверяется рабочим invocation либо отдельной пользовательской операцией test.

## Effect и grant

Один provider effect связывается с exact invocation/attempt/fence. После
durable completion receipt тот же invocation не исполняется повторно.
Поставляемые definitions содержат только типизированные read capabilities;
write/destructive adapters в текущий каталог не входят. Любой новый такой
adapter обязан сначала определить отдельную approval policy и Human Gate
lifecycle, а не наследовать разрешение read-adapter. Expired/stale grant или
изменённый input закрыто отклоняется.

## Диагностика

| Safe code | Действие |
|---|---|
| definition unavailable | проверить catalog revision и enabled state |
| connection unavailable | проверить metadata/credential masked state и отдельный test |
| grant required | выдать exact capability Agent/Workflow через Control Center |
| provider unavailable | проверить exact egress host/SNI/CA и provider status |
| replay or fence conflict | не повторять вручную; сверить authoritative receipt |

Secret values, provider bodies и MCP bearer не печатаются. Ошибка возвращает
stable key; локализованный пользовательский текст находится в YAML i18n.
