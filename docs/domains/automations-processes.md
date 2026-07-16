---
id: DOM-MC-009
title: Automations & Processes
type: domain
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Automations & Processes

## Назначение

Владеет Playbook, ProcessRun, child graph, callbacks, human gates, AutomationSchedule и ScheduledRun.

## Schedule semantics

- Timezone обязательна и хранится как IANA name.
- UI предлагает presets, cron доступен advanced user.
- `coalesce` является default concurrency policy.
- `run_once` является default misfire policy.
- `on_action_or_failure` является default notification policy.
- `Run now` создает отдельное occurrence и не сдвигает regular next run.

## Scheduled result

Outcome: `no_action`, `action_taken`, `requires_human`, `failed`.

`no_action` может не создавать Mattermost post. `requires_human` всегда создает доступный human gate. History и audit сохраняются для всех outcomes.

## Process orchestration

- Coordinator запускает child agents только через MCP.
- Child run имеет parent, purpose, input, completion contract и callback target.
- Callback durable и идемпотентен.
- Parent не считается завершенным, пока обязательные children/gates не завершены.
- Failure policy определяет retry, replace, skip либо human escalation.

## Human-gate lifecycle

`draft result -> review cycles -> manager ready -> owner changes/OK -> final review -> owner OK -> publish/merge -> improver`.

Owner gate относится к конкретному result version. Изменение после OK инвалидирует approval и требует повторного review/gate, кроме заранее разрешенных механических изменений.

## Acceptance

- Две scheduler replicas создают одно occurrence.
- Restart не теряет next run и не создает duplicate.
- Schedule применяет свежий RuntimeRevision.
- Headless run работает без thread.
- Manager запускает параллельные threads и получает callbacks.
- Busy target agent сохраняет несколько delegation requests без потери initiators.
- Final owner OK является обязательным до merge action.
- Improver запускается после завершенного accepted cycle.
