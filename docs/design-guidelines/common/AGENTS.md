# Common Design Guidelines

Документы, общие для Go-сервисов, deploy tooling и будущих платформенных библиотек.

- `docs/design-guidelines/common/check_list.md` - общий чек-лист.
- `docs/design-guidelines/common/project_architecture.md` - зоны, границы ответственности, структура репо.
- `docs/design-guidelines/common/design_principles.md` - DDD/SOLID/DRY/KISS/Clean Architecture.
- `docs/design-guidelines/common/libraries_reusable_code_requirements.md` - общие правила выноса кода в `libs/*`.
- `docs/design-guidelines/common/external_dependencies_catalog.md` - каталог внешних библиотек и инструментов.

Проектный overlay `matter-codex`:

- процессы запускаются из Control Center, schedules, integration events либо
  agent delegation и наблюдаются через durable internal events; Mattermost
  является необязательной поверхностью входящих сообщений и уведомлений;
- Kubernetes, Mattermost и repository-провайдеры подключаются через SDK/интерфейсы/адаптеры;
- модель данных и синхронизация multi-pod проектируются под PostgreSQL (`JSONB` + будущий `pgvector`);
- env/secrets/CI variable names для платформы используют префикс `MATTERCODEX_`, кроме значений внешних runtime-контрактов;
- проектное планирование и документационная каноника задаются корневым `AGENTS.md`, `docs/product/**`, `docs/architecture/**`, `docs/domains/**`, `docs/decisions/**` и `docs/roadmap/**`.
