# Design Guidelines

Этот каталог является локальной адаптацией Go-гайдов из `/home/s/projects/kodex` под самостоятельный проект `matter-codex`.

Документация разделена по областям:

- `docs/design-guidelines/common/` - общие правила архитектуры, зависимостей, секретов и deploy path;
- `docs/design-guidelines/go/` - правила для Go-сервисов;
- `docs/design-guidelines/common/external_dependencies_catalog.md` - каталог внешних библиотек и инструментов.

Стартовая точка перед PR:

- `docs/design-guidelines/common/check_list.md`;
- `docs/design-guidelines/go/check_list.md`, если PR затрагивает Go.

Специфика `matter-codex`, которую нельзя нарушать:

- runtime MVP разворачивается только в Kubernetes;
- production control surface является web-first PWA `services/staff/control-center`;
- Mattermost подключается только как необязательный interaction adapter и не
  участвует в core readiness или authority;
- сервисы платформы пишутся на Go и размещаются в `services/<zone>/<service>/`, где зона отражает runtime-роль deployable;
- каждый обычный ход агента выполняется новым execution-scoped Pod из
  promoted immutable образа его роли; PVC хранит только долговечную рабочую
  область и историю сессии;
- долгоживущее состояние и синхронизация проектируются под PostgreSQL, `JSONB` и будущий `pgvector`;
- интеграции с Kubernetes, Mattermost и GitHub/GitLab проектируются через SDK/интерфейсы/адаптеры, а не через бизнес-логику в shell;
- shell допустим только как bootstrap/deploy wrapper на коротком MVP-срезе.

Процесс разработки и ведения документации задается корневым `AGENTS.md`, `docs/product/**`, `docs/architecture/**`, `docs/domains/**`, `docs/decisions/**` и `docs/guides/**`.
