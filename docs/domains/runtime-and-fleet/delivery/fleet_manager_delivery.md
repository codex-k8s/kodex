---
doc_id: DLV-CK8S-FLEET-MANAGER
type: delivery-plan
title: kodex — поставка fleet-manager
status: active
owner_role: EM
created_at: 2026-05-11
updated_at: 2026-05-12
related_issues: [699, 708, 714, 717, 726, 730]
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
| FLEET-2 | #714 | Сервисный каркас, конфигурация, PostgreSQL-модель, миграции, слой репозитория, health/readiness и outbox. |
| FLEET-3 | #717 | Команды и чтения реестра для нескольких fleet scope, серверов, Kubernetes-кластеров, bootstrap seed `platform-default` и базовые проверки доступа. |
| FLEET-4 | #726 | Проверки связности, health snapshots и события деградации для нескольких кластеров. |
| FLEET-5 | #730 | Правила размещения, `ResolvePlacement` по набору активных кластеров, журнал решений и интеграционный контракт для `runtime-manager`. |
| FLEET-6 | создать перед срезом | Dockerfile, манифесты, migration job, `services.yaml`, smoke-путь, runbook и monitoring. |

## Таблица реализации

| Группа | Контракт | Реализация |
|---|---|---|
| Сервисный процесс | Не отдельная gRPC-группа. | Реализовано в FLEET-2: конфигурация, gRPC runtime, health/readiness, metrics. |
| PostgreSQL и outbox | Не отдельная gRPC-группа. | Реализовано в FLEET-2: начальная схема БД, миграции, repository для readiness/outbox и локальная очередь событий. |
| Fleet scopes | Готов: `CreateFleetScope`, `UpdateFleetScope`, `DisableFleetScope`, `EnableFleetScope`, `GetFleetScope`, `ListFleetScopes`. | FLEET-3 реализует бизнес-команды, чтения, проверки доступа, идемпотентность, optimistic concurrency, command result и outbox-события. |
| Servers | Готов: `RegisterServer`, `UpdateServer`, `DisableServer`, `EnableServer`, `GetServer`, `ListServers`. | FLEET-3 реализует регистрацию, обновление, включение/отключение и чтения нескольких серверов без хранения значений секретов. |
| Kubernetes clusters | Готов: `RegisterKubernetesCluster`, `UpdateKubernetesCluster`, `DisableKubernetesCluster`, `EnableKubernetesCluster`, `GetKubernetesCluster`, `ListKubernetesClusters`. | FLEET-3 реализует реестр нескольких кластеров, default-кластер внутри scope и ссылки на secret без чтения значения секрета. |
| Связность и health | Готов: `RunClusterConnectivityCheck`, `GetClusterHealthSnapshot`, `ListClusterHealthSnapshots`, события `fleet.health.*`. | FLEET-4 реализует проверки Kubernetes API через отдельный checker, `secretresolver`, command result, outbox-события, latest health на cluster и историю snapshots без сохранения kubeconfig. |
| Placement | Готов: `PutPlacementRule`, `GetPlacementRule`, `ListPlacementRules`, `ResolvePlacement`, чтения решений и события `fleet.placement.*`. | Реализовано в FLEET-5: базовый выбор из набора активных кластеров, журнал решений, проверки доступа, идемпотентность и outbox-события. |
| Контур выкладки | Не gRPC-группа. | Запланировано в FLEET-6. |

## Зависимости и блокировки

| Домен или сервис | Связь | Статус |
|---|---|---|
| `runtime-manager` | Основной потребитель `ResolvePlacement`; после FLEET-5 runtime должен перейти с локального default config на fleet decision. | Сейчас не блокирует FLEET-0..FLEET-5. Интеграция нужна отдельным PR после готовности resolver. |
| `project-catalog` | Источник placement policy проекта, репозитория и сервиса. | Текущие проектные контракты достаточны для FLEET-5; дальнейшее расширение ограничений идёт отдельными междоменными PR. |
| `package-hub` | Источник runtime-требований для runtime-нагрузок пакетов и плагинов. | Не блокирует FLEET-0..FLEET-4; нужен контракт требований перед размещением runtime-нагрузки пакета. |
| `agent-manager` | Инициирует runtime через `runtime-manager`. | Не блокирует fleet kickoff; прямой fleet API для agent-manager не планируется в MVP. |
| `access-manager` | Проверка прав на управление fleet scope, серверами, кластерами, health и placement rules. | Ключи действий заведены в FLEET-1; проверки доступа registry-команд подключены в FLEET-3; health-команды и чтения подключены в FLEET-4; placement-поверхность подключается в FLEET-5. |
| Secret resolver/Vault/Kubernetes Secret клиент | Получение значения kubeconfig/service account по разрешённой ссылке. | FLEET-4 использует `secretresolver` и Kubernetes client-go только внутри проверки; значение секрета не сохраняется, не логируется и не попадает в события. |

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

## Рекомендуемый следующий шаг после FLEET-4

После FLEET-5 идти в отдельный интеграционный срез: переключить `runtime-manager` с локального bootstrap fallback на вызов `fleet-manager.ResolvePlacement`, не смешивая эту работу с deploy-контуром FLEET-6.

## Апрув

- request_id: `owner-2026-05-11-fleet-manager-kickoff`
- Решение: approved
- Комментарий: план поставки `fleet-manager` согласован как стартовое целевое состояние FLEET-0.
