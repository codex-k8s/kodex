---
doc_id: DLV-CK8S-INTEGRATION-GATEWAY
type: delivery-plan
title: kodex — поставка integration-gateway
status: active
owner_role: EM
created_at: 2026-05-25
updated_at: 2026-05-25
related_issues: [781, 770]
related_prs: []
related_docsets:
  - docs/domains/integration-gateway/product/requirements.md
  - docs/domains/integration-gateway/architecture/design.md
  - docs/domains/integration-gateway/architecture/api_contract.md
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
| IGW-1 | не назначено | Сервисный каркас: процесс, конфигурация, health/readiness/metrics, HTTP router, route registry, signature verifier interfaces, payload guard, redactor и gRPC clients без provider business logic. |
| IGW-2 | не назначено | Реальный route `POST /v1/provider-webhooks/{provider_slug}`: проверка GitHub/GitLab подписи, лимиты, idempotency mapping и вызов `provider-hub.IngestWebhookEvent`. |
| IGW-3 | не назначено | Callback routes для внешних каналов и пакетов после готовности owner-service contracts: `interaction-hub`, `package-hub` или другой владелец. |
| IGW-4 | не назначено | Security hardening: per-source limits, backpressure policies, audit summary, redaction metrics, replay tests и compatibility tests OpenAPI. |
| IGW-5 | не назначено | Deploy-контур: Dockerfile, manifests, secrets refs, smoke, runbook, monitoring и rollback. |

## Зависимости и блокировки

| Домен или сервис | Связь | Статус |
|---|---|---|
| `provider-hub` | Владеет webhook inbox, дедупликацией и нормализацией provider events. | Первый внутренний контракт готов: `IngestWebhookEvent`. |
| `interaction-hub` | Владеет delivery/callback lifecycle внешних каналов. | Callback route остаётся контрактным заделом до готовности owner-service callback API. |
| `package-hub` | Владеет пакетами, package-owned runtime metadata и package callbacks. | Callback route добавляется только после package-owned контракта. |
| `access-manager` / secret resolver | Нужны для безопасного разрешения secret refs webhook источников. | Способ хранения route registry и secret refs уточняется в IGW-1/IGW-2. |
| `platform-event-log` | Доменные события публикуют сервисы-владельцы. | Gateway не публикует provider business events сам. |

## Критерии начала кода

- Принят IGW-0 docset.
- Для IGW-1 заведён отдельный GitHub Issue.
- Выбран формат route registry для MVP: статическая конфигурация deployment или чтение утверждённой конфигурации владельца.
- Подтверждены лимиты размера payload и timeout для первого provider webhook route.

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
