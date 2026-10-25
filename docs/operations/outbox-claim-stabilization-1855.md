---
id: OPS-OUTBOX-1855
title: Восстановление доставки событий после большой опубликованной истории
type: operations
status: approved
owner: sre
version: 1.0.0
updated: 2026-10-25
---

# Область #1855

Обычные запуски фиксировали `RUN_CHANGED`, однако оба control-plane relay
оставались внутри SQL claim около16минут. Шесть проверенных событий имели
`PENDING`, `attempts=0`, без broker receipt. Опубликованная история одного
ordering key содержала69161строку; за ней накопилось7684неопубликованных.
Авторитетный cursor поэтому опережал WebSocket. Через15секунд после bootstrap
gateway требовал полный resync; NATS ещё не получал проверенные события.

Безопасный `EXPLAIN` без исполнения показал nested anti join: для кандидатов
перечитывался полный индекс `(ordering_key,sequence)` с последующим фильтром
`state <> 'PUBLISHED'`. Частичный индекс того же ключа и точно этого предиката
исключает опубликованную историю из поиска блокирующих предшественников.
`DEAD_LETTER` остаётся блокирующим состоянием. Сам SQL claim не изменён.

`ClaimOutbox` получает собственный бюджет5секунд, ограниченный также исходным
контекстом. Ошибка возвращает прежний закрытый `ErrUnavailable` без частичного
пакета; repository не пишет SQL, payload или driver error в журнал. Существующий
relay прекращает свой цикл при ошибке claim и передаёт её владельцу lifecycle.
Инфраструктурный readiness сам по себе по-прежнему не доказывает продвижение
очереди: при приёмке проверяются фактическая публикация и consumer readback.

## Карта и переходы

Источник — требования ordered durable outbox из `GO-DOC-004`, `GUIDE-DOC-006`
и подтверждённый staging-дефект #1855. Инициатор — прежняя доменная команда
control-plane с проверенным actor/tenant, внешний endpoint и RPC этой команды
сохраняются. Владелец состояния — PostgreSQL control-plane. Relay получает
серверный lease token, публикует прежние exact event bytes с устойчивым event ID,
затем сохраняет broker receipt. Consumers — прежние NATS подписчики и их
авторитетные read paths; schema, subjects, permissions и RuntimeRevision не меняются.

| Переход | Условие и authority | Устойчивый эффект и результат |
| --- | --- | --- |
| Business command → PENDING | Прежняя доменная owner-транзакция | Aggregate, audit/idempotency и outbox фиксируются атомарно по прежнему контракту |
| PENDING → CLAIMED | Настал available_at; нет раннего неопубликованного predecessor; locked row/SKIP LOCKED | Новые exact lease token/expiry и attempt, возвращённый пакет разрешает publish |
| CLAIMED expired → CLAIMED | Истёк прежний lease; тот же predecessor predicate | Новый lease/attempt; прежний token не разрешает finalization |
| Claim timeout/cancel | Исходный context или бюджет5секунд | Публикация по отсутствующему результату запрещена; неизвестный commit восстанавливается через lease expiry |
| CLAIMED → PUBLISHED | Exact event/lease и durable broker receipt | Receipt и published_at сохраняются; следующий ordered event становится eligible |
| CLAIMED → PENDING | Exact lease, прежний bounded retry/backoff | Очередь ждёт available_at; следующие события не обходят predecessor |
| CLAIMED → DEAD_LETTER | Прежнее исчерпание attempts | Следующие события остаются заблокированы; автоматического skip/repair нет |
| Process restart/shutdown | Прежний cancel/join до закрытия pool | Неизвестный claim не создаёт внешний эффект; lease expiry остаётся владельцем восстановления |
| Purge/cancel/owner gate | Прежние специализированные команды | Индекс не создаёт новых событий, grants или обходов terminal/tenant правил |

## Доставка и возврат

Изменение содержит forward-only миграцию20261025000100 и адресный control-plane
образ. Новая миграция не меняет business rows. DDL использует lock_timeout8s и
statement_timeout30s внутри goose-транзакции; при ошибке индекс и запись версии
не принимаются частично. Старый бинарник совместим с добавленным индексом.

Перед живой миграцией завершить QA модельные ходы и проверить активные задачи,
точные cluster/namespace/workload identities, свободные ресурсы и исходную
версию миграций. Порядок задаёт
[адресный immutable migrator](control-plane-immutable-migration-1855.md):
сохранить приватный план с точными image/source/UID и вернуть control-plane
после ограниченного окна недоступности. Прежние две реплики сначала адресно
останавливаются; после подтверждения завершения Pod и claim transactions
выполняется миграция. После terminal Job возвращаются две реплики прежнего
образа, затем отдельным scoped-release устанавливается образ с bounded claim.
RollingUpdate нового бинарника до индекса не гарантирует Ready и не используется.
Plan/journal/readback не содержат Secret values. Применение допустимо только
после component-проверок и независимого review итогового чистого SHA.

При неизвестном создании Job сначала readback exact имени/UID/плана; новый Job
или новый idempotency key не создаётся вслепую. При lock timeout исходный
блокирующий запрос диагностируется; бесконечных повторов и ручного skip нет.
После завершения сверить goose version, index definition/validity и реальное
продвижение старейшего outbox, затем Chrome Console/Network и неизменность
соседних маршрутов. Ready Pod без этих проверок не считается восстановлением.

Возврат приложения — предыдущий exact immutable control-plane image через
сохранённый scoped-release plan. Индекс остаётся: удаление не требуется для
совместимости и возвращает дорогой запрос. Прямые UPDATE outbox, удаление
истории, выдача новых прав, смена NATS/ingress и production сюда не входят.

## Проверки и документация

Публичная локальная точка входа:
`scripts/tests/control-plane-postgres-test.sh '^TestOutboxPublishedHistoryComponent$'`.
Бюджет внешнего запуска300секунд, один disposable PostgreSQL;70тысяч опубликованных
и8тысяч ожидающих безопасных fixture events. Реальный Claim проверяет время,
необход DEAD_LETTER/неистёкшего lease/отложенного predecessor, конкурирующий
worker, чужой token, продвижение после publish и восстановление после timeout
при блокировке таблицы. До исправления воспроизведён FAIL по бюджету5секунд.
После исправления на штатном Go1.26.6 весь component прошёл за10.83секунды;
первый claim выполнился за186мс. Проверка `make check-sql-boundary` на том же
toolchain прошла. Миграции, повторный goose up и существующие read-only проверки
worker/runner прошли. Эти результаты относятся к локальному дереву изменения;
точный зафиксированный SHA приводится в PR. Живая миграция и повторный browser
readback ещё NOT RUN и не заменяются component-тестом.

Context7 недоступен: Invalid API key. Проверены официальные документы
[partial indexes PostgreSQL18](https://www.postgresql.org/docs/18/indexes-partial.html)
и [CREATE INDEX](https://www.postgresql.org/docs/18/sql-createindex.html): предикат
должен соответствовать запросу, обычное построение временно блокирует запись;
`IF NOT EXISTS` не доказывает совпадение определения индекса.
