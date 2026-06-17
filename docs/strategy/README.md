# Стратегия MVP

Этот раздел переводит исходную идею из `docs/idea/as-is.md` и `docs/idea/to-be.md` в согласуемый план реализации.

## Прочитанный контекст

- `docs/idea/as-is.md` - ручная модель "manager + DEV/QA пары + PR + owner gate".
- `docs/idea/to-be.md` - целевая Mattermost-система управления agent run.
- `/home/s/projects/kodex/**` - реальный первый dogfooding-репозиторий и источник требований из текущей ручной работы, но не архитектурная зависимость `matter-codex`.
- `docs/strategy/owner-ux-contract.md` - продуктовый контракт Mattermost-first UX: owner работает кнопками и списками, а не typed id.
- `docs/strategy/single-user-project-chat-model.md` - новая single-user модель: Project = Mattermost team, Chat = private channel, Role = agent role.
- `docs/strategy/acceptance-matrix.md` - проверяемая матрица готовности следующих кодовых PR.
- `docs/strategy/production-gaps.md` - явные ограничения после MVP dogfooding-среза и следующий hardening backlog.

## Внешние источники

- Mattermost custom slash commands: https://developers.mattermost.com/integrate/slash-commands/custom/
- Mattermost interactive messages: https://developers.mattermost.com/integrate/plugins/interactive-messages/
- Mattermost interactive dialogs: https://developers.mattermost.com/integrate/plugins/interactive-dialogs/
- Mattermost bot accounts: https://developers.mattermost.com/integrate/reference/bot-accounts/
- Mattermost API documentation: https://developers.mattermost.com/api-documentation/
- Mattermost Kubernetes deploy: https://docs.mattermost.com/deployment-guide/server/deploy-kubernetes.html
- Mattermost Helm charts: https://github.com/mattermost/mattermost-helm
- Kubernetes client-go: https://github.com/kubernetes/client-go
- OpenAI Codex non-interactive mode: https://developers.openai.com/codex/noninteractive
- OpenAI Codex environment variables: https://developers.openai.com/codex/environment-variables

## Доступные env-ключи

Значения не документируются. В локальном `.env` заданы:

- `BOOTSTRAP_ALLOWED_EMAILS`
- `BOOTSTRAP_OWNER_EMAIL`
- `BOOTSTRAP_PLATFORM_ADMIN_EMAILS`
- `CONTEXT7_API_KEY`
- `GITHUB_OAUTH_CLIENT_ID`
- `GITHUB_OAUTH_CLIENT_SECRET`
- `GITHUB_PAT`
- `GITHUB_REPO`
- `GITHUB_USERNAME`
- `GITHUB_WEBHOOK_SECRET`
- `GIT_BOT_MAIL`
- `GIT_BOT_TOKEN`
- `GIT_BOT_USERNAME`
- `LETSENCRYPT_EMAIL`
- `OPERATOR_SSH_PUBKEY_PATH`
- `OPERATOR_USER`
- `PRODUCTION_DOMAIN`
- `PRODUCTION_NAMESPACE`
- `PUBLIC_BASE_URL`
- `TARGET_HOST`
- `TARGET_PORT`
- `TARGET_ROOT_SSH_KEY`
- `TARGET_ROOT_USER`

## Ключевые выводы

- MVP должен быть не "чат-ботом", а малым orchestration контуром: Mattermost project team и private chat channel как рабочее место, GitHub как источник задач/PR/review, Kubernetes как runtime.
- Главный продуктовый интерфейс - `/agents` menu. Typed slash commands остаются fallback/debug API, но не являются основным owner path.
- Owner не должен помнить repository/account/profile/run/flow/template/Kubernetes Secret identifiers. Известные системе сущности выбираются кнопками, списками, message menus или dialog `select`, а технические id передаются скрытым callback state.
- Flow больше не является центральной сущностью продукта. Основной путь: project -> accounts -> repositories -> roles -> chats -> работа в Mattermost channel/thread. Legacy flow остается только в `Advanced`.
- `matter-codex` является отдельным продуктом. Он может управлять разработкой `kodex`, но `kodex` не должен зависеть от него кодом, схемами БД, API или runtime-контрактами.
- Первый срез лучше делать standalone-сервисом с внутренними модулями: Mattermost surface, orchestrator, runtime, GitHub adapter, credentials, OpenAI accounts, agent profiles и audit.
- Для скорости стартуем с внешнего bot-service поверх slash command, Mattermost REST API и interactive message actions. Mattermost plugin остается расширением, если REST/API не хватит для удобного UX.
- После установки должны появляться дефолтные каналы управления, а project/chat onboarding должен создавать Mattermost teams/channels и привязки к roles/repositories.
- Codex agent запускается через `codex exec --json` в pod. Это дает поток событий, machine-readable статус и совместимость с non-interactive automation.
- OpenAI/Codex-доступ в целевой MVP идет через отдельные account profiles с device-code авторизацией и Kubernetes Secret с `auth.json`, а не через один общий raw API key на все сессии.
- Секреты не передаются в prompt. Runner получает их только как runtime env/file mount из Kubernetes Secret.
