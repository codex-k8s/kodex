---
doc_id: API-CK8S-FLEET-0001
type: api-contract
title: kodex — API-контракт fleet-manager
status: active
owner_role: SA
created_at: 2026-05-11
updated_at: 2026-05-11
related_issues: [699, 708]
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
- Версионирование: стабильный `v1` создан в контрактном срезе до реализации операций.
- Основные операции: fleet scopes, servers, Kubernetes clusters, связность/health, placement rules и `ResolvePlacement` по набору активных кластеров с учётом `runtime_mode` и `runtime_profile`.

## Спецификации

| Контракт | Источник правды |
|---|---|
| gRPC proto | `proto/kodex/fleet/v1/fleet_manager.proto` |
| AsyncAPI | `specs/asyncapi/fleet-manager.v1.yaml` |
| Go-контракты событий | `libs/go/platformevents/fleet/events.gen.go` |

Контрактный срез фиксирует стабильную поверхность `fleet-manager` до сервисной реализации. Реализация операций, БД и миграции идут отдельным срезом.

## Группы операций

### Fleet scopes

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `CreateFleetScope` | Создать логический контур размещения с типизированной ссылкой владельца. | Операторский контур, автоматизация платформы | `command_id`. |
| `UpdateFleetScope` | Обновить логический контур размещения. | Операторский контур, автоматизация платформы | `command_id + expected_version`. |
| `GetFleetScope` | Прочитать scope. | `runtime-manager`, операторский контур | Только чтение. |
| `ListFleetScopes` | Получить список по типу, владельцу и статусу. | Операторский контур | Только чтение. |
| `DisableFleetScope` | Запретить новые размещения в scope. | Операторский контур | `command_id + expected_version`. |

### Servers

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `RegisterServer` | Зарегистрировать сервер или внешнюю ссылку на хост. | Операторский контур | `command_id`. |
| `UpdateServer` | Обновить метаданные, регион, класс мощности или ссылки на секреты. | Операторский контур | `command_id + expected_version`. |
| `GetServer` | Прочитать сервер. | Операторский контур | Только чтение. |
| `ListServers` | Список серверов по статусу, региону и классу. | Операторский контур | Только чтение. |
| `DisableServer` | Запретить использование сервера для новых размещений. | Операторский контур | `command_id + expected_version`. |

### Kubernetes clusters

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `RegisterKubernetesCluster` | Зарегистрировать очередной кластер, default-признак внутри scope и ссылку на secret для доступа. | Операторский контур | `command_id`. |
| `UpdateKubernetesCluster` | Обновить статус, scope, default-признак, region, class или secret ref. | Операторский контур | `command_id + expected_version`. |
| `GetKubernetesCluster` | Прочитать кластер. | `runtime-manager`, операторский контур | Только чтение. |
| `ListKubernetesClusters` | Список кластеров по scope, статусу и health. | Операторский контур | Только чтение. |
| `DisableKubernetesCluster` | Запретить новые размещения в кластер. | Операторский контур | `command_id + expected_version`. |

### Связность и health

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `RunClusterConnectivityCheck` | Проверить доступность Kubernetes API. | Внутренний исполнитель, операторский контур | `command_id`. |
| `GetClusterHealthSnapshot` | Прочитать последний или конкретный snapshot. | Операторский контур, `runtime-manager` | Только чтение. |
| `ListClusterHealthSnapshots` | История snapshot по кластеру. | Операторский контур | Только чтение. |

### Placement

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `PutPlacementRule` | Создать или обновить правило выбора кластера внутри scope. | Операторский контур, автоматизация policy | `command_id + expected_version` при update. |
| `GetPlacementRule` | Прочитать правило. | Операторский контур | Только чтение. |
| `ListPlacementRules` | Список правил по scope и статусу. | Операторский контур | Только чтение. |
| `ResolvePlacement` | Вернуть `fleet_scope_id`, `cluster_id` и объяснение решения для runtime-запроса по `runtime_mode`, `runtime_profile`, ограничениям и требованиям. | `runtime-manager` | `command_id` или `request_fingerprint`. |
| `GetPlacementDecision` | Прочитать сохранённое решение. | `runtime-manager`, операторский контур | Только чтение. |
| `ListPlacementDecisions` | История решений по проекту, репозиторию, scope или cluster. | Операторский контур | Только чтение. |

## Ключи действий доступа

| Область | Ключи |
|---|---|
| Fleet scope | `fleet.scope.create`, `fleet.scope.update`, `fleet.scope.disable`, `fleet.scope.read`, `fleet.scope.list` |
| Server | `fleet.server.register`, `fleet.server.update`, `fleet.server.disable`, `fleet.server.read`, `fleet.server.list` |
| Kubernetes cluster | `fleet.cluster.register`, `fleet.cluster.update`, `fleet.cluster.disable`, `fleet.cluster.read`, `fleet.cluster.list` |
| Health | `fleet.health.check.run`, `fleet.health.read` |
| Placement | `fleet.placement_rule.put`, `fleet.placement_rule.read`, `fleet.placement_rule.list`, `fleet.placement.resolve`, `fleet.placement_decision.read`, `fleet.placement_decision.list` |

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
| `fleet.server.created` | Зарегистрирован сервер. |
| `fleet.server.updated` | Обновлён сервер. |
| `fleet.server.disabled` | Сервер отключён для новых размещений. |
| `fleet.cluster.created` | Зарегистрирован Kubernetes-кластер. |
| `fleet.cluster.updated` | Обновлён Kubernetes-кластер. |
| `fleet.cluster.disabled` | Кластер отключён для новых размещений. |
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
