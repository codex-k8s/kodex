---
doc_id: MAP-CK8S-DOMAIN-PROJECTS-AND-REPOSITORIES
type: issue-map
title: kodex — карта Issue домена проектов и репозиториев
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-05-14
---

# Карта Issue — проекты и репозитории

## TL;DR

Долгоживущая карта домена `projects-and-repositories`.

## Матрица

| Issue/PR | Документы | Волна | Статус | Примечание |
|---|---|---|---|---|
| #628 | `docs/domains/projects-and-repositories/**`, `docs/delivery/waves/wave-008-projects-and-repositories.md`, `docs/delivery/issue-map/waves/wave-008-projects-and-repositories.md` | wave 8 | закрыта как выполненная | Стартовый срез домена: требования, дизайн, модель данных, API-контракт, план поставки и очередь малых PR-срезов. |
| #629 | `docs/domains/projects-and-repositories/architecture/api_contract.md`, `proto/kodex/projects/v1/project_catalog.proto`, `specs/asyncapi/project-catalog.v1.yaml`, `services/internal/project-catalog/**` | wave 8 | закрыта как выполненная | Контракты, сервисный каркас и доменные интерфейсы `project-catalog`. |
| #630 | `docs/domains/projects-and-repositories/architecture/data_model.md`, `services/internal/project-catalog/**` | wave 8 | закрыта как выполненная | PostgreSQL-модель, миграции, слой репозитория, outbox и тесты. |
| #631 | `docs/domains/projects-and-repositories/architecture/api_contract.md`, `services/internal/project-catalog/**`, `libs/go/grpcserver/**`, `libs/go/eventlog/**` | wave 8 | закрыта как выполненная | gRPC-операции, граница проверки доступа через `access-manager`, outbox-публикация в `platform-event-log`, доменные и транспортные тесты. |
| #632 | `docs/domains/projects-and-repositories/product/requirements.md`, `docs/domains/projects-and-repositories/architecture/design.md`, `docs/domains/projects-and-repositories/architecture/data_model.md`, `services/internal/project-catalog/**` | wave 8 | закрыта как выполненная | Политика `services.yaml`, источники проектной документации, операторские переопределения и политика рабочего контура. |
| #633 | `docs/domains/projects-and-repositories/delivery/wave8_project_catalog.md`, `deploy/base/project-catalog/**`, `scripts/**`, `bootstrap/remote/70_finalize_and_report.sh` | wave 8 | закрыта как выполненная | Правила веток, релизная политика, политика размещения, манифесты, проверочный путь и закрытие Wave 8. |
| #639 | `docs/design-guidelines/go/**`, `services/internal/project-catalog/**`, `services/internal/access-manager/**`, `libs/go/**` | после wave 8.2 | закрыта как выполненная | Системный перевод PostgreSQL-сканеров на штатные помощники `pgx`, где не нужна ручная доменная конвертация. |
| #281, #282 | `docs/platform/architecture/repository_onboarding.md`, `docs/domains/projects-and-repositories/**`, `docs/delivery/issue-map/domains/provider-native-work-items.md` | wave 8 + wave 10 | модель выбрана, ждёт реализации | Проектный каталог и основание проектной политики готовы. Владелец выбрал гибридный вариант C: `services.yaml` остаётся Git-декларацией, workspace собирается платформой, шаблоны репозиториев приходят через `package-hub`, Git submodule используется только явно. |
