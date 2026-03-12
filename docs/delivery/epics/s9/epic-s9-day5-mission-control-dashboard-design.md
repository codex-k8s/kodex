---
doc_id: EPC-CK8S-S9-D5-MISSION-CONTROL
type: epic
title: "Epic S9 Day 5: Design для Mission Control Dashboard и console control plane (Issue #351)"
status: in-review
owner_role: SA
created_at: 2026-03-12
updated_at: 2026-03-12
related_issues: [333, 335, 337, 340, 351, 363]
related_prs: []
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-03-12-issue-351-design-epic"
---

# Epic S9 Day 5: Design для Mission Control Dashboard и console control plane (Issue #351)

## TL;DR
- Подготовлен полный Day5 design package Sprint S9 для Mission Control Dashboard: `design_doc`, `api_contract`, `data_model`, `migrations_policy`.
- Зафиксированы typed contracts для snapshot/details/commands/realtime/optional voice path, hybrid persisted projection model и rollout discipline `migrations -> control-plane -> worker -> api-gateway -> web-console`.
- Inline write-path ограничен provider-safe typed commands; PR review/merge/comment collaboration остаются provider deep-link-only в MVP.

## Priority
- `P0`.

## Контекст
- Intake baseline: `#333` (`docs/delivery/epics/s9/epic-s9-day1-mission-control-dashboard-intake.md`).
- Vision baseline: `#335` (`docs/delivery/epics/s9/epic-s9-day2-mission-control-dashboard-vision.md`).
- PRD baseline: `#337` (`docs/delivery/epics/s9/epic-s9-day3-mission-control-dashboard-prd.md`, `docs/delivery/epics/s9/prd-s9-day3-mission-control-dashboard.md`).
- Architecture baseline: `#340` (`docs/delivery/epics/s9/epic-s9-day4-mission-control-dashboard-arch.md` + architecture package).
- Текущий этап: `run:design` в Issue `#351`.
- Scope этапа: только markdown-изменения.

## Design package
- `docs/architecture/initiatives/s9_mission_control_dashboard/README.md`
- `docs/architecture/initiatives/s9_mission_control_dashboard/design_doc.md`
- `docs/architecture/initiatives/s9_mission_control_dashboard/api_contract.md`
- `docs/architecture/initiatives/s9_mission_control_dashboard/data_model.md`
- `docs/architecture/initiatives/s9_mission_control_dashboard/migrations_policy.md`
- `docs/delivery/traceability/s9_mission_control_dashboard_history.md`

## Ключевые design-решения
- Projection model:
  - выбран hybrid persisted model: typed tables для entities/relations/timeline/commands/voice_candidates + JSONB payload fragments для card/detail/timeline projections.
- Transport contract:
  - HTTP/staff contract фиксирует snapshot, entity details, command submit/status и WebSocket realtime stream;
  - gRPC контракт зеркалирует ownership `api-gateway -> control-plane`.
- Command safety:
  - inline write-path ограничен `discussion.create`, `work_item.create`, `discussion.formalize`, `stage.next_step.execute`, `command.retry_sync`;
  - `business_intent_key` и `expected_projection_version` обязательны для dedupe и stale-guard.
- MVP boundaries:
  - provider review/merge/comment editing остаются deep-link-only;
  - voice stream сохраняется как optional isolated candidate contour и не блокирует core dashboard rollout.
- Rollout/rollback:
  - additive migrations + warmup/backfill before read/write exposure;
  - после включения inline writes допустим только limited rollback без автоматического отката provider side effects.

## Context7 верификация
- Через Context7 подтверждён актуальный CLI syntax для continuity issue/PR flow:
  - `/websites/cli_github_manual`
- Новые внешние библиотеки Day5 не выбирались; dependency lock-in остаётся за пределами этого stage.

## Acceptance Criteria (Issue #351)
- [x] Подготовлен design package (`design_doc`, `api_contract`, `data_model`, `migrations_policy`).
- [x] Зафиксированы typed contracts для snapshot, entity details, timeline/comments projection, commands и realtime degraded path.
- [x] Определены schema ownership, migration order, rollback constraints и observability events.
- [x] Явно отделены inline write-path и provider deep-link-only действия MVP.
- [x] Подготовлена follow-up issue `#363` для stage `run:plan`.

## Quality gates
| Gate | Что проверяем | Статус |
|---|---|---|
| `QG-S9-D5-01` Contract completeness | Есть typed API/data/realtime/migration package | passed |
| `QG-S9-D5-02` Boundary integrity | Thin-edge сохранён, domain ownership не ушёл в `api-gateway`/`web-console` | passed |
| `QG-S9-D5-03` Command safety | Dedupe, stale-guard, statuses и failure mapping описаны явно | passed |
| `QG-S9-D5-04` Degraded resilience | Snapshot fallback, stale/degraded semantics и explicit refresh зафиксированы | passed |
| `QG-S9-D5-05` Stage continuity | Создана issue `#363` на `run:plan` без trigger-лейбла | passed |

## Handover в `run:plan`
- Следующий этап: `run:plan`.
- Follow-up issue: `#363`.
- Trigger-лейбл на новую issue ставит Owner после review design package.
- На plan-stage обязательно:
  - декомпозировать execution waves по projection/warmup, internal domain, worker reconcile, edge transport и frontend integration;
  - зафиксировать DoR/DoD, acceptance evidence и owner dependencies;
  - подготовить continuity issue для `run:dev`.
