# Bot-Service Runbook

## Назначение

Этот runbook описывает второй кодовый этап: `matter-codex` bot-service для Mattermost.

В этом PR сервис умеет:

- отвечать на `/healthz`;
- принимать Mattermost slash callback `/mattermost/slash/agents`;
- отвечать на `/agents status`;
- выполнять admin-команды `/agents repo add`, `/agents repo list`, `/agents token check`, `/agents locale get|set`, `/agents profile list`;
- выполнять GitHub adapter команды `/agents github check`, `/agents github branch`, `/agents github pr`;
- принимать GitHub webhook callback `/github/webhook` с HMAC validation;
- автоматически регистрировать repo webhook при `/agents repo add github owner/name [default-branch]`, если GitHub token имеет hook write permission;
- выполнять Kubernetes runtime smoke-команды `/agents runtime smoke|status|cleanup` через client-go, Job и PVC;
- выполнять Codex developer smoke-команды `/agents dev smoke|status|cleanup`, создающие отдельный Job/PVC, branch, commit и draft PR;
- применять storage migrations и хранить repository/profile/audit metadata в PostgreSQL;
- создавать Mattermost repo-channel при добавлении repository;
- хранить Mattermost bot/slash tokens только в Kubernetes Secret;
- создавать базовую Mattermost control surface через `mmctl --local` внутри Mattermost pod: team, каналы и slash command.

## Env contract

Обязательные базовые ключи остаются теми же, что и для Mattermost bootstrap.

Новые ключи:

- `MATTERCODEX_BOT_SERVICE_HOST` - optional, host публичного Ingress;
- `MATTERCODEX_BOT_SERVICE_SITE_URL` - optional, публичный URL bot-service;
- `MATTERCODEX_BOT_SERVICE_INTERNAL_URL` - optional, внутренний callback URL для Mattermost slash command;
- `MATTERCODEX_MATTERMOST_BOT_TOKEN` - нужен для provisioning Mattermost team/channels/slash command;
- `MATTERCODEX_MATTERMOST_SLASH_TOKEN` - optional, обычно заполняется provisioning script в Kubernetes Secret;
- `MATTERCODEX_GITHUB_SECRET` - optional, имя отдельного Kubernetes Secret для GitHub token/webhook secret;
- `MATTERCODEX_GITHUB_TOKEN` - optional, GitHub token для bot-service; deploy-скрипты также принимают legacy `GITHUB_PAT` или `GIT_BOT_TOKEN`;
- `MATTERCODEX_GITHUB_WEBHOOK_SECRET` - optional, secret для `/github/webhook`; deploy-скрипты также принимают legacy `GITHUB_WEBHOOK_SECRET`;
- `MATTERCODEX_LOCALE` - optional, стартовая локаль Mattermost-facing ответов bot-service; Go-дефолт `en`, deploy-скрипты для текущего контура по умолчанию ставят `ru`;
- `MATTERCODEX_DATABASE_DSN` - optional, берется из Kubernetes Secret `mattermost-datasource` для storage/admin-команд;
- `MATTERCODEX_STORAGE_MIGRATIONS_ENABLED` - optional, включает Go migrations на старте;
- `MATTERCODEX_BOT_SERVICE_MAX_GITHUB_WEBHOOK_BYTES` - optional, лимит размера GitHub webhook payload;
- `MATTERCODEX_RUNTIME_ENABLED` - optional, включает Kubernetes runtime adapter;
- `MATTERCODEX_RUNTIME_NAMESPACE` - optional, namespace для Job/PVC runtime-запусков;
- `MATTERCODEX_RUNTIME_SMOKE_IMAGE` - optional, image для smoke Job;
- `MATTERCODEX_AGENT_RUNNER_IMAGE` - optional, image для Codex developer Job; текущий MVP default использует public Node Alpine image;
- `MATTERCODEX_CODEX_PACKAGE` - optional, npm package spec Codex CLI для developer Job;
- `MATTERCODEX_RUNTIME_WORKSPACE_STORAGE_SIZE` - optional, размер PVC рабочего каталога smoke-запуска;
- `MATTERCODEX_RUNTIME_JOB_TTL_SECONDS` - optional, TTL завершенных smoke Job;
- `MATTERCODEX_RUNTIME_LOG_TAIL_LINES` - optional, число последних строк pod log для `/agents runtime status`;
- `MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT` - optional, ServiceAccount для agent/smoke Job;
- `MATTERCODEX_OPENAI_SECRET` - optional, имя Kubernetes Secret с OpenAI API key для Codex runner;
- `MATTERCODEX_OPENAI_API_KEY` - optional, OpenAI/Codex API key для deploy-скрипта; deploy-скрипты также принимают `CODEX_API_KEY` и legacy `OPENAI_API_KEY`;
- `MATTERCODEX_DEFAULT_TEAM_NAME` - optional, по умолчанию `agents`;
- `MATTERCODEX_DEFAULT_TEAM_DISPLAY_NAME` - optional;
- `MATTERCODEX_DEFAULT_CHANNELS` - optional, список `name:Display Name` через запятую.

Скрипты печатают только статус наличия токенов, не значения.

## Render

```bash
bash scripts/k8s/render-bot-service.sh --env-file .env --render-dir /tmp/matter-codex-bot-render
```

В render directory попадают:

- code ConfigMap с Go source archive (`go.mod`, `go.sum`, `libs/go/i18n`, `services/external/bot-service`);
- config ConfigMap;
- ServiceAccount/RBAC для bot-service runtime adapter и agent runner;
- Deployment;
- Service;
- Ingress.

## Remote dry-run

```bash
bash scripts/remote/install-bot-service.sh --env-file .env --dry-run=server
```

Если Mattermost token еще не задан, Secret не создается, а Deployment использует optional secret refs.

Если GitHub token или webhook secret заданы, deploy-скрипты создают отдельный Kubernetes Secret. Значения не печатаются.

Если OpenAI API key задан, deploy-скрипты создают отдельный Kubernetes Secret. Значение не печатается и не попадает в ConfigMap.

## Health-only install

Этот режим можно раскатать без Mattermost bot token:

```bash
bash scripts/remote/install-bot-service.sh --env-file .env --apply --wait
bash scripts/remote/smoke-bot-service.sh --env-file .env --check-url
```

Ожидаемый результат: Kubernetes objects существуют, Deployment готов, `/healthz` отвечает через HTTPS.

Проверка storage migrations после deploy:

```bash
set -euo pipefail
. scripts/lib/env.sh
mattercodex_load_env_file .env
mattercodex_validate_base_env
NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_NAMESPACE")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"
mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q exec statefulset/mattermost-postgres -- psql -U mattermost -d mattermost -Atc 'select version_id, is_applied from goose_db_version order by id;'"
```

Ожидаемый результат: в выводе есть `1|t`.

## Mattermost bot bootstrap

Если `MATTERCODEX_MATTERMOST_BOT_TOKEN` еще не создан, можно выполнить полный bootstrap без пароля администратора через `mmctl --local` внутри Mattermost pod:

```bash
bash scripts/remote/bootstrap-mattermost-bot.sh --env-file .env
```

Скрипт:

- включает `ServiceSettings.EnableUserAccessTokens`, если personal access tokens выключены;
- Mattermost Deployment должен содержать `MM_SERVICESETTINGS_ALLOWEDUNTRUSTEDINTERNALCONNECTIONS` с host из `MATTERCODEX_BOT_SERVICE_INTERNAL_URL`, иначе Mattermost заблокирует slash callback во внутренний Kubernetes Service;
- создает service user `MATTERCODEX_MATTERMOST_BOT_USERNAME`;
- конвертирует service user в bot;
- генерирует `MATTERCODEX_MATTERMOST_BOT_TOKEN`;
- создает team, дефолтные каналы и slash command `/agents`;
- сохраняет bot token и slash token в Kubernetes Secret;
- перезапускает bot-service Deployment.

Значения токенов не выводятся.

## Mattermost provisioning через готовый token

Отдельный provisioning-скрипт через готовый Personal Access Token удалён вместе с предыдущей реализацией. На текущем Go-срезе поддержан один безопасный bootstrap path через `mmctl --local`:

```bash
bash scripts/remote/bootstrap-mattermost-bot.sh --env-file .env
```

Если token уже есть в Kubernetes Secret, повторный deploy bot-service использует существующий Secret и не печатает его значение.

## Ручная проверка владельцем

1. Открыть Mattermost.
2. Перейти в team `agents`.
3. Проверить каналы `agents-control`, `agents-runs`, `agent-alerts`, `agents-audit`.
4. В канале `agents-control` выполнить:

```text
/agents status
```

Ожидаемый результат: ephemeral ответ `matter-codex: online` без вывода секретов. В текущем deploy-контуре ответы по умолчанию русские, потому что `MATTERCODEX_LOCALE` задается как `ru`.

Дополнительная проверка storage/admin-команд:

```text
/agents token check
/agents locale get
/agents locale set en
/agents token check
/agents locale set ru
/agents profile list
/agents repo add github codex-k8s/matter-codex main
/agents repo list
```

Ожидаемый результат: команды отвечают ephemeral-сообщениями, `locale set en` переключает ответы на английский, `locale set ru` возвращает русские ответы, repository появляется в списке, а Mattermost создаёт/показывает канал `repo-codex-k8s-matter-codex`.

Дополнительная проверка GitHub adapter:

```text
/agents token check
/agents github check codex-k8s/matter-codex
/agents github branch dry-run codex-k8s/matter-codex matter-codex-smoke main
/agents github pr dry-run codex-k8s/matter-codex main main Smoke PR dry run
/agents github pr status codex-k8s/matter-codex 4
/agents github webhook ensure codex-k8s/matter-codex
```

Ожидаемый результат:

- token check показывает `github token: configured`;
- repo check показывает default branch и безопасные permission-флаги;
- branch dry-run показывает base sha и `changes: none`;
- PR dry-run проверяет head/base refs и не создает PR;
- PR status показывает state, draft/merged, reviews/comments fetched.
- webhook ensure создает или обновляет repo webhook, если token имеет hook write permission; при нехватке прав команда возвращает безопасную ошибку без вывода token/secret.

При `/agents repo add github owner/name [default-branch]` bot-service также пытается выполнить webhook ensure автоматически и добавляет строку `webhook: ...` в ответ.

Дополнительная проверка Kubernetes runner foundation:

```text
/agents token check
/agents runtime smoke smoke-manual
/agents runtime status smoke-manual
/agents runtime cleanup smoke-manual
```

Ожидаемый результат:

- token check показывает `kubernetes runtime: configured`;
- runtime smoke возвращает run id, Job и PVC без вывода секретов;
- runtime status показывает Job/PVC, pod phase и короткий log tail smoke Job;
- runtime cleanup удаляет Job и PVC.

Дополнительная проверка Codex developer agent:

```text
/agents token check
/agents dev smoke codex-k8s/matter-codex dev-manual
/agents dev status dev-manual
```

Ожидаемый результат:

- developer smoke возвращает run id, branch `matter-codex-dev-dev-manual`, Job и PVC;
- через некоторое время `dev status` показывает pod phase и artifact `pr-url`;
- в GitHub появляется draft PR с безопасным документационным изменением `docs/dogfood/codex-developer-smoke.md`;
- log tail не содержит значений OpenAI/GitHub/Mattermost секретов.

После проверки удалить Kubernetes resources:

```text
/agents dev cleanup dev-manual
```

Если smoke run создал draft PR, его надо закрыть/удалить вручную или оставить как проверочный артефакт до решения владельца. Cleanup удаляет только Kubernetes Job/PVC, а не GitHub branch/PR.

Проверка webhook reject без корректной подписи:

```bash
set -euo pipefail
. scripts/lib/env.sh
mattercodex_load_env_file .env
mattercodex_validate_base_env
curl -sS -o /dev/null -w '%{http_code}\n' \
  -H 'Content-Type: application/json' \
  -H 'X-GitHub-Event: ping' \
  --data '{}' \
  "${MATTERCODEX_BOT_SERVICE_SITE_URL%/}/github/webhook"
```

Ожидаемый результат: `401`.

## Безопасность

- `.env` не коммитится.
- Mattermost tokens не попадают в manifests render output.
- GitHub token и webhook secret хранятся в отдельном Kubernetes Secret и не попадают в ConfigMap.
- Slash token, полученный из Mattermost API, пишется во временный файл с правами `0600`, затем в Kubernetes Secret.
- Логи provisioning показывают только безопасные статусы `exists/created/updated`.
- bot-service получает namespace-scoped Role только на создание/чтение/удаление runtime Job/PVC и чтение pod/log.
- ServiceAccount agent runner создается без automount token; smoke pod также явно отключает automount.
- Codex developer Job получает OpenAI/GitHub credentials только через Kubernetes Secret volume mount.
- `CODEX_HOME/config.toml` задает `shell_environment_policy` с минимальным environment для команд, которые запускает Codex.
- Developer runner сам выполняет push/PR после `codex exec`; prompt contract запрещает Codex агенту пушить branch или создавать PR напрямую.
