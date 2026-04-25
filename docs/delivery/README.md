---
doc_id: IDX-CK8S-DELIVERY-0001
type: domain-index
title: "Delivery Documentation Index"
status: in-review
owner_role: EM
created_at: 2026-03-11
updated_at: 2026-03-12
related_issues: [320, 327]
related_prs: []
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-03-11-delivery-index"
---

# Delivery Documentation

## TL;DR
- `docs/delivery/` хранит process requirements, delivery plan и traceability.
- Старые sprint/epic execution документы перенесены в `docs/deprecated/pre-refactor/delivery/**` и больше не являются рабочей каноникой.
- Текущая программа рефакторинга ведётся через документы волн в `refactoring/**`.
- Root traceability разделена по уровням: `issue_map.md` = master-index, `requirements_traceability.md` = стабильная FR/NFR-матрица, `traceability/*.md` = historical evidence.
- Здесь же лежит migration-map для рефакторинга docs IA.

## Канонические документы
- `docs/delivery/development_process_requirements.md`
- `docs/delivery/delivery_plan.md`
- `docs/delivery/issue_map.md`
- `docs/delivery/requirements_traceability.md`
- `docs/delivery/traceability/README.md`
- `docs/delivery/e2e_mvp_master_plan.md`
- `docs/delivery/documentation_ia_migration_map.md`

## Индексы и каталоги
- `docs/delivery/sprints/README.md` — переходный указатель на архив старых спринтов.
- `docs/delivery/epics/README.md` — переходный указатель на архив старых эпиков.
- `docs/deprecated/pre-refactor/delivery/sprints/README.md`
- `docs/deprecated/pre-refactor/delivery/epics/README.md`
- `docs/delivery/traceability/README.md`
- `docs/deprecated/pre-refactor/delivery/sprints/`
- `docs/deprecated/pre-refactor/delivery/epics/`
- `docs/delivery/traceability/`

## Правила
- Любой перенос документов синхронно отражается в `issue_map`, `requirements_traceability`, `traceability/README.md`, релевантном индексе и `services.yaml`.
- Delivery docs отвечают за sequencing, quality-gates и traceability, но не дублируют product/architecture source of truth.
- Для волны 7+ не создавать новые документы в `docs/delivery/sprints/**` и `docs/delivery/epics/**`.
