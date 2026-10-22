---
id: GO-LIB-PROVIDER-FINANCE-WIRE-001
title: Типизированная проекция финансовых наблюдений
type: library-readme
status: approved
owner: backend
version: 1.0.0
updated: 2026-10-22
---

# Типизированная проекция финансовых наблюдений

Модуль содержит единственную реализацию перевода
runtimecontract.ProviderFinancialObservation в control-plane protobuf.
Два production-потребителя — callback runtime-controller и финансовые
read-проекции control-plane. Их прежние одинаковые функции удалены.
Существующий wire/storage контракт, поля и порядок проверок не меняются.

ObservationToProto сохраняет sequence/state/restriction, порядок окон,
необязательные scalar pointers и точный десятичный текст balance без float,
округления, вывода о наличии денег либо возможности продолжать запуск.
Отсутствие credits отличается от присутствующего пустого объекта, false —
от неизвестного значения. Пустой список окон даёт nil, как прежние адаптеры.
Создаются новые protobuf containers; scalar pointers намеренно разделяются
с input. Caller не должен изменять значения конкурентно с чтением результата.

Библиотека не проверяет observations, не назначает actor/account/lease,
received time, eligibility или approval, не выдаёт grants. Проверки входа,
owner scope и последующие RPC/транзакции остаются в сервисах. Нет I/O, env,
goroutines, context lifecycle, retries, логирования либо telemetry hooks.
Нет отдельной readiness/deploy-конфигурации: библиотеку собирают потребители.
Обратный перевод, необходимый лишь CP transport, остаётся в этом unit.

Зависимости замкнуты на runtimecontract и controlplaneapi; оба не импортируют
эту библиотеку. Общий controlplaneclient не получает новую зависимость,
неиспользующие финансовую проекцию сервисы не подключают новый модуль.
Любое будущее изменение wire должно выполняться отдельно согласованно
с producer/consumer; этот вынос не добавляет compatibility decoder.

Проверка: go test -count=1 ./... и go build ./... из каталога модуля.
