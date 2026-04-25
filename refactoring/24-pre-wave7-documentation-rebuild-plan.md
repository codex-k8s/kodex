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
- Старые `docs/product`, `docs/architecture`, `docs/delivery`, `docs/ops` и `docs/research` уже выведены в `deprecated/docs/**`.
- Старые `cmd`, `deploy`, `libs`, `proto`, `services` и `tools` уже выведены в `deprecated/**` и не являются базой новой реализации.
- Новые документы создаются по шаблонам `docs/templates/**`, а не правятся поверх старых файлов.
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

### 1. Корневая структура и индексы
Создать:
- `docs/product/README.md`;
- `docs/architecture/README.md`;
- `docs/delivery/README.md`;
- `docs/ops/README.md`;
- при необходимости `docs/research/README.md`.

Результат:
- активные каталоги снова существуют;
- каждый каталог объясняет, что в нём каноника, а что ещё не создано;
- `docs/index.md` обновлён под новую структуру.

Шаблоны:
- `docs/templates/index.md`;
- `docs/templates/docset_issue.md`;
- `docs/templates/docset_pr.md`.

### 2. Продуктовый каркас
Создать минимальный продуктовый пакет:
- `docs/product/brief.md`;
- `docs/product/constraints.md`;
- `docs/product/product_model.md`;
- `docs/product/glossary.md`;
- `docs/product/requirements.md`.

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

### 3. Архитектурный каркас платформы
Создать минимальный архитектурный пакет:
- `docs/architecture/c4_context.md`;
- `docs/architecture/c4_container.md`;
- `docs/architecture/domain_map.md`;
- `docs/architecture/service_boundaries.md`;
- `docs/architecture/data_model.md`;
- `docs/architecture/provider_integration_model.md`;
- `docs/architecture/mcp_and_interaction_model.md`.

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

### 4. Wave 7 access package
До кода wave 7 создать отдельный пакет для домена доступа:
- `docs/product/access_and_accounts_requirements.md`;
- `docs/architecture/access_and_accounts_design.md`;
- `docs/architecture/access_and_accounts_data_model.md`;
- `docs/architecture/access_and_accounts_api_contract.md`;
- `docs/delivery/waves/wave7_access_and_accounts.md`.

Что обязательно зафиксировать:
- owner-организация, клиентские организации, организации внешних исполнителей и будущие SaaS-организации;
- пользователи, membership, группы и наследование прав;
- внешние аккаунты и интеграции как отдельный домен;
- связь аккаунтов с организациями, проектами, ролями и политиками;
- audit и acceptance rules для изменений доступа.

Шаблоны:
- `docs/templates/prd.md`;
- `docs/templates/design_doc.md`;
- `docs/templates/api_contract.md`;
- `docs/templates/data_model.md`;
- `docs/templates/delivery_plan.md`.

### 5. Delivery, traceability и acceptance
Создать:
- `docs/delivery/development_process.md`;
- `docs/delivery/delivery_plan.md`;
- `docs/delivery/issue_map.md`;
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

### 6. Ops и первый deploy gate
Создать:
- `docs/ops/production_runbook.md`;
- `docs/ops/bootstrap_runbook.md`;
- `docs/ops/monitoring.md`;
- `docs/ops/alerts.md`;
- `docs/ops/backup_and_cleanup.md`.

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
- Не восстанавливать старые `docs/product`, `docs/architecture`, `docs/delivery`, `docs/ops`, `docs/research` переносом файлов из архива.
- Не создавать sprint/day-epic структуру заново.
- Не ссылаться на архив как на активную канонику.
- Не писать новую реализацию поверх старых каталогов из `deprecated/**`.
- Не начинать кодовую wave 7 до согласования минимального access package.

## Gate перед кодовой wave 7
Кодовая wave 7 может стартовать, когда:
1. создан новый `docs/index.md` и индексы доменов;
2. создан минимальный product package;
3. создан минимальный architecture package;
4. создан wave 7 access package;
5. создан delivery/traceability package под wave-модель;
6. создан минимальный ops package для первого deploy gate;
7. `AGENTS.md` и `docs/design-guidelines/**` больше не указывают на архивные документы как на активные.
