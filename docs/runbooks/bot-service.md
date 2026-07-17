# Bot-Service Runbook

## Назначение

Этот runbook описывает второй кодовый этап: `matter-codex` bot-service для Mattermost.

В этом PR сервис умеет:

- отвечать на `/healthz`;
- принимать Mattermost slash callback `/mattermost/slash/agents`;
- показывать Mattermost menu card по пустому `/agents`;
- принимать Mattermost menu action callback `/mattermost/actions/agents` для кнопок menu card;
- отвечать на `/agents status`;
- выполнять admin-команды `/agents repo add`, `/agents repo list`, `/agents token check`, `/agents locale get|set`, `/agents profile list`, `/agents prompt help|list|show|render|set`, `/agents openai auth|status|list|cleanup`;
- выполнять GitHub adapter/account команды `/agents github account list`, `/agents github check`, `/agents github branch`, `/agents github pr`;
- управлять через Mattermost dialog-кнопки metadata GitHub accounts: add/edit/delete account binding к существующему Kubernetes Secret без ввода raw token в Mattermost;
- принимать GitHub webhook callback `/github/webhook` с HMAC validation;
- автоматически регистрировать repo webhook при `/agents repo add github owner/name [default-branch]`, если GitHub token имеет hook write permission;
- выполнять Kubernetes runtime smoke-команды `/agents runtime smoke|status|cleanup|prune` через client-go, Job, PVC и подготовленный agent-runner image;
- выполнять Codex reviewer-команды `/agents review pr|status|cleanup`, запускающие отдельный Job/PVC для review существующего GitHub PR через OpenAI account, GitHub account и prompt template из agent profile;
- применять storage migrations и хранить repository/profile/audit metadata в PostgreSQL;
- создавать Mattermost repo-channel при добавлении repository;
- хранить Mattermost bot/slash tokens только в Kubernetes Secret;
- создавать базовую Mattermost control surface через `mmctl --local` внутри Mattermost pod: team, каналы и slash command.

## Env contract

Обязательные базовые ключи остаются теми же, что и для Mattermost bootstrap.

Новые ключи:

- `MATTERCODEX_BOT_SERVICE_HOST` - optional, host публичного Ingress;
- `MATTERCODEX_BOT_SERVICE_SITE_URL` - optional, публичный URL bot-service;
- `MATTERCODEX_BOT_SERVICE_INTERNAL_URL` - optional, внутренний callback URL для Mattermost slash command и interactive action buttons;
- `MATTERCODEX_MATTERMOST_INTERNAL_URL` - optional, внутренний URL Mattermost API для bot-service; нужен, если публичный Mattermost закрыт OAuth proxy;
- `MATTERCODEX_MATTERMOST_BOT_TOKEN` - нужен для provisioning Mattermost team/channels/slash command;
- `MATTERCODEX_MATTERMOST_SLASH_TOKEN` - optional, обычно заполняется provisioning script в Kubernetes Secret;
- `MATTERCODEX_GITHUB_SECRET` - optional, имя Kubernetes Secret для reviewer/user GitHub account;
- `MATTERCODEX_AGENT_GITHUB_SECRET` - optional, имя Kubernetes Secret для developer/agent GitHub account;
- `MATTERCODEX_GITHUB_TOKEN` - optional, GitHub token для bot-service и reviewer account; deploy-скрипты также принимают legacy `GITHUB_PAT`;
- `MATTERCODEX_GITHUB_USERNAME` и `MATTERCODEX_GITHUB_EMAIL` - нужны, если задан `MATTERCODEX_GITHUB_TOKEN`/`GITHUB_PAT`; GitHub login/email reviewer account; deploy-скрипты также принимают legacy `GITHUB_USERNAME`/`GITHUB_EMAIL`;
- `MATTERCODEX_AGENT_GITHUB_TOKEN`, `MATTERCODEX_AGENT_GITHUB_USERNAME`, `MATTERCODEX_AGENT_GITHUB_EMAIL` - optional GitHub credentials developer/agent account; если задан token, username/email обязательны; deploy-скрипты также принимают legacy `GIT_BOT_TOKEN`, `GIT_BOT_USERNAME`, `GIT_BOT_MAIL`;
- `MATTERCODEX_GITHUB_WEBHOOK_SECRET` - optional, secret для `/github/webhook`; deploy-скрипты также принимают legacy `GITHUB_WEBHOOK_SECRET`;
- `MATTERCODEX_LOCALE` - optional, стартовая локаль Mattermost-facing ответов bot-service; Go-дефолт `en`, deploy-скрипты для текущего контура по умолчанию ставят `ru`;
- `MATTERCODEX_DATABASE_DSN` - optional, берется из Kubernetes Secret `mattermost-datasource` для storage/admin-команд;
- `MATTERCODEX_STORAGE_MIGRATIONS_ENABLED` - optional, включает Go migrations на старте;
- `MATTERCODEX_BOT_SERVICE_MAX_GITHUB_WEBHOOK_BYTES` - optional, лимит размера GitHub webhook payload;
- `MATTERCODEX_IMAGE_BUILD_STRATEGY` - optional, способ сборки image в remote deploy; default `kaniko`; legacy `docker` требует `docker` или `nerdctl` прямо на целевом сервере;
- `MATTERCODEX_IMAGE_TAG` - optional, tag для bot-service и agent-runner image; при `kaniko` и `--apply` без явного значения deploy-скрипт генерирует уникальный tag из commit и UTC timestamp;
- `MATTERCODEX_IMAGE_REPOSITORY_PREFIX` - optional, registry path prefix для image;
- `MATTERCODEX_IMAGE_REGISTRY_MANAGED` - optional, при `true` render/apply создает встроенный MatterCodex registry в namespace;
- `MATTERCODEX_IMAGE_REGISTRY_NAME`, `MATTERCODEX_IMAGE_REGISTRY_IMAGE`, `MATTERCODEX_IMAGE_REGISTRY_STORAGE_SIZE`, `MATTERCODEX_IMAGE_REGISTRY_HOST_PORT` - optional, параметры встроенного registry;
- `MATTERCODEX_IMAGE_REGISTRY_PULL_HOST` - optional, registry host, через который kubelet тянет image; default `localhost:<host-port>` для single-server контура;
- `MATTERCODEX_IMAGE_REGISTRY_PUSH_HOST` - optional, registry host, в который Kaniko push'ит image изнутри кластера; default Kubernetes service DNS;
- `MATTERCODEX_KANIKO_IMAGE`, `MATTERCODEX_KANIKO_CONTEXT_PVC`, `MATTERCODEX_KANIKO_CONTEXT_STORAGE_SIZE` - optional, параметры Kaniko executor и PVC build context;
- `MATTERCODEX_KANIKO_CPU_REQUEST`, `MATTERCODEX_KANIKO_MEMORY_REQUEST`, `MATTERCODEX_KANIKO_MEMORY_LIMIT` - optional, ресурсы Kaniko Job; defaults `2000m`/`2Gi`/`24Gi`, CPU limit отсутствует; повышенный лимит нужен для snapshot большого agent-runner image и применяется только к временной build-job;
- `MATTERCODEX_KANIKO_JOB_TTL_SECONDS`, `MATTERCODEX_KANIKO_ACTIVE_DEADLINE_SECONDS` - optional, lifecycle limits Kaniko Job;
- `MATTERCODEX_RUNTIME_ENABLED` - optional, включает Kubernetes runtime adapter;
- `MATTERCODEX_RUNTIME_NAMESPACE` - optional, namespace для Job/PVC runtime-запусков;
- `MATTERCODEX_RUNTIME_SMOKE_IMAGE` - optional, legacy image setting; текущий smoke Job запускается через `MATTERCODEX_AGENT_RUNNER_IMAGE`;
- `MATTERCODEX_AGENT_RUNNER_IMAGE` - optional, image для smoke/developer/reviewer/auth Job; текущий default строится от `MATTERCODEX_IMAGE_REGISTRY_PULL_HOST`;
- `MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE` - optional, при `true` install script собирает agent-runner image через выбранную `MATTERCODEX_IMAGE_BUILD_STRATEGY` перед deploy;
- `MATTERCODEX_CODEX_PACKAGE` - optional, npm package spec Codex CLI, который устанавливается в agent-runner image при сборке;
- `MATTERCODEX_RUNTIME_WORKSPACE_STORAGE_SIZE` - optional, размер PVC рабочего каталога smoke-запуска;
- `MATTERCODEX_RUNTIME_JOB_TTL_SECONDS` - optional, TTL завершенных smoke Job;
- `MATTERCODEX_RUNTIME_LOG_TAIL_LINES` - optional, число последних строк pod log для `/agents runtime status`;
- `MATTERCODEX_RUNTIME_LIMITS_ENABLED` - optional, включает render/apply namespace `ResourceQuota` и `LimitRange` для runtime namespace; default `true`;
- `MATTERCODEX_RUNTIME_QUOTA_PODS`, `MATTERCODEX_RUNTIME_QUOTA_JOBS`, `MATTERCODEX_RUNTIME_QUOTA_PVCS` - optional, object count quota для pod, batch Job и PVC в runtime namespace;
- `MATTERCODEX_RUNTIME_QUOTA_REQUESTS_STORAGE` - optional, суммарная quota на requested PVC storage в runtime namespace;
- `MATTERCODEX_RUNTIME_QUOTA_REQUESTS_CPU`, `MATTERCODEX_RUNTIME_QUOTA_REQUESTS_MEMORY`, `MATTERCODEX_RUNTIME_QUOTA_LIMITS_MEMORY` - optional, namespace quota на compute requests и memory limits; дефолты для single-node owner-инсталляции: requests `28`/`96Gi`, memory limits `112Gi`;
- `MATTERCODEX_RUNTIME_LIMIT_DEFAULT_MEMORY`, `MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_CPU`, `MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_MEMORY` - optional, container defaults для pod без явных resources; дефолты agent container: request `500m`/`1Gi`, memory limit `16Gi`, CPU limit отсутствует;
- `MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT` - optional, ServiceAccount для agent/smoke Job;
- `MATTERCODEX_CODEX_AUTH_SECRET` - optional, base name для Kubernetes Secrets с Codex `auth.json`; для account `primary` будет создан secret `${MATTERCODEX_CODEX_AUTH_SECRET}-primary`;
- `MATTERCODEX_DEFAULT_TEAM_NAME` - optional, по умолчанию `agents`;
- `MATTERCODEX_DEFAULT_TEAM_DISPLAY_NAME` - optional;
- `MATTERCODEX_DEFAULT_CHANNELS` - optional, список `name:Display Name` через запятую.

Скрипты печатают только статус наличия токенов, не значения.

## Render

```bash
bash scripts/k8s/render-bot-service.sh --env-file .env --render-dir /tmp/matter-codex-bot-render
```

В render directory попадают:

- встроенный registry manifest, если `MATTERCODEX_IMAGE_REGISTRY_MANAGED=true`;
- PVC для Kaniko build context, если `MATTERCODEX_IMAGE_BUILD_STRATEGY=kaniko`;
- config ConfigMap;
- ResourceQuota/LimitRange для runtime namespace, если `MATTERCODEX_RUNTIME_LIMITS_ENABLED=true`;
- ServiceAccount/RBAC для bot-service runtime adapter и agent runner;
- Deployment;
- Service;
- Ingress.

При `--apply` remote deploy по умолчанию использует `MATTERCODEX_IMAGE_BUILD_STRATEGY=kaniko`: локально создается только tar build context, он передается по SSH во временный pod с PVC, а image собирается Kaniko Job внутри кластера и push'ится во встроенный MatterCodex registry. Kubelet тянет готовые image из этого registry через `MATTERCODEX_IMAGE_REGISTRY_PULL_HOST`. Гигабайтные `docker save` archive через локальную сеть не передаются.

Kaniko Job получает повышенные default resources для тяжелого `agent-runner` image и использует `--skip-unused-stages=true`, чтобы не строить нецелевые Dockerfile stages при `--target`.

Для проверки сборки без изменения bot-service Deployment используйте `scripts/remote/install-bot-service.sh --env-file .env --apply --build-only` и выставьте `MATTERCODEX_BOT_SERVICE_BUILD_IMAGE=false` или `MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE=false`, если нужно собрать только один image.

Legacy strategy `MATTERCODEX_IMAGE_BUILD_STRATEGY=docker` оставлена только для контуров, где `docker` или `nerdctl` есть прямо на целевом сервере. Remote installer не делает локальный Docker build/import fallback.

Agent runner image содержит явный non-root user UID/GID `10001`. Runtime Job дополнительно задает pod/container `securityContext`: `runAsNonRoot`, `runAsUser`, `runAsGroup`, `fsGroup`, `seccompProfile: RuntimeDefault`, dropped capabilities, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`. Writable paths отдаются через volumes: `/workspace` для run PVC, `/codex-home` для device-code auth, `/home/matter-codex` для `gh`/npm/cache и `/tmp` для временных файлов.

Runtime namespace получает `ResourceQuota` `matter-codex-runtime-quota` и `LimitRange` `matter-codex-runtime-container-defaults`. Quota ограничивает общее число pods, batch Jobs, PVC, суммарный requested storage и суммарные cpu/memory requests/limits. LimitRange задает cpu/memory defaults для containers без явных resources, чтобы quota admission не отклоняла agent Job.

Полноценные agent session pod используют defaults из LimitRange. Короткие служебные `smoke`, Codex device-auth и auth-check Job задают собственные resources: requests `100m`/`128Mi`, memory limit `1Gi`, CPU limit отсутствует. Это не позволяет utility Job резервировать agent-sized `16Gi` и блокировать MCP-делегирование при заполненной runtime quota.

Если новый session pod отклонен ResourceQuota или не размещается scheduler из-за нехватки ресурсов, bot-service автоматически удаляет самый старый idle session pod без queued/running turn и повторяет запуск. PVC и snapshot сессии сохраняются. Если безопасного кандидата нет, turn остается queued; активные agent pod механизм capacity reclaim не удаляет.

## Remote dry-run

```bash
bash scripts/remote/install-bot-service.sh --env-file .env --dry-run=server
```

Если Mattermost token еще не задан, Secret не создается, а Deployment использует optional secret refs.

Если GitHub token или webhook secret заданы, deploy-скрипты создают Kubernetes Secret для reviewer/user account. При наличии token в Secret также кладутся `github-username` и `github-email`. Если задан `GIT_BOT_TOKEN` или `MATTERCODEX_AGENT_GITHUB_TOKEN`, deploy-скрипты создают отдельный Secret для developer/agent account с ключами `github-token`, `github-username`, `github-email`. Значения не печатаются.

Codex/OpenAI account authorization не выполняется deploy-скриптом. Основной путь - Mattermost команды ниже.

## Codex/OpenAI account authorization

Developer runner не использует raw API key. Для Codex CLI создается Kubernetes Secret с `auth.json`, полученным через device-code авторизацию из Mattermost.

Первичная авторизация account `primary`:

```text
/agents openai auth primary
/agents openai status primary
/agents openai cleanup primary
/agents openai delete primary
```

Ожидаемый результат:

- `auth primary` создает metadata для OpenAI account и временный Kubernetes Job `mc-codex-auth-primary`;
- `status primary` показывает ссылку `https://auth.openai.com/codex/device` и одноразовый code;
- владелец открывает ссылку в браузере, вводит code и подтверждает account;
- повторный `/agents openai status primary` сохраняет `auth.json` в Secret `${MATTERCODEX_CODEX_AUTH_SECRET}-primary`, помечает account как `authorized` и удаляет auth Job;
- содержимое `auth.json` не выводится в Mattermost, логи, PR или prompt;
- `cleanup primary` удаляет только временный auth Job;
- `delete primary` удаляет OpenAI account metadata, временный auth Job и созданный auth Secret. Удаление блокируется, если account используется agent profile.

Несколько аккаунтов поддерживаются через разные имена:

```text
/agents openai auth reviewer-plus
/agents openai status reviewer-plus
/agents openai list
/agents openai delete reviewer-plus
```

В кнопочном UX то же действие доступно через `/agents` -> `Аккаунты` -> `OpenAI`: кнопки `Auth account`, `Status account`, `Cleanup auth` и `Delete account` открывают формы с именем account. `Delete account` требует подтверждение `delete`.

Agent profile хранит `openai_account_name` и `github_account_name`. Seed profile `reviewer` использует OpenAI account `primary` и GitHub account `primary`; seed profile `developer` использует OpenAI account `primary` и GitHub account `agent`. Agent Job монтирует только Secret выбранных accounts.

## GitHub account metadata

GitHub token создается владельцем с нужными scopes и хранится в Kubernetes Secret. Bot-service не принимает raw token через Mattermost account dialog; dialog управляет только metadata binding:

- account name;
- Kubernetes Secret name;
- optional GitHub username;
- optional git author email;
- status `configured` или `disabled`.

Кнопочный путь:

```text
/agents -> Аккаунты -> GitHub -> Добавить
/agents -> Аккаунты -> GitHub -> Изменить
/agents -> Аккаунты -> GitHub -> Удалить
```

Ожидаемый результат: `Добавить` и `Изменить` сохраняют account metadata в PostgreSQL, `Удалить` удаляет только metadata row после подтверждения `delete`. Kubernetes Secret не удаляется и значение token нигде не печатается.

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

Ожидаемый результат: в выводе есть примененная версия `8|t`.

## Agent prompt templates

Prompt template относится к профилю агента и хранится в PostgreSQL. Bot-service рендерит template перед созданием Job с текущей Mattermost locale и передает готовый Markdown prompt в agent pod через ConfigMap. Agent runner не содержит prompt-текстов в Go-коде. В prompt доступны placeholders locale contract (`.Locale.Code`, `.Locale.Language`) и GitHub account/env contract, чтобы агент знал, что `gh` авторизован через `GH_TOKEN`/`GITHUB_TOKEN`, login доступен через `GITHUB_USERNAME`/`GITHUB_USER`, email - через `GITHUB_EMAIL`.

Базовые seed templates лежат в `services/external/bot-service/internal/domain/service/prompt_seeds/*.md`. На старте bot-service после migrations запускает seeder, который создает отсутствующие templates в PostgreSQL и не перетирает уже отредактированные в Mattermost templates. SQL migrations владеют только schema/profile metadata и не содержат Markdown prompt bodies. В коробке сидятся роли `manager`, `architect`, `developer`, `reviewer`, `docs`, `sre`, `qa-bot`, `ui-designer`, `improver`, `pm-delivery`, `analyst` и `mattercodex-admin`.

Agent-runner image содержит browser tooling для ролей `developer`, `ui-designer` и `qa-bot`: `chromium`, `playwright`, `@playwright/test`, `@playwright/mcp` и `wait-on`. Основной путь browser smoke/e2e в agent pod - Playwright CLI/API; системный `chromium` доступен для диагностики и версионных проверок. Browser artifacts следует сохранять в `/workspace/artifacts/screenshots` или `/workspace/artifacts/playwright`; `playwright-mcp` используется только если роль явно включает его в Codex `config.toml`.

Стартовый OSS-набор:

- `developer/developer_smoke`;
- `developer/implement_task`;
- `developer/fix_review`;
- `reviewer/review_pr`;
- `manager/coordinate_task`;
- `architect/architecture_task`;
- `docs/documentation_task`;
- `sre/operations_task`;
- `qa-bot/regression_task`;
- `improver/feedback_improvement`;
- `pm-delivery/delivery_status`;
- `analyst/analysis_task`;
- `mattercodex-admin/admin_task`.

Управление через Mattermost:

```text
/agents prompt help reviewer review_pr
/agents prompt list
/agents prompt show reviewer review_pr
/agents prompt render reviewer review_pr
/agents prompt set reviewer review_pr <markdown template>
```

Ожидаемый результат:

- `prompt help` показывает доступные placeholders и template-функции;
- `prompt set` ожидает Markdown с Go `text/template` placeholders, test-render'ит его на sample data и сохраняет только если render успешен;
- `prompt render` позволяет проверить сохраненный или переданный inline template без запуска agent Job; строка с языком владельца должна меняться при `/agents locale set en|ru`.

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
/agents
```

Ожидаемый результат: channel-visible menu card с кнопками `Projects`, `Аккаунты`, `Репозитории`, `Roles`, `Chats`, `Advanced`, `Runtime`, `System`, `Help`. Нажатия по кнопкам должны обновлять эту же карточку на выбранный раздел и показывать короткий ephemeral-статус. Кнопка `Назад` возвращает главное меню. В главном меню поля OpenAI и GitHub показывают счетчик готовых accounts в формате `готово/всего`.

Проверка account menu:

- `Аккаунты` -> `OpenAI`: карточка должна показать кнопки `Список accounts`, `Auth account`, `Status account`, `Cleanup auth`, `Delete account`, `Назад`; кнопки auth/status/cleanup/delete открывают dialog с именем account и возвращают результат в dialog.
- `Аккаунты` -> `GitHub`: карточка должна показать кнопки `GitHub accounts`, `Добавить`, `Изменить`, `Удалить`, `Check matter-codex`, `Webhook matter-codex`, `Назад`; `Добавить/Изменить/Удалить` открывают формы metadata CRUD.

Typed-команды остаются fallback-интерфейсом для точной ручной проверки:

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
/agents prompt help reviewer review_pr
/agents prompt render reviewer review_pr
/agents openai list
/agents repo add github codex-k8s/matter-codex main
/agents repo list
```

Ожидаемый результат: команды отвечают ephemeral-сообщениями, `locale set en` переключает ответы на английский, `locale set ru` возвращает русские ответы, profile list показывает OpenAI и GitHub account для профиля, prompt render показывает sample-render без сохранения секретов, repository появляется в списке, а Mattermost создаёт/показывает канал `repo-codex-k8s-matter-codex`.

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

Кнопочная проверка CRUD репозиториев:

1. В Mattermost выполнить `/agents`.
2. Открыть `Репозитории`.
3. Нажать `Добавить репо`, заполнить `Провайдер=GitHub`, `Репозиторий=codex-k8s/matter-codex`, `Ветка=main`, отправить форму.
4. Проверить, что карточка меню обновилась результатом добавления или обновления репозитория.
5. Нажать `Изменить репо`, указать тот же репозиторий и ветку, отправить форму.
6. Нажать `Удалить репо`, указать тестовый репозиторий и ввести `delete` в поле подтверждения.

Удаление в этом сценарии удаляет только запись `matter_codex_repositories`. Канал Mattermost и GitHub webhook не удаляются.

Дополнительная проверка Kubernetes runner foundation:

```text
/agents token check
/agents runtime smoke smoke-manual
/agents runtime status smoke-manual
/agents runtime cleanup smoke-manual
/agents runtime prune 24h
```

Ожидаемый результат:

- token check показывает `kubernetes runtime: configured`;
- runtime smoke возвращает run id, Job и PVC без вывода секретов; Job использует `matter-codex-agent-runner`;
- runtime status показывает Job/PVC, pod phase и короткий log tail smoke Job;
- runtime cleanup удаляет Job и PVC.
- runtime prune по умолчанию работает в dry-run режиме и показывает старые завершенные Job/PVC/ConfigMap, которые будут удалены retention cleanup.

Проверка runtime quota/limits после deploy:

```text
/agents runtime smoke quota-manual
/agents runtime status quota-manual
/agents runtime cleanup quota-manual
```

На кластере `kubectl -n <namespace> get resourcequota matter-codex-runtime-quota` и `kubectl -n <namespace> get limitrange matter-codex-runtime-container-defaults` должны показывать примененные объекты. Runtime smoke должен проходить с `smoke-ok`; это подтверждает, что defaults и quota не блокируют agent Job.

Проверка apply-режима retention cleanup на завершенном smoke run:

```text
/agents runtime smoke prune-manual
/agents runtime status prune-manual
/agents runtime prune 1s
/agents runtime prune 1s --apply
/agents runtime status prune-manual
```

Ожидаемый результат:

- первый `runtime prune 1s` показывает `mode: dry-run` и не удаляет ресурсы;
- `runtime prune 1s --apply` удаляет завершенный Job/PVC/ConfigMap только если run уже завершен;
- активные Job не удаляются и учитываются как skipped;
- после apply `runtime status prune-manual` возвращает, что run не найден.

Дополнительная проверка Codex reviewer agent:

Перед проверкой нужен authorized OpenAI account из профиля `reviewer`, настроенный GitHub account `primary` и существующий открытый GitHub PR.

```text
/agents openai list
/agents review pr codex-k8s/matter-codex <pr-number> review-manual
/agents review status review-manual
```

Ожидаемый результат:

- review pr возвращает run id, PR number, Job и PVC;
- в ответе review pr указан OpenAI account `primary`;
- через некоторое время `review status` показывает pod phase и artifacts `pr-url`, `review-decision`, `review-submitted`;
- Codex reviewer получает `gh` с env `GH_TOKEN`/`GITHUB_TOKEN`, `GITHUB_USERNAME`/`GITHUB_USER`, `GITHUB_EMAIL` и должен сам публиковать inline review comments от reviewer account; если он не отправил review сам, runner отправляет fallback summary review;
- log tail не содержит значений OpenAI/GitHub/Mattermost секретов.

После проверки удалить Kubernetes resources:

```text
/agents review cleanup review-manual
```

Cleanup удаляет только Kubernetes Job/PVC, а не GitHub review/comment.

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
- GitHub token, username, email и webhook secret хранятся в Kubernetes Secret и не попадают в ConfigMap.
- Slash token, полученный из Mattermost API, пишется во временный файл с правами `0600`, затем в Kubernetes Secret.
- Логи provisioning показывают только безопасные статусы `exists/created/updated`.
- bot-service Deployment запускается non-root, с dropped Linux capabilities, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true` и `seccompProfile: RuntimeDefault`.
- Ресурсы bot-service настраиваются через `MATTERCODEX_BOT_SERVICE_CPU_REQUEST`, `MATTERCODEX_BOT_SERVICE_MEMORY_REQUEST` и `MATTERCODEX_BOT_SERVICE_MEMORY_LIMIT`. Значения по умолчанию: requests `100m`/`512Mi`, memory limit `8Gi`, CPU limit отсутствует; увеличенный memory limit нужен для кратковременных пиков при приеме крупных Codex session snapshots.
- bot-service получает namespace-scoped Role на создание/чтение/удаление runtime Job/PVC, чтение pod/log, `pods/exec` для чтения готового `auth.json` из auth Job и create/get/list/update/delete Secret для account-specific Codex auth и session-token cleanup.
- Runtime namespace получает namespace-level ResourceQuota/LimitRange с owner-instance defaults и env overrides, потому что MVP namespace общий для Mattermost, bot-service и agent Job. CPU requests сохраняют scheduler accounting, CPU limits не задаются, а memory limit quota остается ниже физической памяти типового owner-сервера.
- ServiceAccount agent runner создается без automount token; smoke pod также явно отключает automount.
- Codex smoke/auth/developer/reviewer Job запускаются без automount service account token и с non-root securityContext.
- Codex developer/reviewer Job получает Codex `auth.json` выбранного OpenAI account и GitHub token/username/email выбранного GitHub account только через Kubernetes Secret volume mount.
- Developer/reviewer prompt templates хранятся в PostgreSQL, редактируются через Mattermost и передаются agent pod как отрендеренный Markdown через ConfigMap.
- `CODEX_HOME/config.toml` задает `shell_environment_policy` с минимальным environment для команд, которые запускает Codex: `gh` получает только нужные GitHub env, без Mattermost/OpenAI/Kubernetes secret values.
- Codex agent внутри isolated Kubernetes Job запускается с `sandbox_mode = "danger-full-access"`, потому что `workspace-write` требует `bubblewrap`, который в текущем Kubernetes pod падает до выполнения shell-команд. Изоляционная граница MVP для agent run: отдельный pod, отдельный PVC, отключенный automount service account token и минимальные Secret volume mounts.
- Developer runner реализован отдельным Go binary в подготовленном image и сам выполняет push/PR после `codex exec`; prompt contract запрещает Codex агенту пушить branch или создавать PR напрямую, но разрешает отвечать на review threads через `gh` при соответствующей задаче.
- Reviewer runner реализован отдельным Go binary в подготовленном image и дает Codex reviewer доступ к `gh` для inline review comments; если Codex не отправил review сам, runner отправляет fallback summary review после `codex exec`.

## Сессии, role bots и `runs`

- OpenAI account записывается в agent session при ее создании. Изменение account у роли не переносит существующую Codex session: для нового account требуется новый корневой Mattermost thread.
- Статус `blocked` означает подтвержденную cyber-safety блокировку поставщика. Runner не выполняет автоматический обход; пользователь получает ссылку на Trusted Access и продолжает измененную задачу в новом thread.
- Для каждого проекта bootstrap создает публичный канал `runs`. В нем служебный MatterCodex account создает по одной обновляемой карточке на turn; карточки имеют ссылки на trigger, рабочий thread и все parent run cards.
- Role identity всегда должна быть Mattermost bot account. При старте bot-service существующая обычная учетная запись роли конвертируется в bot; fallback с созданием обычного пользователя отсутствует.
- Mattermost не поддерживает read-only состояние отдельного thread. Для контролируемого закрытия истории MatterCodex ставит `thread_context.status = closed`: UI Mattermost по-прежнему позволяет отправить текст, но bot-service не создает turn и просит начать новый корневой thread. Архивация всего канала для этой задачи не применяется.

## Production gaps после MVP

Актуальные production gaps и последовательность их закрытия ведутся в `docs/roadmap/epics-and-waves.md` и `docs/operations/**`.
