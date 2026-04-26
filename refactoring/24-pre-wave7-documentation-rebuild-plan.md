---
doc_id: REF-CK8S-0024
type: documentation-plan
title: "kodex — план пересборки документации перед wave 7"
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-04-25
related_issues: [599, 600, 601, 602]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-25-pre-wave7-docs"
---

# План пересборки документации перед wave 7

## TL;DR
- Перед кодовой wave 7 нужно заново собрать минимальную каноническую документацию в `docs/**`.
- Старые `docs/product`, `docs/architecture`, `docs/delivery`, `docs/ops` и `docs/research` уже выведены в `deprecated/docs/**` и не восстанавливаются как активная структура.
- Старые `cmd`, `deploy`, `libs`, `proto`, `services`, `tools` и полный `services.yaml` уже выведены в `deprecated/**` и не являются базой новой реализации.
- Корневой `services.yaml` до wave 7 остаётся минимальным черновиком без старых сервисов, версий, окружений и runtime-настроек.
- Новые документы создаются по шаблонам `docs/templates/**`, а не правятся поверх старых файлов.
- Выбранная структура wave 6.1: `docs/platform/**` для сквозной платформенной каноники, `docs/domains/**` для доменных пакетов, `docs/catalogs/**` для каталогов расширений и пакетов, `docs/delivery/**` для управления поставкой.
- Цель — войти в wave 7 с понятной моделью доступа, организаций, групп, пользователей и внешних аккаунтов, а также с общей картой новой платформы.

## Что уже считается источником правды
1. `refactoring/task.md` — главный мандат.
2. `refactoring/README.md` — индекс программы.
3. `refactoring/01-program-charter.md` — правила программы.
4. `refactoring/02-doc-governance.md` — правила ведения документации.
5. `refactoring/03-domain-map.md` — доменная карта.
6. `refactoring/06-product-model.md` — продуктовая модель.
7. `refactoring/09-target-architecture.md` — целевая архитектура.
8. `refactoring/10-service-boundaries.md` — границы сервисов.
9. `refactoring/11-data-and-state-model.md` — модель данных и состояния.
10. `refactoring/20-foundation-expansion-wave5-1.md` — enterprise-заделы.
11. `refactoring/21-runtime-deploy-and-bootstrap.md` и `refactoring/22-first-deployment-and-wave7-gate.md` — runtime/deploy/bootstrap и gate первого развёртывания.

## Порядок разработки новой `docs/**`

### 1. Wave 6.1: корневая структура и индексы
Создать:
- `docs/index.md`;
- `docs/platform/README.md`;
- `docs/platform/product/README.md`;
- `docs/platform/architecture/README.md`;
- `docs/platform/delivery/README.md`;
- `docs/platform/ops/README.md`;
- `docs/domains/README.md`;
- `docs/catalogs/README.md`;
- `docs/catalogs/plugins/README.md`;
- `docs/catalogs/guidance-packages/README.md`;
- `docs/catalogs/prompt-roles/README.md`;
- `docs/delivery/README.md`;
- `docs/delivery/waves/README.md`;
- `docs/research/README.md`.

Результат:
- активные каталоги снова существуют, но не повторяют старую плоскую структуру;
- каждый каталог объясняет, что в нём каноника, а что ещё будет создано позже;
- `docs/index.md` становится корневой картой новой документации.

Шаблоны:
- `docs/templates/index.md`;
- `docs/templates/docset_issue.md`;
- `docs/templates/docset_pr.md`.

### 2. Wave 6.1: доменные пакеты и раздельная карта Issue
Создать доменные индексы:
- `docs/domains/access-and-accounts/README.md`;
- `docs/domains/projects-and-repositories/README.md`;
- `docs/domains/provider-native-work-items/README.md`;
- `docs/domains/package-platform/README.md`;
- `docs/domains/agent-orchestration/README.md`;
- `docs/domains/runtime-and-fleet/README.md`;
- `docs/domains/interaction-hub/README.md`;
- `docs/domains/console-and-operations-ux/README.md`;
- `docs/domains/risk-and-release-governance/README.md`;
- `docs/domains/billing-and-cost-accounting/README.md`;
- `docs/domains/knowledge-lifecycle/README.md`.

Создать карту связей:
- `docs/delivery/issue-map/README.md`;
- `docs/delivery/issue-map/index.md`;
- `docs/delivery/issue-map/domains/*.md`;
- `docs/delivery/issue-map/waves/*.md`.

Результат:
- больше нет одного быстрорастущего `issue_map.md`;
- доменные агенты правят разные файлы и реже конфликтуют при merge;
- волновой файл фиксирует временную картину поставки, доменный файл — долгоживущую связь `Issue/PR ↔ документы`.

Шаблоны:
- `docs/templates/issue_map.md`;
- `docs/templates/delivery_plan.md`;
- `docs/templates/definition_of_done.md`.

### 3. Wave 6.2: сквозной продуктовый каркас платформы
Создать минимальный продуктовый пакет:
- `docs/platform/product/brief.md`;
- `docs/platform/product/constraints.md`;
- `docs/platform/product/product_model.md`;
- `docs/platform/product/glossary.md`;
- `docs/platform/product/requirements.md`.

Что обязательно зафиксировать:
- provider-first рабочая модель;
- инициатива как `Issue` со своим типом, а не отдельная проектная сущность;
- flow/stage/role/prompt model;
- организации, группы, пользователи, внешние аккаунты;
- пакетная платформа, руководящая документация, runtime/fleet, billing, release policy и automation как обязательные заделы.

Шаблоны:
- `docs/templates/brief.md`;
- `docs/templates/constraints.md`;
- `docs/templates/prd.md`;
- `docs/templates/nfr.md`;
- `docs/templates/personas.md`.

### 4. Wave 6.3: сквозной архитектурный каркас платформы
Создать минимальный архитектурный пакет:
- `docs/platform/architecture/c4_context.md`;
- `docs/platform/architecture/c4_container.md`;
- `docs/platform/architecture/domain_map.md`;
- `docs/platform/architecture/service_boundaries.md`;
- `docs/platform/architecture/data_model.md`;
- `docs/platform/architecture/provider_integration_model.md`;
- `docs/platform/architecture/mcp_and_interaction_model.md`.

Что обязательно зафиксировать:
- новые сервисные границы без доработки старого control-plane как базы;
- provider-native work items;
- платформенный MCP для быстрых manager-операций;
- хранилище состояния, jobs, run, slot и audit без записи полных логов в БД;
- Vault как каноническое хранилище секретов платформы и её зависимостей, без навязывания проектам единого secret store.

Шаблоны:
- `docs/templates/c4_context.md`;
- `docs/templates/c4_container.md`;
- `docs/templates/adr.md`;
- `docs/templates/alternatives.md`;
- `docs/templates/data_model.md`.

### 5. Wave 6.4: пакет домена доступа перед кодовой wave 7
До кода wave 7 создать отдельный пакет для домена доступа:
- `docs/domains/access-and-accounts/product/requirements.md`;
- `docs/domains/access-and-accounts/architecture/design.md`;
- `docs/domains/access-and-accounts/architecture/data_model.md`;
- `docs/domains/access-and-accounts/architecture/api_contract.md`;
- `docs/domains/access-and-accounts/delivery/wave7_access_and_accounts.md`;
- `docs/delivery/waves/wave-007-access-and-accounts.md`;

Что обязательно зафиксировать:
- owner-организация, клиентские организации, организации внешних исполнителей и будущие SaaS-организации;
- пользователи, членство, группы и наследование прав;
- внешние аккаунты и интеграции как отдельный домен;
- связь аккаунтов с организациями, проектами, ролями и политиками;
- audit и acceptance rules для изменений доступа.

Шаблоны:
- `docs/templates/prd.md`;
- `docs/templates/design_doc.md`;
- `docs/templates/api_contract.md`;
- `docs/templates/data_model.md`;
- `docs/templates/delivery_plan.md`.

### 6. Wave 6.5: delivery, трассируемость и acceptance
Создать:
- `docs/platform/delivery/development_process.md`;
- `docs/platform/delivery/delivery_plan.md`;
- `docs/delivery/requirements_traceability.md`;
- `docs/delivery/waves/README.md`.

Что обязательно зафиксировать:
- работа волнами, а не старыми sprint/day-epic пакетами;
- один PR = один проверяемый срез;
- документы в PR сразу отражают целевое согласованное состояние;
- machine acceptance перед merge проверяет обязательные поля, водяные метки, связи `Issue/PR/MR` и evidence;
- старые документы используются только как справка.

Шаблоны:
- `docs/templates/delivery_plan.md`;
- `docs/templates/definition_of_done.md`;
- `docs/templates/issue_map.md`.

### 7. Wave 6.6: ops и первый deploy gate
Создать:
- `docs/platform/ops/production_runbook.md`;
- `docs/platform/ops/bootstrap_runbook.md`;
- `docs/platform/ops/monitoring.md`;
- `docs/platform/ops/alerts.md`;
- `docs/platform/ops/backup_and_cleanup.md`.

Что обязательно зафиксировать:
- первый deploy новой версии начинается только после готовности минимального вертикального среза;
- cleanup jobs для образов, БД и runtime-старья контролируются платформой как jobs с видимым статусом;
- полные логи не пишутся в PostgreSQL, но последние ошибки и диагностические выдержки доступны из БД;
- уведомления по критичным job failures, release events и owner gates проектируются как часть interaction hub.

Шаблоны:
- `docs/templates/runbook.md`;
- `docs/templates/monitoring.md`;
- `docs/templates/alerts.md`;
- `docs/templates/slo.md`;
- `docs/templates/rollback_plan.md`.

## Что не делать
- Не восстанавливать старые `docs/product`, `docs/architecture`, `docs/ops`, `docs/research` и единый `docs/delivery/issue_map.md` переносом файлов из архива.
- Не создавать sprint/day-epic структуру заново.
- Не ссылаться на архив как на активную канонику.
- Не писать новую реализацию поверх старых каталогов из `deprecated/**`.
- Не начинать кодовую wave 7 до согласования минимального пакета домена доступа.

## Gate перед кодовой wave 7
Кодовая wave 7 может стартовать, когда:
1. создан новый `docs/index.md` и индексы `docs/platform/**`, `docs/domains/**`, `docs/catalogs/**`;
2. создан минимальный сквозной продуктовый пакет в `docs/platform/product/**`;
3. создан минимальный сквозной архитектурный пакет в `docs/platform/architecture/**`;
4. создан пакет домена доступа в `docs/domains/access-and-accounts/**`;
5. создан пакет поставки и трассируемости под wave-модель и раздельную карту `docs/delivery/issue-map/**`;
6. создан минимальный ops-пакет в `docs/platform/ops/**` для первого deploy gate;
7. `AGENTS.md` и `docs/design-guidelines/**` больше не указывают на архивные документы как на активные.
