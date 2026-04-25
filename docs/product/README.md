---
doc_id: IDX-CK8S-PRODUCT-0001
type: domain-index
title: "Product Documentation Index"
status: in-review
owner_role: PM
created_at: 2026-03-11
updated_at: 2026-03-11
related_issues: [320]
related_prs: []
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-03-11-product-index"
---

# Product Documentation

## TL;DR
- На время программы рефакторинга продуктовый source of truth по новой версии находится в `refactoring/**`.
- `docs/product/` хранит краткий продуктовый контекст, ограничения и переходные указатели со старых baseline-путей на новую канонику.
- Старые product baseline документы перенесены в `docs/deprecated/pre-refactor/product/`.
- Здесь не размещаются delivery-планы, sprint day-эпики и runbooks.

## Канонические документы
- `refactoring/task.md` — главный приоритетный документ программы рефакторинга.
- `refactoring/README.md` — навигация по целевой модели и волнам.
- `docs/product/brief.md` — краткая формулировка продукта и ценности.
- `docs/product/constraints.md` — продуктовые и эксплуатационные ограничения.
- `docs/product/requirements_machine_driven.md` — переходный указатель на новую канонику требований.
- `docs/product/agents_operating_model.md` — переходный указатель на новую модель агентов.
- `docs/product/labels_and_trigger_policy.md` — переходный указатель на новую политику запуска.
- `docs/product/stage_process_model.md` — переходный указатель на новую модель flow/stage/role.

## Архив
- `docs/deprecated/pre-refactor/product/README.md`
- `docs/deprecated/pre-refactor/product/requirements_machine_driven.md`
- `docs/deprecated/pre-refactor/product/agents_operating_model.md`
- `docs/deprecated/pre-refactor/product/labels_and_trigger_policy.md`
- `docs/deprecated/pre-refactor/product/stage_process_model.md`

## Правила
- Product docs описывают цели, ограничения и policy, но не дублируют sprint execution.
- Если документ начинает описывать delivery sequencing или реализацию сервиса, ссылка должна вести в `docs/delivery/` или `docs/architecture/`.
- Документы из `docs/deprecated/pre-refactor/product/**` нельзя использовать как основание для новой кодовой реализации без явного решения в `refactoring/**`.
