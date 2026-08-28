---
id: DOM-MC-009
title: Процессы и автоматизации
type: domain
status: approved
owner: architect
version: 2.2.0
updated: 2026-08-23
---

# Процессы и автоматизации

## Workflow

`Workflow` — universal versioned process. Draft содержит name, purpose,
coordinator, allowed Agents, bounded input form/schema, instructions,
concurrency, timeout, completion criteria, Human Gates и result schema. После
validation публикуется immutable Workflow version; Run всегда pin-ит version.

Workflow не предполагает repository, CI/CD, Mattermost room или Kubernetes
workload. Drag-and-drop BPMN editor не является условием исполнения: authority
принадлежит server-owned execution graph.

## Delegation и callback

Coordinator вызывает типизированный MCP tool. Control-plane проверяет active
parent attempt, capability, relationship policy, target eligibility и limits,
после чего одной транзакцией создаёт child Run/node, `DELEGATED_TO` edge,
RuntimeRevision, task, audit и outbox event.

Child terminal result создаёт один callback Turn исходной Session и
`CALLBACK_TO` edge. Explicit и terminal fallback используют одну callback
receipt. Agent-provided parent/root IDs и упоминание человека не являются
authority. Child Run исполняется в отдельной Session, а после всех ожидаемых
callback control-plane создаёт новый coordinator Turn в исходной Session.
WorkflowVersion и допустимые step key pin-ятся сервером при launch; повторное
делегирование уже созданного шага отклоняется.

## Schedule

`Schedule` принадлежит control-plane и запускает Agent или Workflow. Он содержит
timezone, preset/cron, target version policy, bounded input, session policy,
concurrency/misfire policy и notification policy. Manual launch имеет отдельный
source и не создаёт скрытый Schedule.

Scheduler лишь claim-ит due occurrence и просит control-plane materialize-ить
Run. Claim связан с workload, schedule, occurrence, version, attempt, immutable
input digest и fence. Retry создаёт новую attempt; disable/cancel/terminal
закрывают leases и grants owner-транзакцией.

Schedule имеет различимые `ACTIVE`, `PAUSED`, `NEEDS_ATTENTION` и terminal
`ARCHIVED`. Архивация не удаляет Schedule и историю occurrences: detail/read
остаётся доступен по прежнему owner-scoped ref, но изменение, включение и новый
запуск закрыто запрещены. В одной owner-транзакции архивирование проверяет
`MANAGE_SCHEDULES`, expected version и idempotency, переводит Schedule в
`ARCHIVED`, очищает `next_run_at`, отменяет только нематериализованные
`DUE|CLAIMED` occurrences и их leases, фиксирует audit, receipt и
`SCHEDULE_CHANGED`. Уже материализованный Run продолжает исполняться по своему
immutable snapshot и управляется обычным Run lifecycle.

## Human Gate

Workflow может открыть долговечный Gate с safe context и recipient policy.
Состояние живёт независимо от runtime Pod и внешнего канала. Web является
основной surface; optional adapter конкурирует за ту же one-winner resolution.

## События

`workflow.published`, `schedule.created`, `schedule.changed`,
`schedule.occurrence_due`, `run.created`, `run.node_added`,
`run.delegation_created`, `run.callback_delivered`, `owner_gate.opened`,
`owner_gate.resolved`.

## Критерии приёмки

- coordinator запускает минимум два child Agents и получает каждый callback
  ровно один раз;
- live graph показывает server-owned nodes и edges без анализа случайных timeline
  rows во frontend;
- Schedule работает без Mattermost и optional notification failure не портит
  core outcome;
- cancel/retry изменяет полный граф, а не только root row.
