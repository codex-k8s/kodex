---
type: legacy
status: superseded
superseded_by: docs/roadmap/epics-and-waves.md
updated: 2026-07-16
---

> Исторический hardening backlog MVP. Актуальный production roadmap находится в `docs/roadmap/**` и `docs/operations/**`.

# Production Gaps

Этот документ фиксирует ограничения, которые остаются после MVP dogfooding-среза. Он нужен, чтобы не смешивать краткосрочный запуск с production-ready состоянием.

## Закрыто после MVP dogfooding-среза

- У bot-service включены базовые Kubernetes security defaults: non-root UID/GID, dropped capabilities, `allowPrivilegeEscalation: false`, read-only root filesystem, `seccompProfile: RuntimeDefault`.
- У bot-service заданы начальные resource requests/limits.
- Добавлен retention cleanup runtime-ресурсов через `/agents runtime prune [duration] [--apply]`.
- Retention cleanup работает через Kubernetes client-go и labels `app.kubernetes.io/name=matter-codex-agent-runner`, `app.kubernetes.io/component=agent-run`, `matter-codex.dev/run-id`.
- По умолчанию retention cleanup выполняется в dry-run режиме.
- Agent runner image запускается non-root UID/GID `10001`, а smoke/developer/reviewer/auth Job получают `runAsNonRoot`, `seccompProfile: RuntimeDefault`, dropped capabilities, `allowPrivilegeEscalation: false`, read-only root filesystem и writable volume mounts для `/workspace`, `/codex-home`, `/home/matter-codex`, `/tmp`.
- Runtime namespace получает `ResourceQuota` и `LimitRange` с env overrides для pod/job/PVC/storage и cpu/memory requests/limits.

## Остается до production

- `sandbox_mode = "danger-full-access"` остается MVP-решением для isolated pod. Нужно вернуть более строгий sandbox policy после отдельной проверки bubblewrap/user namespace в Kubernetes.
- NetworkPolicy не включаются по умолчанию и не входят в ближайший MVP backlog. Это осознанный риск владельца: агентам может потребоваться ходить во внешние и внутренние endpoints проектов без заранее известного allowlist.
- PostgreSQL и Mattermost остаются single-server manifests. Для production нужен managed PostgreSQL или HA/backup strategy, backup restore drill и upgrade path.
- Нет автоматического scheduled retention controller. Сейчас cleanup запускается вручную через Mattermost.
- Нет per-account rate limit на GitHub/OpenAI usage и явного scheduler-а concurrent agent runs поверх Kubernetes quota.
- Нет полноценной observability цепочки по run events: metrics, alerts, traces и long-term log retention остаются будущей задачей.
- GitHub PAT остается MVP-механизмом. Production-путь должен перейти на GitHub App/installations с меньшими правами и лучшим audit.

## Следующий hardening backlog

1. Добавить scheduled retention Job/controller с теми же правилами, что `/agents runtime prune`.
2. Добавить backup/restore runbook для PostgreSQL и Mattermost data.
3. Вернуться к NetworkPolicy только как к опциональному production hardening после отдельного решения владельца.
