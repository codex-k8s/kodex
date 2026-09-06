---
id: OPS-DOC-1089
title: Восстановление результата очистки credential провайдера
type: operational-guide
status: approved
owner: secret-broker
version: 1.0.0
updated: 2026-09-06
---

# Восстановление результата очистки credential провайдера

Refs #1068, #1089; существующий PR #1069. Исправление сохраняет исходный
`producedCredential` после смены lease generation, successor task, очистки
обнаруженной замены и перезапуска broker. Source prerequisite — Proto commit
`5781d3aa679dc106a50693b415d5ffe1947e1d3f`, перенесённый как
`49d75471abace9a0feaf32f016c1b9a0501d2fb0`. Это не подтверждение готовности
всего CP producer: его consolidated проверки выполняются отдельно.

## Сквозной путь и полномочия

Owner CP назначает текущий task, lease generation, immutable target и origin
первой attempt. Worker вызывает `CleanupProviderCredential` с текущим grant;
canonical request digest включает отдельный `recovery_identity`. Проверенный
transport/authority context остаётся источником допуска. gRPC mapper валидирует
origin до service/store; отсутствие bearer и изменение подписанного origin
закрывают вызов до handler.

Service читает current и origin receipts из Kubernetes, сверяет найденный
descriptor, затем ограниченно читает legacy поколения при отсутствии origin.
Старый digest с task/generation сохранён: каждый legacy ключ вычисляется
прежним алгоритмом для того же exact target. Новые tasks имеют
`legacy_last_generation = 0`; мигрированные — origin generation 1 и верхнюю
границу не более 32. После эффекта запись origin предшествует current receipt.
Ответ текущему worker содержит current receipt и первоначальный descriptor;
CP назначает отдельную cleanup task для обнаруженной замены. Broker не
создаёт доменное событие: авторитетный результат доступен через защищённый
повтор того же RPC и устойчивые receipts, а owner завершает свой task.

## Матрица переходов

| Сценарий | Результат |
| --- | --- |
| Позднее поколение / successor того же target | Исходный descriptor восстанавливается под origin и current |
| Замена уже очищена, broker перезапущен | Receipt сохраняет первоначальный descriptor |
| Legacy поколения с разными descriptors | Закрытый отказ без новой записи |
| Другой immutable target | Старый digest не подходит; origin назначает CP |
| Валидный pending snapshot изменился до fence, Kubernetes CAS conflict | `FailedPrecondition`, один `ErrorInfo`, domain `kodex.provider_credential_cleanup`, reason `CAS_SNAPSHOT_CHANGED`, пустая metadata |
| Fence записан, ответ потерян | Общая ошибка без no-effect proof; следующий exact readback восстанавливает результат |
| Corrupt/foreign object, generic failure | Нет доказательства отсутствия эффекта |
| Изменён origin/legacy range или нет authority | Отказ до handler |

## Локальные проверки и ручной путь

На итоговом production/test коде до этого документа и итогового commit:
`go test -p 1 -race ./...`, `go vet -p 1 ./...`, `go build -p 1 ./...` — PASS.
Полный race log: `/home/s/.k1045/broker-origin-full-race.log`;
providercredential 6.224 s, gRPC 1.452 s. Bootstrap и render scripts,
составляющие оставшуюся часть `test-secret-broker-drafts`, — PASS.
`make lint-proto build-proto check-proto-codegen` — PASS, canonical replay
не изменил generated source; log `/home/s/.k1045/broker-origin-proto.log`.
Первые scoped race трёх пакетов также PASS; root передал первоначальный CAS
patch как непроверенный WIP, и это не было прежним PASS.

В disposable проверке выполнить публичный `make test-secret-broker-drafts`.
Тесты используют fake Kubernetes API и настоящий store/service/transport;
они проверяют запись fence перед потерянным ACK, восстановление после
удаления replacement, successor chains, конфликт legacy и signed authority.
Действующий Kubernetes, staging/production, общий review и полная acceptance
#1031 — NOT RUN этим companion. Секреты и реальные credential не выводились.

Rollback к прежнему consumer после начала передачи `recovery_identity`
недопустим без согласованного owner protocol: он потеряет устойчивый origin.
Не удалять immutable receipts/tombstones для повторения операции.

Проверена актуальная документация Context7 `/websites/kubernetes_io`:
[resourceVersion и конфликт обновления](https://kubernetes.io/docs/reference/using-api/),
[immutable Secret](https://kubernetes.io/docs/reference/kubernetes-api/config-and-storage-resources/secret-v1/).
