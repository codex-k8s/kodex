# `httpserver`

Общий технический HTTP server для `/livez`, `/readyz` и `/metrics`.
Библиотека не регистрирует бизнесовые маршруты, не создаёт root context и не
скрывает goroutine. Владелец процесса передаёт handler метрик, предикат
готовности и отдельно вызывает ограниченный `Shutdown`.

Публичный API состоит из `New`, `Listen`, блокирующего `Serve` и `Shutdown`.
`Listen` резервирует socket после startup barrier, а goroutine и порядок
cancel/join принадлежат composition root. Конфигурация закрыто ограничивает
read-header/read/write/idle timeouts и размер headers; маршруты принимают
только `GET`, ставят `no-store`/`nosniff` и не читают request body сверх 1 MiB.

Библиотека не определяет business readiness, authentication внешнего API,
metric registry или logging. Совместимое расширение может усиливать safe
defaults без добавления бизнесовых endpoints; переименование маршрутов либо
изменение lifecycle требует миграции всех потребителей.
