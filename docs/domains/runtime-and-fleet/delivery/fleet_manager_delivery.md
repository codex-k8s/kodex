---
doc_id: DLV-CK8S-FLEET-MANAGER
type: delivery-plan
title: kodex — поставка fleet-manager
status: active
owner_role: EM
created_at: 2026-05-11
updated_at: 2026-05-11
related_issues: [699, 708]
related_prs: []
related_docsets:
  - docs/domains/runtime-and-fleet/product/fleet_manager_requirements.md
  - docs/domains/runtime-and-fleet/architecture/fleet_manager_design.md
  - docs/domains/runtime-and-fleet/architecture/fleet_manager_data_model.md
  - docs/domains/runtime-and-fleet/architecture/fleet_manager_api_contract.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-11-fleet-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-11
---

# Поставка fleet-manager

## TL;DR

`fleet-manager` поставляется малыми PR-срезами: сначала доменная документация, затем gRPC/AsyncAPI контракты, сервисный каркас и БД, реестр нескольких scope/server/cluster, связность и health, resolver размещения, интеграция с `runtime-manager`, контур выкладки и операционные документы.

Стартовый FLEET-0 не создаёт код. Он фиксирует границу: `runtime-manager` владеет слотами/jobs/workspace, а `fleet-manager` владеет серверами, Kubernetes-кластерами, связностью, health и placement scope.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования fleet-manager | `docs/domains/runtime-and-fleet/product/fleet_manager_requirements.md` |
| Дизайн fleet-manager | `docs/domains/runtime-and-fleet/architecture/fleet_manager_design.md` |
| Модель данных fleet-manager | `docs/domains/runtime-and-fleet/architecture/fleet_manager_data_model.md` |
| API-карта fleet-manager | `docs/domains/runtime-and-fleet/architecture/fleet_manager_api_contract.md` |
| Карта Issue домена | `docs/delivery/issue-map/domains/runtime-and-fleet.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---|---|
| FLEET-0 | #699 | Доменная документация, границы runtime/fleet, MVP с несколькими серверами, scope и кластерами, bootstrap seed `platform-default`, будущие контракты, план поставки и карта Issue. |
| FLEET-1 | #708 | gRPC и AsyncAPI контракты `fleet-manager`, события `fleet.*`, сгенерированные Go-контракты и ключи действий. |
| FLEET-2 | создать перед срезом | Сервисный каркас, конфигурация, PostgreSQL-модель, миграции, слой репозитория, health/readiness и outbox. |
| FLEET-3 | создать перед срезом | Команды и чтения реестра для нескольких fleet scope, серверов, Kubernetes-кластеров, bootstrap seed `platform-default` и базовые проверки доступа. |
| FLEET-4 | создать перед срезом | Проверки связности, health snapshots и события деградации для нескольких кластеров. |
| FLEET-5 | создать перед срезом | Правила размещения, `ResolvePlacement` по набору активных кластеров, журнал решений и интеграционный контракт для `runtime-manager`. |
| FLEET-6 | создать перед срезом | Dockerfile, манифесты, migration job, `services.yaml`, smoke-путь, runbook и monitoring. |

## Таблица реализации

| Группа | Контракт | Реализация |
|---|---|---|
| Fleet scopes | Готов: `CreateFleetScope`, `UpdateFleetScope`, `DisableFleetScope`, `GetFleetScope`, `ListFleetScopes`. | Реестр нескольких scope реализуется в FLEET-3. |
| Servers | Готов: `RegisterServer`, `UpdateServer`, `DisableServer`, `GetServer`, `ListServers`. | Реестр нескольких серверов реализуется в FLEET-3. |
| Kubernetes clusters | Готов: `RegisterKubernetesCluster`, `UpdateKubernetesCluster`, `DisableKubernetesCluster`, `GetKubernetesCluster`, `ListKubernetesClusters`. | Реестр нескольких кластеров реализуется в FLEET-3. |
| Связность и health | Готов: `RunClusterConnectivityCheck`, `GetClusterHealthSnapshot`, `ListClusterHealthSnapshots`, события `fleet.health.*`. | Проверки и snapshots реализуются в FLEET-4. |
| Placement | Готов: `PutPlacementRule`, `GetPlacementRule`, `ListPlacementRules`, `ResolvePlacement`, чтения решений и события `fleet.placement.*`. | Базовый выбор из набора активных кластеров реализуется в FLEET-5. |
| Контур выкладки | Не gRPC-группа. | Запланировано в FLEET-6. |

## Зависимости и блокировки

| Домен или сервис | Связь | Статус |
|---|---|---|
| `runtime-manager` | Основной потребитель `ResolvePlacement`; после FLEET-5 runtime должен перейти с локального default config на fleet decision. | Сейчас не блокирует FLEET-0..FLEET-4. Интеграция нужна в FLEET-5. |
| `project-catalog` | Источник placement policy проекта, репозитория и сервиса. | Текущие проектные контракты достаточны для kickoff; точную форму ограничений подтвердить перед FLEET-5. |
| `package-hub` | Источник runtime-требований для runtime-нагрузок пакетов и плагинов. | Не блокирует FLEET-0..FLEET-4; нужен контракт требований перед размещением runtime-нагрузки пакета. |
| `agent-manager` | Инициирует runtime через `runtime-manager`. | Не блокирует fleet kickoff; прямой fleet API для agent-manager не планируется в MVP. |
| `access-manager` | Проверка прав на управление fleet scope, серверами, кластерами и placement rules. | Ключи действий заведены в FLEET-1; проверка доступа подключается в FLEET-3. |
| Secret resolver/Vault/Kubernetes Secret клиент | Получение значения kubeconfig/service account по разрешённой ссылке. | Не нужен для FLEET-0/FLEET-1; нужен до регистрации реальных кластеров и connectivity checks в FLEET-3/FLEET-4. |

## Критерии начала кода

- Принят FLEET-0 docset.
- Для кодового PR заведён отдельный GitHub Issue.
- Контрактный PR создаёт proto и AsyncAPI до реализации операций.
- Старый код из `deprecated/**` не используется как основа реализации.
- В PR, который закрывает Issue, тело содержит `Closes #...`.

## Критерии завершения fleet-manager MVP

- `fleet-manager` имеет собственную БД, миграции, контракты, события и deploy-контур.
- Несколько fleet scope, server и Kubernetes cluster имеют авторитетные команды и чтения уже в MVP.
- Bootstrap `platform-default` описан как данные fleet и fallback, а не скрытая особенность `runtime-manager` или ограничение MVP.
- `runtime-manager` может получать placement decision из набора активных кластеров и не выбирает cluster самостоятельно.
- `package-hub` и `project-catalog` могут передавать ограничения и требования без владения fleet-состоянием.
- Полные kubeconfig, Kubernetes objects, events и logs не хранятся в PostgreSQL `fleet-manager`.

## После MVP

За пределами MVP остаются:

- автоматический SSH bootstrap сервера;
- установка Kubernetes;
- join-node;
- cluster upgrade;
- разрушительные lifecycle-операции для серверов и кластеров;
- расширенная автоматизация capacity/rebalancing.

## Рекомендуемый следующий шаг после FLEET-1

Идти в FLEET-2: создать сервисный каркас, конфигурацию, PostgreSQL-модель, миграции, слой репозитория, health/readiness и outbox. Команды registry, connectivity checks и placement resolver остаются для следующих срезов.

## Апрув

- request_id: `owner-2026-05-11-fleet-manager-kickoff`
- Решение: approved
- Комментарий: план поставки `fleet-manager` согласован как стартовое целевое состояние FLEET-0.
