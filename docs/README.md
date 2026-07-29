---
id: DOC-MC-001
title: Документация MatterCodex
type: documentation-index
status: approved
owner: architect
version: 1.0.0
updated: 2026-07-29
---

# Документация MatterCodex

## Источник истины

- `product/` - назначение, персоны, процессы, сценарии и требования.
- `architecture/` - компоненты, boundaries, integrations, данные, runtime, artifacts и schedules.
- `domains/` - владение данными, interfaces, events и acceptance.
- `decisions/` - ADR и статус решений.
- `guides/` - правила реализации Go/Vue/contracts/infrastructure/CI.
- `governance/` - кодификация, профиль проверок, открытые решения и
  происхождение адаптированных правил.
- `operations/` - production profiles, SLO, observability, backup, deployment и security.
- `roadmap/` - эпики, result gates и dogfooding bootstrap.
- `runbooks/` - пошаговые инструкции для текущего live contour.
- `design-guidelines/` - детальные технические правила, дополняющие guides.

## Исторический контекст

- `strategy/` - superseded baseline личного single-user MVP. Полезен для понимания текущей реализации, но не определяет target architecture.
- `idea/` - исходные документы идеи.

## Текущий переход

Действующий Mattermost-first инстанс сохраняется как замороженный
legacy-контур. Новая production-платформа реализуется полными unit по
`roadmap/epics-and-waves.md`. После готовности компонентов выполняются
проверяемая миграция данных, staging acceptance, human gate и контролируемый
cutover без постоянного compatibility facade.
