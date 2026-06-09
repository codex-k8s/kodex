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
- control surface сначала Mattermost-first: slash commands, каналы и bot-service;
- сервисы платформы пишутся на Go и размещаются в `services/<zone>/<service>/`;
- агентные запуски выполняются отдельными pod'ами с PVC под checkout рабочей ветки;
- долгоживущее состояние и синхронизация проектируются под PostgreSQL, `JSONB` и будущий `pgvector`;
- интеграции с Kubernetes, Mattermost и GitHub/GitLab проектируются через SDK/интерфейсы/адаптеры, а не через бизнес-логику в shell;
- shell допустим только как bootstrap/deploy wrapper на коротком MVP-срезе.

Процесс разработки и ведения документации задаётся корневым `AGENTS.md` и актуальной проектной документацией в `docs/strategy/**`.
