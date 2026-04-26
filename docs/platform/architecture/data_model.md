---
doc_id: ARC-CK8S-DATA-MODEL-0001
type: data-model
title: kodex — сквозная модель данных
status: active
owner_role: SA
created_at: 2026-04-26
updated_at: 2026-04-26
related_issues: [599, 600, 601, 602]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-26-platform-architecture-frame"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-26
---

# Сквозная модель данных

## TL;DR

PostgreSQL хранит платформенное состояние, но не становится складом всего подряд. У каждого owner-сервиса есть собственная БД или собственный строго изолированный контур владения. Между owner-сервисами нет `FOREIGN KEY`, cross-database join и каскадов. Согласованность поддерживается через контракты, доменные события, outbox/inbox, read-проекции и reconciliation.

## Базовые правила

| Правило | Следствие |
|---|---|
| Один класс данных имеет одного owner-сервиса | Схема, миграции и бизнес-инварианты принадлежат owner-сервису. |
| Database-per-service | Даже при одном физическом PostgreSQL-кластере данные разных owner-сервисов изолированы. |
| Нет реляционных связей между owner-БД | Ссылки на чужие агрегаты хранятся как внешние идентификаторы. |
| Read-проекция не является истиной | UI и поиск могут читать проекции, но команды идут в owner-сервисы. |
| Внешняя система остаётся владельцем своих объектов | Платформа хранит только mirror, metadata, lifecycle и audit, которые нужны ей самой. |
| Полные технические логи не пишутся в PostgreSQL | Храним статус, причину ошибки, ссылки и короткий хвост лога. |

## Что хранит платформа

| Класс | Owner-сервис | Примеры |
|---|---|---|
| Доступ | `access-manager` | User, organization, group, membership, access rule, allowlist, audit entry. |
| Проекты | `project-catalog` | Project, repository, service descriptor, documentation source, branch rule, release policy, placement rule. |
| Provider mirror | `provider-hub` | Provider account, webhook inbox, normalized event, work item projection, relationship, sync cursor, drift status, rate limit state. |
| Пакеты | `package-hub` | Package, package version, catalog source, installation, verification, secret schema, price metadata. |
| Агентная работа | `agent-manager` | Flow, stage, role profile, prompt template version, run, agent session, automation rule, acceptance result, wait state. |
| Fleet | `fleet-manager` | Server, cluster, cluster health, connectivity, placement binding. |
| Runtime | `runtime-manager` | Slot, job, job step, runtime artifact reference, cleanup policy, short log tail. |
| Взаимодействия | `interaction-hub` | Dialog thread, message, approval request, notification, subscription, delivery attempt, channel callback. |
| Операционные проекции | `operations-hub` | Timeline item, operator queue item, aggregate status, lock view, incident view. |
| Биллинг | `billing-hub` | Billing account, cost record, allocation rule, invoice draft, package revenue split. |

## Что не хранит платформа как свою истину

| Внешний владелец | Что остаётся у него |
|---|---|
| GitHub/GitLab | Полное тело `Issue`, `PR/MR`, review, diff, ветки, теги, provider-native relationships и comments как источник истины. |
| Kubernetes | Pod/job/deployment state, cluster events и полные container logs. |
| Container registry | Образы, manifest, blobs и tags как каталог образов. |
| Репозитории проекта | Файлы кода, проектной документации и документации сервисов. |
| Репозитории пакетов | Исходники пакетов, manifest и версии как provider-native Git state. |
| Object storage | Временные voice/media attachments. |

## Минимальные агрегаты по owner-сервисам

### `access-manager`

| Агрегат | Назначение | Важные инварианты |
|---|---|---|
| `Organization` | Контур владения, биллинга и доступа. | У каждой установки есть owner-организация. |
| `User` | Пользователь платформы. | Self-signup запрещён; вход возможен только через разрешённый email и SSO/OIDC. |
| `Group` | Глобальная или организационная группа. | Пользователь может состоять в нескольких группах. |
| `Membership` | Связь пользователя, организации, группы или проекта. | Права вычисляются с учётом наследования и явных исключений. |
| `AccessDecisionAudit` | След принятого решения доступа. | Должен объяснять, почему действие разрешено или запрещено. |

### `project-catalog`

| Агрегат | Назначение | Важные инварианты |
|---|---|---|
| `Project` | Контейнер репозиториев, policy и runtime-ограничений. | Проект принадлежит организации. |
| `RepositoryBinding` | Привязка репозитория к проекту. | Один проект может иметь несколько репозиториев. |
| `DocumentationSource` | Источник проектной или сервисной документации. | Доступ агента может быть read-only или writable. |
| `ServiceDescriptor` | Проверенная часть `services.yaml`. | Не должен тащить устаревшие сервисы из архива. |
| `ReleasePolicy` | Правила веток, gates, rollout и postdeploy. | Ветки и теги остаются provider-native. |
| `PlacementPolicy` | Ограничения инфраструктурного размещения. | Используется `fleet-manager` и `runtime-manager`. |

### `provider-hub`

| Агрегат | Назначение | Важные инварианты |
|---|---|---|
| `ProviderAccount` | Машинный или пользовательский внешний аккаунт. | Область действия ограничена project/repo/role policy. |
| `WebhookEvent` | Сырой входящий provider signal. | Dedup по delivery id или аналогу обязателен. |
| `ProviderWorkItemProjection` | Зеркало `Issue` или `PR/MR`. | Источник истины остаётся у провайдера. |
| `ProviderRelationship` | Нормализованная связь provider-native объектов. | Не заменяет relationship у провайдера. |
| `SyncCursor` | Контур incremental reconciliation. | Должен иметь окно перекрытия и состояние лимитов. |
| `ProviderLimitSnapshot` | Снимок лимитов внешнего API. | Для `gh` это приближённый учёт, а не полный аудит каждой операции. |

### `agent-manager`

| Агрегат | Назначение | Важные инварианты |
|---|---|---|
| `FlowDefinition` | Версионируемый шаблон процесса. | Runtime-истина хранится в БД, repo содержит seed fixtures. |
| `StageDefinition` | Этап flow. | Этап не равен роли агента. |
| `RoleProfile` | Роль агента, права, runtime и policy. | Роль может быть встроенной или пользовательской. |
| `PromptTemplateVersion` | Версия `work` или `revise` шаблона. | Используется через публикацию и audit. |
| `Run` | Агентный запуск. | Не смешивается с platform `job`. |
| `AgentSession` | Продолжаемая сессия агента. | Может быть возобновлена с замечаниями. |
| `AcceptanceResult` | Результат машинной приёмки. | Должен ссылаться на проверенные артефакты. |

### `runtime-manager`

| Агрегат | Назначение | Важные инварианты |
|---|---|---|
| `Slot` | Изолированная runtime-среда. | Базовый вариант первой версии — namespace. |
| `Job` | Техническая операция платформы. | Build, deploy, mirror, cleanup и health-check являются одним классом job. |
| `JobStep` | Детализация выполнения job. | Полный лог не хранится в PostgreSQL. |
| `RuntimeArtifactRef` | Ссылка на внешний технический артефакт. | Платформа не становится реестром образов. |
| `CleanupPolicy` | Правила retention и housekeeping. | Сбой cleanup должен быть видим оператору. |

## Межсервисная синхронизация

### Outbox/inbox

Каждый owner-сервис, меняющий важное состояние, записывает событие в outbox в той же транзакции, где меняет своё состояние. Доставщик публикует событие подписчикам. Подписчик хранит inbox/checkpoint и обрабатывает событие идемпотентно.

Пример удаления организации:
1. `access-manager` меняет состояние организации на archived или deleted.
2. В той же транзакции он пишет событие `access.organization.archived`.
3. `project-catalog` архивирует или блокирует новые операции по проектам организации.
4. `provider-hub` блокирует выбор внешних аккаунтов этой организации.
5. `runtime-manager` останавливает или переводит slots/jobs в запрещённое состояние по policy.
6. `billing-hub` закрывает расчётный период или помечает контур для финального счёта.
7. `operations-hub` обновляет read-проекцию и операторский timeline.

### Reconciliation

Reconciliation нужен для:
- восстановления после потерянных webhook или событий;
- проверки горячих provider-native объектов;
- сверки runtime-состояния с Kubernetes;
- проверки package sources и доступности внешних аккаунтов;
- восстановления read-проекций.

Reconciliation не должен превращаться в постоянный полный обход всех внешних систем. Он использует cursor, окно перекрытия, приоритеты горячих сущностей и rate budget.

## Событийная шина

В первой реализации допустим outbox/inbox на PostgreSQL и worker-доставка. Kafka или другой брокер не является обязательным стартовым требованием.

Причина:
- важнее зафиксировать надёжность публикации рядом с записью owner-состояния;
- поток событий на старте ожидаемо умеренный;
- database-backed outbox проще отладить в первом вертикальном срезе;
- переход на брокер можно сделать позже, если появится нагрузка или нужна независимая масштабируемая доставка.

Требование к дизайну: формат событий, `event_id`, `aggregate_id`, `occurred_at`, schema version и idempotency должны проектироваться так, чтобы будущий брокер не ломал доменные контракты.

## Retention и ограничения хранения

- Сырые webhook payload хранятся с ограниченным retention.
- Короткие log tails по jobs хранятся для быстрого UI и диагностики.
- Полные logs остаются в runtime/logging-контуре.
- Временные медиа имеют срок жизни и ссылку на объектное хранилище.
- Provider mirror не должен бесконечно хранить лишние payload без бизнес-смысла.

## Апрув

- request_id: `owner-2026-04-26-platform-architecture-frame`
- Решение: approved
- Комментарий: модель данных входит в сквозной архитектурный каркас платформы.
