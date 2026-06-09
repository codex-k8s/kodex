# Bot-Service Runbook

## Назначение

Этот runbook описывает второй кодовый этап: `matter-codex` bot-service для Mattermost.

В этом PR сервис умеет:

- отвечать на `/healthz`;
- принимать Mattermost slash callback `/mattermost/slash/agents`;
- отвечать на `/agents status`;
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
- `MATTERCODEX_DEFAULT_TEAM_NAME` - optional, по умолчанию `agents`;
- `MATTERCODEX_DEFAULT_TEAM_DISPLAY_NAME` - optional;
- `MATTERCODEX_DEFAULT_CHANNELS` - optional, список `name:Display Name` через запятую.

Скрипты печатают только статус наличия токенов, не значения.

## Render

```bash
bash scripts/k8s/render-bot-service.sh --env-file .env --render-dir /tmp/matter-codex-bot-render
```

В render directory попадают:

- code ConfigMap с Go source archive (`go.mod`, `go.sum`, `services/external/bot-service`);
- config ConfigMap;
- Deployment;
- Service;
- Ingress.

## Remote dry-run

```bash
bash scripts/remote/install-bot-service.sh --env-file .env --dry-run=server
```

Если Mattermost token еще не задан, Secret не создается, а Deployment использует optional secret refs.

## Health-only install

Этот режим можно раскатать без Mattermost bot token:

```bash
bash scripts/remote/install-bot-service.sh --env-file .env --apply --wait
bash scripts/remote/smoke-bot-service.sh --env-file .env --check-url
```

Ожидаемый результат: Kubernetes objects существуют, Deployment готов, `/healthz` отвечает через HTTPS.

## Mattermost bot bootstrap

Если `MATTERCODEX_MATTERMOST_BOT_TOKEN` еще не создан, можно выполнить полный bootstrap без пароля администратора через `mmctl --local` внутри Mattermost pod:

```bash
bash scripts/remote/bootstrap-mattermost-bot.sh --env-file .env
```

Скрипт:

- включает `ServiceSettings.EnableUserAccessTokens`, если personal access tokens выключены;
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

Ожидаемый результат: ephemeral ответ `matter-codex: online` без вывода секретов.

## Безопасность

- `.env` не коммитится.
- Mattermost tokens не попадают в manifests render output.
- Slash token, полученный из Mattermost API, пишется во временный файл с правами `0600`, затем в Kubernetes Secret.
- Логи provisioning показывают только безопасные статусы `exists/created/updated`.
