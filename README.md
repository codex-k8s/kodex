# matter-codex

Mattermost-first система управления Codex-агентами для разработки через короткие PR, reviewer loop и финальное решение владельца.

## Статус

Сейчас проект находится на этапе согласования MVP. Исходная идея описана в `docs/idea/**`, текущая стратегия и план реализации - в `docs/strategy/**`.

## Быстрые ссылки

- [AS-IS](docs/idea/as-is.md)
- [TO-BE](docs/idea/to-be.md)
- [Стратегия MVP](docs/strategy/README.md)
- [Архитектура MVP](docs/strategy/architecture.md)
- [Deployment and rollout](docs/strategy/deployment-and-rollout.md)
- [PR roadmap](docs/strategy/pr-roadmap.md)
- [Open decisions](docs/strategy/open-decisions.md)
- [Mattermost bootstrap runbook](docs/runbooks/mattermost-bootstrap.md)
- [Bot-service runbook](docs/runbooks/bot-service.md)

## Mattermost Bootstrap

Kubernetes-операции выполняются на целевом сервере через SSH по `.env`.

```bash
bash scripts/env/check-env.sh --env-file .env
bash scripts/remote/k8s-preflight.sh --env-file .env
bash scripts/remote/install-foundation.sh --env-file .env --dry-run=server
bash scripts/remote/install-mattermost.sh --env-file .env --dry-run=server
```

### Изменение схемы Mattermost

Matter-codex осознанно меняет PostgreSQL-схему Mattermost во время установки. При `scripts/remote/install-mattermost.sh --apply` после старта Mattermost применяется миграция `deploy/k8s/mattermost/migrations/000001_post_message_max_length.sql`.

Миграция меняет колонку `posts.message` на `varchar(200000)`. Mattermost вычисляет runtime-лимит текста поста из размера этой колонки и делит его на 4 для худшего случая UTF-8, поэтому фактический лимит становится 50000 символов. Это нужно, чтобы длинные финальные ответы агентов не терялись.

Bot-service при этом все равно режет исходящие thread-сообщения по фактическому лимиту, прочитанному из `information_schema`, поэтому стандартный Mattermost с дефолтным лимитом тоже поддерживается.

Это осознанное изменение схемы стороннего приложения. Перед подготовкой matter-codex к opensource и перед обновлением Mattermost это решение надо отдельно пересмотреть.

Также `scripts/remote/install-mattermost.sh --apply` проверяет и включает `ServiceSettings.EnableUserAccessTokens=true`. Это нужно для role bot identities: агентские ответы публикуются от отдельных Mattermost users/bots через user access tokens. Если настройка выключена, Mattermost отклоняет эти token-ы, и bot-service вынужденно публикует сообщения от сервисного `matter-codex`.

## Bot-Service

Health-only deploy без Mattermost token:

```bash
bash scripts/remote/install-bot-service.sh --env-file .env --apply --wait
bash scripts/remote/smoke-bot-service.sh --env-file .env --check-url
```

Полный Mattermost bootstrap без ручного вывода token:

```bash
bash scripts/remote/bootstrap-mattermost-bot.sh --env-file .env
```
