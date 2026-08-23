---
id: OPS-MC-001
title: Эксплуатационная основа
type: operations-index
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-23
---

# Эксплуатационная основа

| Код | Файл | Назначение |
| --- | --- | --- |
| `OPS-MC-001` | `README.md` | Индекс эксплуатационной каноники. |
| `OPS-MC-002` | `deployment-profiles.md` | Поддерживаемые web-only и optional Mattermost профили. |
| `OPS-MC-003` | `slo-capacity.md` | SLO, capacity и warm/runtime resources. |
| `OPS-MC-004` | `observability.md` | Метрики, логи, traces и диагностика service graph. |
| `OPS-MC-005` | `backup-restore.md` | Backup, restore и fresh-install boundaries. |
| `OPS-MC-006` | `deployment-rollbacks.md` | Release, rollout и rollback новой установки. |
| `OPS-MC-007` | `security-secrets.md` | Security и жизненный цикл секретов. |
| `OPS-MC-008` | `runtime-retention.md` | Retention Pod, PVC, events и artifacts. |

Пошаговые инструкции находятся в `docs/runbooks`. Kubernetes readiness каждого
Pod проверяет только сам процесс и его прямую инфраструктуру; доступность
межсервисного рабочего графа проверяется отдельным smoke/diagnostic контуром.
