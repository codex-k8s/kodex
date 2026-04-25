---
doc_id: IDX-CK8S-TEMPLATES-0001
type: template-index
title: "Template Catalog Index"
status: active
owner_role: KM
created_at: 2026-03-11
updated_at: 2026-04-25
related_issues: [320]
related_prs: []
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-03-11-template-index"
---

# Template Catalog

## TL;DR
- `docs/templates/` содержит только канонические шаблоны документов.
- Для навигации по фактической проектной документации используется `docs/index.md`, а не этот каталог.
- Внешний публичный источник шаблонов: `github.com/codex-k8s/kodex-doc-templates-ru`, локальный submodule `external-docs/templates`.
- Изменения в локальной копии и внешнем репозитории шаблонов синхронизируются через PR в соответствующих репозиториях.

## Шаблоны по ролям
- PM: `problem.md`, `brief.md`, `scope_mvp.md`, `constraints.md`, `project_charter.md`, `success_metrics.md`, `prd.md`, `nfr.md`, `user_story.md`
- EM/KM: `delivery_plan.md`, `epic.md`, `definition_of_done.md`, `issue_map.md`, `roadmap.md`, `docset_issue.md`, `docset_pr.md`
- SA: `c4_context.md`, `c4_container.md`, `adr.md`, `alternatives.md`, `design_doc.md`, `api_contract.md`, `data_model.md`, `migrations_policy.md`
- QA: `test_strategy.md`, `test_plan.md`, `test_matrix.md`, `regression_checklist.md`, `postdeploy_review.md`
- SRE: `release_plan.md`, `release_notes.md`, `rollback_plan.md`, `runbook.md`, `monitoring.md`, `alerts.md`, `slo.md`, `incident_playbook.md`, `incident_postmortem.md`

## Правила применения
- Шаблон остаётся шаблоном: фактический документ создаётся в активной структуре `docs/platform/**`, `docs/domains/**`, `docs/catalogs/**`, `docs/delivery/**` или `docs/research/**`.
- Сквозные документы платформы создаются в `docs/platform/**`; документы конкретного домена — внутри соответствующего `docs/domains/<domain>/**`; каталоги плагинов, пакетов руководящей документации, ролей и шаблонов промптов — в `docs/catalogs/**`.
- Шаблон `issue_map.md` применяется только к доменным и волновым картам внутри `docs/delivery/issue-map/**`, а не к одному общему файлу на весь проект.
- Если template-path меняется, в том же PR синхронизируются актуальные контракты ролей, flow и шаблонов промптов.
- Индексы проектов, handover-пакеты и исторические sprint artifacts не хранятся в `docs/templates/`.

## Смежные индексы
- Корневой индекс: `docs/index.md`
- План пересборки документации: `refactoring/24-pre-wave7-documentation-rebuild-plan.md`
- Локальные инструкции: `docs/templates/AGENTS.md`
- Исходный публичный репозиторий шаблонов: `github.com/codex-k8s/kodex-doc-templates-ru`
- Архив старых материалов: `deprecated/README.md`
