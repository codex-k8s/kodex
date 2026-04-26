---
doc_id: IDX-CK8S-DOCS-0001
type: docs-index
title: kodex — индекс документации
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-04-26
---

# Индекс документации kodex

## Текущее состояние

Активная проектная каноника всё ещё опирается на `refactoring/**`, но новая структура `docs/**` уже задаёт целевую информационную архитектуру. Старые документы, код и инфраструктурные каталоги находятся в `deprecated/**` и не используются как база новой версии.

## Активные разделы

| Раздел | Назначение |
|---|---|
| `docs/platform/` | Сквозная документация платформы: продуктовая модель, архитектура, поставка и эксплуатация на уровне всей системы. |
| `docs/domains/` | Самодостаточные доменные пакеты документации, которые можно развивать и позже переносить в отдельные репозитории. |
| `docs/catalogs/` | Каталоги плагинов, пакетов руководящей документации и ролей/шаблонов. |
| `docs/delivery/` | Правила поставки, карты Issue, трассируемость и приёмка. |
| `docs/research/` | Исследовательские и исходные материалы, которые не являются каноникой без переоформления. |
| `docs/design-guidelines/` | Инженерные требования к проектированию, Go, Vue и общим правилам кода. |
| `docs/templates/` | Шаблоны для новых документов. |
| `docs/media/` | Медиа-ресурсы активной документации. |

## Основной маршрут чтения

1. Начать с `refactoring/task.md` и `refactoring/README.md`.
2. Для сквозного продуктового каркаса читать `docs/platform/product/brief.md`, `docs/platform/product/constraints.md`, `docs/platform/product/product_model.md`, `docs/platform/product/glossary.md` и `docs/platform/product/requirements.md`.
3. Для сквозной архитектуры читать `docs/platform/architecture/c4_context.md`, `docs/platform/architecture/c4_container.md`, `docs/platform/architecture/domain_map.md`, `docs/platform/architecture/service_boundaries.md`, `docs/platform/architecture/data_model.md`, `docs/platform/architecture/provider_integration_model.md` и `docs/platform/architecture/mcp_and_interaction_model.md`.
4. Для структуры документации читать `docs/index.md`, `docs/platform/README.md`, `docs/domains/README.md`, `docs/delivery/README.md`.
5. Для конкретного домена переходить в `docs/domains/<domain>/README.md`.
6. Для связи с GitHub Issue и PR использовать `docs/delivery/issue-map/`, а не один общий большой файл.
7. Для новых документов брать шаблоны из `docs/templates/index.md`.

## Правила развития

- Новые домены добавляются в `docs/domains/` как отдельные пакеты.
- Каждый домен хранит свои продуктовые, архитектурные документы, документы поставки и эксплуатации внутри своего каталога.
- Сквозные решения, которые затрагивают всю платформу, живут в `docs/platform/`.
- Каталоги плагинов, пакетов руководящей документации и ролей не смешиваются с доменными документами.
- Карта Issue разбита по доменам и отдельным рабочим срезам, чтобы параллельные агенты не конфликтовали в одном файле.
- Материалы из `deprecated/**` можно читать только как справку.

## Внешние источники

Внешние репозитории руководящей документации подключены как исходные `submodule` в `docs/external/**`, шаблоны — в `docs/templates/**`, а пакеты — в `packages/**`.
Штатный рабочий контур предполагает checkout всех submodule из `.gitmodules`. Если источник недоступен, платформа должна показать проблему доступа.
Правила работы описаны в `docs/external/AGENTS.md` и `packages/AGENTS.md`.

## Что не делать

- Не восстанавливать старые `docs/**` переносом из `deprecated/**`.
- Не создавать один растущий `issue_map.md` на весь проект.
- Не начинать новую кодовую реализацию поверх старых `cmd`, `deploy`, `libs`, `proto`, `services`, `tools` и старого `services.yaml` из `deprecated/**`.
