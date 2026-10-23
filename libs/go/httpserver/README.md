---
id: GO-LIB-HTTPSERVER-001
title: Технический HTTP server
type: component
status: approved
owner: backend
version: 1.4.0
updated: 2026-10-22
---

# `httpserver`

Общий технический HTTP server для `/healthz`, `/readyz` и `/metrics`.
Библиотека также принимает ограниченный набор service-owned `ExactGETRoute`
для безопасного readback: только точный `GET` без query/body и с общими
защитными заголовками. Библиотека не регистрирует изменяющие состояние или
произвольные маршруты, не создаёт root context и не
запускает goroutine при создании сервера. Владелец процесса передаёт handler
метрик, предикат готовности и отдельно вызывает ограниченный `Shutdown`.

Публичный API состоит из `New`, `StandardProcessConfig`, `ExactGETRoute`,
`Listen`, блокирующего `Serve`, адаптеров `Worker`/`ShutdownWorker` и `Shutdown`.
`StandardProcessConfig` возвращает одинаковые лимиты для небольших gateway/job:
2 с на заголовки, 5 с на чтение
и запись, 30 с простоя, 32 КиБ заголовков и 128 соединений. Он не открывает
socket и не выбирает адрес за вызывающего; другие компоненты могут передать
собственный `Config`.
`Listen` резервирует socket после startup barrier, а goroutine и порядок
cancel/join принадлежат composition root. `Worker()` возвращает функцию
`func(context.Context) error`, пригодную для `serviceruntime.StartWorkers`
без зависимости библиотеки от конкретной worker group. Создание функции
ничего не запускает; при выполнении она запускает `Serve` и ждёт его
результат либо отмену контекста. При отмене функция возвращает `ctx.Err()`,
но не закрывает listener и не дожидается `Serve`: владелец отдельно вызывает
`Shutdown` в прежнем порядке. Буфер результата позволяет `Serve` вернуть
ошибку и после завершения worker. Это не замена адаптеров, которые сами
выполняют shutdown и join, например у планировщика автоматизаций.
`ShutdownWorker(serve, shutdown, timeout)` предназначен именно для
существующих self-shutdown workers: он принимает принадлежащие потребителю
callbacks `http.Server.ListenAndServe` и `Shutdown`, ничего не запускает
при создании и не меняет конфигурацию или маршруты сервера. При выполнении
запускает serve, а при отмене создаёт отдельный shutdown context со значениями
работника, но без его отмены/deadline, затем с переданным timeout вызывает
shutdown и дожидается возврата serve. `http.ErrServerClosed`, включая
обёрнутый, считается штатным; остальные serve/shutdown errors сохраняются,
в ветке отмены объединяются. При раннем возврате serve shutdown не вызывается.
Callbacks и положительный timeout проверяет владелец до создания;
библиотека не подставляет defaults, не читает env, не назначает authority
и не повторяет операции. Shutdown callback обязан соблюдать context;
возврат serve не означает, что после timeout завершились все handlers или
hijacked-соединения. Их отдельный жизненный цикл остаётся у владельца.
Этот adapter не заменяет `Server.Worker()`: его cancel/shutdown/join
контракт намеренно иной. Прежние локальные serveHTTP у двух потребителей
удалены, совместимые обёртки не оставлены.

Конфигурация закрыто ограничивает
read-header/read/write/idle timeouts и размер headers; маршруты принимают
только `GET`, ставят `no-store`/`nosniff` и не читают request body сверх 1 MiB.

Библиотека не определяет business readiness, authentication внешнего API,
metric registry или logging. Совместимое расширение может усиливать safe
defaults без добавления бизнесовых endpoints; переименование маршрутов либо
изменение lifecycle требует миграции всех потребителей.

## Подпакет `streamproxy`

Отдельный stateless-подпакет разделяет транспорт двух независимых registry
authorizers, не добавляя маршруты в технический сервер. `NewClient` задаёт
переданный timeout и запрещает redirects; стандартный transport, включая
его proxy-настройки, не меняется. `Timeouts` и `NewMTLSServer` собирают
`http.Server` с TLS1.3, `RequireAndVerifyClientCert`, переданным pool CA
и бюджетами заголовков/потока/простоя. Проверка конфигурации, открытие listener,
Serve/Shutdown и cancel/join принадлежат composition root.

`Forward` вызывается только после service-owned авторизации. Он сохраняет
фиксированный backend вызывающего, копирует path/query и headers без
Authorization, передаёт выбранное body или nil и ограничивает ответ заданным
числом байт. Тело ответа закрывается; прежние ошибки копирования после
отправленного статуса не превращаются в новый ответ или retry. Значения
timeouts/limit не нормализуются и не получают новых defaults.

Это не универсальный защищённый reverse proxy: он не выбирает CN, CA,
репозитории, методы, credential files или destination allowlist, не фильтрует
hop headers и не усиливает прежние URL/body semantics. Владельцы вызывают
его с собственным доверенным backend и бюджетом, оставляя authority до
вызова. Подпакет не создаёт goroutines, root context, метрики или логи и
возвращает прежние закрытые upstream diagnostics без исходной ошибки.
Изменение wire/authority требует отдельного изменения обоих потребителей;
совместимые обёртки старых конструкторов не оставлены.
