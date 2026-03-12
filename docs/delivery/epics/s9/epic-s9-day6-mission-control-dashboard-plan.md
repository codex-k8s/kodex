---
doc_id: EPC-CK8S-S9-D6-MISSION-CONTROL
type: epic
title: "Epic S9 Day 6: Plan для Mission Control Dashboard и console control plane (Issue #363)"
status: in-review
owner_role: EM
created_at: 2026-03-12
updated_at: 2026-03-12
related_issues: [333, 335, 337, 340, 351, 363, 369, 370, 371, 372, 373, 374, 375]
related_prs: []
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-03-12-issue-363-plan"
---

# Epic S9 Day 6: Plan для Mission Control Dashboard и console control plane (Issue #363)

## TL;DR
- Подготовлен execution package Sprint S9 для перехода в `run:dev` по Mission Control Dashboard.
- Созданы отдельные handover issues `#369..#375` для foundation, domain, reconcile, transport, UI, observability и conditional voice contour.
- Зафиксированы sequencing-waves, quality-gates, DoR/DoD и owner decisions для core rollout `migrations -> control-plane -> worker -> api-gateway -> web-console`.
- `#375` остаётся условным follow-up потоком и не блокирует core MVP wave.

## Контекст
- Stage continuity: `#333 -> #335 -> #337 -> #340 -> #351 -> #363`.
- Входной baseline: design package Day5
  - `docs/architecture/initiatives/s9_mission_control_dashboard/design_doc.md`
  - `docs/architecture/initiatives/s9_mission_control_dashboard/api_contract.md`
  - `docs/architecture/initiatives/s9_mission_control_dashboard/data_model.md`
  - `docs/architecture/initiatives/s9_mission_control_dashboard/migrations_policy.md`
- Scope текущего stage: только markdown-изменения и handover backlog.
- Правило continuity: trigger-лейблы `run:dev` на implementation issues ставит только Owner и только по wave-sequencing.

## Execution package (S9-E01..S9-E07)

| Stream | Implementation issue | Wave | Priority | Краткий scope |
|---|---:|---|---|---|
| `S9-E01` | #369 | Wave 1 | P0 | Projection schema, additive indexes, warmup/backfill foundation |
| `S9-E02` | #370 | Wave 2 | P0 | `control-plane` active-set model, relation graph и command lifecycle |
| `S9-E03` | #371 | Wave 3 | P0 | `worker` reconcile, provider sync/retry и webhook echo dedupe |
| `S9-E04` | #372 | Wave 3 | P0 | Contract-first `api-gateway` transport и realtime envelope |
| `S9-E05` | #373 | Wave 4 | P0 | `web-console` dashboard shell, board/list toggle и side panel integration |
| `S9-E06` | #374 | Wave 5 | P0 | Observability, rollout-readiness, rollback discipline и evidence gate |
| `S9-E07` | #375 | Wave 6 (conditional) | P1 | Optional voice-candidate contour под отдельный feature flag |

## Sequencing constraints
- Wave 1 (`#369`) обязательна до любого read/write exposure и до старта downstream `run:dev` потоков.
- Wave 2 (`#370`) стартует только после подтверждённого foundation-evidence по `#369`.
- Wave 3 (`#371`, `#372`) допускает ограниченный параллелизм только после завершения `#370`:
  - `#371` закрывает reconcile/retry correctness и duplicate echo handling;
  - `#372` синхронизирует edge transport с уже зафиксированным command/state contract.
- Wave 4 (`#373`) запускается после стабилизации backend + transport контуров и не дублирует projection policy во frontend.
- Wave 5 (`#374`) обязательна перед handover в `run:qa` и before enabling core write-path на rollout.
- Wave 6 (`#375`) запускается только отдельным owner decision и не блокирует core dashboard MVP.

## Quality gates (`run:plan`)

| Gate | Что проверяем | Статус |
|---|---|---|
| `QG-S9-D6-01` | Для всех execution streams созданы отдельные handover issues `#369..#375` | passed |
| `QG-S9-D6-02` | Sequencing-waves и зависимости зафиксированы в delivery-документации | passed |
| `QG-S9-D6-03` | Core rollout и conditional voice contour явно разделены | passed |
| `QG-S9-D6-04` | Rollout order `migrations -> control-plane -> worker -> api-gateway -> web-console` сохранён без отклонений | passed |
| `QG-S9-D6-05` | Traceability синхронизирована (`issue_map`, delivery plan, sprint/epic docs, traceability history, indexes) | passed |
| `QG-S9-D6-06` | Scope этапа ограничен markdown-only изменениями | passed |

## Definition of Ready / Done для перехода в `run:dev`

### Definition of Ready (`run:dev` launch)
- [x] Design package Day5 (`#351`) подтверждён как source of truth.
- [x] Execution backlog создан отдельными issue `#369..#375`.
- [x] Зафиксированы sequencing-waves и зависимости между foundation, backend, transport, UI и observability.
- [x] Core/conditional split сохранён: `#375` не блокирует запуск core waves.
- [x] Trigger-лейблы на implementation issues не выставлены автоматически.

### Definition of Done (`run:plan` stage)
- [x] Выпущен plan-эпик Day6 с execution package, QG и DoR/DoD.
- [x] Созданы handover issues `#369..#375` без trigger-лейблов.
- [x] Обновлены `delivery_plan`, sprint/epic каталоги, `issue_map` и history-пакет Sprint S9.
- [x] Зафиксированы blockers, risks и owner decisions для следующего stage.

## Self-check (common checklist)
- Проверен scope изменений: только markdown-документы (`*.md`), без кода, YAML/JSON, Dockerfile и shell-скриптов.
- Проверена консистентность stage-policy: `run:dev` остаётся единственным кодовым этапом, trigger-лейблы на follow-up issues не выставлялись.
- Проверена traceability-синхронизация для новых документов и issue refs.
- Новые внешние зависимости не выбирались; dependency catalog не менялся.
- Секреты, токены и environment values в артефактах не публиковались.

## Blockers, risks, owner decisions

| Тип | ID | Описание | Статус |
|---|---|---|---|
| blocker | `BLK-S9-D6-01` | До старта `run:dev` нужен owner review/approval plan package по Issue `#363` | open |
| blocker | `BLK-S9-D6-02` | Trigger-лейблы `run:dev` на implementation issues `#369..#375` должен выставить Owner по wave-sequencing | open |
| risk | `RSK-S9-D6-01` | Warmup/backfill foundation в `#369` может задержать downstream streams и увеличить rework при преждевременном параллелизме | monitoring |
| risk | `RSK-S9-D6-02` | Параллельный запуск `#371` и `#372` до стабилизации `#370` приведёт к transport/domain drift | monitoring |
| risk | `RSK-S9-D6-03` | Scope creep от optional voice contour способен размазать core MVP acceptance и замедлить release | monitoring |
| risk | `RSK-S9-D6-04` | Отставание `#374` по observability/evidence заблокирует `run:qa` даже при функциональной готовности UI и backend | monitoring |
| owner-decision | `OD-S9-D6-01` | Core Mission Control rollout выполняется только по issues `#369..#374`; `#375` запускается отдельным решением | proposed |
| owner-decision | `OD-S9-D6-02` | `run:dev` triggers выставляются Owner по waves, без массового старта всех implementation issues одновременно | proposed |
| owner-decision | `OD-S9-D6-03` | Handover в `run:qa` допускается только после закрытия `#374` и подтверждённого rollout-readiness evidence | proposed |

## Tooling validation
- Попытка использовать Context7 для GitHub CLI manual завершилась ошибкой `Monthly quota exceeded`.
- Для неинтерактивного PR/issue flow синтаксис сверен локально:
  - `gh issue create --help`
  - `gh pr create --help`
  - `gh pr edit --help`
- Новые внешние библиотеки на этапе Day6 не выбирались.

## Acceptance Criteria (Issue #363)
- [x] Подготовлен execution package по потокам projection schema, `control-plane`, `worker`, `api-gateway`, `web-console`, observability и conditional voice contour.
- [x] Для каждого потока зафиксированы scope, зависимости, required checks, rollout order и expected success evidence.
- [x] Guardrails сохранены явно: provider deep-link-only actions, degraded fallback, voice isolation и owner-managed review gate.
- [x] Подготовлены follow-up issues для `run:dev`: core backlog `#369..#374` и conditional continuation `#375`.

## Handover в `run:dev`
- Следующий operational stage: `run:dev`.
- Core implementation issues для запуска по waves: `#369..#374`.
- Conditional follow-up issue: `#375` (не блокирует core rollout и запускается только по отдельному owner decision).
- Для каждого `run:dev` потока обязательны:
  - PR с проверками и evidence;
  - синхронное обновление traceability документов;
  - переход в `state:in-review` на PR и Issue после завершения итерации.
