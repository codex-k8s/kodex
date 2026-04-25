---
doc_id: IDX-CK8S-SPRINTS-BRIDGE-0001
type: bridge-index
title: Переходный указатель старых спринтов
status: active
owner_role: EM
created_at: 2026-04-25
updated_at: 2026-04-25
related_issues: []
related_prs: []
---

# Переходный указатель старых спринтов

## TL;DR
- Старые sprint-планы перенесены в `docs/deprecated/pre-refactor/delivery/sprints/`.
- Для новых волн реализации этот каталог больше не используется как рабочая каноника.
- Текущий план рефакторинга ведётся через `refactoring/task.md`, `refactoring/README.md` и связанные документы волн.

## Куда смотреть
- Архив старых спринтов: `docs/deprecated/pre-refactor/delivery/sprints/README.md`.
- Текущая программа рефакторинга: `refactoring/README.md`.
- Главная задача и приоритеты: `refactoring/task.md`.
- Расклад волн после wave 6: `refactoring/24-implementation-waves-after-wave6.md`.

## Правило для агентов
Не создавайте новые документы в `docs/delivery/sprints/**`.
Если нужна новая delivery-декомпозиция, добавляйте её в актуальные документы `refactoring/**` или в GitHub Issues текущей волны.
