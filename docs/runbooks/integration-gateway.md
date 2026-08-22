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
- risk/approval policy;
- application grant, mTLS peer, method, fence и replay watermark.

Gateway не предоставляет universal HTTP/API proxy. Provider credentials
остаются в credential filesystem/secret storage; role Pod получает только
session-scoped MCP binding. Raw provider response не выходит в audit/event/PWA.

## Probes

`/healthz` проверяет процесс, `/readyz` — локальный config/credential boundary,
authority sidecar и egress policy snapshot. Control-plane и external providers
не вызываются на Kubernetes probe. Connection test является отдельной
пользовательской операцией и возвращает typed result.

## Effect и approval

Один provider effect связывается с exact invocation/attempt/fence. Retry с тем
же intent не выполняет внешний эффект повторно после durable receipt. Опасная
операция открывает server-owned Human Gate; решение в web продолжает effect один
раз. Expired/stale grant или изменённый input закрыто отклоняется.

## Диагностика

| Safe code | Действие |
|---|---|
| definition unavailable | проверить catalog revision и enabled state |
| connection unavailable | проверить metadata/credential masked state и отдельный test |
| grant required | выдать exact capability Agent/Workflow через Control Center |
| approval required | разрешить Human Gate в web |
| provider unavailable | проверить exact egress host/SNI/CA и provider status |
| replay or fence conflict | не повторять вручную; сверить authoritative receipt |

Secret values, provider bodies и MCP bearer не печатаются. Ошибка возвращает
stable key; локализованный пользовательский текст находится в YAML i18n.
