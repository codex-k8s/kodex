---
doc_id: EPC-CK8S-S12-D6-GITHUB-RATE-LIMIT
type: epic
title: "Epic S12 Day 6: Plan для GitHub API rate-limit resilience (Issue #423)"
status: approved
owner_role: EM
created_at: 2026-03-13
updated_at: 2026-03-13
related_issues: [366, 413, 416, 418, 420, 423, 425, 426, 427, 428, 429, 430, 431]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-03-13-issue-423-plan"
  approved_by: "ai-da-stas"
  approved_at: 2026-03-13
---

# Epic S12 Day 6: Plan для GitHub API rate-limit resilience (Issue #423)

## TL;DR
- Подготовлен execution package Sprint S12 для перехода в `run:dev` по GitHub API rate-limit resilience.
- Созданы отдельные handover issues `#425..#431` для schema foundation, `control-plane` semantics, worker auto-resume, `agent-runner` handoff, contract-first visibility transport, `web-console` UX и observability/readiness gate.
- Зафиксированы sequencing-waves, quality-gates, DoR/DoD и owner decisions для rollout `migrations -> control-plane -> worker -> agent-runner -> api-gateway -> web-console`.
- Документный контур `intake -> vision -> prd -> arch -> design -> plan` согласован и завершён; дальнейший handover идёт только через owner-managed execution waves `#425..#431`.

## Контекст
- Stage continuity: `#366 -> #413 -> #416 -> #418 -> #420 -> #423`.
- Входной baseline: design package Day5
  - `docs/architecture/initiatives/s12_github_api_rate_limit_resilience/design_doc.md`
  - `docs/architecture/initiatives/s12_github_api_rate_limit_resilience/api_contract.md`
  - `docs/architecture/initiatives/s12_github_api_rate_limit_resilience/data_model.md`
  - `docs/architecture/initiatives/s12_github_api_rate_limit_resilience/migrations_policy.md`
- Scope текущего stage: только markdown-изменения и handover backlog.
- Правило continuity: trigger-лейблы `run:dev` на implementation issues ставит только Owner и только по wave-sequencing.

## Execution package (S12-E01..S12-E07)

| Stream | Implementation issue | Wave | Priority | Краткий scope |
|---|---:|---|---|---|
| `S12-E01` | #425 | Wave 1 | P0 | `control-plane` schema foundation: additive wait/evidence tables, dominant wait linkage и repository contracts |
| `S12-E02` | #426 | Wave 2 | P0 | `control-plane` classification, contour attribution, typed visibility projection и deterministic resume policy |
| `S12-E03` | #427 | Wave 3 | P0 | `worker` auto-resume sweeps, bounded retry attempts, manual-action escalation и replay scheduling |
| `S12-E04` | #428 | Wave 4 | P0 | `agent-runner` typed signal handoff, session snapshot persistence и deterministic resume payload |
| `S12-E05` | #429 | Wave 5 | P0 | Contract-first `api-gateway` visibility exposure, DTO/casters и additive transport contracts |
| `S12-E06` | #430 | Wave 6 | P0 | `web-console` wait queue, run visibility, contour attribution и manual-action guidance на typed API |
| `S12-E07` | #431 | Wave 7 | P0 | Observability, rollout/rollback readiness и обязательный evidence gate перед `run:qa` |

## Sequencing constraints
- Wave 1 (`#425`) закладывает persisted source-of-truth для `github_rate_limit_waits`, `github_rate_limit_wait_evidence` и dominant wait linkage до любого live visibility/replay path.
- Wave 2 (`#426`) стартует только после подтверждённого foundation-evidence по `#425` и остаётся единственным owner для classification, recovery hints и typed projection semantics.
- Wave 3 (`#427`) реализует reconciliation строго поверх contracts `#425` + `#426` и не переизобретает domain classification локально в `worker`.
- Wave 4 (`#428`) разрешена только после готовности `#427`: `agent-runner` должен handoff'ить typed signal в уже готовый domain/resume контур, а не формировать recovery policy внутри pod.
- Wave 5 (`#429`) открывает только thin-edge visibility surface и не может стартовать до стабилизации persisted/domain/worker/runner foundation.
- Wave 6 (`#430`) интегрирует UX только поверх typed transport contracts и не дублирует classification/recovery logic во frontend.
- Wave 7 (`#431`) обязательна перед handover в `run:qa`: без отдельного observability/readiness evidence controlled wait capability считается незавершённой, даже если core flow локально работает.

## Quality gates (`run:plan`)

| Gate | Что проверяем | Статус |
|---|---|---|
| `QG-S12-D6-01` | Для всех execution streams созданы отдельные handover issues `#425..#431` | passed |
| `QG-S12-D6-02` | Sequencing-waves и зависимости зафиксированы в delivery-документации | passed |
| `QG-S12-D6-03` | Ownership split сохранён: `control-plane`/`worker`/`agent-runner`/`api-gateway`/`web-console` не смешивают доменные обязанности | passed |
| `QG-S12-D6-04` | Rollout order `migrations -> control-plane -> worker -> agent-runner -> api-gateway -> web-console` выражен явно | passed |
| `QG-S12-D6-05` | `#431` зафиксирован как обязательный evidence gate перед `run:qa` | passed |
| `QG-S12-D6-06` | Traceability синхронизирована (`issue_map`, delivery plan, sprint/epic docs, traceability history, initiative package) | passed |
| `QG-S12-D6-07` | Scope этапа ограничен markdown-only изменениями и без auto-trigger labels на follow-up issues | passed |

## Definition of Ready / Done для перехода в `run:dev`

### Definition of Ready (`run:dev` launch)
- [x] Design package Day5 (`#420`) подтверждён как source of truth.
- [x] Execution backlog создан отдельными issues `#425..#431`.
- [x] Зафиксированы sequencing-waves и зависимости между schema foundation, domain semantics, worker reconciliation, runner handoff, transport/UI visibility и observability gate.
- [x] `#431` выражен как обязательный readiness gate и не размыт между functional waves.
- [x] Trigger-лейблы на implementation issues не выставлены автоматически.

### Definition of Done (`run:plan` stage)
- [x] Выпущен plan-эпик Day6 с execution package, QG и DoR/DoD.
- [x] Созданы handover issues `#425..#431` без trigger-лейблов.
- [x] Обновлены `delivery_plan`, sprint/epic каталоги, `issue_map`, traceability history и initiative package Sprint S12.
- [x] Зафиксированы blockers, risks и owner decisions для следующего stage.

## Self-check (common checklist)
- Проверен scope изменений: только markdown-документы (`*.md`), без кода, YAML/JSON, Dockerfile и shell-скриптов.
- Проверена консистентность stage-policy: `run:dev` остаётся единственным кодовым этапом, trigger-лейблы на follow-up issues не выставлялись.
- Проверена traceability-синхронизация для day6-эпика и issue refs `#425..#431`.
- Новые внешние зависимости не выбирались; dependency catalog не менялся.
- Секреты, токены и environment values в артефактах не публиковались.

## Blockers, risks, owner decisions

| Тип | ID | Описание | Статус |
|---|---|---|---|
| blocker | `BLK-S12-D6-01` | До старта `run:dev` нужен owner review/approval plan package по Issue `#423` | open |
| blocker | `BLK-S12-D6-02` | Trigger-лейблы `run:dev` на implementation issues `#425..#431` должен выставить Owner по wave-sequencing | open |
| risk | `RSK-S12-D6-01` | Если `#425` разрастётся beyond schema/repository foundation, drift между persisted model и domain semantics заблокирует дальнейшие waves | monitoring |
| risk | `RSK-S12-D6-02` | Параллельный старт `#427` или `#428` до стабилизации `#426` может вернуть local retry drift и неконсистентный resume policy | monitoring |
| risk | `RSK-S12-D6-03` | Если `#429`/`#430` начнут выводить UX до готовности `#428`, owner/operator visibility потеряет связь с каноническим wait projection | monitoring |
| risk | `RSK-S12-D6-04` | Отставание `#431` по evidence заблокирует `run:qa`, даже если functional scope будет локально реализован | monitoring |
| owner-decision | `OD-S12-D6-01` | Core rollout выполняется только по волнам `#425 -> #426 -> #427 -> #428 -> #429 -> #430 -> #431`; массовый параллельный старт всех streams запрещён | proposed |
| owner-decision | `OD-S12-D6-02` | `run:dev` triggers выставляются Owner по waves, без автоматического старта при создании follow-up issues | proposed |
| owner-decision | `OD-S12-D6-03` | Handover в `run:qa` допускается только после закрытия `#431` и подтверждённого readiness evidence | proposed |
| owner-decision | `OD-S12-D6-04` | Predictive budgeting, multi-provider governance и adapter-specific notifications остаются отдельным follow-up контуром после core Sprint S12 | proposed |

## Tooling validation
- Context7 `/github/docs` использован для повторной верификации GitHub guidance по primary/secondary rate limits, `Retry-After`, wait-at-least-one-minute и exponential backoff.
- Для неинтерактивного PR/issue flow синтаксис дополнительно сверен локально:
  - `gh issue create --help`
  - `gh pr create --help`
  - `gh pr edit --help`
- Новые внешние библиотеки на этапе Day6 не выбирались.

## Acceptance Criteria (Issue #423)
- [x] Подготовлен execution package по потокам schema foundation, `control-plane`, `worker`, `agent-runner`, `api-gateway`, `web-console` и observability/readiness.
- [x] Для каждого потока зафиксированы scope, зависимости, required checks, rollout order и expected success evidence.
- [x] Guardrails сохранены явно: split `platform PAT` vs `agent bot-token`, hard-failure separation, no infinite local retries, thin-edge boundary и owner-managed review gate.
- [x] Подготовлены follow-up issues для `run:dev`: core backlog `#425..#431` без trigger-лейблов.

## Handover в `run:dev`
- Следующий operational stage: `run:dev`.
- Implementation issues для запуска по waves: `#425..#431`.
- `#425` закладывает schema foundation, `#426` владеет classification/projection, `#427` закрывает worker auto-resume, `#428` интегрирует runner handoff, `#429` ограничен thin-edge transport, `#430` переводит capability в staff UX, `#431` остаётся обязательным evidence gate перед `run:qa`.
- Для каждого `run:dev` потока обязательны:
  - PR с проверками и evidence;
  - синхронное обновление traceability документов;
  - переход в `state:in-review` на PR и Issue после завершения итерации.
