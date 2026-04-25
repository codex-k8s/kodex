---
doc_id: BRG-CK8S-PRODUCT-AGENTS-0001
type: bridge-document
title: Переходный указатель модели агентов
status: active
owner_role: PM
created_at: 2026-04-25
updated_at: 2026-04-25
related_issues: []
related_prs: []
---

# Переходный указатель модели агентов

## TL;DR
- Старый документ с operating model агентов перенесён в `docs/deprecated/pre-refactor/product/agents_operating_model.md`.
- Новая модель строится вокруг задач провайдера, flow, stage, role, шаблонов промптов, платформенного MCP и runtime-контура.
- Для волны 7+ используйте документы `refactoring/**`, а не старую label-first модель.

## Активные источники
- `refactoring/08-provider-native-work-model.md` — задачи, PR/MR, комментарии и связи у провайдера.
- `refactoring/10-agent-orchestration-model.md` — flow, stage, role, prompt templates и запуск агентов.
- `refactoring/12-interaction-hub-and-mcp.md` — платформенный MCP и взаимодействие с пользователем.
- `refactoring/19-flow-role-prompt-and-settings-ux.md` — UX управления flow, ролями и шаблонами.

## Архив
Исторический документ доступен по пути:
`docs/deprecated/pre-refactor/product/agents_operating_model.md`.
