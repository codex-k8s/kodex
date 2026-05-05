---
doc_id: DLV-CK8S-PROJ-WAVE8
type: delivery-plan
title: kodex — поставка project-catalog
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
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-05-wave8-project-catalog-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-05
---

# Поставка project-catalog

## TL;DR

`project-catalog` поставляется малыми PR-срезами: сначала стартовый доменный срез, затем контракты и сервисный каркас, PostgreSQL-модель, gRPC-операции, `services.yaml` и документационные источники, затем правила веток, релизные политики и эксплуатационный контур.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования домена | `docs/domains/projects-and-repositories/product/requirements.md` |
| Дизайн домена | `docs/domains/projects-and-repositories/architecture/design.md` |
| Модель данных | `docs/domains/projects-and-repositories/architecture/data_model.md` |
| API-контракт | `docs/domains/projects-and-repositories/architecture/api_contract.md` |
| Волновой план | `docs/delivery/waves/wave-008-projects-and-repositories.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---|---|
| Стартовый срез | #628 | Доменная документация, план поставки и карты связей готовы. |
| 8.1 | #629 | Контракты `project-catalog`, сервисный каркас и доменные интерфейсы. |
| 8.2 | #630 | PostgreSQL-модель, миграции, слой репозитория, outbox и тесты. |
| 8.3 | #631 | gRPC-операции, проверки доступа, события и транспортные тесты. |
| 8.4 | #632 | Политика `services.yaml`, источники документации и политика рабочего контура. |
| 8.5 | #633 | Правила веток, релизная политика, политика размещения, манифесты deploy и закрытие Wave 8. |

## Связь с задачами подключения репозиториев

Задачи #281 и #282 остаются открытыми после стартового среза. Wave 8 создаёт проектный каталог и основание проектной политики для этих сценариев, но полное закрытие подключения репозиториев требует `provider-hub` и provider-native рабочих сущностей.

Решение:
- часть про проект, репозиторий и политику закрывается в Wave 8;
- создание или сканирование репозитория у провайдера, первичный PR и provider-native связи закрываются после появления `provider-hub`;
- финальный статус #281 и #282 уточняется в закрывающем срезе Wave 8 и в плане Wave 10.

## Критерии начала кода

- Принят пакет доменной документации `projects-and-repositories`.
- Для каждого следующего PR есть отдельный GitHub Issue.
- PR, который завершает Issue, содержит `Closes #...` в теле PR.
- Старый код из `deprecated/**` не используется как основа реализации.

## Критерии завершения Wave 8

- `project-catalog` имеет свой контур данных, миграций, контрактов и событий.
- Проекты, репозитории, политика `services.yaml`, источники документации, правила веток, релизная политика и политика размещения имеют авторитетные команды и чтения.
- Сервис публикует `project.*` события через outbox и `platform-event-log`.
- `agent-manager`, `runtime-manager`, `provider-hub` и `operations-hub` могут опираться на контракты `project-catalog`.
- Документы и карты Issue обновлены, хвосты перенесены в следующие волны явно.

## Апрув

- request_id: `owner-2026-05-05-wave8-project-catalog-kickoff`
- Решение: approved
- Комментарий: план поставки `project-catalog` согласован как целевое состояние стартового среза.
