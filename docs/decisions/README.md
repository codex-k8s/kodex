---
id: ADR-MC-000
title: Реестр архитектурных решений
type: decision-index
status: approved
owner: architect
version: 1.0.1
updated: 2026-08-23
---

# Реестр архитектурных решений

| ADR | Решение | Статус |
| --- | --- | --- |
| `ADR-MC-002` | Универсальная web-first модель `Organization`, `Project` и `Agent` | approved |
| `ADR-MC-004` | `RuntimeRevision` и неизменяемая привязка учетной записи поставщика | approved |
| `ADR-MC-005` | Два режима интеграций и обязательные согласования | approved |
| `ADR-MC-006` | Bounded artifact storage boundary без обязательного S3 | approved |
| `ADR-MC-007` | Долговечные расписания и очередь запусков в PostgreSQL | approved |
| `ADR-MC-008` | BuildKit и неизменяемые образы ролей | approved |
| `ADR-MC-009` | Публичная редакция AGPL и коммерческая лицензия | approved/legal-review-required |
| `ADR-MC-010` | Терминальная обработка блокировок политики поставщика | approved |
| `ADR-MC-011` | Настраиваемые политики координации и внимания инициатора | approved |
| `ADR-DOC-004` | Transactional outbox, broker-neutral relay, NATS и durable inbox | approved |
| `ARCH-MC-011` | Owner-approved web-first reset и fresh baseline | approved |

Статус `approved` фиксирует принятое владельцем направление. Для `ADR-MC-009` юридическая проверка текстов лицензий и CLA остается обязательным условием выпуска, но повторный выбор модели не требуется.
