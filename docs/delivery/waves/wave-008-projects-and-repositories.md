---
doc_id: DLV-CK8S-WAVE-008
type: delivery-plan
title: kodex — волна 8, проекты и репозитории
status: active
owner_role: EM
created_at: 2026-05-05
updated_at: 2026-05-05
related_issues: [628, 629, 630, 631, 632, 633, 281, 282]
related_prs: []
related_docsets:
  - docs/domains/projects-and-repositories/product/requirements.md
  - docs/domains/projects-and-repositories/architecture/design.md
  - docs/domains/projects-and-repositories/architecture/data_model.md
  - docs/domains/projects-and-repositories/architecture/api_contract.md
  - docs/domains/projects-and-repositories/delivery/wave8_project_catalog.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-05-wave8-project-catalog-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-05
---

# Волна 8 — проекты и репозитории

## TL;DR

Волна 8 реализует сервис-владелец `project-catalog`: проекты, репозитории, `services.yaml`, источники проектной документации, правила веток, релизные политики и политику размещения.

Стартовый срез закрывает #628 и открывает кодовую очередь #629-#633.

## Входные документы

| Документ | Путь |
|---|---|
| Требования домена | `docs/domains/projects-and-repositories/product/requirements.md` |
| Дизайн домена | `docs/domains/projects-and-repositories/architecture/design.md` |
| Модель данных | `docs/domains/projects-and-repositories/architecture/data_model.md` |
| API-обзор | `docs/domains/projects-and-repositories/architecture/api_contract.md` |
| Детальный план поставки | `docs/domains/projects-and-repositories/delivery/wave8_project_catalog.md` |

## Структура работ

| Направление | Issue | Результат |
|---|---|---|
| Стартовый срез | #628 | Доменный пакет, план поставки и карты связей. |
| Контракты и каркас | #629 | Proto и AsyncAPI как источники правды, каркас сервиса `project-catalog`, доменные типы и интерфейсы. |
| PostgreSQL и слой репозитория | #630 | Миграции, слой репозитория, outbox, оптимистичная конкуренция, тесты. |
| gRPC и события | #631 | Команды, чтения, граница проверки доступа, `project.*` события. |
| `services.yaml` и документация | #632 | Политика, источники документации, политика рабочего контура. |
| Правила веток, релизы, размещение и deploy | #633 | Правила веток, релизная политика, политика размещения, манифесты и закрывающий контрольный срез. |

## Критерии начала

- Волна 7 принята.
- Post-wave аудит N+1 завершён.
- Для Wave 8 заведены малые GitHub Issues.
- Стартовый PR содержит `Closes` для стартовой Issue.

## Критерии завершения

- `project-catalog` владеет своим контуром данных, миграций, контрактов и событий.
- Сервис не читает и не меняет БД других сервисов.
- Provider-native операции остаются в `provider-hub`.
- Runtime-исполнение остаётся в `runtime-manager`.
- Документы и карты Issue отражают, какие части #281 и #282 закрыты, а какие переходят в provider-native слой.

## Карты Issue

- Доменная карта: `docs/delivery/issue-map/domains/projects-and-repositories.md`.
- Волновая карта: `docs/delivery/issue-map/waves/wave-008-projects-and-repositories.md`.
