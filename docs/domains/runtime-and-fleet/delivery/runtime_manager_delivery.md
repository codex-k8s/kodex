---
doc_id: DLV-CK8S-RUNTIME-MANAGER
type: delivery-plan
title: kodex — поставка runtime-manager
status: active
owner_role: EM
created_at: 2026-05-07
updated_at: 2026-05-29
related_issues: [655, 656, 657, 658, 659, 660, 661, 662, 949, 966, 975]
related_prs: []
related_docsets:
  - docs/domains/runtime-and-fleet/product/requirements.md
  - docs/domains/runtime-and-fleet/architecture/design.md
  - docs/domains/runtime-and-fleet/architecture/data_model.md
  - docs/domains/runtime-and-fleet/architecture/api_contract.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-07-runtime-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-07
---

# Поставка runtime-manager

## TL;DR

`runtime-manager` поставляется малыми PR-срезами: сначала доменная документация, затем контракты, сервисный каркас и БД, жизненный цикл слотов, подготовка workspace, platform jobs, эксплуатационный контур, cleanup/prewarm/reuse и интеграция с fleet placement. `fleet-manager` остаётся отдельным сервисом-владельцем серверов, кластеров, health и placement decisions; runtime вызывает его для новых слотов и jobs без slot.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования домена | `docs/domains/runtime-and-fleet/product/requirements.md` |
| Дизайн домена | `docs/domains/runtime-and-fleet/architecture/design.md` |
| Модель данных | `docs/domains/runtime-and-fleet/architecture/data_model.md` |
| API-обзор | `docs/domains/runtime-and-fleet/architecture/api_contract.md` |
| Карта Issue | `docs/delivery/issue-map/domains/runtime-and-fleet.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---|---|
| RTM-0 | #655 | Доменная документация, границы runtime/fleet, карта Issue и план поставки готовы. |
| RTM-1 | #656 | gRPC и AsyncAPI контракты `runtime-manager`, события и сгенерированные Go-контракты готовы. |
| RTM-2 | #657 | Сервисный каркас, PostgreSQL-модель, миграции, repository, health/readiness, outbox и базовые тесты готовы. |
| RTM-3 | #658 | Жизненный цикл слотов готов: reserve, extend lease, release, fail, fleet refs, проверка доступа через `access-manager` и `runtime.slot.*` события. |
| RTM-4 | #659 | Workspace materialization готова: source refs, writable/read-only, local paths, fingerprint и ошибки подготовки. |
| RTM-5 | #660 | Platform job MVP готов: job/step state machine, short log tail, full log ref, executor boundary и `runtime.job.*` события. |
| RTM-6 | #661 | Эксплуатационный контур готов: Dockerfile, manifests, DB bootstrap, migration job, `services.yaml`, путь проверки готовности и runbook. |
| RTM-7 | #662 | Cleanup, retention, prewarm pool, deterministic reuse и видимость cleanup failures готовы. |
| RTM-FLEET-1 | #735 | `runtime-manager` переключён на `fleet-manager.ResolvePlacement` для `PrepareRuntime`, `ReserveSlot` и `CreateJob` без slot. |
| RTM-K8S-2 | #949 | Kubernetes job worker устойчив к остановке процесса, повторному claim, удалённому Kubernetes Job, таймауту и частым ошибкам claim/report/complete. |
| RTM-K8S-4 | #966 | Kubernetes job worker и executor запускают `JOB_TYPE_AGENT_RUN` через ограниченный Kubernetes Job на базе typed `AgentRunExecutionSpec` без произвольной команды, значений секретов, kubeconfig, prompt/transcript и больших логов. |
| RTM-RUNNER-1 | #975 | Добавлена рабочая нагрузка `agent-runner`: образ для эксплуатации содержит `/kodex/bin/agent-runner`, команда `run` читает смонтированный `.kodex/context/agent-run.json`, сверяет digest/fingerprint и сообщает безопасные статусы в `agent-manager` через существующие gRPC-команды. |
| RTM-RUNNER-2 | #990 | Зафиксирован contract-first `CodexSessionExecutionSpec` для будущего запуска Codex CLI: instruction/result schema refs и digest, snapshot, hook/callback refs, timeout, fixed runner profile, output/result refs и secret refs без значений; runner валидирует spec и без фактического Codex executor завершает job `agent_execution_contract_unavailable`. |
| RTM-RUNNER-3 | без отдельного Issue | `agent-runner` запускает `codex exec` по полному `CodexSessionExecutionSpec` для workspace refs `workspace://.kodex/execution/...`: production image содержит `/usr/local/bin/codex` и проверяет CLI при сборке, runner сверяет instruction/result schema digest, передаёт instruction через stdin, использует fixed executable/args/profile sandbox, сообщает `running`/`completed`/`failed` через `ReportAgentRunState` и сохраняет только bounded summary/result digest/schema ref без raw prompt, transcript, stdout/stderr, tool payload, provider payload, kubeconfig и secret values. |
| RTM-RUNNER-E2E | без отдельного Issue | Kubernetes executor передаёт `agent-runner` конфигурацию отчёта в `agent-manager`: адрес сервиса идёт строкой, gRPC token подключается только через Kubernetes `SecretKeyRef`, а runtime job сохраняет только безопасные refs. Это замыкает цепочку со стороны runtime `JOB_TYPE_AGENT_RUN` -> Kubernetes Job -> `agent-runner` -> `codex exec` -> `ReportAgentRunState` для уже материализованных workspace refs `workspace://.kodex/execution/...`. |

## Таблица реализации

Контракты зафиксированы в `proto/kodex/runtime/v1/runtime_manager.proto` и `specs/asyncapi/runtime-manager.v1.yaml`; Go-артефакты генерируются в `proto/gen/go/kodex/runtime/v1/**` и `libs/go/platformevents/runtime/events.gen.go`.

| Группа | Контракт | Реализация |
|---|---|---|
| Слоты | Готов: `PrepareRuntime`, `ReserveSlot`, `ExtendSlotLease`, `ReleaseSlot`, `MarkSlotFailed`, `GetSlot`, `ListSlots`, события `runtime.slot.*`. | Команды `ReserveSlot`, `ExtendSlotLease`, `ReleaseSlot`, `MarkSlotFailed`, чтения `GetSlot`/`ListSlots`, идемпотентность с actor scope, проверка доступа через `access-manager`, проверка версии агрегата, lease expiry guard и вызов `fleet-manager.ResolvePlacement` готовы. `PrepareRuntime` готов как фасад: получает fleet decision, создаёт слот и запускает подготовку workspace одной идемпотентной командой. |
| Workspace materialization | Готов: старт, отчёт прогресса, чтения и события `runtime.workspace.*`. | Готовы команды `StartWorkspaceMaterialization`, `ReportWorkspaceMaterializationProgress`, чтения `GetWorkspaceMaterialization`/`ListWorkspaceMaterializations`, хранение нормализованных source refs, access mode, local path, fingerprint и безопасных ошибок подготовки. При старте слот переходит в `materializing`, при успехе в `ready`, при ошибке в `failed`. Runtime проверяет совпадение проекта workspace policy и слота, а слот хранит активную попытку подготовки для защиты от поздних отчётов старых исполнителей. |
| Platform jobs | Готов: создание, claim с `lease_token`, progress, complete/fail/cancel, чтения и события `runtime.job.*`. | Команды `CreateJob`, `ClaimRunnableJob`, `ReportJobStepProgress`, `CompleteJob`, `FailJob`, `CancelJob`, чтения `GetJob`/`ListJobs`, PostgreSQL repository, проверка доступа и gRPC-подключение готовы. `CreateJob` без slot получает fleet-ссылки через `ResolvePlacement`; `CreateJob` со slot наследует refs из slot. Исполнитель получает короткий lease и одноразовый `lease_token`. `agent_run` является отдельным каноническим типом задания для agent Run и не подменяется `build`/`deploy`/`housekeeping`; typed `AgentRunExecutionSpec` хранит безопасные refs/digest/fingerprint, сверяется со slot и завершённой materialization, а задание без spec остаётся ожидающим с безопасной диагностикой и не claim-ится для исполнения. Kubernetes-исполнитель готов для заданий `health_check` и `agent_run` с валидным spec: он включается явным env-флагом, читает ссылку на секрет кластера через `fleet-manager`, создаёт ограниченный Kubernetes Job через `client-go`, различает остановку worker-а и терминальные ошибки Kubernetes Job, переиспользует детерминированный Kubernetes Job при повторном claim и завершает runtime job через штатные lifecycle-команды. Для `agent_run` используются image/profile/context/workspace refs из spec, фиксированная команда runner-а, PVC mount и env только со safe refs без значений секретов. Адрес `agent-manager` передаётся runner-у строкой, а gRPC token подключается через Kubernetes `SecretKeyRef`, поэтому `runtime-manager` не читает и не сохраняет значение token. Optional `CodexSessionExecutionSpec` добавляет safe refs Codex CLI запуска без prompt body: checked execution input материализуется отдельным объектом или файлом workspace и читается только по ref/digest. `agent-runner` проверяет смонтированный context, digest, fingerprint и execution spec; для workspace refs `workspace://.kodex/execution/...` вызывает `codex exec` фиксированным executable/args, передаёт instruction через stdin и schema через `--output-schema`, затем через `agent-manager.ReportAgentRunState` фиксирует безопасный `running`/`completed`/`failed` state. Object-store refs требуют отдельного безопасного механизма чтения и не исполняются через fallback. |
| Runtime artifact refs | Готов: запись и чтение ссылок на внешние runtime-артефакты. | Команды `RecordRuntimeArtifactRef`/`ListRuntimeArtifactRefs` готовы; PostgreSQL хранит только ссылку, digest и ограниченную диагностику без blob, полного лога или registry catalog. |
| Cleanup/prewarm/reuse | Готов: политики очистки, пакетная очистка, prewarm pool и события cleanup/prewarm. | Готовы команды `CreateOrUpdateCleanupPolicy`, `RunCleanupBatch`, `CreateOrUpdatePrewarmPool`, `ReconcilePrewarmPool`, PostgreSQL repository, проверка доступа и gRPC-подключение. Очистка переводит устаревшие слоты в `cleaned`, очищает короткие хвосты логов job и job step по политике и публикует видимый `runtime.cleanup.failed`, если очистку блокирует активная работа. Cleanup policy временно отклоняет `organization` scope, пока runtime не получает проекцию организации для слотов. Prewarm pool создаёт базовые `code_only` слоты под runtime profile; `ReserveSlot` переиспользует только безопасный prewarmed/ready слот с совпадающим fingerprint и совместимым project/repository scope. Организационный scope для prewarm фиксируется как состояние политики, но остаётся `insufficient`, пока runtime не получает проекцию организации для слотов. |
| Deploy/manifests | Не gRPC-группа. | Готовы Dockerfile, service/deployment manifests, migration job, `services.yaml`, DB bootstrap wiring, путь проверки готовности и эксплуатационные документы. |

## Эксплуатационный контур

`runtime-manager` разворачивается как внутренний сервис с двумя портами:
- `http:8080` для `/health/livez`, `/health/readyz` и будущих технических метрик;
- `grpc:9090` для внутреннего `RuntimeManagerService`.

Порядок выкладки:
1. PostgreSQL stack и `kodex-postgres-bootstrap-databases`.
2. `platform-event-log-migrations`.
3. `access-manager-migrations` и `access-manager`.
4. `runtime-manager-migrations`.
5. `runtime-manager`.

Runtime-сервис использует:
- собственную БД `kodex_runtime_manager`;
- общий event log через `KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_DSN`;
- shared token для входящего gRPC;
- `access-manager` как проверку доступа для runtime-команд и чтений;
- `fleet-manager` как владельца placement decision для новых слотов и jobs без slot;
- явные slot defaults `KODEX_RUNTIME_MANAGER_SLOT_DEFAULT_FLEET_SCOPE_ID` и `KODEX_RUNTIME_MANAGER_SLOT_DEFAULT_CLUSTER_ID` только для оставшихся внутренних контуров, которые ещё не переведены на `ResolvePlacement`.

Проверки:
- readiness и gRPC boundary проверяются Go tests или будущим Go integration runner;
- shell smoke для доменного сценария не используется;
- адреса, домены и креды из локального `bootstrap/host/config.env` не публикуются в Issue/PR.

Операционные документы:
- `docs/domains/runtime-and-fleet/ops/runtime_manager_runbook.md`;
- `docs/domains/runtime-and-fleet/ops/runtime_manager_monitoring.md`.

## Синхронизация с параллельными доменами

| Домен | Когда синхронизироваться | Причина |
|---|---|---|
| `agent-manager` | До RTM-1 и RTM-3 | `Run` остаётся у `agent-manager`; runtime принимает external run refs и возвращает slot/job refs. |
| `project-catalog` | До RTM-1 и RTM-4 | Workspace policy, release policy, placement constraints и source refs должны совпадать с проектным контрактом. |
| `provider-hub` | До RTM-4 и RTM-5 | Provider refs и ускоряющие сигналы после работы slot-агентов. |
| `package-hub` | До RTM-4, RTM-5 и RTM-7 | Руководящие пакеты и runtime-нагрузки плагинов. |
| `access-manager` | До RTM-1 и RTM-2 | Действия доступа для runtime-команд, проверка actor и реакция на блокировки. |
| `fleet-manager` | До RTM-1, RTM-3 и RTM-FLEET-1 | Поля fleet scope/cluster ref, `ResolvePlacement`, health и правила размещения. |
| `operations-hub` | До RTM-5 и RTM-6 | Набор полей, который нужен операторским экранам и центру внимания. |
| `billing-hub` | После RTM-5 | Будущие записи затрат по runtime usage. |

## Критерии начала кода

- Принят доменный пакет `runtime-and-fleet`.
- Для каждого кодового PR есть отдельный GitHub Issue.
- Контрактный PR создаёт proto и AsyncAPI до реализации операций.
- Старый код из `deprecated/**` не используется как основа реализации.
- В PR, который закрывает Issue, тело содержит `Closes #...`.

## Критерии завершения домена

- `runtime-manager` имеет собственную БД, миграции, контракты, события и deploy-контур.
- Slot, workspace materialization, job, job step, short log tail, runtime artifact refs, cleanup policy и prewarm pool имеют авторитетные команды и чтения.
- Runtime публикует `runtime.*` события через outbox и `platform-event-log`.
- Полные логи и registry catalog не хранятся в PostgreSQL.
- `agent-manager`, `project-catalog`, `package-hub`, `operations-hub` и будущий release/governance контур могут опираться на runtime-контракты.
- Runtime не выбирает кластер самостоятельно и не блокирует multi-cluster: placement decision принадлежит `fleet-manager`.

## Апрув

- request_id: `owner-2026-05-07-runtime-manager-kickoff`
- Решение: approved
- Комментарий: план поставки `runtime-manager` согласован как целевое состояние RTM-0.
