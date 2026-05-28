# Агент #3 — пакетная платформа

## Зона ответственности

Агент #3 ведёт домен пакетной платформы. Основной сервис: `package-hub`.

Ответственность:
- источники пакетов;
- локальный доступный каталог пакетов;
- версии пакетов, manifest, схемы секретов и статусы проверки;
- установки пакетов в области platform, organization, project и repository;
- события `package.*`;
- доменная документация `docs/domains/package-platform/**`.

`package-hub` не владеет магазином пакетов как бизнес-продуктом, исходниками пакетов, runtime-нагрузками, Kubernetes-размещением, сырыми секретами, provider-native операциями, биллингом и UI.

## Что уже сделано

| Срез | Статус | Результат |
|---|---|---|
| PKG-1 | готово | Доменная документация, границы, модель, API-обзор и план поставки. |
| PKG-2 | готово | gRPC/AsyncAPI контракты, события и действия доступа. |
| PKG-3.1 | готово | Процесс сервиса, gRPC runtime, health и metrics. |
| PKG-3.2 | готово | PostgreSQL-модель источников, пакетов, версий, manifest и ценовых метаданных. |
| PKG-3.3 | готово | PostgreSQL-модель установок, схем секретов, проверок, идемпотентности и оптимистичная конкуренция. |
| PKG-3.4 | готово | Outbox, базовые чтения и команда проверки версии пакета. |
| PKG-4.1 | готово | Команды источников пакетов: подключить, обновить, отключить. |
| PKG-4.2 | готово | Синхронизация доступного каталога и проверка manifest. |
| PKG-5.1 | готово | Запрос установки пакета и чтения установок. |
| PKG-5.2 | готово | Изменение, отключение и снятие установки. |
| PKG-5.3a | готово | Чтение схем секретов версий пакетов и сохранение схем из manifest при синхронизации каталога. |
| PKG-5.3b | готово | Сверка заполненности секретов установки через `access-manager.ListPackageInstallationSecretRefs` и `secretresolver.Checker` без чтения значения секрета. |
| PKG-6.1 | готово | Специализация видов пакетов: `plugin`, `guidance`, `store`, `platform_content`; правила manifest и модели чтения через `package_kind`. |
| PKG-6.2 | готово | Руководящие пакеты читаются как `package_kind=guidance` через каталог, установки и manifest; использование в workspace остаётся за `agent-manager`. |
| PKG-6.3a | готово | Пакеты `store` и `platform_content` читаются через каталог, установки и manifest; бизнес-система магазина, provider-native синхронизация, checkout и runtime-размещение остаются вне `package-hub`. |
| PKG-7 | готово | Эксплуатационный контур `package-hub`: Dockerfile, Kubernetes manifests, migration job, config, health/metrics, smoke-проверка и runbook. |
| AGO-0 | готово | Временное переключение: стартовая доменная документация `agent-manager`, границы и междоменные интеграции. |
| AGO-1 | готово | Временное переключение: gRPC/AsyncAPI контракты `agent-manager`, события `agent.*` и действия доступа без сервисной реализации. |
| AGO-2 | готово | Временное переключение: сервисный каркас `agent-manager`, health/readiness/metrics, gRPC registration и outbox skeleton без БД, миграций, deploy и бизнес-операций. |
| AGO-3 | готово | Временное переключение: PostgreSQL-модель flow, stage, role, prompt template, версий, command result и service-local outbox; storage/use-case слой готов, gRPC handler wiring вынесен в следующий срез. |
| AGO-3b | готово | Временное переключение: gRPC handlers, casters и безопасное отображение ошибок для flow, role и prompt подключены к storage/use-case слою; session/run остаются вне среза. |
| AGO-4 | готово | Временное переключение: авторитетная модель session/run, слой хранения, use-case, gRPC handlers, результат команды, ожидаемая версия, защита активной session от дублей, stage-bound проверка роли и service-local outbox события для session/run готовы; руководящие пакеты, runtime и приёмка остаются следующими срезами. |
| AGO-5 | готово | Временное переключение: `agent-manager` читает активные guidance installations и manifest/version metadata через `package-hub`, фиксирует refs/digests/policy-safe summary в `AgentRun` и не сохраняет тексты пакетов, scripts, assets, package source или manifest payload. |
| AGO-6 | готово | Временное переключение: контекст руководящих пакетов в workspace зафиксирован; `AgentRun.guidance_refs` превращаются в `runtime.WorkspaceSource.kind=guidance_package`, runtime готовит путь `.kodex/guidance/<safe_local_name>` только для чтения, сгенерированный контекст живёт в `.kodex/context/agent-run.json`, а прямой checkout из `agent-manager` запрещён. |
| AGO-7 | готово | Временное переключение: при явно включённом `KODEX_AGENT_MANAGER_RUNTIME_PREPARATION_ENABLED` `StartAgentRun` читает workspace policy у `project-catalog`, собирает runtime request с project/source refs, role/run context и замороженными `guidance_refs`, вызывает `runtime-manager.PrepareRuntime` и фиксирует в `Run` только runtime refs, fingerprint/diagnostic summary и безопасный статус ошибки. До deploy wiring `agent-manager` подготовка runtime остаётся opt-in. |
| AGO-8 | готово | Временное переключение: `agent-manager` получил базовый machine acceptance lifecycle для `RequestAcceptance`, `RecordAcceptanceResult`, `GetAcceptanceResult` и `ListAcceptanceResults`; хранит только safe refs/status/bounded details, поддерживает idempotency, expected version и service-local outbox events без executor, QA runner, Human gate, governance decision engine и provider write pipeline. |
| AGO-9a | готово | Временное переключение: `agent-manager` получил intent-only follow-up lifecycle для `CreateFollowUpIntent`; хранит session/run/stage/acceptance refs, provider target refs, тип следующей provider-native задачи, safe title/summary/hints, idempotency trace и статус, публикует `agent.follow_up.requested` без executor, Human gate, QA runner и прямого provider write. |
| AGO-9b | готово | Временное переключение: `agent-manager` получил safe activity timeline для истории действий агента и tool calls; `RecordAgentActivity`/`ListAgentActivities` хранят только kind/status/tool metadata/timestamps/summary/digest/refs/details без raw tool input/output, stdout/stderr, prompt, transcript, provider payload и workspace paths. |
| AGO-9c | готово | Временное переключение: `agent-manager` получил `DispatchFollowUpIntent` для create-path provider follow-up; команда резервирует dispatch до provider write, вызывает typed `provider-hub.CreateIssue` с deterministic provider command id, сохраняет только `provider_operation_ref`, safe result refs и статус `created`/`failed`, поддерживает idempotency/expected version и не ходит напрямую в GitHub/GitLab. |
| AGO-9c.1 | готово | Временное переключение: `DispatchFollowUpIntent` приведён к единственной целевой typed-модели: явный `FollowUpDispatchKind` и typed `oneof` поддерживают `create_issue`, `update_issue`, `create_comment`, `update_comment`; `agent-manager` вызывает только typed provider-hub операции, сохраняет safe refs/status/operation ref, поддерживает reserve-before-write, idempotency и expected version. |
| AGO-9c.2 | готово | Временное переключение: typed follow-up dispatch расширен на `update_pull_request` и `create_review_signal`; `agent-manager` вызывает только `provider-hub.UpdatePullRequest`/`CreateReviewSignal`, строго сверяет PR/MR target refs, требует provider expected version для PR update, сохраняет safe refs/status/operation ref и отделяет provider-native review signal от governance decision. |
| AGO-9d | готово | Временное переключение: `agent-manager` получил Human gate wait/result lifecycle; хранит orchestration state, interaction/governance refs и normalized outcome `approve`/`reject`/`request_changes`/`answer`, но не владеет transport delivery, governance decision body или raw payload. |
| AGO-10 | готово | Временное переключение: эксплуатационный контур `agent-manager` готов для первого backend deploy: Dockerfile, Kubernetes manifests, migration job, PostgreSQL bootstrap/env/secret wiring, `services.yaml`, smoke-путь, runbook и monitoring docs. |
| AGO-11 | готово | Временное переключение: `agent-manager` получил Human gate resume consumer; безопасно потребляет `interaction.request.response_recorded` из platform event log, находит ожидание по owner-side ref, записывает normalized outcome через существующий lifecycle и не меняет producer/transport `interaction-hub`. |
| AGO-12 | готово | Временное переключение: `agent-manager` получил request-side интеграцию Human gate с `interaction-hub.RequestHumanGate`; при включённом runtime switch создаёт transport request после локального replay-check, сохраняет только `interaction_request_ref`, safe summary/status/refs и не переносит owner inbox, callback body или delivery lifecycle из `interaction-hub`. |
| AGO-13 | готово | Временное переключение: таксономия исходов Human gate выровнена с `interaction-hub`; request-side `RequestHumanGate` передаёт `approve`/`reject`/`request_changes`/`answer`, обработчик события нормализует те же исходы из `interaction.request.response_recorded`, а `reject` и `request_changes` разделены по смыслу без хранения raw response payload. |

## Текущий бэклог

| Срез | Статус | Почему не завершён |
|---|---|---|
| PKG-6.3b+ | частично заблокировано | Реальная синхронизация источников из Git/store зависит от `provider-hub`, пакета магазина и адаптера источника. |

## Блокировки от других доменов

| Домен или сервис | Что блокирует | Решение |
|---|---|---|
| `project-catalog` | Привязку пакетных источников и руководящих пакетов к проектной политике. | `package-hub` не должен владеть проектной политикой; ждём готовую модель проекта. |
| `provider-hub` | Получение пакетов и каталогов из Git/provider-native источников. | `package-hub` принимает нормализованный снимок, а adapter/provider-контур получает исходные данные. |
| `agent-manager` | Монтирование руководящих пакетов в workspace агента. | Контракт готов: `agent-manager` замораживает безопасные refs, а runtime-контур получает источники `guidance_package`. |
| `platform-mcp-server` | Чтение установок, manifest и руководящих пакетов через MCP-инструменты. | MCP только маршрутизирует чтения к `package-hub`; пакетная истина и установки остаются у `package-hub`. |
| `runtime-manager` и `fleet-manager` | Запуск runtime-нагрузок пакетов и размещение в Kubernetes. | `package-hub` публикует событие установки и хранит требования; runtime/fleet исполняют. |
| Bootstrap/adoption #281/#282 | Использование руководящих пакетов, пакетов из магазина, шаблонов репозиториев и внешних источников при подключении репозитория. | Выбран вариант C из `docs/platform/architecture/repository_onboarding.md`: Git submodule не обязателен, workspace собирается из `services.yaml`, установленных пакетов, шаблонов и `source_ref`. |

## Рекомендуемый следующий шаг

После PKG-5.3b и PKG-7 независимого package-hub среза без соседних доменов почти не осталось. Интеграционные сценарии магазина продолжают ждать `provider-hub`, внешний адаптер магазина и runtime/fleet-контур. В `agent-orchestration` после AGO-13 рациональный следующий срез — MCP/tool boundary для запуска orchestration-команд или дальнейшая связка Human gate с governance policy refs, без смешивания с executor, QA runner и прямым provider write из `agent-manager`.

## Временное переключение

Агент #3 временно выполняет AGO-0..AGO-13 в домене `agent-orchestration`, чтобы зафиксировать стартовые границы `agent-manager`, его transport-контракты, сервисный каркас, модель хранения flow/role/prompt/session/run/activity/acceptance/follow-up intent/Human gate wait-result, gRPC-доступ к этим операциям, защиту session/run инвариантов, зависимость от `package-hub` для чтения установленных руководящих пакетов, границу передачи guidance refs в runtime workspace, opt-in вызов `runtime-manager.PrepareRuntime` без checkout из `agent-manager`, базовый lifecycle machine acceptance, intent-only follow-up, safe activity timeline, typed follow-up dispatch через `provider-hub.CreateIssue`/`UpdateIssue`/`CreateComment`/`UpdateComment`/`UpdatePullRequest`/`CreateReviewSignal`, Human gate refs/outcome без хранения raw payload, event-driven resume по safe `interaction.request.response_recorded`, request-side создание Human gate через typed `interaction-hub.RequestHumanGate`, выровненную таксономию `approve`/`reject`/`request_changes`/`answer` и эксплуатационный контур первого backend deploy. Код `package-hub` в этих срезах не меняется.
