---
id: LIB-KUBERNETES-ADMISSION-001
title: Проверка наблюдаемого admission gate Kubernetes
type: guide
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-25
---

# Проверка наблюдаемого admission gate Kubernetes

`Check` обслуживает общую read-only проверку policy/binding у publisher adapters.
Первый потребитель — mailbox publication, второй — integration egress publication.
Библиотека не устанавливает policy, не публикует ресурсы и не выдаёт полномочий.

API принимает context, готовый typed admissionregistration/v1 client, точное имя,
функцию ожидаемых ресурсов и три фиксированные ошибки вызывающего в `Failures`.
Порядок: GET policy, GET binding, source, JSON decode, identity/version/generation/
type-checking/spec checks. Ошибка первого GET останавливает второй; ошибка чтения
не вызывает source. Неполная конфигурация отклоняется до чтения.

Policy требует UID/resourceVersion, положительное и наблюдаемое generation,
непустой type-checking status без warnings. Binding требует UID/resourceVersion.
Оба spec сравниваются через `SameJSON`, сохраняя прежние nil/empty различия.
Ошибка чтения, source или состояния возвращает точную настроенную sentinel error;
внешние ошибки и тела ресурсов не отражаются. Это проверка двух последовательных
наблюдений, а не атомарный snapshot, proof фактического admission каждого запроса
или защита от изменения policy после чтения.

Клиент и его TLS/RBAC, namespace guard, закрытый реестр name/source и бюджет
context принадлежат вызывающему. Библиотека не создаёт client, retry, deadline,
goroutine, worker, lease, audit/event или cleanup. Отдельных observability hooks
нет: это синхронный шаг существующей операции и её прежней error boundary.
Не передавать credentials или Secret objects как expected resources.

Проверка: при отключённом workspace выполнить `go test -count=1 ./...` и
`go build ./...` из каталога модуля. Unit-тесты используют synthetic fake client,
не kubeconfig или живой кластер. Общая точка `scripts/test-go-modules.sh`
обнаруживает module автоматически. Смена wire/spec/ошибок требует совместимого
изменения обоих callers; автоматического исправления или миграции policy нет.
