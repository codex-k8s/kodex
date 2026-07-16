# Архитектура проекта

Актуальные границы и структура определены документами:

- `docs/architecture/README.md`;
- `docs/architecture/high-level-architecture.md`;
- `docs/architecture/domain-map.md`;
- `docs/architecture/service-boundaries.md`;
- `docs/guides/repository-structure.md`;
- `docs/guides/infrastructure.md`.

## Эволюционный переход

Зоны `services/external/**`, `services/internal/**`, `services/jobs/**` и `services/dev/**` остаются целевой структурой deployables. Действующий bot-service сохраняется как compatibility deployable до выделения согласованных сервисных границ. Новый код не должен расширять его без application/domain boundary.

## Неизменные правила

- Kubernetes resources описываются typed Go adapter либо manifests/templates под `deploy/**`, но не embedded shell в Go.
- Shell остается коротким bootstrap/deploy wrapper и не содержит business orchestration.
- Agent runner вызывает готовые CLI прямыми `exec.CommandContext` с явными аргументами.
- Secrets передаются через references/mounts и не попадают в prompt/logs/docs.
- Один bounded context владеет своими tables, migrations и repositories.
- Transport/SDK details изолированы в adapters.
- Source contracts и generated code разделены.
- Переход выполняется characterization-first и без big-bang rewrite.
