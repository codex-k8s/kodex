---
doc_id: DPD-CK8S-PRE-REFACTOR-0001
type: deprecated-index
title: Архив документов до рефакторинга
status: archived
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-04-25
related_issues: []
related_prs: []
---

# Архив документов до рефакторинга

## TL;DR
- `docs/deprecated/pre-refactor/**` хранит исторические документы старой модели платформы.
- Эти документы не являются каноникой для новых волн реализации.
- Frontmatter внутри перенесённых документов мог остаться историческим; архивный путь всегда имеет приоритет и означает режим «только для справки».
- Новая каноника программы рефакторинга находится в `refactoring/**` и в активных переходных документах `docs/product/*.md`.

## Каталоги
- `docs/deprecated/pre-refactor/product/` — старые product baselines по machine-driven модели, лейблам, ролям и этапам.
- `docs/deprecated/pre-refactor/delivery/sprints/` — старые sprint-планы.
- `docs/deprecated/pre-refactor/delivery/epics/` — старые day-эпики и epic-каталоги.

## Правила использования
- Агент может читать этот каталог только как историческую справку, если нужно понять, почему старое решение было вытеснено.
- Нельзя использовать документы из этого каталога как основание для новой кодовой реализации.
- Если активная документация расходится с архивом, приоритет всегда у `refactoring/**`, `AGENTS.md`, `docs/design-guidelines/**` и активных индексов `docs/**`.
