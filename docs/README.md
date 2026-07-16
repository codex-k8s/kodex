# Документация MatterCodex

## Источник истины

- `product/` - назначение, персоны, процессы, сценарии и требования.
- `architecture/` - компоненты, boundaries, integrations, данные, runtime, artifacts и schedules.
- `domains/` - владение данными, interfaces, events и acceptance.
- `decisions/` - ADR и статус решений.
- `guides/` - правила реализации Go/Vue/contracts/infrastructure/CI.
- `operations/` - production profiles, SLO, observability, backup, deployment и security.
- `roadmap/` - эпики, result gates и dogfooding bootstrap.
- `runbooks/` - пошаговые инструкции для текущего live contour.
- `design-guidelines/` - детальные технические правила, дополняющие guides.

## Исторический контекст

- `strategy/` - superseded baseline личного single-user MVP. Полезен для понимания текущей реализации, но не определяет target architecture.
- `idea/` - исходные документы идеи.

## Текущий переход

Действующий Mattermost-first инстанс сохраняется. Переход к универсальной production-платформе выполняется эволюционно по `roadmap/epics-and-waves.md`, с compatibility migrations и human gates по законченным типам результата.
