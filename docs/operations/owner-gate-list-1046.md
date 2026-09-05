---
id: OPS-DOC-1046-OWNER-GATE-LIST
title: Авторитетный поиск owner gates для Home
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Поиск и страницы owner gates

Источник: Issue #1046, Epic #1018 и исходный MVP Home/Kanban. Существующий
`GET /api/v1/owner-gates` передаёт `query`, `state`, `projectRef` и страницу
в `PlatformQueryService.ListOwnerGates`. Новый RPC и новая permission не нужны.

| Сценарий | Authority и владелец | Ответ и потребитель |
| --- | --- | --- |
| Первая страница | Проверенный actor/organization; CP PostgreSQL проверяет действующий VIEW проекта либо OWNER/ADMINISTRATOR | Gates и total после точных project/state/query фильтров; HTTP и PWA используют серверный total |
| Продолжение | Cursor связан с actor, organization и всеми фильтрами; eligibility повторяется | Стабильный порядок createdAt/ref DESC, ограниченная страница, следующий cursor |
| Чужой actor или изменённые фильтры cursor | Cursor не является authority | INVALID_ARGUMENT; исходные данные не выдаются |
| Нет совпадений | Тот же owner predicate, включая подсчёт | Пустая страница, total=0, пустой cursor |
| Gate изменил состояние между страницами | Новый авторитетный snapshot, без обещания исторической выборки | Актуальный total; ранее выданная позиция не зависит от сохранения старого state |
| Утрата VIEW | Чтение и подсчёт повторяют актуальную membership eligibility | Скрытые gates исключены из rows и total |

Query — буквальная подстрока title, prompt или context summary без учёта регистра,
не SQL wildcard. Максимум 200 символов. Неизвестный state закрыто отклоняется.
Один repeatable-read snapshot связывает total, страницу и полные gate DTO.
Команд, idempotency receipts, переходов состояния и событий у этого read path нет.
Авторитетное чтение после resolve/cancel/expiry остаётся тем же List/GetOwnerGate.
