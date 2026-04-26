---
doc_id: DLV-CK8S-WAVE-006-3
type: delivery-plan
title: kodex — волна 6.3, сквозной архитектурный каркас платформы
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

# Волна 6.3 — сквозной архитектурный каркас платформы

## Кратко

- Что поставляем: активный архитектурный пакет `docs/platform/architecture/**`.
- Когда: после продуктового каркаса и до доменного пакета доступа.
- Главный риск: начать кодовую реализацию без зафиксированных сервисов-владельцев, данных, синхронизации с провайдером и MCP-границ.
- Что нужно от владельца: принять PR как согласование архитектурного каркаса.

## Входные артефакты

| Артефакт | Путь |
|---|---|
| Главный мандат | `refactoring/task.md` |
| Индекс программы | `refactoring/README.md` |
| Доменная карта | `refactoring/03-domain-map.md` |
| Целевая архитектура | `refactoring/09-target-architecture.md` |
| Границы сервисов | `refactoring/10-service-boundaries.md` |
| Модель данных | `refactoring/11-data-and-state-model.md` |
| Интеграция с провайдерами | `refactoring/12-provider-integration-model.md` |
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

- C4-контекст и C4-контейнеры показывают границы платформы, внешние системы и целевые контейнеры.
- Сервисы-владельцы и запреты на владение зафиксированы в активной документации.
- Модель «БД на сервис», отсутствие SQL-связей между сервисами-владельцами и outbox/inbox зафиксированы.
- Модель, где провайдер остаётся источником рабочих артефактов, входящий журнал webhook, инкрементальная сверка, приоритеты горячих, тёплых и холодных сущностей и учёт лимитов описаны на архитектурном уровне.
- Платформенный MCP описан как тонкий пограничный инструментальный контур без доменного владения.
- `interaction-hub` отделён от `agent-manager` и сервисов-владельцев.
- Новые документы добавлены в индексы и карты поставки.

## Не входит

- Детальная архитектура домена доступа.
- SQL-схемы конкретных сервисов.
- OpenAPI, gRPC и AsyncAPI спецификации.
- Кодовая реализация.
- Перенос старых документов из `deprecated/**`.

## Апрув

- request_id: `owner-2026-04-26-wave6-3-platform-architecture-frame`
- Решение: approved
- Комментарий: слияние PR считается фактом согласования волны 6.3.
