---
id: ARCH-MC-009
title: Автоматизации и playbooks
type: architecture
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Автоматизации и playbooks

## Playbook

Playbook является versioned prompt-driven процессом, а не визуальным workflow graph.

Он содержит:

- input JSON Schema;
- coordinator agent policy;
- Markdown instruction template;
- allowed agents/integrations;
- concurrency и timeout;
- callback contract;
- completion criteria;
- human gates;
- result schema.

ProcessRun фиксирует используемую версию. Изменение Playbook не меняет уже запущенный run.

## Cross-thread orchestration

Coordinator использует MCP:

- `create_thread`;
- `delegate_agent`;
- `request_human_gate`;
- `report_process_status`;
- `publish_artifact`.

Child thread хранит parent process ID. Completion event ставит callback в очередь coordinator session. Callback не зависит от Mattermost mention и не запускается повторно при replay event.

## AutomationSchedule

Schedule target:

- конкретный Agent;
- versioned Playbook.

Поля:

- cron или interval;
- IANA timezone;
- prompt/version;
- session policy `new|persistent|rolling`;
- concurrency `forbid_overlap|queue_all|coalesce`;
- misfire `skip|run_once|catch_up|within_grace_period`;
- notification `always|on_action|on_failure|on_action_or_failure|audit_only`;
- destination room;
- max runtime/retry policy.

Рекомендуемые defaults: `new`, `coalesce`, `run_once`, `on_action_or_failure`.

## Durable scheduling

Пользовательские schedules хранятся в PostgreSQL. Scheduler выбирает due rows через `FOR UPDATE SKIP LOCKED`, создает occurrence и queue job в одной transaction. Уникальный `(schedule_id, scheduled_for)` предотвращает duplicates.

River рассматривается как PostgreSQL-backed execution queue для transactional enqueueing и retries. Его in-memory periodic scheduler не является источником истины; next run и misfire semantics реализуются доменной моделью. Cron parsing выполняется готовой библиотекой, а не собственным parser.

Kubernetes CronJob не используется для пользовательских schedules: он не знает sessions, provider affinity, grants, approvals и Mattermost delivery.

## Headless execution

ScheduledRun может не иметь ConversationBinding. При `no_action` результат остается в audit. При action/failure система создает Mattermost post согласно delivery policy. Agent может через MCP создать отдельные threads и delegated runs.

## Human-gate процесс результата

Playbook для разработки или документации должен поддерживать:

1. worker result;
2. 2-3 reviewer cycles;
3. manager pre-gate check;
4. owner feedback;
5. worker fix;
6. final reviewer cycle;
7. owner OK;
8. reviewer merge;
9. improver run по feedback цикла.

Следующая независимая волна может стартовать параллельно, если не зависит от непринятого результата.
