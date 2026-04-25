---
doc_id: MAP-XXXX
type: issue-map
title: "Issue ↔ Docs Map"
status: draft
owner_role: KM
created_at: YYYY-MM-DD
updated_at: YYYY-MM-DD
---

# Issue ↔ Docs Map

## TL;DR
Матрица трассируемости для одного домена или одной волны: Issue/PR ↔ документы ↔ релизы.

Этот шаблон не используется для единого общего файла на весь проект. Общая навигация живёт в `docs/delivery/issue-map/index.md`, а длинные таблицы разделяются на:
- `docs/delivery/issue-map/domains/<domain>.md`;
- `docs/delivery/issue-map/waves/<wave>.md`.

## Матрица
| Issue/PR | DocSet | PRD | Design | ADRs | Test Plan | Release Notes | Postdeploy | Status |
|---|---|---|---|---|---|---|---|---|
| #123 | docset/issues/issue-123.md | PRD-... | DSG-... | ADR-... | TST-... | REL-... | PDR-... | ... |

## Правила
- Если нет обязательного документа — статус `blocked`.
- Ссылки должны быть кликабельны.
- Доменный файл содержит долгоживущие связи домена.
- Волновой файл содержит снимок активной волны и не обязан включать соседние домены.
- Один PR должен править только релевантный доменный файл и, если нужно, файл текущей волны.
