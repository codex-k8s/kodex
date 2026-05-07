---
doc_id: DLV-CK8S-PROJ-WAVE8
type: delivery-plan
title: kodex — поставка project-catalog
status: completed
owner_role: EM
created_at: 2026-05-05
updated_at: 2026-05-07
related_issues: [628, 629, 630, 631, 632, 633, 281, 282]
related_prs: []
related_docsets:
  - docs/domains/projects-and-repositories/product/requirements.md
  - docs/domains/projects-and-repositories/architecture/design.md
  - docs/domains/projects-and-repositories/architecture/data_model.md
  - docs/domains/projects-and-repositories/architecture/api_contract.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-05-wave8-project-catalog-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-05
---

# Поставка project-catalog

## TL;DR

`project-catalog` поставлен малыми PR-срезами: доменная документация, контракты, сервисный каркас, PostgreSQL-модель, gRPC-операции, `services.yaml`, источники документации, правила веток, релизные политики, политика размещения и минимальный эксплуатационный контур.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования домена | `docs/domains/projects-and-repositories/product/requirements.md` |
| Дизайн домена | `docs/domains/projects-and-repositories/architecture/design.md` |
| Модель данных | `docs/domains/projects-and-repositories/architecture/data_model.md` |
| API-обзор | `docs/domains/projects-and-repositories/architecture/api_contract.md` |
| Волновой план | `docs/delivery/waves/wave-008-projects-and-repositories.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---|---|
| Стартовый срез | #628 | Доменная документация, план поставки и карты связей готовы. |
| 8.1 | #629 | Контракты `project-catalog`, сервисный каркас и доменные интерфейсы готовы. |
| 8.2 | #630 | PostgreSQL-модель, миграции, слой репозитория, outbox, инвентарь выкладки и тесты. |
| 8.3 | #631 | gRPC-операции, проверки доступа, события и транспортные тесты. |
| 8.4 | #632 | Политика `services.yaml`, управляемая через Git, импорт проверенной проекции, источники документации и политика рабочего контура. |
| 8.5 | #633 | Правила веток, релизная политика, релизные линии, политика размещения, манифесты выкладки, проверочный путь и закрытие Wave 8. |

## Состояние контрактов

| Артефакт | Текущий статус | Когда становится источником правды |
|---|---|---|
| API-обзор `project-catalog` | Принят как карта операций и событий. | Используется вместе с proto и AsyncAPI. |
| gRPC proto | Готов как стабильный `v1`. | `proto/kodex/projects/v1/project_catalog.proto` — источник правды gRPC-транспорта. |
| AsyncAPI | Готов как стабильный `v1`. | `specs/asyncapi/project-catalog.v1.yaml` — источник правды событий `project.*`. |
| Таблица реализованных операций | Ведётся с первого кодового PR. | Обновляется в каждом PR, где меняется состав команд, чтений или событий. |

## Реализация операций

Срез #629 фиксирует стабильный транспортный контракт и запускаемый каркас сервиса. Срез #630 добавляет PostgreSQL-модель, миграции, слой репозитория, оптимистичную конкуренцию, идемпотентный след, сервисный outbox и минимальный инвентарь выкладки. Срез #631 подключает gRPC-обработчики к доменному сервису, проверке доступа через `access-manager` и доставке outbox в `platform-event-log`. Срез #633 закрывает правила веток, релизные политики, релизные линии, политику размещения и промышленный путь запуска сервиса в Kubernetes.

| Операция | Контракт | Реализация |
|---|---|---|
| `CreateProject` | Готов в proto. | gRPC, доменная команда, проверка доступа и outbox подключены. |
| `UpdateProject` | Готов в proto. | gRPC, доменная команда, проверка доступа и outbox подключены. |
| `GetProject` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `ListProjects` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `AttachRepository` | Готов в proto. | gRPC, доменная команда, проверка доступа и outbox подключены. |
| `UpdateRepository` | Готов в proto. | gRPC, доменная команда, проверка доступа и outbox подключены. |
| `DetachRepository` | Готов в proto. | gRPC, доменная команда, проверка доступа и outbox подключены. |
| `GetRepository` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `ListRepositories` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `ImportServicesPolicy` | Готов в proto. | gRPC, доменная команда, проверка доступа, outbox, построение `ServiceDescriptor`, проверка и синхронизация источников документации из нормализованного payload подключены. |
| `GetServicesPolicy` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `ListServiceDescriptors` | Готов в proto. | gRPC-чтение и проверка доступа подключены; чтение ограничено последней `valid + synced/overridden` политикой и использует проекцию, построенную `project-catalog`. |
| `CreatePolicyEditProposal` | Готов в proto. | gRPC и сохранение предложения подключены; создание provider PR развивается в #632. |
| `CreatePolicyOverride` | Готов в proto. | gRPC, доменная команда, проверка доступа и outbox подключены. |
| `CancelPolicyOverride` | Готов в proto. | gRPC, доменная команда, проверка доступа, оптимистичная конкуренция, outbox и PostgreSQL-слой подключены. |
| `ListPolicyOverrides` | Готов в proto. | gRPC-чтение и проверка доступа подключены; активные переопределения также входят в `GetWorkspacePolicy`. |
| `PutDocumentationSource` | Готов в proto. | gRPC, доменная команда, проверка доступа и outbox подключены. |
| `GetDocumentationSource` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `ListDocumentationSources` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `GetWorkspacePolicy` | Готов в proto. | gRPC-чтение и проверка доступа подключены; источники ограничены активной проверенной политикой, фильтр сервисов сужает код, сервисную документацию и документацию зависимостей. |
| `PutBranchRules` | Готов в proto. | gRPC, доменная команда, проверка доступа и outbox подключены. |
| `GetBranchRules` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `ListBranchRules` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `PutReleasePolicy` | Готов в proto. | gRPC, доменная команда, проверка доступа и outbox подключены. |
| `GetReleasePolicy` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `ListReleasePolicies` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `PutReleaseLine` | Готов в proto. | gRPC, доменная команда, проверка доступа и outbox подключены. |
| `GetReleaseLine` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `ListReleaseLines` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `PutPlacementPolicy` | Готов в proto. | gRPC, доменная команда, проверка доступа и outbox подключены. |
| `GetPlacementPolicy` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |
| `ListPlacementPolicies` | Готов в proto. | gRPC-чтение и проверка доступа подключены. |

## Эксплуатационный контур

| Артефакт | Статус | Назначение |
|---|---|---|
| Dockerfile `project-catalog` | Готов. | Содержит стадии `prod`, `dev` и `migrations`; образ миграций запускает `goose` по каталогу миграций сервиса. |
| Kubernetes-манифест сервиса | Готов. | Описывает `ServiceAccount`, `Service`, `Deployment`, gRPC/HTTP-порты, readiness/liveness, `/metrics`, ресурсы, контекст безопасности, ожидание БД `project-catalog` и БД `platform-event-log`. |
| Kubernetes-задание миграций | Готово. | Запускает миграции БД `project-catalog` отдельным `Job` после доступности PostgreSQL и до запуска сервиса. |
| Инвентарь `services.yaml` | Готов. | Фиксирует версии, образы, описание сервиса для выкладки, миграционный образ, БД, контракты и порядок зависимостей выкладки. |
| Рендер манифестов | Готов. | `cmd/manifest-render` рендерит шаблоны `deploy/base/**` во временный каталог перед применением. |
| Сборка проверочных образов | Готова. | `scripts/build-project-catalog-images.sh` собирает образы `project-catalog`, его миграций и минимальных зависимостей проверочного контура; версии и имена образов берутся через общий shell-helper из `services.yaml` и переопределений через env. |
| Проверка готовности | Готова. | `scripts/smoke-project-catalog.sh` применяет PostgreSQL, миграции `platform-event-log`, `access-manager`, миграции `project-catalog`, выкладку `project-catalog` и проверяет `/health/readyz`; базовые образы по умолчанию берутся из внутреннего реестра зеркал. |

Проверочный путь не выполняет удалённую выкладку сам по себе: он требует нормализованный `bootstrap.env`, доступ к Kubernetes-кластеру и уже доступные образы в реестре или локально загруженные образы. Серверная проверка выполняется отдельной операторской командой после подготовки целевого контура.

## Реализация событий

| Событие | Контракт | Публикация |
|---|---|---|
| `project.project.created` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.project.updated` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.project.archived` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.project.disabled` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.repository.attached` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.repository.updated` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.repository.detached` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.services_policy.imported` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.policy_override.created` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.policy_override.expired` | Готово в AsyncAPI. | Отложена до maintenance/job-среза; авторитетные чтения уже фильтруют истёкшие переопределения по времени. |
| `project.policy_override.cancelled` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.documentation_source.created` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.documentation_source.updated` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.documentation_source.disabled` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.branch_rules.created` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.branch_rules.updated` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.branch_rules.disabled` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.release_policy.created` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.release_policy.updated` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.release_policy.archived` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.release_policy.disabled` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.release_line.created` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.release_line.updated` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.release_line.archived` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.release_line.disabled` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.placement_policy.created` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.placement_policy.updated` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |
| `project.placement_policy.disabled` | Готово в AsyncAPI. | Пишется в сервисный outbox и публикуется в `platform-event-log`. |

## Связь с задачами подключения репозиториев

Задачи #281 и #282 остаются открытыми после завершения Wave 8. Wave 8 создаёт проектный каталог и основание проектной политики для этих сценариев, но полное закрытие подключения репозиториев требует `provider-hub` и provider-native рабочих сущностей.

Решение:
- часть про проект, репозиторий и политику закрывается в Wave 8;
- создание или сканирование репозитория у провайдера, первичный PR и provider-native связи закрываются после появления `provider-hub`;
- финальный статус #281 и #282 фиксируется в плане provider-native слоя.

## Критерии начала кода

- Принят пакет доменной документации `projects-and-repositories`.
- Для каждого следующего PR есть отдельный GitHub Issue.
- PR, который завершает Issue, содержит `Closes #...` в теле PR.
- Первый контрактный PR создаёт proto и AsyncAPI до реализации операций, чтобы источник правды не оставался только в markdown.
- Реализация политики `services.yaml` должна исходить из выбранной модели: Git/PR хранит источник намерения, `project-catalog` хранит проверенную операционную проекцию.
- Старый код из `deprecated/**` не используется как основа реализации.

## Критерии завершения Wave 8

- `project-catalog` имеет свой контур данных, миграций, контрактов и событий.
- Проекты, репозитории, проверенная проекция `services.yaml`, источники документации, правила веток, релизная политика и политика размещения имеют авторитетные команды и чтения.
- UI-изменения декларативной проектной политики создают PR с правкой `services.yaml`; прямые переопределения ограничены аварийным сценарием, сроком действия и аудитом.
- Сервис публикует `project.*` события через outbox и `platform-event-log`.
- `agent-manager`, `runtime-manager`, `provider-hub` и `operations-hub` могут опираться на контракты `project-catalog`.
- Документы и карты Issue обновлены, хвосты перенесены в следующие волны явно.

## Апрув

- request_id: `owner-2026-05-05-wave8-project-catalog-kickoff`
- Решение: approved
- Комментарий: план поставки `project-catalog` согласован как целевое состояние стартового среза.
