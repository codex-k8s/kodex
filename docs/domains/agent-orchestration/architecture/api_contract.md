---
doc_id: API-CK8S-AGENT-ORCHESTRATION-0001
type: api-contract
title: kodex — API-обзор agent-manager
status: active
owner_role: SA
created_at: 2026-05-12
updated_at: 2026-06-02
related_issues: [733, 739, 744, 753, 755, 698, 759, 772, 322, 782, 795, 809, 820, 834, 842, 862, 866, 891, 897, 905, 918, 937, 954, 968, 984, 994, 999, 1011, 1015]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-12-agent-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-12
---

# API-обзор: agent-manager

## TL;DR

- Тип API: внутренний gRPC `AgentManagerService`, доменные события `agent.*`, MCP-инструменты через `platform-mcp-server`, Codex hook events через `codex-hook-ingress`.
- Аутентификация: gateway, MCP или сервисный токен; доменные команды дополнительно проверяются через `access-manager`.
- Версионирование: стабильное транспортное пространство имён `kodex.agents.v1`.
- Основные операции: flow, role, prompt template, session, run, safe activity timeline, acceptance, follow-up и pending self-deploy plan.

## Спецификации

- gRPC proto: `proto/kodex/agents/v1/agent_manager.proto`.
- Сгенерированный Go-контракт: `proto/gen/go/kodex/agents/v1/**`.
- AsyncAPI: `specs/asyncapi/agent-manager.v1.yaml`.
- Сгенерированные Go-контракты событий: `libs/go/platformevents/agent/events.gen.go`.
- MCP-инструменты: публикуются через `platform-mcp-server` и маршрутизируются к `agent-manager`.
- Codex hook events: приходят через `codex-hook-ingress`, а не через MCP tools.
- Внешний HTTP для пользовательской и операторской консоли: через профильный gateway, не напрямую из доменного сервиса.

Этот документ является обзором целевого API. Машинные спецификации являются источником правды для транспорта, а документ должен обновляться синхронно с изменением транспортной спецификации.

## Операции

| Операция | Вид | Доступ | Идемпотентность | Примечание |
|---|---|---|---|---|
| `CreateFlow` | gRPC command | `agent.flow.manage` | `CommandMeta.command_id` | Создаёт flow в scope. |
| `UpdateFlow` | gRPC command | `agent.flow.manage` | `command_id` + expected version | Меняет отображаемые метаданные flow, не активную immutable-версию. |
| `CreateFlowVersion` | gRPC command | `agent.flow.manage` | `command_id` | Создаёт новую версию flow из определения. |
| `ActivateFlowVersion` | gRPC command | `agent.flow.manage` | `command_id` + expected version | Делает версию активной для новых запусков. |
| `GetFlow` | gRPC query | `agent.flow.read` | нет | Читает flow и активную версию. |
| `ListFlows` | gRPC query | `agent.flow.read` | нет | Список flow по scope/status. |
| `CreateRoleProfile` | gRPC command | `agent.role.manage` | `command_id` | Создаёт роль агента. |
| `UpdateRoleProfile` | gRPC command | `agent.role.manage` | `command_id` + expected version | Меняет профиль роли и доступные MCP-инструменты. |
| `GetRoleProfile` | gRPC query | `agent.role.read` | нет | Читает профиль роли. |
| `ListRoleProfiles` | gRPC query | `agent.role.read` | нет | Список ролей по scope/kind/status. |
| `GetPromptTemplate` | gRPC query | `agent.prompt.read` | нет | Читает метаданные prompt template и активную версию без обхода роли. |
| `ListPromptTemplates` | gRPC query | `agent.prompt.read` | нет | Список prompt template по роли и назначению. |
| `CreatePromptTemplateVersion` | gRPC command | `agent.prompt.manage` | `command_id` | Создаёт версию prompt для роли по `source_ref`, объектной ссылке и digest без передачи свободного текста prompt в события. |
| `ActivatePromptTemplateVersion` | gRPC command | `agent.prompt.manage` | `command_id` + expected version | Активирует prompt version для новых запусков. |
| `GetPromptTemplateVersion` | gRPC query | `agent.prompt.read` | нет | Читает одну версию prompt. |
| `ListPromptTemplateVersions` | gRPC query | `agent.prompt.read` | нет | Список версий prompt по роли, назначению и статусу. |
| `StartAgentSession` | gRPC command | `agent.session.start` | `command_id` | Создаёт новую сессию или продолжает активную `open`/`waiting` session по тому же `scope + provider_work_item_ref`; повторное продолжение фиксируется как результат команды без нового `agent.session.created`. |
| `StartAgentRun` | gRPC command | `agent.run.start` | `command_id` | Создаёт `Run`, фиксирует версии flow/stage/role/prompt, проверяет stage-bound связку через `StageRoleBinding`, разрешает guidance selection hints через `package-hub` и сохраняет только безопасные refs/summary без manifest payload; прямой запуск роли без stage остаётся отдельным допустимым режимом. |
| `RecordRunState` | gRPC command | `agent.run.update` | `command_id` + expected version | Фиксирует переход `Run` после сигнала от runtime, MCP-инструмента или `codex-hook-ingress`; переход проходит через доменную state machine и не может вернуть terminal run обратно в работу. |
| `ReportAgentRunState` | gRPC command | `agent.run.update` | `command_id` + expected version | Принимает bounded report от `agent-runner` для уже созданного runtime job: `queued`/`running`/`started`/`completed`/`failed`/`cancelled`/`timed_out`, `run_id`, `session_id`, `runtime_slot_ref`, `runtime_job_ref`, safe summary, diagnostic digest и failure code. `started` и `running` переводят `Run` в `running`; `timed_out` переводит `Run` в `failed` с safe `failure_code`; `cancelled` переводит `Run` в `cancelled`. Команда сверяет привязку `Run` к slot/job, поддерживает replay/conflict и не принимает prompt, transcript, raw tool payload, stdout/stderr, provider payload, workspace paths, kubeconfig или secret values. |
| `GetAgentRunRuntimeStatus` | gRPC query | `agent.run.read` | нет | Возвращает безопасную runtime-наблюдаемость по одному `Run`: сохранённый `runtime_job_ref`, актуальный статус job через `runtime-manager.GetJob`, safe error code/summary, timestamps, версии и признак ожидания Human gate. В ответ не попадают `job_input_json`, логи, workspace paths, prompt, provider payload или секреты. |
| `RecordSessionStateSnapshot` | gRPC command | `agent.session.update` | `command_id` + expected version | Записывает метаданные Codex session JSON/JSONL в объектном хранилище и обновляет указатель на актуальный снимок сессии. |
| `RequestAcceptance` | gRPC command | `agent.acceptance.run` | `command_id` | Создаёт pending acceptance result по session/run/stage. Команда может принять typed `governance_context` refs, если проверка привязана к risk/gate/release policy; batch-запросы остаются расширением поверх существующего proto. |
| `RecordAcceptanceResult` | gRPC command | `agent.acceptance.update` | `command_id` + expected version | Фиксирует безопасный результат проверки и меняет статус через optimistic concurrency; `target_ref`, `details_json` и `governance_context` проходят safe-storage guard, а `human_gate` может быть записан только как `waiting` с owner/gate/risk/governance ref. |
| `GetAcceptanceResult` | gRPC query | `agent.acceptance.read` | нет | Читает один результат приёмки. |
| `ListAcceptanceResults` | gRPC query | `agent.acceptance.read` | нет | Список результатов приёмки по session/run/stage/status. |
| `CreateFollowUpIntent` | gRPC command | `agent.follow_up.create` | `command_id` или `idempotency_key` | Формирует авторитетное намерение следующей provider-native задачи по session/run/stage/acceptance refs. Команда сохраняет только safe provider target refs, typed governance refs, тип следующего work item, bounded title/summary/hints, digest и статус; provider write не выполняется. |
| `DispatchFollowUpIntent` | gRPC command | `agent.follow_up.create` | `command_id` + expected version | Переводит `planned/requested` follow-up intent в одну из typed provider-команд `create_issue`, `update_issue`, `create_comment`, `update_comment`, `update_pull_request` или `create_review_signal`. Команда принимает явный `FollowUpDispatchKind` и соответствующий typed `oneof`, перед внешним write атомарно резервирует dispatch локальным bump версии и deterministic provider command ref от intent, затем сохраняет только `provider_operation_ref`, safe result refs и статус `created`/`updated`/`commented`/`review_signaled`/`failed`. Для PR/MR update требуется provider expected version, а review signal фиксируется как provider-native сигнал без governance decision. |
| `RecordAgentActivity` | gRPC command | `agent.activity.record` | `command_id` или `idempotency_key` | Записывает одну safe timeline entry для session/run: kind, tool metadata, status, timings, summary, digest, bounded error, safe refs/details и correlation trace без raw tool payload, prompt, transcript, stdout/stderr или workspace paths. |
| `ListAgentActivities` | gRPC query | `agent.activity.read` | cursor | Читает safe timeline по session или run с фильтрами kind/status и cursor pagination для будущего UI. |
| `RequestHumanGate` | gRPC command | `agent.human_gate.request` | `command_id` или `idempotency_key` | Создаёт авторитетное ожидание owner decision в `agent-manager`: session/run/stage/acceptance refs, provider target refs, safe summary, `interaction_request_ref` и typed `governance_context`. При включённой request-side интеграции команда создаёт transport request через `interaction-hub.RequestHumanGate`, передаёт действия `approve`/`reject`/`request_changes`/`answer` и сохраняет только safe request ref; transport request/response остаётся у `interaction-hub`, governance/risk/release decision — у `governance-manager`. |
| `RecordHumanGateDecision` | gRPC command | `agent.human_gate.request` | `command_id` + expected version | Записывает normalized outcome `approve`/`reject`/`request_changes`/`answer`, safe summary и refs на `interaction_response`/typed `governance_context`, переводя ожидание в `resolved` через optimistic concurrency без копирования внешних payload. |
| `GetHumanGateRequest` | gRPC query | `agent.session.read` | нет | Читает одно ожидание/решение Human gate. |
| `ListHumanGateRequests` | gRPC query | `agent.session.read` | cursor | Читает ожидания/решения по session/run/stage/status/outcome. |
| `CreateSelfDeployPlan` | gRPC command | `agent.self_deploy.plan` | `command_id` или `idempotency_key` | Фиксирует pending orchestration plan для self-deploy по typed plan input. Команда сохраняет project/repository refs, provider signal/source/merge commit refs, affected service keys, path categories, `services.yaml` ref/digest, ожидаемые runtime job types `build`/`deploy`/`health_check`, typed governance refs, safe summary и fingerprint. Runtime jobs не создаются до owner/governance approval; approval path передаёт эти safe поля в `governance-manager.PrepareSelfDeployPlanGate` и ждёт `approved` gate status. Raw webhook body, provider response, diff, полный `services.yaml`, prompt/transcript, секреты и токены не принимаются. |
| `CreateSelfDeployPlanFromSignal` | gRPC command | `agent.self_deploy.plan` | `provider_signal_ref` + fingerprint | Создаёт тот же pending plan из safe project-side signal input. `provider_signal_ref` обязателен и является signal-level ключом: повтор того же signal с тем же fingerprint возвращает существующий plan даже при новом `command_id`, а другой fingerprint по тому же signal ref получает conflict. Встроенный consumer получает `provider.repository.changed` как trigger, вызывает `project-catalog.GetSelfDeploySignal` и передаёт сюда только `ready` input с проверенными `services.yaml` ref/digest и affected service keys; non-ready статусы не создают plan. |
| `GetSelfDeployPlan` | gRPC query | `agent.self_deploy.read` | нет | Читает один safe self-deploy plan по id. |
| `ListSelfDeployPlans` | gRPC query | `agent.self_deploy.read` | `page_token` | Читает self-deploy plans по scope/project/repository/provider signal/status с bounded pagination; широкий список без ограничителя недопустим. |
| `GetAgentSession` | gRPC query | `agent.session.read` | нет | Читает сессию. |
| `ListAgentSessions` | gRPC query | `agent.session.read` | `page_token` | Читает безопасные session summaries для операторского UI по scope/provider/status/инициатору и временному окну: session refs/status, latest run refs, active run count, признаки ожидания Human gate/follow-up с safe refs, latest activity summary, timestamps и version. |
| `ListAgentRuns` | gRPC query | `agent.run.read` | нет | Читает запуски по session/status/provider target. |
| `ListAgentRunSummaries` | gRPC query | `agent.run.read` | `page_token` | Читает безопасные run summaries по scope/session/provider/status/role и временному окну: run refs/status, runtime job ref из сохранённого `Run`, safe summary/error, Human gate/follow-up flags, latest activity summary, timestamps и version без live runtime fan-out. |

## Инструменты MCP

`platform-mcp-server` должен предоставлять типизированные инструменты, которые маршрутизируются в `agent-manager`:

| Инструмент | Назначение |
|---|---|
| `agent.session.start` | Начать или продолжить агентную сессию по пользовательскому запросу. |
| `agent.run.start` | Запустить роль в рамках session/stage. |
| `agent.run.record_state` | Зафиксировать общий переход `Run` от доверенного сервисного контура. |
| `agent.run.runtime_status` | Получить безопасное состояние runtime job для `Run` без доступа к Kubernetes или БД `runtime-manager`. |
| `agent.run.list` | Получить безопасный список `Run` summaries для операторского UI без чтения соседних БД, Kubernetes или provider API. |
| `agent.session.list` | Получить безопасный список session summaries для командного центра. |
| `agent.session.record_snapshot` | Зафиксировать ссылку на актуальный Codex session state без передачи содержимого JSON через MCP. |
| `agent.acceptance.request` | Запустить машинную приёмку. |
| `agent.follow_up.request` | Сформировать следующий provider-native шаг как intent с safe refs. |
| `agent.activity.record` | Зафиксировать безопасную timeline entry от owner-side интеграции. |
| `agent.activity.list` | Получить безопасную историю действий по session/run для UI. |
| `agent.gate.request` | Зафиксировать owner decision wait в `agent-manager` с refs на interaction/governance request lifecycle. |
| `agent.gate.resolve` | Записать normalized owner decision outcome и refs на interaction/governance result, чтобы flow мог продолжиться или завершиться без копирования внешних данных. |
| `agent.self_deploy.plan` | Создать pending self-deploy plan из typed input или safe provider/project signal без постановки runtime jobs. |
| `agent.self_deploy.list` | Прочитать safe self-deploy plans для owner/governance review. |

MCP-инструменты не должны принимать свободный JSON для provider-операций. Если нужно создать `Issue`, комментарий или `PR/MR`, инструмент вызывает `provider-hub` через типизированный provider-контракт.

## Codex hook events

Codex hooks не являются MCP-инструментами. `agent-manager` получает их только после нормализации во входном контуре `codex-hook-ingress`.

| Hook event | Как влияет на `agent-manager` |
|---|---|
| `SessionStart` | Создаёт или связывает Codex-сессию с существующим `AgentSession` и `Run`. |
| `UserPromptSubmit` | Фиксирует безопасный факт нового пользовательского ввода и связывает его с session/run context. |
| `PreToolUse` | Даёт сигнал намерения вызвать инструмент; следующий CHI-срез должен передавать только sanitized tool metadata в `RecordAgentActivity`, а risk-controlled действия могут привести к gate или realtime-событию. |
| `PermissionRequest` | Преобразуется в запрос risk/gate evaluation через `governance-manager`; доставка человеку остаётся у `interaction-hub`. |
| `PostToolUse` | Передаёт безопасный итог инструмента, provider artifact signal или bounded error; persistent история пишется как `AgentActivity` без raw stdout/stderr, tool response или provider payload. |
| `Stop` | Фиксирует контрольную точку хода, pending actions и безопасную итоговую сводку. |

Контрольные точки сжатия контекста и session snapshot остаются внутренними событиями `agent-manager`/`runtime-manager`. Они не описываются как Codex hooks и не проходят через `platform-mcp-server`.

## Интеграции с другими сервисами

| Сервис | Вызовы из `agent-manager` | Правило |
|---|---|---|
| `package-hub` | `ListPackageInstallations(package_kind=guidance)`, `GetPackageInstallation`, `ListPackages(package_kind=guidance)`, `GetPackage`, `GetPackageVersion`, `GetPackageManifest` | Только чтение установок, версии и проверенного manifest руководящего пакета; `agent-manager` сохраняет refs, версии, digest и безопасную summary, но не manifest payload, `SKILL.md`, scripts, assets или package source. |
| `runtime-manager` | `PrepareRuntime`, `CreateJob(job_type=JOB_TYPE_AGENT_RUN)`, `GetJob` | Состояние runtime остаётся у runtime. `agent-manager` передаёт `WorkspaceSource.kind=guidance_package` для замороженных `guidance_refs` и `WorkspaceSource.kind=generated_context` для `.kodex/context/agent-run.json`; при включённом dispatch собирает typed `AgentRunExecutionSpec` из safe refs/digest/fingerprint и передаёт его в `CreateJob` без raw payload только после `slot_status=ready` и `workspace_materialization_status=completed`. Вложенный `CodexSessionExecutionSpec` строится из safe refs: instruction object ref/digest из `PromptTemplateVersion.TemplateObject`, result schema ref/digest из конфигурации `KODEX_AGENT_MANAGER_CODEX_SESSION_RESULT_SCHEMA_REF` и `KODEX_AGENT_MANAGER_CODEX_SESSION_RESULT_SCHEMA_DIGEST`, session/workspace snapshot ref, hook/callback refs, `KODEX_AGENT_MANAGER_CODEX_SESSION_TIMEOUT`, фиксированный runner profile, output/result refs и secret refs без значений. Если обязательные refs/digest не готовы или не проходят безопасную валидацию, `agent-manager` фиксирует состояние waiting/diagnostic и не вызывает `CreateJob` с неполным input. Checkout, materialization, job state и исполнение выполняет runtime; текущий `agent-runner` исполняет только workspace refs `workspace://.kodex/execution/...` и не строит prompt fallback. Для отчёта runner-а `runtime-manager` добавляет в Kubernetes Job адрес `agent-manager` и ссылку `SecretKeyRef` на gRPC token; `agent-manager` не передаёт значение token в spec и не хранит его в `Run`. Поверхность чтения `GetAgentRunRuntimeStatus` читает актуальное состояние job только через `runtime-manager.GetJob` и возвращает наружу bounded safe поля. |
| `provider-hub` | `CreateIssue`, `UpdateIssue`, `CreateComment`, `UpdateComment`, `UpdatePullRequest`, `CreateReviewSignal` для dispatch follow-up intent; чтение проекций и ускоряющий сигнал сверки | Provider-native состояние и write pipeline остаются у provider. `agent-manager` не ходит напрямую в GitHub/GitLab и сохраняет только safe operation/result refs. |
| `project-catalog` | Чтение workspace policy, release policy, project/repository refs | Проектная policy остаётся у project. |
| `governance-manager` | Risk assessment, record review signal, request gate, read gate/release decision | Risk/gate/release decisions остаются у governance. `agent-manager` хранит только typed refs `risk_assessment_ref`, `gate_request_ref`, `gate_decision_ref`, `release_decision_package_ref`, `release_decision_ref`, `risk_profile_ref`, `gate_policy_ref`, `release_policy_ref` и не копирует decision/evidence body. |
| `access-manager` | Проверка действий, ролей, аккаунтов и scope | `agent-manager` не вычисляет права сам. |
| `interaction-hub` | `RequestHumanGate` для создания owner-visible Human gate request; событие `interaction.request.response_recorded` для возобновления Human gate | Диалог, callback body, owner inbox и доставка остаются у interaction. `agent-manager` передаёт только safe owner/session/run/provider refs, target actor ref из session owner, bounded summary и допустимые действия `approve`/`reject`/`request_changes`/`answer`, а затем потребляет только safe refs/status/action/version из event log и хранит refs + normalized owner outcome. |
| `codex-hook-ingress` | Нормализованные Codex hook events: lifecycle, permission, tool result и stop summary | Hook transport и очистка входа остаются у hook ingress; `agent-manager` хранит только своё состояние. |

`codex-hook-ingress` не хранит долгую историю tool calls. Он очищает событие, строит route plan и держит короткую realtime/ops feed; каноническая persistent история действий для UI записывается в `agent-manager.RecordAgentActivity`.

## Модель ошибок

| Ошибка | Когда возвращается |
|---|---|
| `invalid_argument` | Невалидный flow, stage, role, prompt, transition, provider target, request context, acceptance batch-запрос, небезопасный `target_ref`, небезопасный `details_json`, небезопасный `governance_context`, небезопасный follow-up payload или небезопасная activity timeline запись. |
| `permission_denied` | `access-manager` запретил действие или роль не имеет нужного MCP-инструмента. |
| `not_found` | Flow, роль, prompt, session, run или acceptance result не найдены. |
| `already_exists` | Дубликат slug или повтор создания активной сущности в scope. |
| `failed_precondition` | Нельзя запустить роль без prompt, workspace policy, provider target или обязательного решения; `human_gate` acceptance пытаются закрыть финальным статусом вместо ожидания owner decision; follow-up создаётся из незавершённого run или неположительного acceptance либо dispatch выполняется из terminal intent. |
| `aborted` | Конфликт expected version или устаревший `Run` state. |
| `unavailable` | Временная ошибка package, runtime, provider, interaction или event log. |

## События

| Событие | Когда публикуется |
|---|---|
| `agent.session.created` | Создана новая агентная сессия. |
| `agent.session.updated` | Изменился текущий этап или статус сессии. |
| `agent.run.requested` | Запрошен ролевой запуск. |
| `agent.run.started` | Runtime подтвердил подготовку workspace и, при включённой постановке задания, принял `JOB_TYPE_AGENT_RUN`; payload обязан содержать `runtime_slot_ref`, а `runtime_job_ref` заполняется после `CreateJob`. |
| `agent.run.waiting` | Запуск ожидает человека, готовности runtime materialization, provider или retry; payload обязан содержать машинный `reason_code`. |
| `agent.run.completed` | Ролевой запуск завершён. |
| `agent.run.failed` | Ролевой запуск завершился ошибкой; payload обязан содержать `failure_code`. |
| `agent.run.cancelled` | Ролевой запуск отменён runner/runtime контуром; payload содержит только safe refs, status и version. |
| `agent.session.snapshot_recorded` | Зафиксирован новый снимок Codex session state. |
| `agent.acceptance.requested` | Запрошена машинная приёмка; payload может содержать только typed governance refs без decision body. |
| `agent.acceptance.completed` | Приёмка завершилась статусом `passed` или `skipped`; payload содержит safe refs/status/version и optional governance refs. |
| `agent.acceptance.failed` | Приёмка завершилась статусом `failed` и содержит машинный `reason_code` без сырых payload. |
| `agent.follow_up.requested` | Зафиксирован follow-up intent с safe refs/status/summary и optional governance refs; provider command ещё не выполнена. |
| `agent.follow_up.created` | Follow-up provider-native задача создана или подтверждена. |
| `agent.follow_up.updated` | Существующая provider-native задача обновлена typed provider-командой. |
| `agent.follow_up.commented` | Комментарий к provider-native артефакту создан или обновлён typed provider-командой. |
| `agent.follow_up.review_signaled` | Provider-native review signal создан typed provider-командой; governance decision не создаётся и не хранится в `agent-manager`. |
| `agent.follow_up.failed` | Provider command завершилась безопасно классифицированной ошибкой; payload содержит только intent ref, operation ref, status и reason/failure code. |
| `agent.human_gate.requested` | Flow ожидает owner decision; payload содержит только session/run/stage/gate refs, typed governance refs, status, reason и safe summary. |
| `agent.human_gate.resolved` | `agent-manager` получил normalized owner outcome и refs на interaction/governance result, после чего внешний scheduler/executor может продолжить или завершить связанный flow. |
| `agent.self_deploy.plan_requested` | Зафиксирован pending self-deploy plan после safe provider/project signal или typed plan input; payload содержит только plan id, project/repository/source refs, affected service keys, path categories, expected runtime job types и fingerprint без raw webhook/diff/YAML. |
| `agent.flow.version_activated` | Активирована версия flow. |
| `agent.role.version_activated` | Активирована версия роли. |
| `agent.prompt.version_activated` | Активирована версия prompt. |

Activity timeline не добавляет новое `agent.*` событие в этом срезе: это read/write-модель `agent-manager` для UI и аудита. Высокочастотная realtime-лента остаётся в `codex-hook-ingress`, а доменные события публикуются только для значимых lifecycle/acceptance/follow-up изменений.

## Состояние реализации

| Область | Статус |
|---|---|
| Доменная документация | Подготовлена как стартовый срез. |
| gRPC proto | Подготовлен как контрактный срез `AGO-1`. |
| AsyncAPI `agent.*` | Подготовлен как контрактный срез `AGO-1`. |
| Go-реализация `agent-manager` | Сервисный каркас готов. Операции flow, role, prompt, session, run, machine acceptance, follow-up intent, provider follow-up dispatch, safe activity timeline, Human gate wait/result и self-deploy plan подключены к слою хранения и use-case через gRPC handlers. `StartAgentSession` защищает активную session от дублей по provider target, `StartAgentRun` фиксирует версии роли/prompt, проверяет stage-bound связку flow/stage/role, замораживает безопасные guidance refs из `package-hub`, читает workspace policy у `project-catalog`, вызывает `runtime-manager.PrepareRuntime` и при включённом `KODEX_AGENT_MANAGER_RUNTIME_JOB_DISPATCH_ENABLED` ставит `JOB_TYPE_AGENT_RUN` через `runtime-manager.CreateJob` с typed `AgentRunExecutionSpec` только после готовности slot/materialization. Spec строится из `agent_run_id`, `slot_id`, expected materialization id/fingerprint, runtime-owned workspace/context refs, context digest, runner profile/image refs, фиксированного runner mode, allowed secret refs без значений и reporting targets; `KODEX_AGENT_MANAGER_RUNTIME_JOB_RUNNER_IMAGE_REF` обязателен при включённом dispatch. Вложенный `CodexSessionExecutionSpec` заполняется из `PromptTemplateVersion.TemplateObject`, безопасных result schema/hook/timeout настроек, session/workspace snapshot refs, callback/output/result refs и allowed secret refs без значений; при недостающих refs/digest `Run` остаётся в безопасном waiting/diagnostic state с кодом `execution_input_unavailable`, а replay после готовности создаёт job тем же deterministic runtime command id. Если runtime ещё материализует workspace, `Run` остаётся в `waiting`, а replay `StartAgentRun` повторяет idempotent `PrepareRuntime` и выполняет dispatch после готовности без создания второго job; если runtime возвращает terminal `failed`/`cancelled`, `Run` переходит в безопасный `failed`. В `Run` сохраняются только runtime refs, `runtime_job_ref`, fingerprint/diagnostic summary и безопасная классификация ошибок подготовки или постановки задания; workspace paths, файлы, prompt text, flow files, package payload, логи и `job_input_json` остаются вне БД `agent-manager`. `GetAgentRunRuntimeStatus` даёт безопасную поверхность чтения для UI/MCP и owner-оператора: объединяет сохранённый `Run` с актуальным статусом job из `runtime-manager.GetJob`, показывает safe error/summary/timestamps/version и не читает Kubernetes, БД runtime или сырые логи. `ListAgentSessions` и `ListAgentRunSummaries` дают безопасные списки для командного центра и экрана исполнений: фильтруют по scope/session/provider/status/role и времени создания, возвращают session/run refs, runtime job ref из сохранённого `Run`, Human gate/follow-up flags, latest activity summary, timestamps и version без live fan-out в runtime/provider/Kubernetes. `RecordRunState` применяет общую state machine и публикует только AsyncAPI-совместимые lifecycle-события. `ReportAgentRunState` принимает от `agent-runner` typed safe report `queued`/`running`/`started`/`completed`/`failed`/`cancelled`/`timed_out`, сверяет `run_id`/`session_id`/`runtime_slot_ref`/`runtime_job_ref`, expected version и replay payload, сохраняет bounded summary/digest/failure code в `Run` и не принимает raw prompt, transcript, tool payload, stdout/stderr, provider payload, workspace paths, kubeconfig или секреты. `timed_out` фиксируется как `failed` с safe `failure_code`, а `cancelled` публикует `agent.run.cancelled`. `RecordAgentActivity`/`ListAgentActivities` хранят и читают только bounded safe timeline entries без raw tool input/response, stdout/stderr, prompt, transcript, provider payload или workspace paths. `RequestAcceptance`/`RecordAcceptanceResult`/`GetAcceptanceResult`/`ListAcceptanceResults` реализуют базовый lifecycle результата приёмки с idempotency, expected version, безопасными `target_ref`/`details_json`, typed `governance_context`, `human_gate` waiting-only guard и outbox events. `CreateFollowUpIntent` создаёт intent-only состояние с idempotency, проверкой session/run/stage/acceptance связей, safe title/summary/provider refs, optional governance refs и событием `agent.follow_up.requested`; `DispatchFollowUpIntent` до provider write резервирует dispatch локальной версией и deterministic provider command id, вызывает только typed `provider-hub` команды `CreateIssue`/`UpdateIssue`/`CreateComment`/`UpdateComment`/`UpdatePullRequest`/`CreateReviewSignal`, сохраняет `provider_operation_ref`, safe result refs и статус `created`/`updated`/`commented`/`review_signaled`/`failed`. `RequestHumanGate`/`RecordHumanGateDecision`/`GetHumanGateRequest`/`ListHumanGateRequests` хранят orchestration wait/result, normalized outcome, interaction refs, typed governance refs, idempotency и outbox events без transport payload, governance decision body, prompt/transcript/logs/PII. При включённом `KODEX_AGENT_MANAGER_INTERACTION_HUB_REQUEST_ENABLED` `RequestHumanGate` создаёт request через `interaction-hub.RequestHumanGate`, передаёт safe owner/session/run/provider/governance refs и bounded summary, сохраняет только `interaction_request_ref` и не переносит delivery lifecycle. Встроенный event consumer читает `interaction.request.response_recorded`, находит ожидающий Human gate по `owner_request_ref`, сверяет refs/version/fingerprint и записывает тот же normalized result через существующий lifecycle. `CreateSelfDeployPlan` фиксирует pending plan для self-deploy из typed input, а consumer `provider.repository.changed` использует событие только как trigger, вызывает `project-catalog.GetSelfDeploySignal` и передаёт в `CreateSelfDeployPlanFromSignal` только `ready` project-side input с обязательным `provider_signal_ref`, проверенным `services.yaml` digest и affected service keys. Non-ready статусы `project-catalog` не создают plan и остаются безопасной диагностикой ожидания. Повтор того же signal с тем же fingerprint возвращает существующий plan, конфликтующий fingerprint отклоняется. Обе команды публикуют `agent.self_deploy.plan_requested`, хранят только project/repository/source refs, service keys/path categories, `services.yaml` digest, expected runtime job types, governance refs, safe summary и fingerprint; `GetSelfDeployPlan`/`ListSelfDeployPlans` дают безопасное чтение. Runtime build/deploy jobs не создаются без owner/governance approval. QA runner, executor и provider write adapters остаются отдельными срезами. |
| Интеграция с `package-hub` | Реализована как чтение guidance installations, package/version metadata и manifest validation state; сырое содержимое manifest и package source в `agent-manager` не сохраняются. |
| Интеграция с runtime | Реализованы прямые вызовы `PrepareRuntime`, opt-in `CreateJob(job_type=JOB_TYPE_AGENT_RUN, AgentRunExecutionSpec)` для старта `AgentRun` и safe read через `GetJob` для `GetAgentRunRuntimeStatus`; executor, Kubernetes-размещение, БД runtime и выполнение задания не входят в `agent-manager`. |
| Интеграция с provider/interaction/hooks | Follow-up dispatch подключён к typed `provider-hub` write-командам `CreateIssue`, `UpdateIssue`, `CreateComment`, `UpdateComment`, `UpdatePullRequest` и `CreateReviewSignal` через gRPC client. `agent-manager` не реализует provider write adapter и не использует прямой GitHub/GitLab доступ. Для Human gate `agent-manager` создаёт request через typed `interaction-hub.RequestHumanGate` при включённой интеграции, хранит wait/result и refs, а также потребляет безопасный `interaction.request.response_recorded` из platform event log для resume; transport/request/response lifecycle остаётся у `interaction-hub`, governance/risk/release decision — у `governance-manager`, hook routing — у `codex-hook-ingress`. |

## Совместимость

- `v1` контракт должен покрыть согласованный объём доменного API, даже если реализация поставляется по срезам.
- Если контракт опережает реализацию, delivery-документ фиксирует реализованные и отложенные операции.
- События должны проектироваться так, чтобы переход с PostgreSQL event log на брокер не ломал payload.
- `Run` должен сохранять immutable-ссылки и версии flow/stage/role/prompt/guidance, включая digest роли и prompt, чтобы новая версия конфигурации не меняла старые результаты.

## Апрув

- request_id: `owner-2026-05-12-agent-manager-kickoff`
- Решение: approved
- Комментарий: API-обзор `agent-manager` согласован как стартовое целевое состояние; proto и AsyncAPI зафиксированы контрактным срезом.
