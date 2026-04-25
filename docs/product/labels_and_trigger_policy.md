---
doc_id: BRG-CK8S-PRODUCT-LABELS-0001
type: bridge-document
title: Переходный указатель политики запуска
status: active
owner_role: PM
created_at: 2026-04-25
updated_at: 2026-04-25
related_issues: []
related_prs: []
---

# Переходный указатель политики запуска

## TL;DR
- Старый документ по labels and trigger policy перенесён в `docs/deprecated/pre-refactor/product/labels_and_trigger_policy.md`.
- Новая модель не должна сводиться к GitHub labels: запуск задаётся flow, automation rules, provider events, cron rules, UI-командами и платформенным MCP.
- Лейблы провайдера остаются одним из возможных сигналов, но не единственным источником истины.

## Активные источники
- `refactoring/08-provider-native-work-model.md` — provider-native сущности и события.
- `refactoring/10-agent-orchestration-model.md` — правила запуска flow/stage/role.
- `refactoring/15-risk-release-and-automation.md` — автоматизация по расписанию и событиям.
- `refactoring/12-interaction-hub-and-mcp.md` — быстрые операции через платформенный MCP.

## Архив
Исторический документ доступен по пути:
`docs/deprecated/pre-refactor/product/labels_and_trigger_policy.md`.
