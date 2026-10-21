---
id: GO-LIB-HTTPSERVER-001
title: Технический HTTP server
type: component
status: approved
owner: backend
version: 1.1.0
updated: 2026-10-21
---

# `httpserver`

Общий технический HTTP server для `/healthz`, `/readyz` и `/metrics`.
Библиотека также принимает ограниченный набор service-owned `ExactGETRoute`
для безопасного readback: только точный `GET` без query/body и с общими
защитными заголовками. Библиотека не регистрирует изменяющие состояние или
произвольные маршруты, не создаёт root context и не
скрывает goroutine. Владелец процесса передаёт handler метрик, предикат
готовности и отдельно вызывает ограниченный `Shutdown`.

Публичный API состоит из `New`, `StandardProcessConfig`, `ExactGETRoute`,
`Listen`, блокирующего `Serve` и `Shutdown`. `StandardProcessConfig` возвращает
одинаковые лимиты для небольших gateway/job: 2 с на заголовки, 5 с на чтение
и запись, 30 с простоя, 32 КиБ заголовков и 128 соединений. Он не открывает
socket и не выбирает адрес за вызывающего; другие компоненты могут передать
собственный `Config`.
`Listen` резервирует socket после startup barrier, а goroutine и порядок
cancel/join принадлежат composition root. Конфигурация закрыто ограничивает
read-header/read/write/idle timeouts и размер headers; маршруты принимают
только `GET`, ставят `no-store`/`nosniff` и не читают request body сверх 1 MiB.

Библиотека не определяет business readiness, authentication внешнего API,
metric registry или logging. Совместимое расширение может усиливать safe
defaults без добавления бизнесовых endpoints; переименование маршрутов либо
изменение lifecycle требует миграции всех потребителей.
