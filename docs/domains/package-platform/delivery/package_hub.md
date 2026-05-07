---
doc_id: DLV-CK8S-PACKAGE-HUB
type: delivery-plan
title: kodex — поставка package-hub
status: active
owner_role: EM
created_at: 2026-05-06
updated_at: 2026-05-07
related_issues: [642, 646, 650]
related_prs: []
related_docsets:
  - docs/domains/package-platform/product/requirements.md
  - docs/domains/package-platform/architecture/design.md
  - docs/domains/package-platform/architecture/data_model.md
  - docs/domains/package-platform/architecture/api_contract.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-06-package-platform-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-06
---

# Поставка package-hub

## TL;DR

`package-hub` поставляется малыми PR-срезами: сначала доменный пакет документации, затем контракты, сервисный каркас, PostgreSQL-модель, источники магазинов, установки пакетов, специализация видов пакетов и эксплуатационный контур.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования домена | `docs/domains/package-platform/product/requirements.md` |
| Дизайн домена | `docs/domains/package-platform/architecture/design.md` |
| Модель данных | `docs/domains/package-platform/architecture/data_model.md` |
| API-обзор | `docs/domains/package-platform/architecture/api_contract.md` |
| Карта Issue | `docs/delivery/issue-map/domains/package-platform.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---|---|
| PKG-1 | #642 | Доменная документация, план поставки, каталоги и карта связей готовы. |
| PKG-2 | #646 | gRPC и AsyncAPI контракты `package-hub`, действия доступа и список событий готовы. |
| PKG-3.1 | #650 | Сервисный процесс, служебный HTTP-контур, общий gRPC runtime и регистрация `PackageHubService` готовы без БД-логики. |
| PKG-3.2 | не назначено | PostgreSQL-модель источников, пакетов, версий, manifest и ценовых метаданных готова. |
| PKG-3.3 | не назначено | PostgreSQL-модель установок, схем секретов, проверки, идемпотентности и optimistic concurrency готова. |
| PKG-3.4 | не назначено | Outbox и первые repository-backed gRPC операции готовы. |
| PKG-4 | не назначено | Источники магазинов, синхронизация доступного каталога и проверка manifest готовы. |
| PKG-5 | не назначено | Установки пакетов, версии, привязки секретов и события установки готовы. |
| PKG-6 | не назначено | Специализация плагинов, руководящих пакетов, магазина и пакетов пользовательского контента платформы готова. |
| PKG-7 | не назначено | Манифесты deploy, migration job, config, health, metrics и runbook готовы. |

## Синхронизация с параллельными доменами

| Домен | Когда синхронизироваться | Причина |
|---|---|---|
| `project-catalog` | Перед PKG-4 и PKG-5 | `services.yaml` и workspace policy могут ссылаться на руководящие пакеты и package sources, но проектная политика остаётся у `project-catalog`. |
| `provider-hub` | Перед PKG-4 | Репозитории-источники пакетов, webhook, PR и Git-истина пакета идут через provider-native контур. |
| `access-manager` | Перед PKG-2 | Действия доступа для источников, установок, верификации и секретов должны быть согласованы с access catalog. |
| `runtime-manager` и `fleet-manager` | Перед PKG-5 и PKG-7 | `package-hub` фиксирует установку и требования, но runtime-нагрузку, Kubernetes и размещение исполняет runtime/fleet контур. |
| `agent-manager` | Перед PKG-6 | Руководящие пакеты и возможности пакетов влияют на подготовку агентного контекста. |
| `billing-hub` | После PKG-5 | Ценовые метаданные и факты установки нужны для будущего учёта, но счета не живут в `package-hub`. |

## Критерии начала кода

- Принят пакет доменной документации `package-platform`.
- Для каждого следующего PR есть отдельный GitHub Issue.
- PR, который завершает Issue, содержит `Closes #...` в теле PR.
- Контрактный PR создаёт proto и AsyncAPI до реализации операций.
- Старый код из `deprecated/**` не используется как основа реализации.

## Критерии завершения домена

- `package-hub` имеет свой контур данных, миграций, контрактов и событий.
- Доступные пакеты, установленные пакеты, источники магазинов, версии, manifest, схемы секретов и верификация имеют авторитетные команды и чтения.
- Сервис публикует `package.*` события через outbox и `platform-event-log`.
- `agent-manager`, `runtime-manager`, `interaction-hub`, `billing-hub` и операторская консоль могут опираться на контракты `package-hub`.
- Документы и карты Issue обновлены, хвосты перенесены в следующие срезы явно.

## Апрув

- request_id: `owner-2026-05-06-package-platform-kickoff`
- Решение: approved
- Комментарий: план поставки `package-hub` согласован как целевое состояние стартового среза.
