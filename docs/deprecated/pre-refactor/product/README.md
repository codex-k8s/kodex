---
doc_id: DPD-CK8S-PRE-REFACTOR-PRODUCT-0001
type: deprecated-index
title: Архив product baseline до рефакторинга
status: archived
owner_role: PM
created_at: 2026-04-25
updated_at: 2026-04-25
related_issues: []
related_prs: []
---

# Архив product baseline до рефакторинга

## TL;DR
- Здесь сохранены старые product-документы, которые больше не являются каноникой новой платформы.
- Активная каноника продуктовой модели находится в `refactoring/**` и переходных документах `docs/product/*.md`.
- Архив нужен только для сравнения и извлечения полезных идей без переноса старых ограничений в новую реализацию.

## Содержимое
- `requirements_machine_driven.md` — старый baseline требований.
- `agents_operating_model.md` — старая operating model агентов.
- `labels_and_trigger_policy.md` — старая label/trigger policy.
- `stage_process_model.md` — старая stage model.

## Ограничения
- Не использовать эти документы как source of truth для волны 7+.
- Не восстанавливать старые label-first и sprint-first решения без отдельного design-решения в `refactoring/**`.
