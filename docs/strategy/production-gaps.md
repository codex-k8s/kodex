# Production Gaps

Этот документ фиксирует ограничения, которые остаются после MVP dogfooding-среза. Он нужен, чтобы не смешивать краткосрочный запуск с production-ready состоянием.

## Закрыто в PR 10

- У bot-service включены базовые Kubernetes security defaults: non-root UID/GID, dropped capabilities, `allowPrivilegeEscalation: false`, read-only root filesystem, `seccompProfile: RuntimeDefault`.
- У bot-service заданы начальные resource requests/limits.
- Добавлен retention cleanup runtime-ресурсов через `/agents runtime prune [duration] [--apply]`.
- Retention cleanup работает через Kubernetes client-go и labels `app.kubernetes.io/name=matter-codex-agent-runner`, `app.kubernetes.io/component=agent-run`, `matter-codex.dev/run-id`.
- По умолчанию retention cleanup выполняется в dry-run режиме.

## Остается до production

- Agent runner image пока запускается как root из `node:22-alpine`. Для production нужен отдельный non-root image с явным `USER`, writable volume paths для `gh`, `npm/codex`, `HOME`, `CODEX_HOME` и проверкой dogfood flow.
- `sandbox_mode = "danger-full-access"` остается MVP-решением для isolated pod. Нужно вернуть более строгий sandbox policy после отдельной проверки bubblewrap/user namespace в Kubernetes.
- NetworkPolicy еще не включены по умолчанию. Нужно добавить allowlist ingress/egress с учетом ingress-controller namespace, DNS, GitHub, OpenAI/Codex endpoints, Mattermost internal callbacks и PostgreSQL.
- PostgreSQL и Mattermost остаются single-server manifests. Для production нужен managed PostgreSQL или HA/backup strategy, backup restore drill и upgrade path.
- Нет автоматического scheduled retention controller. Сейчас cleanup запускается вручную через Mattermost.
- Нет quota/rate limit на число одновременных agent runs, PVC суммарный размер и GitHub/OpenAI account usage.
- Нет полноценной observability цепочки по run events: metrics, alerts, traces и long-term log retention остаются будущей задачей.
- GitHub PAT остается MVP-механизмом. Production-путь должен перейти на GitHub App/installations с меньшими правами и лучшим audit.

## Следующий hardening backlog

1. Сделать non-root agent-runner image и обновить Job securityContext.
2. Добавить namespace ResourceQuota и LimitRange для agent runs.
3. Добавить опциональные NetworkPolicy templates и server-side dry-run проверку.
4. Добавить scheduled retention Job/controller с теми же правилами, что `/agents runtime prune`.
5. Добавить backup/restore runbook для PostgreSQL и Mattermost data.
