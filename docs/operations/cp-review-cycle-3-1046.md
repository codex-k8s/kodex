---
id: OPS-DOC-CP-REVIEW-CYCLE-3-1046
title: Исправления owner boundaries после второго общего review
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-06
---

# Исправления control-plane #1046

Refs #1085, #1089, #1096; существующий полный PR #1071. Основание — независимые
architecture/security review `5035e6268ad7cb24c381855ab25b0958ff4c204e`.
Нормализованная ветка сохранена; applied migrations не изменяются.

## Карта сценариев и переходов

| Требование / actor / вход | Owner transaction | Receipt, audit, read и consumer |
|---|---|---|
| #1085 runtime-controller → ClaimExecution | Expected-invalid candidate откатывает подготовку и завершает весь root graph. Audit фиксируется для каждого изменённого Run; ошибка PostgreSQL откатывает всю transaction | Empty RuntimeItems не означает отсутствие эффекта. Terminal/expiry сохраняет command receipt. Повтор K не забирает новый B; новый K допускает прогресс |
| #1085 настоящий idle poll | Ни нового claim, ни terminal graph, ни expired lease | Не создаёт receipt/audit |
| #1085 expiry-only | Возвращаются все expired leases, даже если terminal Run уже не переводится в QUEUED | Audit Run и receipt атомарны. Существующие authoritative lease/graph reads показывают expiry; новый event contract не вводится |
| #1089 broker → cleanup completion | Stable produced A может прийти до либо после отдельной очистки A. Current credential, чужой account/tenant/UID и self-cycle закрыты | COMPLETED A принимается только с exact pins, terminal receipt/completion descriptor, completed_at, без safe error/superseded marker. Повторный provider effect не создаётся |
| #1096 browser → avatar upload | Agent блокируется; avatar.manage проверяется до OCC/receipt и повторно перед finalize | Replay требует agent.view; nextActions пересчитываются. Object compensation сохраняется |
| #1096 browser → artifact upload | Upload eligibility проверяется до object Put и в финальной transaction перед receipt | Оба receipt branches повторяют FILE read predicate и свежую projection |
| #1096 browser → terminal Delete Connection/Schedule/Environment | Tombstone resolver читает retained owner/project/related refs; текущие manage/delete и read права предшествуют ответу | Exact replay сохраняется. Обычные get/list deleted не открываются. Connection grants пересобираются по текущему читателю; terminal nextActions пусты |

## Проверки и ручная приёмка

Публичный вход: `make test-control-plane-postgres`, включая отдельный Avatar
component. Runtime regression: mixed, all-stale, lost ACK K → новый B → replay K,
truly idle, expiry-only и SQL failure после изменения графа. Cleanup: оба порядка
completion, exact replay, чужой descriptor, self-cycle, неполный receipt,
immutable terminal proof. Upload и terminal delete используют новый proof после
revoke, сохраняя отдельный Project/organization grant; foreign signed Project
также не разрешает tombstone.

Исторические FAIL новой оснастки сохранены: повторное имя Project/фиксированные
sibling refs, raw principal в scope helper, отсутствующий promoted image при
узком запуске, сравнение разных reader projections и попытка изменить защищённый
completion descriptor. Mapping project.view на Environment исправлен точным
PROJECT target. Итоговые PASS/FAIL привязываются к exact checkpoint отдельно.

Ручная проверка после отдельного разрешения: повторить upload/delete после
отзыва точных прав, сохранив login/project доступ; snapshot не возвращается.
Поменять порядок ACK authorization/credential cleanup и проверить DELETED только
после завершения graph. Проверить all-stale claim с потерей ответа и новым Run.
Live/deploy не выполнялись.

Context7: `/jackc/pgx`, transactions, nested savepoints, Commit/Rollback, ErrNoRows.
Новые Proto/HTTP поля не требуются. Rollback не удаляет durable receipts/cleanup
историю и не возвращает отозванные полномочия. Секреты, credential bytes и
персональные данные не публикуются.
