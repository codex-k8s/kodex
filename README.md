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

## Bot-Service

Health-only deploy без Mattermost token:

```bash
bash scripts/remote/install-bot-service.sh --env-file .env --apply --wait
bash scripts/remote/smoke-bot-service.sh --env-file .env --check-url
```

Полный Mattermost provisioning после добавления `MATTERCODEX_MATTERMOST_BOT_TOKEN` в `.env`:

```bash
bash scripts/remote/provision-bot-service.sh --env-file .env
```
