# Deployment And Rollout

## Цель rollout

Нужно быстро получить live-контур, где владелец может вручную тестировать каждый PR:

1. Mattermost доступен по HTTPS.
2. `matter-codex` bot-service подключен к Mattermost.
3. Существуют дефолтные каналы управления агентной системой.
4. Bot-service умеет отвечать на `/agents status`.
5. После каждого PR сервис раскатывается в Kubernetes.
6. Владелец проверяет сценарий и только потом merge.

## Доступные env-источники

Локальный `.env` содержит ключи для:

- доступа к целевому серверу: `TARGET_HOST`, `TARGET_PORT`, `TARGET_ROOT_USER`, `TARGET_ROOT_SSH_KEY`;
- оператора Kubernetes: `OPERATOR_USER`, `OPERATOR_SSH_PUBKEY_PATH`;
- production namespace/domain/base URL: `PRODUCTION_NAMESPACE`, `PRODUCTION_DOMAIN`, `PUBLIC_BASE_URL`;
- TLS: `LETSENCRYPT_EMAIL`;
- GitHub: `GITHUB_*`, `GIT_BOT_*`;
- OpenAI: `OPENAI_API_KEY`;
- bootstrap allowlist: `BOOTSTRAP_*`;
- Context7: `CONTEXT7_API_KEY`.

Скрипты должны валидировать наличие ключей и печатать только имена отсутствующих ключей. `OPENAI_API_KEY` считается bootstrap/smoke fallback. Целевая авторизация agent sessions выполняется через OpenAI account profiles и device-code flow.

## Предлагаемый install path

Для самого быстрого MVP:

1. `scripts/env/check-env.sh` - локальная проверка `.env` без вывода значений.
2. `scripts/remote/bootstrap-host.sh` - SSH preflight целевого host.
3. `scripts/k8s/install-foundation.sh` - namespace, secrets, ingress/TLS prerequisites.
4. `scripts/k8s/install-mattermost.sh` - Mattermost + PostgreSQL + file PVC.
5. `scripts/remote/install-bot-service.sh` - `matter-codex` deployment/service/ingress на целевом сервере.
6. `scripts/remote/provision-bot-service.sh` - Mattermost team, slash command и дефолтные каналы через API.
7. `scripts/remote/smoke-bot-service.sh` - readiness и bot `/healthz`.

## Mattermost install strategy

Официальная Kubernetes-документация Mattermost ведет к Helm/Operator и рекомендует managed PostgreSQL/object storage для production. Для короткого MVP есть два варианта.

### Вариант A: official Helm/Operator

Плюсы:

- ближе к официальной Kubernetes-документации;
- проще будущие upgrades;
- меньше собственного manifest-кода.

Минусы:

- больше движущихся частей;
- может потребовать enterprise-oriented настройки;
- сложнее быстро диагностировать на малом single-node кластере.

### Вариант B: собственные manifests для single-server MVP

Плюсы:

- полный контроль над namespace, PVC, PostgreSQL и ingress;
- быстрее отлаживать;
- проще встроить в наши bootstrap scripts;
- лучше подходит для dogfooding на одном сервере.

Минусы:

- upgrades и HA придется проектировать отдельно;
- больше ответственности за security defaults;
- нужно следить за совместимостью Mattermost image и PostgreSQL.

## Рекомендация

Для MVP выбрать вариант B: собственные Kubernetes manifests вокруг single-server Mattermost, PostgreSQL и PVC. В документации и скриптах сразу оставить upgrade path к official Helm/Operator или managed services.

Причина: задача требует очень короткого срока и ручного тестирования каждого PR. Управляемые manifests проще резать на маленькие проверяемые срезы.

## Deployment gates

Каждый deploy PR проходит:

- render manifests без секретов в output;
- `kubectl apply --dry-run=server`, если кластер доступен;
- rollout status для измененных workloads;
- health endpoint сервиса;
- Mattermost bot smoke или безопасный `/agents status`;
- наличие дефолтных Mattermost-каналов;
- отчет в Mattermost/консоль без секретов.

## Ручная проверка владельцем

После каждого deploy владелец получает:

- URL Mattermost;
- имя канала;
- команду или кнопку для проверки;
- ожидаемый результат;
- список известных ограничений текущего PR.
