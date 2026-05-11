---
doc_id: DM-CK8S-FLEET-0001
type: data-model
title: kodex — модель данных fleet-manager
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

# Модель данных: fleet-manager

## TL;DR

- Ключевые сущности: `FleetScope`, `Server`, `KubernetesCluster`, `ClusterConnectivityCheck`, `ClusterHealthSnapshot`, `PlacementRule`, `PlacementDecision` и локальный outbox.
- Все ссылки на организации, проекты, репозитории, пакеты и runtime хранятся как внешние идентификаторы без SQL-связей с чужими БД.
- Секреты, kubeconfig, полное состояние Kubernetes, events и логи не хранятся в БД `fleet-manager`.

## Базовые правила

- БД `fleet-manager` принадлежит только `fleet-manager`.
- Таблицы не имеют `FOREIGN KEY` в БД других сервисов.
- Состояние меняется короткими транзакциями с версией агрегата.
- Долгие проверки Kubernetes выполняются вне SQL-блокировки и сохраняют результат отдельной командой.
- События записываются в локальный outbox и доставляются в `platform-event-log`.
- Сырые секреты в БД не хранятся.

## Сущности

### `FleetScope`

Назначение: логический контур размещения, в котором fleet выбирает один или несколько кластеров.

Важные инварианты:

- MVP default cluster представлен через scope, а не через скрытую настройку runtime;
- scope может быть platform-wide, организационным, проектным или репозиторным;
- scope не является владельцем проекта или организации, а только хранит внешние ссылки.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор scope. |
| `scope_key` | text | no | unique | Читаемый ключ, например `platform-default`. |
| `scope_type` | text | no | indexed | `platform`, `organization`, `project`, `repository`, `service`. |
| `scope_owner_id` | UUID | yes | indexed | Внешняя ссылка на организацию, проект, репозиторий или сервис. |
| `display_name` | text | no |  | Название для оператора. |
| `status` | text | no | indexed | `active`, `suspended`, `draining`, `archived`. |
| `is_default` | boolean | no | indexed | Используется как default path MVP. |
| `created_at` | timestamptz | no | indexed | Создание. |
| `updated_at` | timestamptz | no | indexed | Обновление. |
| `version` | bigint | no | monotonic | Оптимистичная конкуренция. |

### `Server`

Назначение: управляемый или внешний сервер, который может быть связан с Kubernetes-кластером.

Важные инварианты:

- не каждый внешний Kubernetes-кластер обязан иметь отдельный server;
- SSH-ключи и root-доступы не хранятся в БД;
- server health не заменяет cluster health.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор сервера. |
| `server_key` | text | no | unique | Читаемый ключ. |
| `provider_type` | text | no | indexed | `bare_metal`, `vps`, `cloud`, `managed`, `unknown`. |
| `status` | text | no | indexed | `active`, `suspended`, `draining`, `decommissioned`. |
| `primary_address_ref` | text | no | default '' | Безопасная ссылка или hostname без секрета. |
| `region` | text | no | default '' | Регион или зона. |
| `capacity_class` | text | no | default '' | Класс мощности для placement. |
| `secret_store_type` | text | no | default '' | Тип хранилища секрета для доступа к серверу. |
| `secret_store_ref` | text | no | default '' | Ссылка на секрет без значения. |
| `created_at` | timestamptz | no | indexed | Создание. |
| `updated_at` | timestamptz | no | indexed | Обновление. |
| `version` | bigint | no | monotonic | Версия. |

### `KubernetesCluster`

Назначение: Kubernetes-кластер, доступный для runtime-размещения.

Важные инварианты:

- cluster принадлежит одному fleet scope как основному контуру размещения;
- kubeconfig и токены хранятся только по ссылке на secret;
- статус `draining` запрещает новые размещения, но не обязан немедленно останавливать существующие runtime-объекты.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор кластера. |
| `fleet_scope_id` | UUID | no | indexed | Внешне видимый scope внутри БД fleet. |
| `server_id` | UUID | yes | indexed | Связанный server, если есть. |
| `cluster_key` | text | no | unique | Читаемый ключ. |
| `status` | text | no | indexed | `active`, `suspended`, `draining`, `unreachable`, `decommissioned`. |
| `api_endpoint_ref` | text | no | default '' | Безопасная ссылка на endpoint или hostname. |
| `secret_store_type` | text | no | default '' | Тип хранилища секрета kubeconfig/service account. |
| `secret_store_ref` | text | no | default '' | Ссылка на секрет без значения. |
| `kubernetes_version` | text | no | default '' | Последняя известная версия. |
| `region` | text | no | default '' | Регион или зона. |
| `capacity_class` | text | no | default '' | Класс мощности. |
| `last_health_status` | text | no | default '' | `unknown`, `healthy`, `degraded`, `unhealthy`. |
| `last_health_checked_at` | timestamptz | yes | indexed | Последняя проверка. |
| `created_at` | timestamptz | no | indexed | Создание. |
| `updated_at` | timestamptz | no | indexed | Обновление. |
| `version` | bigint | no | monotonic | Версия. |

### `ClusterConnectivityCheck`

Назначение: попытка проверки связности с Kubernetes API.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор проверки. |
| `cluster_id` | UUID | no | indexed | Кластер. |
| `status` | text | no | indexed | `pending`, `running`, `succeeded`, `failed`, `timed_out`. |
| `started_at` | timestamptz | yes |  | Начало. |
| `finished_at` | timestamptz | yes |  | Завершение. |
| `latency_ms` | bigint | yes |  | Время ответа API server. |
| `error_code` | text | no | default '' | Классификация ошибки. |
| `error_message` | text | no | default '' | Короткая ошибка без секрета. |
| `created_at` | timestamptz | no | indexed | Создание. |

### `ClusterHealthSnapshot`

Назначение: ограниченный снимок состояния кластера для оператора и placement.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор snapshot. |
| `cluster_id` | UUID | no | indexed | Кластер. |
| `health_status` | text | no | indexed | `healthy`, `degraded`, `unhealthy`, `unknown`. |
| `capacity_status` | text | no | indexed | `ok`, `limited`, `exhausted`, `unknown`. |
| `summary_json` | jsonb | no | default {} | Ограниченная сводка: nodes, allocatable, quotas, pressure flags. |
| `checked_at` | timestamptz | no | indexed | Время проверки. |
| `error_code` | text | no | default '' | Ошибка, если есть. |
| `error_message` | text | no | default '' | Короткое сообщение. |

### `PlacementRule`

Назначение: правило выбора кластера внутри fleet scope.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор правила. |
| `fleet_scope_id` | UUID | no | indexed | Область действия. |
| `rule_key` | text | no | unique with scope | Читаемый ключ. |
| `status` | text | no | indexed | `active`, `disabled`, `archived`. |
| `priority` | bigint | no | indexed | Порядок применения. |
| `match_json` | jsonb | no | default {} | Условия: project/repository/service/package/runtime profile. |
| `constraints_json` | jsonb | no | default {} | Требования: region, class, labels, isolation, health. |
| `created_at` | timestamptz | no | indexed | Создание. |
| `updated_at` | timestamptz | no | indexed | Обновление. |
| `version` | bigint | no | monotonic | Версия. |

### `PlacementDecision`

Назначение: сохранённое решение размещения для диагностики и повторяемости.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор решения. |
| `command_id` | UUID | yes | unique when present | Идемпотентность команды. |
| `request_fingerprint` | text | no | indexed | Отпечаток входа. |
| `status` | text | no | indexed | `resolved`, `rejected`. |
| `fleet_scope_id` | UUID | yes | indexed | Выбранный scope. |
| `cluster_id` | UUID | yes | indexed | Выбранный cluster. |
| `project_id` | UUID | yes | indexed | Внешний project ref. |
| `repository_id` | UUID | yes | indexed | Внешний repository ref. |
| `runtime_profile` | text | no | default '' | Профиль runtime. |
| `input_json` | jsonb | no | default {} | Ограниченный вход без секретов. |
| `reason_code` | text | no | default '' | Причина выбора или отказа. |
| `reason_message` | text | no | default '' | Короткое объяснение. |
| `used_default_path` | boolean | no | default false | Решение принято через MVP default cluster. |
| `created_at` | timestamptz | no | indexed | Создание. |

### `FleetManagerOutboxEvent`

Назначение: локальная очередь доменных событий, которые `fleet-manager` доставляет в общий `platform-event-log`.

Поля должны соответствовать общему паттерну адаптера `libs/go/outbox` и использовать отдельный доставщик в `platform-event-log`.

## Индексы

Минимально нужны индексы:

- `FleetScope(scope_type, scope_owner_id, status)`;
- `FleetScope(is_default, status)`;
- `Server(status, provider_type)`;
- `KubernetesCluster(fleet_scope_id, status)`;
- `KubernetesCluster(last_health_status, last_health_checked_at)`;
- `ClusterConnectivityCheck(cluster_id, created_at)`;
- `ClusterHealthSnapshot(cluster_id, checked_at)`;
- `PlacementRule(fleet_scope_id, status, priority)`;
- `PlacementDecision(request_fingerprint)`;
- `PlacementDecision(project_id, repository_id, created_at)`;
- `PlacementDecision(fleet_scope_id, cluster_id, created_at)`.

## Что не хранится

| Внешний владелец | Что остаётся у него |
|---|---|
| Secret store | Kubeconfig, service account token, SSH key, cloud credentials. |
| Kubernetes | Полное состояние objects, events, pod logs и controller status. |
| `runtime-manager` | Slots, jobs, workspace materialization, runtime artifact refs. |
| `project-catalog` | Project policy, placement policy как часть `services.yaml`, release policy. |
| `package-hub` | Установки пакетов, manifest пакета и runtime-требования как пакетная истина. |

## Апрув

- request_id: `owner-2026-05-11-fleet-manager-kickoff`
- Решение: approved
- Комментарий: модель данных `fleet-manager` согласована как стартовое целевое состояние FLEET-0.
