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
| Wave 8.5 | #633 | #652 | готово | Правила веток, релизная политика, политика размещения, Dockerfile, Kubernetes-манифесты, migration job и путь проверки готовности. |
| Wave 8 closeout | #633 | #654 | готово | Статусы Wave 8, карты Issue и документы поставки приведены к завершённому состоянию. |
| ONB-1 | #794 | готово | `CreateRepositoryBootstrapPullRequest` готовит project-side bootstrap-контекст для существующего binding и вызывает `provider-hub CreateBootstrapPullRequest` без Git-клиента, генерации шаблона и adoption scan. |
| ONB-2 | #810 | готово | `CreateProviderRepository` резервирует pending project-owned repository binding, вызывает `provider-hub CreateRepository`, сохраняет безопасные provider refs и `base_branch` для последующего bootstrap PR. |
| ONB-3 | #818 | готово | `ImportBootstrapServicesPolicy` принимает проверенный merge/artifact-сигнал, импортирует checked `services.yaml` и атомарно переводит pending repository binding в `active` без прямого GitHub/GitLab доступа. |
| ONB-4 | #864 | готово | `ReconcileBootstrapMergeSignal` принимает safe provider bootstrap merge signal и checked artifact metadata, валидирует signal/artifact/binding и вызывает `ImportBootstrapServicesPolicy` без GitHub/GitLab-клиента в `project-catalog`. |
| ONB-5 | #881 | готово | `project-catalog` сохраняет project-side `OnboardingSignalReconciliation` для bootstrap merge signal: безопасный fingerprint, refs, artifact metadata, итоговый статус, короткую сводку и безопасный error code/summary без raw provider payload. |
| Event consumer | #893 | готово | Общий `libs/go/eventconsumer` читает `platform-event-log` через `eventlog.Store` с lease/checkpoint, handler registry, retry/backoff и safe diagnostics; consumer `project-catalog` принимает `provider.repository.bootstrap_merged`, восстанавливает safe signal input, вызывает `ReconcileBootstrapMergeSignal` при наличии checked artifact/payload и фиксирует `OnboardingSignalReconciliation(needs_review)` без импорта, если событие не содержит checked artifact input. |
| Adoption import | #917 | готово | `project-catalog` принимает `provider.repository.adoption_merged` через отдельный event consumer, вызывает `ReconcileAdoptionMergeSignal` при наличии checked artifact/payload, импортирует checked `services.yaml` projection и активирует или обновляет repository binding; lightweight scan snapshot остаётся planning-сигналом и не импортируется как policy. |

Итог: `project-catalog` имеет стабильные `v1` контракты, БД, миграции, gRPC-слой, outbox-публикацию в `platform-event-log`, deploy-манифесты и контур проверок готовности. Операции из Wave 8 реализованы; ONB-1 добавил project-side bootstrap команду для уже существующего repository binding, ONB-2 добавил project-side создание provider repo/base ref через `provider-hub` и связывание результата с binding, ONB-3 добавил импорт проверенной политики после merge bootstrap PR и активацию binding, ONB-4 добавил явный reconciliation path от safe provider merge signal к import use-case, #893 добавил общий event consumer runtime и первый project-side consumer safe merge signal, а #917 добавил симметричный adoption import path после checked adoption merge signal.

## Что уже сделано по `runtime-manager`

| Срез | Issue | PR | Статус | Результат |
|---|---:|---:|---|---|
| RTM-0 | #655 | #664 | готово | Доменная документация, границы runtime/fleet, карта Issue и план поставки. |
| RTM-1 | #656 | #669 | готово | gRPC/AsyncAPI контракты `runtime-manager`, события и сгенерированные Go-контракты. |
| RTM-2 | #657 | #672 | готово | Сервисный каркас, PostgreSQL-модель, миграции, repository, health/readiness, outbox и базовые тесты. |
| RTM-3 | #658 | #676 | готово | Жизненный цикл слотов: reserve, extend lease, release, fail, чтения, идемпотентность и bootstrap-граница fleet. |
| RTM-4 | #659 | #683 | готово | Workspace материализация: `source_ref`, access mode, local paths, fingerprint, progress и безопасные ошибки подготовки. |
| RTM-5 | #660 | #687 | готово | Platform job MVP: job/step state machine, claim lease, progress, complete/fail/cancel, short log tail и runtime artifact refs. |
| RTM-6 | #661 | #691 | готово | Dockerfile, manifests, PostgreSQL bootstrap, migration job, `services.yaml`, путь проверки готовности, runbook и monitoring-документы. |
| RTM-7 | #662 | #696 | готово | Cleanup policy, cleanup batch, prewarm pool, deterministic slot reuse, очистка хвостов логов и видимость cleanup failures. |
| RTM-K8S-1 | #940 | готово | Основа Kubernetes-исполнителя: включаемый явно исполнитель заданий `health_check` получает ссылку на секрет кластера через `fleet-manager.GetKubernetesCluster`, создаёт ограниченный Kubernetes Job через `client-go` и завершает runtime job через `ReportJobStepProgress`, `CompleteJob` или `FailJob` без хранения kubeconfig, значений секретов и больших логов. В том же runtime-контракте добавлен канонический тип задания `agent_run` для запуска agent Run без подмены на `build`/`deploy`/`housekeeping`; отдельный исполнитель остаётся за следующим срезом `agent-manager`/runtime. |
| RTM-K8S-2 | #949 | готово | Kubernetes job worker укреплён вокруг реального исполнения `health_check`: остановка процесса не превращается в `FailJob`, повторный claim переиспользует детерминированный Kubernetes Job, терминальные состояния Kubernetes Job классифицируются безопасными кодами, а ошибки claim/report/complete повторяются с увеличивающейся задержкой. |
| RTM-K8S-3 | #961 | готово | Зафиксирован typed `AgentRunExecutionSpec` для будущего безопасного исполнения `agent_run`: refs на Run/slot/materialization/workspace/context, digest/fingerprint, runner profile/image, фиксированный runner mode, secret refs без значений и reporting target refs. `runtime-manager` валидирует spec, сверяет завершённую materialization и не отдаёт `agent_run` без spec в claim для исполнения. |
| RTM-K8S-4 | #966 | готово | Kubernetes job worker и executor запускают `JOB_TYPE_AGENT_RUN` через ограниченный Kubernetes Job на базе typed `AgentRunExecutionSpec`: runner image/profile/context/workspace refs берутся из spec, команда runner-а фиксирована, workspace монтируется через PVC, secret refs передаются только как ссылки без значений, kubeconfig, prompt/transcript и большие логи не сохраняются. |

Итог: `runtime-manager` имеет стабильные `v1` контракты, БД, миграции, gRPC-слой, outbox, deploy-контур, путь проверки готовности и реализованный runtime MVP по слотам, материализация workspace, platform jobs, cleanup, prewarm и reuse.

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
| FLEET-6 | #738 | готово | Dockerfile, Kubernetes-манифесты, PostgreSQL bootstrap, migration job, `services.yaml`, путь проверки готовности, runbook и monitoring-документы `fleet-manager`. |

Итог: `fleet-manager` имеет сервисный процесс, БД, registry-поверхность, health-поверхность, placement rules, базовый `ResolvePlacement`, журнал решений и эксплуатационный контур. RTM-FLEET-1 готов: `runtime-manager` вызывает fleet decision для новых runtime-операций.

## Что зафиксировано по `platform-mcp-server`

| Срез | Issue | Статус | Результат |
|---|---:|---|---|
| MCP-0 | #747 | готово | Документационный пакет сервисной границы `platform-mcp-server`: ответственность, MVP-группы инструментов, безопасность, связи с hooks #698 и delivery-план. Код, proto и AsyncAPI не входят. |
| MCP-1 | #753 | готово | Стратегия контрактов: MCP-инструменты описываются через MCP SDK, JSON Schema и snapshot-проверки `tools/list`; Codex hooks вынесены в `codex-hook-ingress`; YAML-каталог не является каноникой. Код, proto и AsyncAPI не входят. |
| MCP-2 | #760 | готово | Сервисный каркас: процесс, конфигурация через env, health/readiness/metrics, MCP Streamable HTTP, проверка bearer-токена, `diagnostics.mcp_status.read`, каталог маршрутов к сервисам-владельцам и snapshot-проверка `tools/list`. Бизнес-маршруты, входной контур hooks, хранилище skills и манифесты выкладки не входят. |
| MCP-3 | #771 | готово | Подключены первые инструменты `agent-manager` к готовой поверхности владельца: старт сессии, старт `Run`, запись состояния `Run`, запись session snapshot и безопасная диагностика run context. Acceptance, follow-up и Human gate не регистрируются до реализации владельца. |
| MCP-3g | #830 | готово | Подключены governance-инструменты жизненного цикла gate поверх GOV-4: `governance.gate.request/get/list/submit_decision/cancel/expire` маршрутизируются в `governance-manager`, возвращают только безопасные ссылки, статусы и сводки и не хранят состояние решений в MCP. |
| MCP-3r | #841 | готово | Подключены governance-инструменты оценки риска поверх GOV-5: `governance.risk.evaluate/reevaluate/get/list` маршрутизируются в `governance-manager`, принимают только типизированные ссылки и ограниченные сводки и возвращают assessment refs/status/risk class, matched rule refs/counts, required gate refs, version/timestamps. |
| MCP-3d | #852 | готово | Подключены инструменты релизных решений поверх GOV-6: package prepare/get/list, decision request/submit/get/list, blocking signal record/resolve/list и safety-loop record/get маршрутизируются в `governance-manager`, возвращают только безопасные ссылки, статусы, сводки, счётчики и version/timestamps и не хранят состояние release в MCP. |
| MCP-4 | #780 | готово | Подключены инструменты чтения и записи provider-данных к реализованной поверхности `provider-hub`: проекции, комментарии, связи, artifact signal, операции Issue/PR/comment/review, создание репозитория и bootstrap/adoption PR. MCP не ходит напрямую в GitHub/GitLab, не хранит provider-состояние и не возвращает сырой provider payload. |
| MCP-4o | #933 | готово | Подключены маршруты к готовым сервисным поверхностям владельцев: `agent.human_gate.request/get/list` через `agent-manager`, `interaction.owner_inbox.list/get/respond` через `interaction-hub`, `governance.signal.record_review/list_review` через `governance-manager`. MCP не хранит состояние Human gate, входящих задач владельца или review signals. |

## Текущий бэклог агента #1

| Направление | Статус | Что осталось |
|---|---|---|
| Bootstrap пустого репозитория | ONB-1, ONB-2, ONB-3, ONB-4, ONB-5 и #893 готовы, полный сценарий открыт: #281, #748 | `project-catalog` владеет проектной политикой и binding: `CreateProviderRepository` создаёт provider repo/base ref через `provider-hub CreateRepository` и сохраняет безопасные refs в pending binding; `CreateRepositoryBootstrapPullRequest` проверяет существующий binding, provider target, `base_branch`, prepared files, watermark и checked `services.yaml`, затем вызывает `provider-hub CreateBootstrapPullRequest`; `ReconcileBootstrapMergeSignal` принимает safe provider merge signal и checked artifact metadata, валидирует signal/artifact/binding, ведёт safe `OnboardingSignalReconciliation` journal и вызывает `ImportBootstrapServicesPolicy`; `ImportBootstrapServicesPolicy` импортирует checked projection и переводит binding в `active`; consumer `provider.repository.bootstrap_merged` доставляет safe merge signal из `platform-event-log`, запускает полный import path при наличии checked artifact/payload и фиксирует `needs_review`, если checked artifact input ещё не передан. Выбор и применение шаблона остаётся отдельным срезом. |
| Adoption существующего репозитория | project-side import готов: #917, полный сценарий открыт: #282 | Provider-side lightweight scan snapshot готов как safe planning signal, но он не содержит checked `services.yaml` payload и не импортируется как policy. Project-side consumer `provider.repository.adoption_merged` принимает только safe merge signal с checked artifact/payload, вызывает `ReconcileAdoptionMergeSignal`, ведёт safe journal и импортирует checked projection без прямого GitHub/GitLab доступа. |
| UI/gateway для проектов и runtime | запланировано позже | Делать после определения фактических экранов `web-console` и состава `staff-gateway` ручек. |
| `project.policy_override.expired` | запланировано позже | Контракт события есть; нужна логика обслуживания или platform job, которая будет снимать истёкшие переопределения как операционный срез. |
| Организационные runtime-политики | частично заблокировано | Cleanup для `organization` scope отклоняется, а prewarm хранит политику без фактической раскладки, пока runtime не получает проекцию организации на слоты. |
| Реальный исполнитель platform jobs | первый Kubernetes-срез готов: #940, надёжность worker-а готова: #949, контракт `agent_run` готов: #961, первый executor `agent_run` готов: #966 | `runtime-manager` исполняет безопасное задание `health_check` и `agent_run` с валидным `AgentRunExecutionSpec` через включаемый явно Kubernetes-исполнитель. Worker устойчив к остановке процесса, повторному claim и повторной сверке уже созданного Kubernetes Job. `agent_run` без spec не claim-ится, а `agent_run` со spec запускается через фиксированный runner command, workspace PVC mount и safe refs без значений секретов. Задания `build`/`deploy`, нагрузки slot-агента, исполнитель workspace materialization и расширенные типы заданий остаются отдельными срезами после согласования с `agent-manager`/ops-контуром. |
| Интеграция `runtime-manager` с fleet placement | готово: #735 | RTM-FLEET-1 перевёл `PrepareRuntime`, `ReserveSlot` и `CreateJob` без slot на `fleet-manager.ResolvePlacement`; runtime сохраняет только `fleet_scope_id` и `cluster_id`. |
| Deploy-контур `fleet-manager` | готово: #738 | FLEET-6 добавил Dockerfile, manifests, PostgreSQL bootstrap, migration job, runbook и monitoring без изменения registry/health/placement бизнес-логики. |
| `platform-mcp-server` | готово: #747, #753, #760, #771, #780, #830, #841, #852, #933 | MCP-0 фиксирует границы, группы инструментов, безопасность и план поставки; MCP-1 фиксирует стратегию контрактов через MCP SDK, JSON Schema и snapshot-проверки `tools/list`, а также отделяет Codex hooks в `codex-hook-ingress`; MCP-2 добавляет сервисный каркас; MCP-3 подключает первые маршруты к `agent-manager`; MCP-3g подключает жизненный цикл gate к `governance-manager`; MCP-3r подключает оценку риска к `governance-manager`; MCP-3d подключает релизные решения к `governance-manager`; MCP-4 подключает маршруты чтения и записи provider-данных к `provider-hub`; MCP-4o подключает Human gate, входящие задачи владельца и review signals к готовым операциям сервисов-владельцев без хранения бизнес-состояния в MCP. |

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

- #281 и #282 требуют provider-native операций: создать или просканировать репозиторий, открыть bootstrap/adoption PR, связать provider Issue/PR/MR с локальными проектом и репозиторием; PRV-8a закрывает provider-side открытие bootstrap PR для заранее существующего пустого repo по готовым файлам и refs, ONB-1 закрывает project-side вызов для существующего binding, ONB-2 закрывает project-side создание provider repo/base ref, ONB-3 закрывает project-side import checked policy after merge, ONB-4 закрывает явный project-side reconciliation command от safe provider merge signal к import use-case, ONB-5 закрывает project-side journal, #893 закрывает общий consumer framework и event-driven delivery path safe bootstrap merge signal с checked artifact input, #917 закрывает project-side adoption import после checked adoption merge signal, но полный adoption сценарий остаётся открытым;
- `CreatePolicyEditProposal` в `project-catalog` сохраняет предложение, но создание PR с правкой `services.yaml` должно идти через provider-контур;
- workspace `source_ref` и сигналы после работы slot-агентов должны синхронизироваться с provider-проекциями, но runtime не должен напрямую ходить в GitHub/GitLab.

Нужны новые или уточнённые контракты:

- следующий adoption-срез должен связать Go checks/CLI/readiness с journal `adoption_merge` и показать владельцу разницу между lightweight scan snapshot и checked artifact import;
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
- `agent-manager` зависит от `platform-mcp-server` для MCP-инструментов run/session/Human gate и будущих flow-связок; MCP не владеет flow/role/prompt и не конфликтует с AGO-3. Risk assessment, review signals, gate decision, release decision package, release decision, blocking signal и safety-loop state остаются у `governance-manager`, а MCP-3g/MCP-3r/MCP-3d/MCP-4o дают только тонкие governance-маршруты.
- `agent-manager`, `runtime-manager`, `provider-hub`, `governance-manager` и `interaction-hub` зависят от `codex-hook-ingress` для будущего приёма Codex hook events; домен ведёт агент #5, а #1 сохраняет только историческую связь через разделение MCP и hooks.
- `provider-hub` зависит от `platform-mcp-server` только как от внешней инструментальной поверхности; provider write pipeline остаётся у `provider-hub`, а MCP-4 только маршрутизирует реализованные операции и не переносит bootstrap/adoption бизнес-логику в MCP.
- `operations-hub` и будущий `staff-gateway` зависят от чтений `project-catalog` и `runtime-manager` для операторских экранов.

## Рекомендуемый следующий шаг

Для агента #1 нет незавершённого локального Wave 8, RTM или FLEET среза, который нужно закрыть до соседних доменов. После #933 рационально идти в один из трёх вариантов:

- продолжить MCP чтения project/runtime/fleet/package через сервисы-владельцы без хранения бизнес-состояния в MCP;
- продолжить bootstrap пустого репозитория следующим ONB-срезом: закрыть детерминированный template executor без переноса шаблонов в `project-catalog` либо связать Go checks/CLI с adoption journal для существующего репозитория;
- перейти к runtime/fleet интеграции с реальным исполнителем platform jobs после согласования `agent-manager` и ops-контуров.
