---
id: GUIDE-MC-001
title: Руководства разработки MatterCodex
type: guide-index
status: approved
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Руководства разработки MatterCodex

| Код | Файл | Назначение |
| --- | --- | --- |
| `GUIDE-MC-001` | `docs/guides/README.md` | Индекс. |
| `GUIDE-MC-002` | `docs/guides/repository-structure.md` | Структура монорепозитория. |
| `GUIDE-MC-003` | `docs/guides/backend-go.md` | Правила Go backend. |
| `GUIDE-MC-004` | `docs/guides/frontend-vue.md` | Правила Vue Control Center. |
| `GUIDE-MC-005` | `docs/guides/infrastructure.md` | Kubernetes, images и GitOps. |
| `GUIDE-MC-006` | `docs/guides/ci-baseline.md` | Минимальные CI gates. |
| `GUIDE-MC-007` | `docs/guides/contract-quality.md` | OpenAPI/AsyncAPI/Proto. |
| `GUIDE-MC-008` | `docs/guides/documentation.md` | Правила документации и agent instructions. |

Детальные языковые требования из `docs/design-guidelines/**` действуют, если не противоречат новому architecture baseline. В ходе структурной волны они должны быть перенесены либо явно сопоставлены с этими guides.

Если реализация требует нарушить руководство, автор останавливается и готовит ADR либо решение владельца до написания кода.
