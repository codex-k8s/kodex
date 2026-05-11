---
doc_id: PRD-CK8S-FLEET-0001
type: prd
title: kodex — требования fleet-manager
status: active
owner_role: PM
created_at: 2026-05-11
updated_at: 2026-05-11
related_issues: [699, 708]
related_prs: []
related_docsets:
  - docs/domains/runtime-and-fleet/product/requirements.md
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

# PRD: fleet-manager

## TL;DR

- Что строим: `fleet-manager` как сервис-владелец серверов, Kubernetes-кластеров, связности, health и placement scope.
- Для кого: `runtime-manager`, `project-catalog`, `package-hub`, будущий `agent-manager`, оператор платформы и операционные экраны.
- Почему: runtime не должен владеть реестром серверов и кластеров, а размещение runtime-нагрузок не должно жить в конфигурации одного сервиса.
- MVP: реестр для нескольких fleet scope, серверов и Kubernetes-кластеров; bootstrap создаёт `platform-default` scope/cluster только как seed для одиночной установки.
- Критерии успеха: runtime получает готовое решение размещения из набора активных кластеров, оператор видит состояние кластеров, а одиночная установка остаётся частным случаем общей модели нескольких кластеров.

## Проблема и цель

Проблема:

- `runtime-manager` уже умеет работать с `fleet_scope_id` и `cluster_id`, но не должен становиться владельцем серверов, kubeconfig и состояния кластеров;
- default-кластер в конфигурации удобен для bootstrap, но опасен как долгосрочная архитектурная истина;
- `project-catalog` хранит placement policy, но не должен проверять доступность Kubernetes-кластера;
- `package-hub` описывает требования пакетов и плагинов, но не должен выбирать инфраструктурный контур;
- оператору нужно видеть связность, health и деградацию инфраструктуры без прямого чтения Kubernetes.

Цель:

- выделить сервис-владелец инфраструктурного контура;
- описать модель fleet scope -> server -> cluster -> placement decision;
- дать `runtime-manager` детерминированный способ получить размещение;
- сохранить `platform-default` как bootstrap seed/fallback, а не как ограничение MVP;
- зафиксировать будущие контракты без преждевременной реализации лишнего.

## Пользователи и роли

| Роль | Главный сценарий |
|---|---|
| Оператор платформы | Видит серверы, кластеры, состояние связности, health и причины недоступности размещения. |
| `runtime-manager` | Запрашивает placement decision и исполняет слоты/jobs на выбранном `fleet_scope_id` и `cluster_id`. |
| `project-catalog` | Передаёт проверенную placement policy проекта и репозитория как вход в runtime/fleet-контур. |
| `package-hub` | Даёт требования пакета или плагина к runtime-нагрузке, но не выбирает кластер. |
| `agent-manager` | Запрашивает runtime через `runtime-manager`; обычно не ходит в `fleet-manager` напрямую. |
| `operations-hub` | Строит операторские проекции по fleet health и событиям деградации. |

## Функциональные требования

| ID | Требование | Приоритет |
|---|---|---|
| FLT-FR-1 | `fleet-manager` должен хранить fleet scope как логический контур размещения. | Обязательно |
| FLT-FR-2 | В MVP `fleet-manager` должен хранить несколько серверов, если кластеры управляются платформой или привязаны к конкретным хостам. | Обязательно |
| FLT-FR-3 | В MVP `fleet-manager` должен хранить несколько Kubernetes-кластеров как отдельные агрегаты с состоянием связности и health. | Обязательно |
| FLT-FR-4 | В MVP API и БД должны поддерживать несколько fleet scope и несколько cluster; `platform-default` scope/cluster создаётся bootstrap-ом только как seed/fallback. | Обязательно |
| FLT-FR-5 | Сырые kubeconfig, токены и ключи не должны храниться в БД `fleet-manager`; хранится только ссылка на secret. | Обязательно |
| FLT-FR-6 | `fleet-manager` должен хранить результаты проверок связности и health без полного копирования состояния Kubernetes. | Обязательно |
| FLT-FR-7 | `fleet-manager` должен уметь вернуть placement decision для runtime-запроса. | Обязательно |
| FLT-FR-8 | Placement decision должен быть детерминированным по входу, версии правил и состоянию health на момент решения. | Обязательно |
| FLT-FR-9 | `fleet-manager` должен публиковать события `fleet.*` через outbox и `platform-event-log`. | Обязательно |
| FLT-FR-10 | `fleet-manager` не должен запускать слоты, jobs, runtime-нагрузки пакетов или агентные запуски. | Обязательно |
| FLT-FR-11 | Сервис должен давать оператору объяснимую причину отказа размещения. | Обязательно |
| FLT-FR-12 | Модель MVP должна поддерживать dedicated clusters для организаций, проектов и отдельных тяжёлых репозиториев как данные реестра, без автоматического provisioning. | Обязательно |

## Критерии приёмки

| ID | Критерий |
|---|---|
| FLT-AC-1 | Если `runtime-manager` запрашивает размещение без явного cluster ref, fleet выбирает подходящий активный кластер из реестра и фиксирует решение. |
| FLT-AC-2 | Если подходящего активного кластера нет, fleet возвращает отказ с причиной, а runtime не создаёт слот вслепую. |
| FLT-AC-3 | Если у проекта есть ограничения размещения, fleet применяет их к доступным scope/cluster и возвращает выбранный контур или объяснимый отказ. |
| FLT-AC-4 | Если кластер помечен как `draining` или `suspended`, новые runtime-размещения туда не выдаются. |
| FLT-AC-5 | Если health-check падает, оператор видит snapshot, короткую ошибку и событие `fleet.health.degraded`. |
| FLT-AC-6 | Если создаётся service scope, он ссылается на `project_id`, необязательный `repository_id` и `service_key`, а не на `ServiceDescriptor.id`. |
| FLT-AC-7 | Bootstrap создаёт `platform-default` scope/cluster для одиночной установки, но оператор может зарегистрировать дополнительные серверы, scope и кластеры уже в MVP. |

## Что не входит

- Не владеть slot lifecycle, workspace materialization, job status и runtime artifact refs.
- Не запускать Kubernetes Jobs, Pods или runtime-нагрузки пакетов.
- Не владеть `Run`, flow, ролями, prompt и агентными сессиями.
- Не владеть проектной policy, `services.yaml`, branch rules и release policy.
- Не хранить полный список Kubernetes objects, events и logs как собственную истину.
- Не выполнять автоматический SSH bootstrap, установку Kubernetes, join-node и upgrade cluster в MVP.
- Не выполнять разрушительные lifecycle-операции для серверов и кластеров в MVP.
- Не реализовывать UI и gateway в стартовом срезе.

## Нефункциональные требования

| ID | Категория | Требование |
|---|---|---|
| FLT-NFR-1 | Надёжность | Команды должны быть идемпотентны, а изменяемые агрегаты должны иметь версии. |
| FLT-NFR-2 | Безопасность | Секреты кластера хранятся только по ссылке на secret store; значения не попадают в БД и логи. |
| FLT-NFR-3 | Наблюдаемость | Cluster connectivity, health, placement decisions и ошибки должны иметь структурированные логи, метрики и события. |
| FLT-NFR-4 | Масштабирование | Проверки health и связности должны иметь настраиваемый параллелизм и таймауты. |
| FLT-NFR-5 | Совместимость | `platform-default` seed не должен становиться особым режимом: одиночная установка остаётся частным случаем реестра нескольких кластеров. |

## Зависимости

| Зависимость | Зачем нужна |
|---|---|
| `runtime-manager` | Основной потребитель placement decision и владелец исполнения на выбранном cluster ref. |
| `project-catalog` | Источник проверенной placement policy проекта, репозитория и сервиса. |
| `package-hub` | Источник требований пакета или плагина к runtime-нагрузке. |
| `agent-manager` | Будущий инициатор runtime-запросов через `runtime-manager`. |
| `access-manager` | Проверка прав на управление fleet scope, серверами, кластерами и политиками размещения. |
| `operations-hub` | Операторские проекции по health, связности и ошибкам размещения. |

## Апрув

- request_id: `owner-2026-05-11-fleet-manager-kickoff`
- Решение: approved
- Комментарий: требования `fleet-manager` согласованы как стартовое целевое состояние FLEET-0.
