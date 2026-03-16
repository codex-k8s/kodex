---
doc_id: EPC-CK8S-S13-D5-QUALITY-GOVERNANCE
type: epic
title: "Epic S13 Day 5: Design для Quality Governance System (Issue #494)"
status: in-review
owner_role: SA
created_at: 2026-03-16
updated_at: 2026-03-16
related_issues: [466, 469, 470, 471, 476, 484, 488, 494, 512]
related_prs: []
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-03-16-issue-494-design-epic"
---

# Epic S13 Day 5: Design для Quality Governance System (Issue #494)

## TL;DR
- Подготовлен Day5 design package Sprint S13 для handover в `run:plan`:
  - `design_doc`,
  - `api_contract`,
  - `data_model`,
  - `migrations_policy`.
- Зафиксированы typed contracts для hidden draft handoff, semantic wave map, evidence/verification surfaces, waiver/residual-risk decisions, release readiness и governance-gap feedback.
- Подготовлен owner review-ready baseline без кодовых изменений и без reopening policy semantics Sprint S13.

## Контекст
- Stage continuity: `#469 -> #471 -> #476 -> #484 -> #494 -> #512`.
- Входной baseline: PRD package Day3 и architecture package Day4 (`ADR-0015`, `ALT-0007`, initiative architecture docs).
- Scope Day5: только markdown-изменения, без runtime/code/migration execution.

## Артефакты Day5
- `docs/architecture/initiatives/s13_quality_governance_system/README.md`
- `docs/architecture/initiatives/s13_quality_governance_system/design_doc.md`
- `docs/architecture/initiatives/s13_quality_governance_system/api_contract.md`
- `docs/architecture/initiatives/s13_quality_governance_system/data_model.md`
- `docs/architecture/initiatives/s13_quality_governance_system/migrations_policy.md`

## Ключевые design-решения
- Canonical aggregate:
  - `control-plane` остаётся единственным owner package aggregate, wave lineage, decision ledger и projections.
- Hidden draft discipline:
  - raw `internal working draft` остаётся internal-only metadata;
  - publishable bridge наружу начинается только с `semantic wave map`.
- Separate constructs:
  - `risk tier`, `evidence completeness`, `verification minimum`, `waiver state`, `release readiness`, `governance feedback` не схлопываются в один boolean gate.
- Transport split:
  - `agent-runner` репортит typed signals;
  - staff/private API даёт read/decision surfaces;
  - GitHub comment остаётся read-only mirror.
- Rollout/backfill:
  - bounded backfill разрешён только по evidence-backed history;
  - rollout order фиксирован как `migrations -> control-plane -> worker -> api-gateway -> web-console`.

## Runtime и migration impact
- Runtime impact на этапе Day5: отсутствует.
- Migration impact для `run:dev`:
  - additive schema под owner `control-plane`;
  - optional nullable linkage в `flow_events`;
  - staged feature-flag enablement для domain, feedback jobs, UI и comment mirror.

## Acceptance criteria status (Issue #494)
- [x] Подготовлен design package (`design_doc`, `api_contract`, `data_model`, `migrations_policy`).
- [x] Явно описаны transport/data surfaces для change package, wave map, evidence completeness, verification status, waiver/residual-risk, release readiness и governance-gap feedback.
- [x] Сохранены architecture-level guardrails: single owner `control-plane`, worker reconciliation only under policy, thin-edge surfaces, hidden draft discipline и boundary `Sprint S13 -> Sprint S14`.
- [x] Подготовлена follow-up issue `#512` для `run:plan` без trigger-лейбла.

## Quality gates
| Gate | Что проверяем | Статус |
|---|---|---|
| `QG-S13-D5-01` Contract completeness | Есть design/API/data/migration package | passed |
| `QG-S13-D5-02` Hidden draft integrity | Raw draft не попадает в publishable projections | passed |
| `QG-S13-D5-03` Construct fidelity | Risk/evidence/verification/waiver/release/feedback остались раздельными constructs | passed |
| `QG-S13-D5-04` Boundary integrity | `control-plane` owner, `worker` reconcile-only, edge/UI thin | passed |
| `QG-S13-D5-05` Rollout discipline | Есть bounded backfill + rollout order + rollback notes | passed |
| `QG-S13-D5-06` Stage continuity | Design package готов к handover в `run:plan` | passed |

## Handover в `run:plan`
- Следующий этап: `run:plan`.
- Follow-up issue: `#512` (без trigger-лейбла; trigger ставит Owner после review).
- На plan-stage обязательно:
  - декомпозировать execution waves для package foundation, worker reconciliation/backfill, edge/UI surfaces и governance evidence gates;
  - зафиксировать DoR/DoD, rollout sequencing и test strategy;
  - подготовить следующую owner-managed issue для `run:dev`.
