---
id: ARCH-MC-009
title: Workflow, делегирование и расписания
type: architecture
status: approved
owner: architect
version: 1.0.0
updated: 2026-08-22
---

# Workflow, делегирование и расписания

## Workflow version

Workflow draft проходит validation и публикуется immutable version. Specification
содержит purpose, coordinator Agent, allowlisted Agents, bounded input,
instructions, concurrency, timeout, completion criteria, Human Gates и result
schema. Manual Run pin-ит exact version и source `CONTROL_CENTER` либо
`SYSTEM_ASSISTANT`.

Workflow не кодирует Mattermost room, Git repository, Kubernetes workload или
другую external entity. Такие references поступают только через typed
IntegrationGrant и bounded launch input.

## Делегирование

Coordinator runtime получает platform MCP server с typed tools:

- `delegate_agent`;
- `continue_session`;
- `return_to_coordinator`;
- `request_human_gate`;
- `request_owner_attention`;
- `list_active_work` и `request_sync`;
- `publish_artifact`.

MCP — протокол runtime-вызова. Каждый tool maps в специализированную
control-plane command и имеет закрытую input/output schema. Generic CRUD или
universal command proxy не используется.

`delegate_agent` принимает target ref из server catalog и bounded work input.
Control-plane повторно проверяет parent active attempt, Workflow policy,
capability, target eligibility и limits и одной транзакцией создаёт child Run,
Agent node, `DELEGATED_TO` edge, RuntimeRevision, task, audit и outbox events.
Root actor/policy/lineage назначаются сервером.

Child completion создаёт один callback receipt и отдельный FIFO Turn parent
Session. Explicit `return_to_coordinator` и terminal fallback расходуют одну
receipt; duplicate delivery не создаёт второй continuation.

## Human Gate

Agent либо Workflow открывает Gate через typed MCP tool. Owner transaction
фиксирует root/run/node/turn/attempt, recipient policy, safe context digest,
version, expiry и `WAITING_HUMAN` graph state. Runtime Pod не удерживается ради
многодневного решения.

Web resolution является основной. Optional Mattermost capability может подать
то же решение, но первая valid version — единственный winner. Continuation
создаёт свежую RuntimeRevision и attempt ровно один раз.

## Schedule

Schedule хранит target kind/ref, version policy, timezone, preset/cron, bounded
input, session policy, concurrency/misfire, retry и notification policy.
Mattermost destination не обязателен.

Owner API принимает закрытые presets `HOURLY`, `DAILY`, `WEEKDAYS`, `WEEKLY`,
локальное время, день недели для weekly и IANA timezone. Control-plane
валидирует timezone и сам материализует внутреннее cron-представление; frontend
не вычисляет следующий запуск. При несуществующем локальном времени во время
DST-перехода occurrence переносится на первую существующую минуту после
разрыва. Для пропущенного времени создаётся не более одного bounded occurrence,
после чего `next_run_at` сразу переводится в будущее.

Automation-scheduler не вычисляет cron lifecycle и не создаёт Run локально. Он:

1. claim-ит due occurrences специализированным RPC;
2. получает exact occurrence/version/attempt/fence/input digest;
3. просит control-plane materialize-ить Run source `SCHEDULE`;
4. фиксирует typed result либо отдаёт lease на bounded retry.

Несколько replicas безопасны за счёт owner-side lock/claim. Cancel, disable,
target archive и terminal occurrence закрывают leases/grants одной транзакцией.
Claim атомарно сохраняет immutable snapshot имени, target, schedule version и
bounded input вместе с digest и заранее двигает `next_run_at`. Materialization
использует только этот snapshot, поэтому параллельное изменение Schedule не
меняет уже принятую occurrence. Пока occurrence `CLAIMED` или `MATERIALIZED`,
тот же Schedule повторно не claim-ится. Истёкший `CLAIMED` occurrence не
теряется: control-plane выдаёт для той же immutable occurrence новый lease и
монотонное поколение, после чего прежние lease/fence закрыто отклоняются.

## Результат и уведомления

Terminal Run сохраняет structured result и Artifact bindings. Notification
policy создаёт отдельные delivery attempts. Optional adapter failure остаётся
retryable incident и не меняет успешный Run outcome.

## Проверяемый lifecycle

| Путь | Обязательный атомарный эффект |
| --- | --- |
| workflow launch | Session/Turn/root Run/node/revision/task/audit/outbox |
| delegate | child Run/node/edge/revision/task/audit/outbox |
| callback | receipt + parent FIFO Turn + edge/event exactly once |
| gate open | Gate + graph waiting + event |
| gate resolve | winner receipt + Gate terminal + one continuation |
| schedule due | occurrence claim exact attempt/fence/input |
| schedule materialize | occurrence linked to one Run + event |
| cancel | full graph/claims/leases/grants/gates terminal |
| retry | new attempt/revision/lease + immutable predecessor lineage |
