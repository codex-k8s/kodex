# Стратегия MVP

Этот раздел переводит исходную идею из `docs/idea/as-is.md` и `docs/idea/to-be.md` в согласуемый план реализации.

## Прочитанный контекст

- `docs/idea/as-is.md` - ручная модель "manager + DEV/QA пары + PR + owner gate".
- `docs/idea/to-be.md` - целевая Mattermost-система управления agent run.
- `/home/s/projects/kodex/AGENTS.md` - правила работы основной платформы.
- `/home/s/projects/kodex/refactoring/task.md` - Kubernetes-first, provider-first, agent-manager, flow/role/prompt в БД.
- `/home/s/projects/kodex/refactoring/09-target-architecture.md` - owner-сервисы, `agent-manager`, `runtime-manager`, `provider-hub`, `interaction-hub`.
- `/home/s/projects/kodex/refactoring/11-data-and-state-model.md` - владение состоянием и database-per-service как целевая модель.
- `/home/s/projects/kodex/specs/asyncapi/agent-manager.v1.yaml` - события agent lifecycle.
- `/home/s/projects/kodex/bootstrap/README.md` - live-safe bootstrap/deploy подход.

## Внешние источники

- Mattermost custom slash commands: https://developers.mattermost.com/integrate/slash-commands/custom/
- Mattermost interactive messages: https://developers.mattermost.com/integrate/plugins/interactive-messages/
- Mattermost interactive dialogs: https://developers.mattermost.com/integrate/plugins/interactive-dialogs/
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
- `OPENAI_API_KEY`
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

- MVP должен быть не "чат-ботом", а малым orchestration контуром: Mattermost thread как карточка run, GitHub как источник PR/review, Kubernetes как runtime.
- Первый срез лучше делать standalone-сервисом, но с доменными границами `agent-manager`, `runtime-manager`, `provider-hub`, `interaction-hub`, чтобы позже не конфликтовать с `kodex`.
- Mattermost Plugin откладывается. Для скорости стартуем с внешнего bot-service: slash command, bot account REST API, interactive message actions.
- Codex agent запускается через `codex exec --json` в pod. Это дает поток событий, machine-readable статус и совместимость с non-interactive automation.
- Секреты не передаются в prompt. Runner получает их только как runtime env/file mount из Kubernetes Secret.
