---
id: OPS-DOC-COMPLETION-RECEIPT-1881
title: Согласованное чтение receipt при завершении с артефактами
type: operations
status: approved
owner: manager
version: 1.0.1
updated: 2026-10-25
---

# Receipt до проверки активного lease

Issue [#1881](https://github.com/codex-k8s/kodex/issues/1881). Подготовка
артефактов и основная owner-транзакция CompleteExecution используют одну
существующую advisory scope: organization, actor, operation и idempotency key.
Preflight сначала получает эту lock, затем читает receipt и сравнивает intent;
только отсутствие receipt разрешает проверку активного lease и подготовку bytes.
Вторая идентичная доставка не требует заново активного уже завершённого lease.

Путь: independently decoded RuntimeWorkService.CompleteExecution → прежние
verified transport principal и domain service → prepareCommandObjects →
canonical advisory/receipt → свежие lease/fence/generation, capability,
root-actor artifact.upload, deadline и CLEAN checks → preflight commit →
ObjectStore PUT → основной Execute с прежними project/advisory locks →
owner state/artifact/usage/receipt/audit/events commit. ObjectStore не выполняется
в открытой DB-транзакции; main transaction повторяет существующие проверки.
Новые API, migrations, authority, runtime grants и idempotency scope не вводятся.

| Сценарий | Авторитетный результат |
| --- | --- |
| Fresh completion с artifacts | Matching receipt отсутствует; все прежние guards обязательны, объекты готовятся после commit preflight, business effect фиксирует основной Execute. |
| Concurrent matching completion | B ждёт ту же lock, которой A защищает receipt; после commit A matching receipt читается до active-lease predicate. Один persistent outcome, повтор без второго artifact/usage/event. |
| Sequential replay / lost ACK | Независимо декодированный тот же запрос возвращает сохранённый результат; новый PUT не требуется. |
| Тот же key, иной intent | Прежний idempotency conflict; active lease и сохранённый receipt не разрешают подмену результата. |
| Чужая identity/tenant либо неверный новый lease/fence/generation | Прежние verified context, scope и lease guards закрывают запрос; новый receipt или ObjectStore effect не создаются. |
| Completion без artifacts | Preflight не нужен; основной Execute уже сериализует exact scope прежней lock. |
| Cancel/deadline/revocation либо отмена ожидания lock | Свежая работа не получает права по старому key; существующие lease/deadline/authority guards и bounded contexts сохраняются. Matching receipt не создаёт новую attempt/grant и не открывает terminal root. |
| Unknown commit/ObjectStore failure | Нового SQL retry нет; authoritative receipt readback. При неизвестном COMMIT prepared objects сохраняются по [#1889](artifact-commit-ack-1889.md); подтверждённый rollback допускает cleanup. Ошибка не объявляется успешным effect. |

Новая оснастка `TestCompletionReceiptComponent` использует обычного Agent,
actual owner/worker claim и два независимых protobuf decode одинаковых bytes.
Test-only tracer удерживает A после main lease lock, затем допускает B к
старому missing-receipt окну либо к ожиданию новой advisory lock. Каналы имеют
cancel-контракт; sleep/retry и production hooks отсутствуют. Safe proof содержит
только closed outcomes, SQL-stage labels, состояния, cardinalities и bool pins.
Client COMMIT ACK order не используется как authority сохранённого результата.

До исправления root подтвердил RED на test-only `b65c9711e`: A SUCCEEDED,
B FORBIDDEN, matching receipt1, lease COMPLETED, exact pins/capabilities=true;
PUT1 вне DB tx. Это доказательство нового common defect, а не установление
причины старого PG3 #1836. После исправления тот же неизменённый component
должен пройти; unit SKIP без DSN не является PG PASS. Старые suites
#1836/#1838/#1848 не запускаются. #1848 отдельно относится к доставке результатов
интеграций, а не к этому artifact preflight.

Проверены системные аналоги: обычный Artifact upload и role-image owner writes
уже используют canonical scoped lock до receipt; completion без artifacts
проходит защищённый основной Execute. Assistant plan staging — отдельный
proposal/draft lifecycle, который этим изменением не завершается и не получает
новых разрешений; политика его replay не переносится на completion автоматически.

Ручная проверка после отдельного разрешения поставки: в QA-проекте завершить
обычного сотрудника с небольшим CLEAN artifact, повторить доставку того же
completion и сверить единственный immutable artifact, usage и историю. Не
подставлять DB state/lease и не повторять внешний business WRITE. Staging,
browser и полный PDLC не подтверждаются локальным component тестом.
Rollback — предыдущий CP image, без отката БД; он возвращает известное окно
ложного Forbidden. Секреты и credentials не входят в код, test proof и логи.
