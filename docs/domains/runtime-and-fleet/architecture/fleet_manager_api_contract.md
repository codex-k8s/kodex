---
doc_id: API-CK8S-FLEET-0001
type: api-contract
title: kodex — API-контракт fleet-manager
status: active
owner_role: SA
created_at: 2026-05-11
updated_at: 2026-05-11
related_issues: [699]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-11-fleet-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-11
---

# API-контракт: fleet-manager

## TL;DR

- Тип API: внутренний gRPC для команд и чтений, AsyncAPI для `fleet.*` событий.
- Аутентификация: внутренний сервисный контур; изменяющие команды принимают `CommandMeta` и проверяются через `access-manager`.
- Версионирование: стабильный `v1` создаётся в контрактном срезе до реализации операций.
- Основные операции: fleet scopes, servers, Kubernetes clusters, связность/health, placement rules и `ResolvePlacement` по набору активных кластеров.

## Спецификации

| Контракт | Будущий источник правды |
|---|---|
| gRPC proto | `proto/kodex/fleet/v1/fleet_manager.proto` |
| AsyncAPI | `specs/asyncapi/fleet-manager.v1.yaml` |
| Go-контракты событий | `libs/go/platformevents/fleet/events.gen.go` |

В FLEET-0 эти файлы ещё не создаются. Документ фиксирует будущий контракт, чтобы FLEET-1 не проектировал API с нуля и не смешивал fleet с runtime.

## Группы операций

### Fleet scopes

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `CreateOrUpdateFleetScope` | Создать или обновить логический контур размещения с типизированной ссылкой владельца. | Операторский контур, автоматизация платформы | `command_id + expected_version` при update. |
| `GetFleetScope` | Прочитать scope. | `runtime-manager`, операторский контур | Только чтение. |
| `ListFleetScopes` | Получить список по типу, владельцу и статусу. | Операторский контур | Только чтение. |
| `SuspendFleetScope` | Запретить новые размещения в scope. | Операторский контур | `command_id + expected_version`. |

### Servers

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `RegisterServer` | Зарегистрировать сервер или внешнюю ссылку на хост. | Операторский контур | `command_id`. |
| `UpdateServer` | Обновить метаданные, регион, класс мощности или ссылки на секреты. | Операторский контур | `command_id + expected_version`. |
| `GetServer` | Прочитать сервер. | Операторский контур | Только чтение. |
| `ListServers` | Список серверов по статусу, региону и классу. | Операторский контур | Только чтение. |
| `SuspendServer` | Запретить использование сервера для новых размещений. | Операторский контур | `command_id + expected_version`. |

### Kubernetes clusters

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `RegisterKubernetesCluster` | Зарегистрировать очередной кластер, default-признак внутри scope и ссылку на secret для доступа. | Операторский контур | `command_id`. |
| `UpdateKubernetesCluster` | Обновить статус, scope, default-признак, region, class или secret ref. | Операторский контур | `command_id + expected_version`. |
| `GetKubernetesCluster` | Прочитать кластер. | `runtime-manager`, операторский контур | Только чтение. |
| `ListKubernetesClusters` | Список кластеров по scope, статусу и health. | Операторский контур | Только чтение. |
| `SuspendKubernetesCluster` | Запретить новые размещения в кластер. | Операторский контур | `command_id + expected_version`. |

### Связность и health

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `RunClusterConnectivityCheck` | Проверить доступность Kubernetes API. | Внутренний исполнитель, операторский контур | `command_id`. |
| `RecordClusterHealthSnapshot` | Записать ограниченный health snapshot после проверки. | Внутренний исполнитель, fleet controller | `command_id`. |
| `GetClusterHealthSnapshot` | Прочитать последний или конкретный snapshot. | Операторский контур, `runtime-manager` | Только чтение. |
| `ListClusterHealthSnapshots` | История snapshot по кластеру. | Операторский контур | Только чтение. |

### Placement

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `PutPlacementRule` | Создать или обновить правило выбора кластера внутри scope. | Операторский контур, автоматизация policy | `command_id + expected_version` при update. |
| `GetPlacementRule` | Прочитать правило. | Операторский контур | Только чтение. |
| `ListPlacementRules` | Список правил по scope и статусу. | Операторский контур | Только чтение. |
| `ResolvePlacement` | Вернуть `fleet_scope_id`, `cluster_id` и объяснение решения для runtime-запроса. | `runtime-manager` | `command_id` или `request_fingerprint`. |
| `GetPlacementDecision` | Прочитать сохранённое решение. | `runtime-manager`, операторский контур | Только чтение. |
| `ListPlacementDecisions` | История решений по проекту, репозиторию, scope или cluster. | Операторский контур | Только чтение. |

## MVP и отложенный объём

| Область | MVP | После MVP |
|---|---|---|
| Fleet scope | Несколько scope, включая bootstrap seed `platform-default`. | Автоматическое создание scope из внешних provisioning-сценариев. |
| Server | Несколько server-записей с метаданными и ссылками на секреты. | Автоматический SSH bootstrap, установка Kubernetes и join-node. |
| Cluster | Несколько Kubernetes-кластеров, один default-кластер внутри scope только как fallback. | Cluster upgrade, разрушительные lifecycle-операции и расширенное обслуживание. |
| Health | Проверка связности + ограниченный health snapshot по каждому cluster. | Прогноз ёмкости, quota policy, автоматический rebalancing и capacity automation. |
| Placement | Выбор активного cluster из реестра по ограничениям, health и default fallback. | Взвешенный выбор, размещение с учётом стоимости и риска, multi-region. |
| Runtime integration | `runtime-manager` вызывает `ResolvePlacement` и получает fleet decision. | Дальнейшие улучшения идут через развитие placement policy. |

## Отложенные операции после MVP

| Операция | Почему не входит в MVP |
|---|---|
| `BootstrapServerOverSsh` | Автоматический SSH bootstrap сервера отложен; в MVP оператор регистрирует уже доступный контур и ссылки на секреты. |
| `InstallKubernetes` | Установка Kubernetes отложена; MVP не становится инсталлятором кластера. |
| `JoinNode` | Join-node автоматизация отложена; реестр уже может хранить несколько серверов и кластеров. |
| `UpgradeKubernetesCluster` | Upgrade cluster требует отдельной операционной политики и rollback-плана. |
| `DecommissionKubernetesCluster` | Разрушительная lifecycle-операция требует отдельной политики миграции runtime-нагрузок и owner approval. |

## Модель ошибок

| Код | Смысл |
|---|---|
| `FLEET_SCOPE_NOT_FOUND` | Fleet scope не найден или недоступен. |
| `FLEET_CLUSTER_NOT_FOUND` | Кластер не найден или недоступен. |
| `FLEET_CLUSTER_UNAVAILABLE` | Кластер не подходит для новых размещений. |
| `FLEET_CONNECTIVITY_FAILED` | Проверка связности с Kubernetes API упала. |
| `FLEET_HEALTH_DEGRADED` | Кластер доступен, но health не позволяет безопасное размещение. |
| `FLEET_PLACEMENT_REJECTED` | Не найден подходящий контур размещения. |
| `FLEET_PERMISSION_DENIED` | Действие запрещено policy или `access-manager`. |
| `FLEET_SECRET_REF_INVALID` | Ссылка на secret отсутствует или недоступна по metadata-check. |

## События

| Событие | Когда публикуется |
|---|---|
| `fleet.scope.created` | Создан fleet scope. |
| `fleet.scope.updated` | Обновлён fleet scope. |
| `fleet.server.registered` | Зарегистрирован сервер. |
| `fleet.server.updated` | Обновлён сервер. |
| `fleet.server.suspended` | Сервер приостановлен. |
| `fleet.cluster.registered` | Зарегистрирован Kubernetes-кластер. |
| `fleet.cluster.updated` | Обновлён Kubernetes-кластер. |
| `fleet.cluster.suspended` | Кластер приостановлен для новых размещений. |
| `fleet.cluster.decommissioned` | Кластер выведен из эксплуатации; событие относится к разрушительному lifecycle после MVP. |
| `fleet.health.checked` | Health check завершён. |
| `fleet.health.degraded` | Health перешёл в degraded/unhealthy. |
| `fleet.placement.resolved` | Placement успешно разрешён. |
| `fleet.placement.rejected` | Placement отклонён с причиной. |

## Совместимость

- Контракты `v1` должны покрыть согласованный объём fleet API, даже если реализация пойдёт несколькими срезами.
- Если контракт опережает код, документ поставки содержит таблицу реализованного и отложенного объёма.
- Bootstrap `platform-default` должен быть частью данных и API, а не временной особенностью только runtime config.
- API регистрации, чтения, health и placement должен быть рассчитан на несколько scope, серверов и кластеров уже в MVP.
- `fleet-manager` не публикует OpenAPI напрямую; внешние HTTP-сценарии позже идут через подходящий gateway.

## Апрув

- request_id: `owner-2026-05-11-fleet-manager-kickoff`
- Решение: approved
- Комментарий: API-карта `fleet-manager` согласована как стартовое целевое состояние FLEET-0.
