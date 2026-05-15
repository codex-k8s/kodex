# Агент #1 — проекты, репозитории и runtime

## Зона ответственности

Агент #1 ведёт три связанных контура:

- домен проектов и репозиториев, основной сервис `project-catalog`;
- домен runtime и fleet в части `runtime-manager`;
- домен runtime и fleet в части `fleet-manager`;
- сервисный пакет `platform-mcp-server` как MCP-поверхность без бизнес-состояния.

`project-catalog` отвечает за:

- проекты и репозитории как локальную платформенную модель;
- проверенную операционную проекцию `services.yaml`;
- источники проектной и сервисной документации;
- правила веток, релизные политики, релизные линии и политику размещения;
- связи проекта с provider-native репозиториями через сохранённые provider-ссылки;
- команды, чтения и события `project.*`.

`runtime-manager` отвечает за:

- слоты и их жизненный цикл;
- подготовку workspace и статус материализация;
- platform jobs, steps, короткий хвост логов и ссылки на внешние runtime-артефакты;
- политики очистки, пакетную очистку, prewarm pool и безопасное переиспользование слотов;
- deploy-контур самого `runtime-manager`;
- команды, чтения и события `runtime.*`.

`fleet-manager` отвечает за:

- серверы, Kubernetes-кластеры, связность, health и placement scope;
- реестр нескольких серверов, scope и кластеров в MVP;
- bootstrap seed `platform-default` для одиночной установки;
- будущий выбор `fleet_scope_id` и `cluster_id` для runtime;
- события `fleet.*`.

`platform-mcp-server` отвечает за:

- MCP-поверхность инструментов платформы;
- проверку actor/source/run/session/slot binding;
- минимальную policy/auth boundary;
- маршрутизацию вызовов к сервисам-владельцам;
- безопасные ответы и ограниченную диагностику без хранения чужой бизнес-истины.

`codex-hook-ingress` отвечает за:

- приём нормализованных Codex hook events от hook emitter или локального sidecar;
- очистку и проверку безопасного hook envelope;
- маршрутизацию hook events в `agent-manager`, `runtime-manager`, `provider-hub` и `interaction-hub`;
- отделение command hooks Codex от MCP-протокола.

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
| RTM-3 | #658 | #676 | готово | Жизненный цикл слотов: reserve, extend lease, release, fail, чтения, идемпотентность и bootstrap-граница fleet. |
| RTM-4 | #659 | #683 | готово | Workspace материализация: `source_ref`, access mode, local paths, fingerprint, progress и безопасные ошибки подготовки. |
| RTM-5 | #660 | #687 | готово | Platform job MVP: job/step state machine, claim lease, progress, complete/fail/cancel, short log tail и runtime artifact refs. |
| RTM-6 | #661 | #691 | готово | Dockerfile, manifests, PostgreSQL bootstrap, migration job, `services.yaml`, smoke-путь, runbook и monitoring-документы. |
| RTM-7 | #662 | #696 | готово | Cleanup policy, cleanup batch, prewarm pool, deterministic slot reuse, очистка хвостов логов и видимость cleanup failures. |

Итог: `runtime-manager` имеет стабильные `v1` контракты, БД, миграции, gRPC-слой, outbox, deploy-контур, smoke-путь и реализованный runtime MVP по слотам, материализация workspace, platform jobs, cleanup, prewarm и reuse.

## Что уже сделано по `fleet-manager`

| Срез | Issue | Статус | Результат |
|---|---:|---|---|
| FLEET-0 | #699 | готово | Доменная документация, границы runtime/fleet, MVP с несколькими серверами, scope и кластерами, bootstrap seed `platform-default`, будущие контракты, план поставки и карта Issue. |
| FLEET-1 | #708 | готово | gRPC и AsyncAPI контракты `fleet-manager`, события `fleet.*`, сгенерированные Go-контракты и ключи действий доступа. |
| FLEET-2 | #714 | готово | Сервисный каркас, конфигурация, PostgreSQL-модель, миграции, repository, health/readiness, metrics и outbox без registry/health/placement команд. |
| FLEET-3 | #717 | готово | Registry-команды и чтения нескольких scope/server/cluster, bootstrap seed `platform-default`, проверки доступа, идемпотентность, optimistic concurrency, command result и outbox-события. |
| FLEET-4 | #726 | готово | Проверки связности Kubernetes API, health snapshots, чтения health, события `fleet.health.*`, command result и безопасное получение kubeconfig через `secretresolver`. |
| FLEET-5 | #730 | готово | Правила размещения, базовый `ResolvePlacement`, журнал placement decisions, проверки доступа, идемпотентность и outbox-события `fleet.placement.*`. |
| RTM-FLEET-1 | #735 | готово | `runtime-manager` вызывает `fleet-manager.ResolvePlacement` для `PrepareRuntime`, `ReserveSlot` и `CreateJob` без slot; fleet остаётся владельцем выбора кластера и журнала решений. |
| FLEET-6 | #738 | готово | Dockerfile, Kubernetes-манифесты, PostgreSQL bootstrap, migration job, `services.yaml`, smoke-путь, runbook и monitoring-документы `fleet-manager`. |

Итог: `fleet-manager` имеет сервисный процесс, БД, registry-поверхность, health-поверхность, placement rules, базовый `ResolvePlacement`, журнал решений и эксплуатационный контур. RTM-FLEET-1 готов: `runtime-manager` вызывает fleet decision для новых runtime-операций.

## Что зафиксировано по `platform-mcp-server`

| Срез | Issue | Статус | Результат |
|---|---:|---|---|
| MCP-0 | #747 | готово | Документационный пакет сервисной границы `platform-mcp-server`: ответственность, MVP-группы инструментов, безопасность, связи с hooks #698 и delivery-план. Код, proto и AsyncAPI не входят. |
| MCP-1 | #753 | готово | Стратегия контрактов: MCP tools описываются через MCP SDK, JSON Schema и snapshot-проверки `tools/list`; Codex hooks вынесены в `codex-hook-ingress`; YAML-каталог не является каноникой. Код, proto и AsyncAPI не входят. |

## Текущий бэклог агента #1

| Направление | Статус | Что осталось |
|---|---|---|
| Bootstrap пустого репозитория | модель выбрана, provider-side PR готов частично: #281, #748 | Проектная часть готова в `project-catalog`; выбран вариант C: `services.yaml` как Git-декларация, установки и шаблоны репозиториев хранятся в `package-hub`, provider-native запись идёт через `provider-hub`, материализация workspace выполняется в `runtime-manager`. PRV-8a умеет создать bootstrap branch/PR для заранее существующего пустого repo по готовым файлам и refs; end-to-end вызов, создание repo/base ref и adoption остаются отдельными срезами. |
| Adoption существующего репозитория | модель выбрана, ждёт реализации: #282 | Проектная часть готова в `project-catalog`; adoption должен поддерживать агентную роль и быстрый шаблонный режим после проверки конфликтов. |
| UI/gateway для проектов и runtime | запланировано позже | Делать после определения фактических экранов `web-console` и состава `staff-gateway` ручек. |
| `project.policy_override.expired` | запланировано позже | Контракт события есть; нужна логика обслуживания или platform job, которая будет снимать истёкшие переопределения как операционный срез. |
| Организационные runtime-политики | частично заблокировано | Cleanup для `organization` scope отклоняется, а prewarm хранит политику без фактической раскладки, пока runtime не получает проекцию организации на слоты. |
| Реальный исполнитель platform jobs | запланировано позже | `runtime-manager` хранит и выдаёт jobs; конкретный исполнитель на Kubernetes или агентный исполнитель нужен отдельным срезом после согласования с `agent-manager`/ops-контуром. |
| Интеграция `runtime-manager` с fleet placement | готово: #735 | RTM-FLEET-1 перевёл `PrepareRuntime`, `ReserveSlot` и `CreateJob` без slot на `fleet-manager.ResolvePlacement`; runtime сохраняет только `fleet_scope_id` и `cluster_id`. |
| Deploy-контур `fleet-manager` | готово: #738 | FLEET-6 добавил Dockerfile, manifests, PostgreSQL bootstrap, migration job, smoke, runbook и monitoring без изменения registry/health/placement бизнес-логики. |
| `platform-mcp-server` | готово: #747, #753 | MCP-0 фиксирует границы, группы инструментов, безопасность и план поставки; MCP-1 фиксирует стратегию контрактов через MCP SDK, JSON Schema и snapshot-проверки `tools/list`, а также отделяет Codex hooks в `codex-hook-ingress`. |
| `codex-hook-ingress` | решение выбрано: #753, ждёт реализации #698 | Входной контур hooks должен принимать нормализованные Codex hook events от hook emitter или sidecar; это не часть MCP-сервера и не закрывается кодом MCP-1. |

## Блокировки от `access-manager`

Снято:

- базовая проверка доступа для `project-catalog` и `runtime-manager` подключена через `access-manager`;
- action/scope ключи заведены в `accesscatalog` и используются сервисами;
- команды и чтения текущих срезов не блокируются отсутствием membership UI или полных организационных экранов.

Реальные оставшиеся блокировки:

- проектные экраны членства, групп и организаций должны опираться на `access-manager`, а не на локальные сущности `project-catalog`;
- организационный scope для cleanup/prewarm требует проекции организации или явного контракта, по которому `runtime-manager` сможет сопоставить слоты с организацией;
- подтверждение владельца и политики риска для части операций должны приходить из общего контура доступа и governance, а не дублироваться в доменных сервисах.

Нужны новые или уточнённые контракты:

- модель чтения членства, групп и организаций для операторских экранов проектов;
- контракт организационной проекции для runtime-политик;
- единый способ проверки подтверждение владельца для операций, которые запускаются из `staff-gateway`, MCP или agent-manager.

## Блокировки от `provider-hub`

Снято:

- `project-catalog` не обязан быть Git-клиентом и уже хранит provider-ссылки как часть привязки репозитория;
- `runtime-manager` не блокируется provider-доступом для текущих RTM-0..RTM-7 срезов.

Реальные оставшиеся блокировки:

- #281 и #282 требуют provider-native операций: создать или просканировать репозиторий, открыть bootstrap/adoption PR, связать provider Issue/PR/MR с локальными проектом и репозиторием; PRV-8a уже закрывает открытие bootstrap PR для заранее существующего пустого repo по готовым файлам и refs, но создание repo/base ref, проектный вызов и adoption scan остаются открытыми;
- `CreatePolicyEditProposal` в `project-catalog` сохраняет предложение, но создание PR с правкой `services.yaml` должно идти через provider-контур;
- workspace `source_ref` и сигналы после работы slot-агентов должны синхронизироваться с provider-проекциями, но runtime не должен напрямую ходить в GitHub/GitLab.

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
- контракт передачи требований пакета в материализация workspace;
- контракт запуска runtime-нагрузки для пакета или плагина после подтверждённой установки.

## Блокировки для других агентов

- `provider-hub` зависит от `project-catalog` при привязке provider-native объектов к локальному проекту и репозиторию.
- `package-hub` зависит от `project-catalog`, когда пакетные источники и руководящие пакеты становятся частью проектной политики.
- `agent-manager` зависит от `runtime-manager` для слотов, материализация workspace и platform jobs, но `Run` остаётся сущностью `agent-manager`.
- `runtime-manager` зависит от `fleet-manager` для целевого `ResolvePlacement`; RTM-FLEET-1 убрал локальный выбор кластера из `PrepareRuntime`, `ReserveSlot` и `CreateJob` без slot.
- `agent-manager` зависит от `platform-mcp-server` для будущих MCP-инструментов run/session/gate; MCP не владеет flow/role/prompt и не конфликтует с AGO-3.
- `agent-manager`, `runtime-manager`, `provider-hub` и `interaction-hub` зависят от `codex-hook-ingress` для будущего приёма Codex hook events; #698 остаётся отдельной реализационной задачей.
- `provider-hub` зависит от `platform-mcp-server` только как от внешней инструментальной поверхности; provider write pipeline остаётся у `provider-hub`, а bootstrap/adoption PRV-8a не переносится в MCP.
- `operations-hub` и будущий `staff-gateway` зависят от чтений `project-catalog` и `runtime-manager` для операторских экранов.

## Рекомендуемый следующий шаг

Для агента #1 нет незавершённого локального Wave 8, RTM или FLEET среза, который нужно закрыть до соседних доменов. После MCP-1 рационально идти в один из трёх вариантов:

- выполнить MCP-2: сервисный каркас `platform-mcp-server` без бизнес-маршрутов;
- дождаться `provider-hub` bootstrap/adoption контракта и закрывать #281/#282;
- перейти к runtime/fleet интеграции с реальным исполнителем platform jobs после согласования `agent-manager` и ops-контуров.
