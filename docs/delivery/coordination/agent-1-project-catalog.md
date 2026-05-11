# Агент #1 — проекты, репозитории и runtime

## Зона ответственности

Агент #1 ведёт три связанных контура:

- домен проектов и репозиториев, основной сервис `project-catalog`;
- домен runtime и fleet в части `runtime-manager`;
- домен runtime и fleet в части `fleet-manager`.

`project-catalog` отвечает за:

- проекты и репозитории как локальную платформенную модель;
- проверенную операционную проекцию `services.yaml`;
- источники проектной и сервисной документации;
- правила веток, релизные политики, релизные линии и политику размещения;
- связи проекта с provider-native репозиториями через сохранённые provider-ссылки;
- команды, чтения и события `project.*`.

`runtime-manager` отвечает за:

- слоты и их жизненный цикл;
- подготовку workspace и статус materialization;
- platform jobs, steps, короткий хвост логов и ссылки на внешние runtime-артефакты;
- политики очистки, пакетную очистку, prewarm pool и безопасное переиспользование слотов;
- deploy-контур самого `runtime-manager`;
- команды, чтения и события `runtime.*`.

`fleet-manager` отвечает за:

- серверы, Kubernetes-кластеры, связность, health и placement scope;
- default fleet scope и default cluster в MVP;
- будущий выбор `fleet_scope_id` и `cluster_id` для runtime;
- события `fleet.*`.

Агент #1 не владеет пользователями, организациями, членством, внешними аккаунтами, сырыми секретами, provider-native операциями записи, пакетным каталогом, магазином пакетов, UI и внешними gateway.

## Что уже сделано по `project-catalog`

| Срез | Issue | PR | Статус | Результат |
|---|---:|---:|---|---|
| Wave 8 kickoff | #628 | #634 | готово | Доменная документация, границы, API-карта, план поставки и карты связей. |
| Wave 8.1 | #629 | #637 | готово | gRPC/AsyncAPI контракты, сгенерированный Go-код, сервисный каркас и доменные интерфейсы. |
| Wave 8.2 | #630 | #638 | готово | PostgreSQL-модель, миграции, слой репозитория, outbox, инвентарь выкладки и тесты. |
| Pgx helpers | #639 | #640 | готово | Простые PostgreSQL-сканеры переведены на штатные помощники. |
| Wave 8.3 | #631 | #641 | готово | gRPC-операции, проверки доступа через `access-manager`, события и транспортные тесты. |
| Wave 8.4.1 | #632 | #644 | готово | Импорт и проверенная проекция `services.yaml`, построение описаний сервисов. |
| Wave 8.4.2 | #632 | #649 | готово | `GetWorkspacePolicy`, источники документации, операторские переопределения и политика рабочего контура. |
| Wave 8.5 | #633 | #652 | готово | Правила веток, релизная политика, политика размещения, Dockerfile, Kubernetes-манифесты, migration job и smoke-путь. |
| Wave 8 closeout | #633 | #654 | готово | Статусы Wave 8, карты Issue и документы поставки приведены к завершённому состоянию. |

Итог: `project-catalog` имеет стабильные `v1` контракты, БД, миграции, gRPC-слой, outbox-публикацию в `platform-event-log`, deploy-манифесты и smoke-контур. Все операции из `proto/kodex/projects/v1/project_catalog.proto` реализованы в рамках согласованного объёма Wave 8.

## Что уже сделано по `runtime-manager`

| Срез | Issue | PR | Статус | Результат |
|---|---:|---:|---|---|
| RTM-0 | #655 | #664 | готово | Доменная документация, границы runtime/fleet, карта Issue и план поставки. |
| RTM-1 | #656 | #669 | готово | gRPC/AsyncAPI контракты `runtime-manager`, события и сгенерированные Go-контракты. |
| RTM-2 | #657 | #672 | готово | Сервисный каркас, PostgreSQL-модель, миграции, repository, health/readiness, outbox и базовые тесты. |
| RTM-3 | #658 | #676 | готово | Жизненный цикл слотов: reserve, extend lease, release, fail, чтения, идемпотентность и MVP default fleet boundary. |
| RTM-4 | #659 | #683 | готово | Workspace materialization: source refs, access mode, local paths, fingerprint, progress и безопасные ошибки подготовки. |
| RTM-5 | #660 | #687 | готово | Platform job MVP: job/step state machine, claim lease, progress, complete/fail/cancel, short log tail и runtime artifact refs. |
| RTM-6 | #661 | #691 | готово | Dockerfile, manifests, PostgreSQL bootstrap, migration job, `services.yaml`, smoke-путь, runbook и monitoring-документы. |
| RTM-7 | #662 | #696 | готово | Cleanup policy, cleanup batch, prewarm pool, deterministic slot reuse, очистка хвостов логов и видимость cleanup failures. |

Итог: `runtime-manager` имеет стабильные `v1` контракты, БД, миграции, gRPC-слой, outbox, deploy-контур, smoke-путь и реализованный runtime MVP по слотам, workspace materialization, platform jobs, cleanup, prewarm и reuse.

## Что уже сделано по `fleet-manager`

| Срез | Issue | Статус | Результат |
|---|---:|---|---|
| FLEET-0 | #699 | готово | Доменная документация, границы runtime/fleet, MVP default cluster, будущие контракты, план поставки и карта Issue. |

Итог: `fleet-manager` пока не имеет кода. Зафиксирована целевая граница и порядок поставки: контракты, сервисный каркас, default scope/cluster, health, placement resolver и deploy-контур.

## Текущий бэклог агента #1

| Направление | Статус | Что осталось |
|---|---|---|
| Bootstrap пустого репозитория | открыто: #281 | Проектная часть готова в `project-catalog`; создание provider-native репозитория, первичный PR и связи ждут `provider-hub`/gateway-срез. |
| Adoption существующего репозитория | открыто: #282 | Проектная часть готова в `project-catalog`; scan у провайдера, bootstrap PR и provider relationships ждут `provider-hub`. |
| UI/gateway для проектов и runtime | запланировано позже | Делать после определения фактических экранов `web-console` и состава `staff-gateway` ручек. |
| `project.policy_override.expired` | запланировано позже | Контракт события есть; нужна логика обслуживания или platform job, которая будет снимать истёкшие переопределения как операционный срез. |
| Организационные runtime-политики | частично заблокировано | Cleanup для `organization` scope отклоняется, а prewarm хранит политику без фактической раскладки, пока runtime не получает проекцию организации на слоты. |
| Реальный исполнитель platform jobs | запланировано позже | `runtime-manager` хранит и выдаёт jobs; конкретный исполнитель на Kubernetes или агентный исполнитель нужен отдельным срезом после согласования с `agent-manager`/ops-контуром. |
| Контракты `fleet-manager` | следующий локальный срез | После FLEET-0 нужен FLEET-1: proto, AsyncAPI, события `fleet.*`, ключи действий и таблица реализации операций. |

## Блокировки от `access-manager`

Снято:

- базовая проверка доступа для `project-catalog` и `runtime-manager` подключена через `access-manager`;
- action/scope ключи заведены в `accesscatalog` и используются сервисами;
- команды и чтения текущих срезов не блокируются отсутствием membership UI или полных организационных экранов.

Реальные оставшиеся блокировки:

- проектные экраны членства, групп и организаций должны опираться на `access-manager`, а не на локальные сущности `project-catalog`;
- организационный scope для cleanup/prewarm требует проекции организации или явного контракта, по которому `runtime-manager` сможет сопоставить слоты с организацией;
- owner approval и политики риска для части операций должны приходить из общего контура доступа и governance, а не дублироваться в доменных сервисах.

Нужны новые или уточнённые контракты:

- модель чтения членства, групп и организаций для операторских экранов проектов;
- контракт организационной проекции для runtime-политик;
- единый способ проверки owner approval для операций, которые запускаются из `staff-gateway`, MCP или agent-manager.

## Блокировки от `provider-hub`

Снято:

- `project-catalog` не обязан быть Git-клиентом и уже хранит provider-ссылки как часть привязки репозитория;
- `runtime-manager` не блокируется provider-доступом для текущих RTM-0..RTM-7 срезов.

Реальные оставшиеся блокировки:

- #281 и #282 требуют provider-native операций: создать или просканировать репозиторий, открыть bootstrap PR, связать provider Issue/PR/MR с локальными проектом и репозиторием;
- `CreatePolicyEditProposal` в `project-catalog` сохраняет предложение, но создание PR с правкой `services.yaml` должно идти через provider-контур;
- workspace source refs и сигналы после работы slot-агентов должны синхронизироваться с provider-проекциями, но runtime не должен напрямую ходить в GitHub/GitLab.

Нужны новые или уточнённые контракты:

- сопоставление `provider_slug + provider_repository_id/full_name` с `project_id + repository_id`;
- команда provider-контура на создание bootstrap/policy PR по проверенному предложению `project-catalog`;
- событие или команда ускоряющего сигнала после появления provider-native артефакта из runtime/agent workspace.

## Блокировки от `package-hub`

Снято:

- текущие `project-catalog` и `runtime-manager` срезы не требуют готового `package-hub`;
- проектная политика может хранить ссылки и требования без того, чтобы `project-catalog` становился владельцем пакетного каталога.

Реальные оставшиеся блокировки:

- проверка доступности руководящих пакетов и пакетных источников в проектной политике должна опираться на `package-hub`;
- сборка workspace агента с руководящими пакетами требует контракта между `package-hub`, `agent-manager` и runtime workspace;
- запуск runtime-нагрузок пакетов или плагинов требует связки установки пакета, требований manifest и runtime/fleet placement.

Нужны новые или уточнённые контракты:

- контракт чтения установленных руководящих пакетов по project/repository scope;
- контракт передачи требований пакета в workspace materialization;
- контракт запуска runtime-нагрузки для пакета или плагина после подтверждённой установки.

## Блокировки для других агентов

- `provider-hub` зависит от `project-catalog` при привязке provider-native объектов к локальному проекту и репозиторию.
- `package-hub` зависит от `project-catalog`, когда пакетные источники и руководящие пакеты становятся частью проектной политики.
- `agent-manager` зависит от `runtime-manager` для слотов, workspace materialization и platform jobs, но `Run` остаётся сущностью `agent-manager`.
- `runtime-manager` зависит от `fleet-manager` для целевого `ResolvePlacement`; до интеграционного среза runtime использует MVP default refs.
- `operations-hub` и будущий `staff-gateway` зависят от чтений `project-catalog` и `runtime-manager` для операторских экранов.

## Рекомендуемый следующий шаг

Для агента #1 нет незавершённого локального RTM или Wave 8 среза, который нужно закрыть до соседних доменов. После FLEET-0 рационально идти в один из трёх вариантов:

- идти в FLEET-1 и создать контракты `fleet-manager`;
- дождаться `provider-hub` bootstrap/adoption контракта и закрывать #281/#282;
- начать gateway/UI-срез только после согласования состава `staff-gateway` ручек.
