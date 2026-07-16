---
id: ADR-MC-000
title: Реестр архитектурных решений
type: decision-index
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Реестр архитектурных решений

| ADR | Решение | Статус |
| --- | --- | --- |
| `ADR-MC-001` | Эволюционный modular monolith и поэтапное выделение сервисов | proposed |
| `ADR-MC-002` | Universal Organization/Workspace/Agent model | proposed |
| `ADR-MC-003` | Hybrid UI/GitOps ownership | proposed |
| `ADR-MC-004` | RuntimeRevision и immutable provider account affinity | proposed |
| `ADR-MC-005` | Два integration modes и mandatory approvals | proposed |
| `ADR-MC-006` | S3 как canonical artifact/session storage | proposed |
| `ADR-MC-007` | PostgreSQL-backed schedules и shared durable run queue | proposed |
| `ADR-MC-008` | BuildKit и immutable role images | proposed |
| `ADR-MC-009` | AGPLv3 + commercial licensing direction | proposed/legal-review-required |

После owner approval документационного PR статусы технических ADR меняются на `accepted`. Лицензионный ADR остается `proposed` до юридической проверки и отдельного решения о публикации.
