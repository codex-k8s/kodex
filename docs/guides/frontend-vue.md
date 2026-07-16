---
id: GUIDE-MC-004
title: Control Center на Vue
type: guide
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Control Center на Vue

## Назначение

Control Center закрывает сложную настройку organizations, workspaces, agents, providers, integrations, schedules, playbooks, runtime, artifacts и audit. Mattermost остается conversational surface.

## Стек

- Vue 3 Composition API;
- TypeScript strict mode;
- generated OpenAPI client;
- Pinia только для cross-page state;
- Vue Router;
- выбранная и зафиксированная component library либо собственный малый design system;
- Playwright для critical E2E.

Версии утверждаются dependency catalog на момент реализации.

## UX

- Entity list/dashboard/editor вместо command console.
- Technical IDs скрыты или read-only в advanced diagnostics.
- Presets для schedule, role и integration setup.
- Secret input очищается после submit и не возвращается API.
- Effective configuration объясняет inheritance и `managed_by`.
- Dangerous action показывает target/effect и требует явного confirmation.
- Error state сохраняет введенные несекретные данные и предлагает next action.

## Архитектура

```text
apps/control-center/src/
  app/
  pages/
  features/
  entities/
  shared/
  generated/
```

Generated client не импортирует UI. Business-specific composables живут в features/entities, а не в global store.

## Quality

- ESLint/format/typecheck/unit tests;
- component tests для complex editors;
- Playwright happy/failure/permission paths;
- accessibility checks для keyboard, labels, focus и contrast;
- screenshots desktop/mobile для critical flows;
- no secret/token в browser storage и telemetry.
