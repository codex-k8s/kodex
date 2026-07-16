---
id: GUIDE-MC-008
title: Документация и инструкции агентов
type: guide
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Документация и инструкции агентов

## Метаданные

Активный document имеет front matter: stable `id`, title, type, status, owner, version и updated date.

Statuses: `draft`, `proposed`, `approved`, `superseded`, `archived`.

## Источник истины

- Product behavior — `docs/product`.
- System architecture — `docs/architecture` и ADR.
- Domain ownership — `docs/domains`.
- Implementation rules — `docs/guides` и детальные design-guidelines.
- Operations — `docs/operations` и runbooks.
- Roadmap — `docs/roadmap`.

Документ не копирует правило другого раздела без ссылки и объяснения локального контекста.

## Изменения

- Существенное решение обновляет product/domain/architecture и ADR в одном result cycle.
- Код не принимается, если он расходится с approved docs без отдельного ADR.
- Improver обновляет instructions/guides только через reviewable proposal.
- Исторический документ получает `superseded` и ссылку на замену.

## Язык

Проектные документы, PR/issues/review comments по умолчанию пишутся на выбранной project/user locale, если `AGENTS.md` не задает более конкретное правило. Кодовые identifiers и upstream terminology не переводятся искусственно.

## Agent instructions

InstructionSet должен быть коротким index с ссылками на релевантные documents. Нельзя копировать весь corpus в prompt. Runtime materializer передает manifest и дает агенту читать нужные файлы.
