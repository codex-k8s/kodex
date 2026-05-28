---
doc_id: MON-CK8S-AGENT-MANAGER-0001
type: monitoring
title: "agent-manager — наблюдаемость"
status: active
owner_role: SRE
created_at: 2026-05-27
updated_at: 2026-05-27
related_issues: [897]
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-27-agent-manager-deploy"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-27
---

# Наблюдаемость: agent-manager

## TL;DR

- Дашборды: readiness, gRPC, PostgreSQL, outbox, session/run lifecycle, activity timeline, acceptance, follow-up dispatch, Human gate wait/result и dependency status.
- Метрики: latency gRPC-команд, ошибки БД/outbox, backlog outbox, частота lifecycle events, failed follow-up dispatch и dependency unavailable.
- Логи: только безопасные идентификаторы `request_id`, `session_id`, `run_id`, `stage_id`, `activity_id`, `acceptance_result_id`, `follow_up_intent_id`, `human_gate_request_id`, `provider_operation_ref`.
- Алерты: недоступность readiness, падение migration job, outbox backlog, рост dependency unavailable, stuck waiting states и конфликтующие idempotency/expected version ошибки сверх baseline.

## Источники данных

- HTTP health: `/health/livez`, `/health/readyz`.
- HTTP metrics: `/metrics`.
- gRPC server metrics: общий runtime из `libs/go/grpcserver`.
- PostgreSQL: БД `kodex_agent_manager` и общая БД `kodex_platform_event_log`.
- Kubernetes: deployment, migration job, pod status и events.
- Логи приложения: structured logs без секретов, DSN, raw prompt, transcript, stdout/stderr, workspace paths, provider payload, PII и приватных endpoint.

## Дашборды

| Название | Ссылка | Для чего | Owner |
|---|---|---|---|
| Agent manager overview | TBD | Общий статус сервиса, readiness, gRPC, БД и outbox. | SRE |
| Agent run lifecycle | TBD | Статусы session/run, runtime preparation, state transitions и завершения. | SRE |
| Agent activity timeline | TBD | Частота safe activity records, tool activity statuses и ошибки записи timeline. | SRE |
| Acceptance and follow-up | TBD | Acceptance results, follow-up intent dispatch и provider operation refs. | SRE |
| Human gate waits | TBD | Ожидания owner decision, outcomes и возраст незакрытых ожиданий. | SRE |

## Golden signals

- Latency: длительность gRPC-команд, PostgreSQL-запросов, owner-service calls и outbox publish.
- Traffic: количество gRPC-запросов, session/run transitions, activity records, acceptance results, follow-up dispatch commands и Human gate decisions.
- Errors: gRPC-коды, ошибки БД, dependency unavailable, safe validation rejects, idempotency conflicts и outbox failures.
- Saturation: `MaxInFlight`, active PostgreSQL connections, размер outbox backlog, возраст старейшего outbox события, число long-waiting Human gate records.

## Доменный мониторинг

- Количество `AgentSession` по статусам.
- Количество `AgentRun` по статусам, включая runtime preparation status.
- Количество activity entries по `activity_kind`, `tool_category` и status.
- Количество acceptance results по status и kind.
- Количество follow-up intents по status и dispatch kind.
- Количество provider follow-up failures по retryable/permanent классификации.
- Количество Human gate waits по status/outcome и возраст старейшего `waiting`.
- Частота `agent.*` событий и возраст недоставленных local outbox records.

## Логи

Логи должны содержать только безопасные идентификаторы:

- `request_id`, `trace_id`, `actor_type`, `actor_id`;
- `session_id`, `run_id`, `stage_id`, `role_profile_id`;
- `activity_id`, `acceptance_result_id`, `follow_up_intent_id`, `human_gate_request_id`;
- `provider_target_ref`, `provider_operation_ref`, `runtime_ref`, `workspace_fingerprint`;
- короткий `error_code`, retryable/permanent classification и bounded diagnostic summary.

В логи не попадают:

- gRPC tokens, DSN, Vault tokens, webhook secrets;
- raw prompt, prompt template body, transcript, session dump;
- raw tool input/output, stdout/stderr, workspace files, local paths, kubeconfig;
- raw provider request/response, provider payload, GitHub/GitLab tokens;
- email, имена пользователей, приватные домены и адреса серверов из bootstrap-профиля.

## Проверки и рутинные health checks

- Liveness: процесс отвечает на `/health/livez`.
- Readiness: процесс видит БД `agent-manager` и, при включённой outbox-доставке, БД `platform-event-log`.
- gRPC integration check: `AgentManagerService/ListAgentRuns` должен давать application-level статус, а не сетевую ошибку.
- Outbox: oldest unpublished event не должен выходить за допустимое окно.
- Human gate waits: long-waiting records должны соответствовать реальным ожиданиям owner decision, а не зависшему callback.
- Follow-up dispatch: repeated failed intents должны иметь safe error summary и provider operation ref, если write успел начаться.

## Алерты

- `agent-manager` readiness недоступен дольше установленного окна.
- `agent-manager` migration job завершился ошибкой.
- Outbox backlog растёт или самое старое событие старше допустимого порога.
- Частота `dependency unavailable` выросла по одному owner-сервису.
- Доля `Aborted`/expected version conflicts выросла выше baseline.
- Число `AgentRun` в промежуточном статусе растёт без новых transitions.
- Human gate waits старше допустимого окна без matching interaction/governance refs.
- Follow-up dispatch failures растут по одному provider target или dispatch kind.
- Runtime preparation errors растут после rollout `runtime-manager` или `project-catalog`.

## Открытые вопросы

- Конкретные Prometheus recording rules и alert rules будут закреплены после появления штатного observability stack.
- Dashboard для executor/QA runner не относится к `agent-manager`, пока эти исполнители не реализованы отдельными сервисами.
- Transport-доставка Human gate и callback monitoring относятся к `interaction-hub`; `agent-manager` наблюдает только orchestration wait/result state.

## Апрув

- request_id: `owner-2026-05-27-agent-manager-deploy`
- Решение: approved
- Комментарий: monitoring-документ входит в эксплуатационный контур AGO-10.
