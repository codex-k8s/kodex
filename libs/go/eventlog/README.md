# Event log

## Назначение

`libs/go/eventlog` содержит общий PostgreSQL-клиент доставки доменных событий для MVP.

Пакет не владеет доменной логикой. Он даёт:

- append-only журнал `platform_event_log` в отдельной БД `platform-event-log`;
- идемпотентную запись события по `event_id`;
- независимый checkpoint для каждого потребителя;
- короткую аренду checkpoint, чтобы один потребитель не обрабатывался несколькими worker одновременно;
- базовую веерную доставку: разные `consumer_name` читают один и тот же поток независимо.

## Граница ответственности

Сервис-владелец по-прежнему пишет событие в свой outbox в транзакции с изменением агрегата. После commit сервисный доставщик публикует outbox-событие в `platform_event_log` через отдельное подключение к БД `platform-event-log`.

Потребитель читает общий журнал через свой `consumer_name`, обрабатывает событие идемпотентно и только после этого двигает checkpoint.

Миграциями общего журнала владеет `services/internal/platform-event-log`. Миграция в `libs/go/eventlog/migrations` нужна как fixture для интеграционных тестов библиотеки и должна совпадать с миграцией владельца схемы.

## Проверки

- Обычные unit-тесты: `cd libs/go/eventlog && go test ./...`.
- PostgreSQL-интеграция: `KODEX_EVENTLOG_TEST_DATABASE_DSN=... go test ./... -run TestPostgresIntegration -count=1`.
