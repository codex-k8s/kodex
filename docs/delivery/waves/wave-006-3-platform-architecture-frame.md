---
doc_id: DLV-CK8S-WAVE-006-3
type: delivery-plan
title: kodex — wave 6.3, сквозной архитектурный каркас платформы
status: active
owner_role: SA
created_at: 2026-04-26
updated_at: 2026-04-26
related_issues: [599, 600, 601, 602]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-26-wave6-3-platform-architecture-frame"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-26
---

# Wave 6.3 — сквозной архитектурный каркас платформы

## TL;DR

- Что поставляем: активный архитектурный пакет `docs/platform/architecture/**`.
- Когда: после продуктового каркаса и до доменного пакета доступа.
- Главный риск: начать кодовую реализацию без зафиксированных owner-сервисов, данных, provider-синхронизации и MCP-границ.
- Что нужно от Owner: принять PR как согласование архитектурного каркаса.

## Входные артефакты

| Артефакт | Путь |
|---|---|
| Главный мандат | `refactoring/task.md` |
| Индекс программы | `refactoring/README.md` |
| Доменная карта | `refactoring/03-domain-map.md` |
| Целевая архитектура | `refactoring/09-target-architecture.md` |
| Границы сервисов | `refactoring/10-service-boundaries.md` |
| Модель данных | `refactoring/11-data-and-state-model.md` |
| Provider-интеграция | `refactoring/12-provider-integration-model.md` |
| Продуктовый каркас | `docs/platform/product/**` |

## Объём

Создать и связать:
- `docs/platform/architecture/c4_context.md`;
- `docs/platform/architecture/c4_container.md`;
- `docs/platform/architecture/domain_map.md`;
- `docs/platform/architecture/service_boundaries.md`;
- `docs/platform/architecture/data_model.md`;
- `docs/platform/architecture/provider_integration_model.md`;
- `docs/platform/architecture/mcp_and_interaction_model.md`;
- волновую карту `docs/delivery/issue-map/waves/wave-006-3-platform-architecture-frame.md`.

## Критерии готовности

- C4 context и container показывают границы платформы, внешние системы и целевые контейнеры.
- Owner-сервисы и запреты на владение зафиксированы в активной документации.
- Database-per-service, отсутствие cross-owner SQL-связей и outbox/inbox зафиксированы.
- Provider-first модель, webhook inbox, incremental reconciliation, hot/warm/cold приоритеты и учёт лимитов описаны на архитектурном уровне.
- Платформенный MCP описан как thin-edge инструментальный контур без доменного владения.
- `interaction-hub` отделён от `agent-manager` и owner-сервисов.
- Новые документы добавлены в индексы и delivery-карты.

## Не входит

- Детальная архитектура домена доступа.
- SQL-схемы конкретных сервисов.
- OpenAPI, gRPC и AsyncAPI спецификации.
- Кодовая реализация.
- Перенос старых документов из `deprecated/**`.

## Апрув

- request_id: `owner-2026-04-26-wave6-3-platform-architecture-frame`
- Решение: approved
- Комментарий: merge PR считается фактом согласования wave 6.3.
