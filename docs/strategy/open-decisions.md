# Decisions

Этот документ фиксирует решения после первичного review. `matter-codex` является самостоятельным продуктом и не проектируется как часть `kodex`. `kodex` может быть первым dogfooding-репозиторием, но не архитектурной зависимостью.

## 1. Модель изоляции agent run

### Принято для MVP

Один Mattermost runtime namespace.

Mattermost, bot-service, agent pod и PVC создаются в одном namespace. Фактическое имя задается `MATTERCODEX_NAMESPACE` или `PRODUCTION_NAMESPACE`.

Ограничения для реализации:

- создавать отдельный ServiceAccount на run или role;
- использовать label-based ownership;
- запрещать mount чужих PVC;
- не давать agent pod права читать Kubernetes API;
- оставить runtime interface так, чтобы при необходимости перейти на namespace-per-run без изменения orchestrator.

## 2. Mattermost install path

### Принято для MVP

Custom manifests для single-server MVP. HA, managed PostgreSQL/object storage и official Helm/Operator остаются upgrade path после dogfooding.

## 3. Mattermost control surface

### Принято для MVP

Стартуем с external bot-service, но он обязан управлять Mattermost control surface:

- создавать дефолтные каналы после установки;
- создавать project/repo channels при onboarding;
- поддерживать несколько каналов на project/repo;
- запускать manager sessions thread-ами в нужном канале;
- показывать карточки run, actions и blockers в thread.

Mattermost plugin остается допустимым вариантом, если первые ручные проверки покажут, что REST API, slash commands, interactive dialogs и buttons дают недостаточно удобный UX.

## 4. GitHub PAT или GitHub App

### Принято для MVP

Bot PAT для MVP, потому что ключи уже есть и это быстрее. В доменной модели использовать `provider account` и `credential`, чтобы позже перейти на GitHub App без переименования сущностей.

## 5. OpenAI accounts

### Принято для MVP

Авторизация OpenAI accounts должна идти через device-code flow. Система должна позволять:

- авторизовать несколько OpenAI accounts;
- дать каждому account безопасное имя и status;
- ограничивать account по agent profiles, projects и лимитам;
- выбирать account для каждой agent session;
- наследовать default account из agent profile.

Raw API key не является runtime-моделью доступа агентов.

## 6. Agent profile config

### Принято для MVP

Agent profile должен управлять не только prompt, но и Codex runtime config:

- OpenAI account;
- model policy;
- sandbox/approval policy;
- MCP servers;
- `config.toml` overlay;
- env bindings для credentials;
- например Context7 MCP и ссылка на его API key credential.

## 7. Prompt templates в БД или Git

### Принято для MVP

БД как источник правды, Git fixtures только для seed/default templates.

## 8. Один сервис или микросервисы

### Принято для MVP

Modular monolith: один deployable service с явными внутренними модулями.
