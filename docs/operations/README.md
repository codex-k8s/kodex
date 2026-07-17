---
id: OPS-MC-001
title: Эксплуатационная основа
type: operations-index
status: approved
owner: sre
version: 0.2.0
updated: 2026-07-17
---

# Эксплуатационная основа

| Код | Файл | Назначение |
| --- | --- | --- |
| `OPS-MC-001` | `docs/operations/README.md` | Индекс. |
| `OPS-MC-002` | `docs/operations/deployment-profiles.md` | Начальная и промышленная топологии. |
| `OPS-MC-003` | `docs/operations/slo-capacity.md` | SLO, capacity и resource control. |
| `OPS-MC-004` | `docs/operations/observability.md` | Метрики, логи, трассировки и оповещения. |
| `OPS-MC-005` | `docs/operations/backup-restore.md` | Backup, PITR и restore drills. |
| `OPS-MC-006` | `docs/operations/deployment-rollbacks.md` | CI/CD, развертывание и откат. |
| `OPS-MC-007` | `docs/operations/security-secrets.md` | Безопасность и жизненный цикл секретов. |
| `OPS-MC-008` | `docs/operations/runtime-retention.md` | Хранение и очистка pod, PVC и архивов сессий. |
| `OPS-MC-009` | `docs/operations/interaction-capability-retention.md` | Ограниченное хранение и очистка capability интерактивных callback. |

Конкретные команды для инцидентов и установки остаются в `docs/runbooks`. Эксплуатационные документы определяют обязательный промышленный контракт, а пошаговая инструкция — конкретное выполнение.
