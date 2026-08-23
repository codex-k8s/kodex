# Архитектура проекта

Актуальные границы и структура определены документами:

- `docs/architecture/README.md`;
- `docs/architecture/high-level-architecture.md`;
- `docs/architecture/domain-map.md`;
- `docs/architecture/service-boundaries.md`;
- `docs/guides/repository-structure.md`;
- `docs/guides/infrastructure.md`.

## Целевой reset

Зоны `services/external/**`, `services/internal/**`, `services/jobs/**`,
`services/staff/**` и `services/dev/**` остаются целевой структурой deployables.
Production Control Center размещается только в `services/staff/control-center`.
Legacy bot-service, compatibility facade, dual-write и cutover path в
fresh-install профиле отсутствуют. Mattermost реализуется отдельным
необязательным adapter unit.

## Неизменные правила

- Kubernetes resources описываются typed Go adapter либо manifests/templates под `deploy/**`, но не embedded shell в Go.
- Shell остается коротким bootstrap/deploy wrapper и не содержит business orchestration.
- Agent runner вызывает готовые CLI прямыми `exec.CommandContext` с явными аргументами.
- Secrets передаются через references/mounts и не попадают в prompt/logs/docs.
- Один bounded context владеет своими tables, migrations и repositories.
- Transport/SDK details изолированы в adapters.
- Source contracts и generated code разделены.
- Fresh install использует единую baseline schema без legacy backfill.
