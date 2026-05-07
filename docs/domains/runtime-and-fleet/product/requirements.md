---
doc_id: PRD-CK8S-RUNTIME-0001
type: prd
title: kodex — требования домена runtime и fleet
status: active
owner_role: PM
created_at: 2026-05-07
updated_at: 2026-05-07
related_issues: [655, 656, 657, 658, 659, 660, 661, 662]
related_prs: []
related_docsets:
  - docs/platform/product/requirements.md
  - docs/platform/product/product_model.md
  - docs/platform/architecture/domain_map.md
  - docs/platform/architecture/service_boundaries.md
  - refactoring/21-runtime-deploy-and-bootstrap.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-07-runtime-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-07
---

# PRD: runtime и fleet

## TL;DR

- Что строим: домен `runtime-manager` для слотов, workspace materialization, platform jobs, короткой диагностики, cleanup, prewarm и reuse; рядом фиксируем границу `fleet-manager` для серверов, Kubernetes-кластеров, связности и размещения.
- Для кого: `agent-manager`, `worker`, `agent-runner`, `project-catalog`, `package-hub`, `operations-hub`, будущие release/governance контуры и оператор платформы.
- Почему: агентная работа, сборка, выкладка, очистка и технические проверки не должны размазываться по `agent-manager`, `project-catalog`, shell-скриптам или UI.
- Минимум первой версии: один Kubernetes-кластер как явно описанный стартовый контур, slot как namespace, job как техническая операция, локальная подготовка workspace и короткий хвост лога без хранения полного лога в PostgreSQL.
- Критерии успеха: runtime-состояние можно читать и контролировать через `runtime-manager`, а кластерная доступность и размещение не смешиваются с runtime-истиной.

## Проблема и цель

Проблема:

- `agent-manager` должен запускать агентную работу, но не должен сам управлять namespace, checkout, build, deploy или cleanup;
- `project-catalog` знает состав workspace, но не должен выполнять checkout;
- `worker` может исполнять техническую работу, но не должен владеть статусом задания;
- `agent-runner` работает внутри подготовленного слота, но не должен сам решать, можно ли переиспользовать runtime;
- старые shell-подходы легко превращают первый deploy в неуправляемую инфраструктурную автоматику без доменного владельца;
- полные технические логи и образы нельзя копить в БД платформы.

Цель:

- создать сервис-владелец среды исполнения;
- разделить агентный запуск, техническое задание, слот и инфраструктурный контур;
- дать платформе управляемый путь подготовки workspace и технических операций;
- сделать cleanup, retention и ошибки среды видимыми оператору;
- заложить переход от одного стартового кластера к нескольким кластерам без переписывания модели.

## Пользователи и роли

| Роль | Главный сценарий |
|---|---|
| Оператор платформы | Видит слоты, задания, ошибки подготовки, короткий хвост лога, состояние cleanup и следующий ожидаемый action. |
| `agent-manager` | Запрашивает слот и техническую подготовку среды для агентного запуска, но сохраняет владение `Run` и сессией. |
| `worker` | Исполняет технические задания и reconciliation по поручению `runtime-manager`, не владея конечной истиной. |
| `agent-runner` | Исполняет ролевого агента внутри подготовленного слота и передаёт runtime-сигналы через разрешённый контракт. |
| `project-catalog` | Отдаёт проверенную workspace policy, источники документации, правила веток, релизную и placement policy. |
| `package-hub` | Отдаёт сведения об установленных пакетах и runtime-требованиях плагинов. |
| `operations-hub` | Строит операторские проекции по runtime-событиям, сбоям и активным заданиям. |
| `billing-hub` | В будущем получает факты использования runtime для учёта затрат. |

## Функциональные требования

| ID | Требование | Приоритет |
|---|---|---|
| RTM-FR-1 | `runtime-manager` должен хранить жизненный цикл слота как каноническое runtime-состояние. | Обязательно |
| RTM-FR-2 | В первой версии слот должен поддерживать модель namespace-per-task в Kubernetes. | Обязательно |
| RTM-FR-3 | Модель слота должна оставлять задел под nested cluster и другие формы изоляции. | Обязательно |
| RTM-FR-4 | `runtime-manager` должен поддерживать reserve, extend lease, release, fail и cleanup переходы слота. | Обязательно |
| RTM-FR-5 | `runtime-manager` должен хранить platform job как техническую операцию среды: mirror, build, deploy, cleanup, health-check или housekeeping. | Обязательно |
| RTM-FR-6 | `Run` и агентная сессия принадлежат `agent-manager`; `runtime-manager` хранит только внешние ссылки на них. | Обязательно |
| RTM-FR-7 | `runtime-manager` должен хранить шаги job, статус, время, короткий хвост лога и ссылку на полный источник логов. | Обязательно |
| RTM-FR-8 | Полные build/deploy/container logs не должны храниться в PostgreSQL. | Обязательно |
| RTM-FR-9 | Платформа не должна заводить собственную сущность-владельца образа; registry остаётся владельцем образов. | Обязательно |
| RTM-FR-10 | Workspace materialization должна принимать проверенную workspace policy и фиксировать результат подготовки источников. | Обязательно |
| RTM-FR-11 | Runtime должен различать writable и read-only источники workspace. | Обязательно |
| RTM-FR-12 | Runtime должен фиксировать deterministic fingerprint для безопасного reuse. | Обязательно |
| RTM-FR-13 | Reuse слота допустим только при совпадении fingerprint, runtime profile, source refs и безопасном состоянии среды. | Обязательно |
| RTM-FR-14 | Prewarm slots должны быть управляемой capability с policy, пулом и видимым дефицитом. | Обязательно |
| RTM-FR-15 | Cleanup и retention jobs должны быть видимы как runtime-события и операторские сигналы при сбое. | Обязательно |
| RTM-FR-16 | `runtime-manager` должен исполнять runtime на уже выбранном fleet scope и не должен сам владеть реестром серверов и кластеров. | Обязательно |
| RTM-FR-17 | `fleet-manager` должен владеть серверами, Kubernetes-кластерами, связностью, health и placement scope. | Обязательно |
| RTM-FR-18 | В MVP допускается один default cluster через явный fleet ref или config, но API и БД не должны считать его единственным возможным контуром. | Обязательно |
| RTM-FR-19 | Runtime-события должны публиковаться через outbox и `platform-event-log`. | Обязательно |
| RTM-FR-20 | Runtime должен давать `operations-hub` и будущему UI достаточно данных для диагностики без прямого чтения Kubernetes и БД сервиса. | Обязательно |

## Критерии приёмки

| ID | Критерий |
|---|---|
| RTM-AC-1 | Если `agent-manager` запрашивает работу агента, runtime выделяет или создаёт слот, но не создаёт агентный `Run` как свою сущность. |
| RTM-AC-2 | Если workspace source недоступен, ошибка фиксируется в runtime-состоянии и не превращается в молчаливый partial checkout. |
| RTM-AC-3 | Если build/deploy job падает, оператор видит статус, классификацию ошибки, короткий хвост лога и ссылку на полный источник. |
| RTM-AC-4 | Если cleanup job падает, это становится видимым runtime-сигналом, а не только записью в логах Kubernetes. |
| RTM-AC-5 | Если свободного prewarmed slot нет, runtime делает cold start и фиксирует причину деградации. |
| RTM-AC-6 | Если slot reuse небезопасен, runtime обязан подготовить новый контур или заново materialize workspace. |
| RTM-AC-7 | Если организация или проект заблокированы через доменное событие, runtime перестаёт запускать новые задания по этому контуру и переводит активные объекты по своей policy. |

## Что не входит

- Не владеть `Run`, flow, stage, role, prompt, acceptance machine и агентными сессиями.
- Не владеть `Issue`, `PR/MR`, комментариями, provider relationships и webhook провайдера.
- Не владеть проектами, репозиториями, `services.yaml`, правилами веток и release policy.
- Не владеть серверным и кластерным реестром.
- Не доставлять уведомления и не владеть Human gate.
- Не хранить полные технические логи, образы, Kubernetes events и registry catalog как собственную истину.
- Не реализовывать пользовательский интерфейс.

## Нефункциональные требования

| ID | Категория | Требование |
|---|---|---|
| RTM-NFR-1 | Надёжность | Команды должны быть идемпотентны по `command_id`, а конкурентные изменения должны использовать версии агрегатов. |
| RTM-NFR-2 | Масштабирование | Размеры пулов, параллелизм job, лимиты Kubernetes API и DB pool должны задаваться конфигурацией. |
| RTM-NFR-3 | Наблюдаемость | Slot, job, workspace и cleanup должны иметь структурированные логи, метрики, трассировку и события. |
| RTM-NFR-4 | Хранение | Runtime хранит только platform state, короткую диагностику и ссылки на первоисточник. |
| RTM-NFR-5 | Безопасность | Доступ к секретам идёт через разрешённые ссылки и policy; сырые секреты не хранятся в БД runtime. |
| RTM-NFR-6 | Расширяемость | Namespace первой версии не должен попасть в доменную модель как единственная возможная форма слота. |

## Зависимости

| Зависимость | Зачем нужна |
|---|---|
| `access-manager` | Проверка прав вызывающей стороны и реакция на блокировки организаций, пользователей и внешних аккаунтов. |
| `project-catalog` | Workspace policy, проектные источники, release policy, placement policy и `services.yaml` projection. |
| `provider-hub` | Provider-native артефакты, ускоряющие сигналы после работы агента и ссылки на `Issue/PR/MR`. |
| `package-hub` | Runtime-требования плагинов и руководящие пакеты как источники workspace. |
| `agent-manager` | Agent `Run`, сессии, flow и запрос на подготовку runtime. |
| `fleet-manager` | Серверы, кластеры, health, connectivity и placement scope. |
| `operations-hub` | Операторские проекции runtime-событий. |
| `interaction-hub` | Будущие уведомления о сбоях, Human gate и обратная связь. |
| `billing-hub` | Будущий учёт затрат runtime. |

## Апрув

- request_id: `owner-2026-05-07-runtime-manager-kickoff`
- Решение: approved
- Комментарий: требования домена runtime и fleet согласованы как целевое состояние.
