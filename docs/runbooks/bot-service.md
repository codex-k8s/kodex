# Bot-Service Runbook

## Назначение

Этот runbook описывает второй кодовый этап: `matter-codex` bot-service для Mattermost.

В этом PR сервис умеет:

- отвечать на `/healthz`;
- принимать Mattermost slash callback `/mattermost/slash/agents`;
- показывать Mattermost menu card по пустому `/agents`;
- принимать Mattermost menu action callback `/mattermost/actions/agents` для кнопок menu card;
- принимать Mattermost interactive action callback `/mattermost/actions/flow`;
- отвечать на `/agents status`;
- выполнять admin-команды `/agents repo add`, `/agents repo list`, `/agents token check`, `/agents locale get|set`, `/agents profile list`, `/agents prompt help|list|show|render|set`, `/agents openai auth|status|list|cleanup`;
- выполнять GitHub adapter/account команды `/agents github account list`, `/agents github check`, `/agents github branch`, `/agents github pr`;
- управлять через Mattermost dialog-кнопки metadata GitHub accounts: add/edit/delete account binding к существующему Kubernetes Secret без ввода raw token в Mattermost;
- принимать GitHub webhook callback `/github/webhook` с HMAC validation;
- автоматически регистрировать repo webhook при `/agents repo add github owner/name [default-branch]`, если GitHub token имеет hook write permission;
- выполнять Kubernetes runtime smoke-команды `/agents runtime smoke|status|cleanup|prune` через client-go, Job, PVC и подготовленный agent-runner image;
- выполнять Codex developer smoke-команды `/agents dev smoke|status|cleanup`, создающие отдельный Job/PVC, branch, commit и draft PR через OpenAI account, GitHub account и prompt template из agent profile;
- выполнять Codex reviewer-команды `/agents review pr|status|cleanup`, запускающие отдельный Job/PVC для review существующего GitHub PR через OpenAI account, GitHub account и prompt template из agent profile;
- выполнять developer-review flow-команды `/agents flow start|status|card|cleanup`, которые запускают developer agent, автоматически передают созданный PR reviewer agent, повторяют fix-попытку при `request_changes`, блокируют flow после трех попыток и публикуют Mattermost card с owner buttons;
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
- `MATTERCODEX_RUNTIME_ENABLED` - optional, включает Kubernetes runtime adapter;
- `MATTERCODEX_RUNTIME_NAMESPACE` - optional, namespace для Job/PVC runtime-запусков;
- `MATTERCODEX_RUNTIME_SMOKE_IMAGE` - optional, legacy image setting; текущий smoke Job запускается через `MATTERCODEX_AGENT_RUNNER_IMAGE`;
- `MATTERCODEX_AGENT_RUNNER_IMAGE` - optional, image для smoke/developer/reviewer/auth Job; текущий MVP default `matter-codex-agent-runner:dev`;
- `MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE` - optional, при `true` install script собирает agent-runner image на целевом сервере перед deploy;
- `MATTERCODEX_CODEX_PACKAGE` - optional, npm package spec Codex CLI, который устанавливается в agent-runner image при сборке;
- `MATTERCODEX_RUNTIME_WORKSPACE_STORAGE_SIZE` - optional, размер PVC рабочего каталога smoke-запуска;
- `MATTERCODEX_RUNTIME_JOB_TTL_SECONDS` - optional, TTL завершенных smoke Job;
- `MATTERCODEX_RUNTIME_LOG_TAIL_LINES` - optional, число последних строк pod log для `/agents runtime status`;
- `MATTERCODEX_RUNTIME_LIMITS_ENABLED` - optional, включает render/apply namespace `ResourceQuota` и `LimitRange` для runtime namespace; default `true`;
- `MATTERCODEX_RUNTIME_QUOTA_PODS`, `MATTERCODEX_RUNTIME_QUOTA_JOBS`, `MATTERCODEX_RUNTIME_QUOTA_PVCS` - optional, object count quota для pod, batch Job и PVC в runtime namespace;
- `MATTERCODEX_RUNTIME_QUOTA_REQUESTS_STORAGE` - optional, суммарная quota на requested PVC storage в runtime namespace;
- `MATTERCODEX_RUNTIME_QUOTA_REQUESTS_CPU`, `MATTERCODEX_RUNTIME_QUOTA_REQUESTS_MEMORY`, `MATTERCODEX_RUNTIME_QUOTA_LIMITS_CPU`, `MATTERCODEX_RUNTIME_QUOTA_LIMITS_MEMORY` - optional, namespace quota на compute requests/limits;
- `MATTERCODEX_RUNTIME_LIMIT_DEFAULT_CPU`, `MATTERCODEX_RUNTIME_LIMIT_DEFAULT_MEMORY`, `MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_CPU`, `MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_MEMORY` - optional, container defaults для pod без явных resources;
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

- code ConfigMap с Go source archive (`go.mod`, `go.sum`, `libs/go/i18n`, `services/external/bot-service`);
- config ConfigMap;
- ResourceQuota/LimitRange для runtime namespace, если `MATTERCODEX_RUNTIME_LIMITS_ENABLED=true`;
- ServiceAccount/RBAC для bot-service runtime adapter и agent runner;
- Deployment;
- Service;
- Ingress.

Agent runner image не попадает в render directory. При `--apply` deploy script по умолчанию собирает отдельный image из `services/jobs/agent-runner/Dockerfile`. Если на целевом сервере есть `docker` или `nerdctl`, сборка идет там; если builder'а на сервере нет, но доступен remote `k3s ctr`/`ctr` import и локальный Docker, script собирает image локально и импортирует его в Kubernetes runtime по SSH.

Agent runner image содержит явный non-root user UID/GID `10001`. Runtime Job дополнительно задает pod/container `securityContext`: `runAsNonRoot`, `runAsUser`, `runAsGroup`, `fsGroup`, `seccompProfile: RuntimeDefault`, dropped capabilities, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`. Writable paths отдаются через volumes: `/workspace` для run PVC, `/codex-home` для device-code auth, `/home/matter-codex` для `gh`/npm/cache и `/tmp` для временных файлов.

Runtime namespace получает `ResourceQuota` `matter-codex-runtime-quota` и `LimitRange` `matter-codex-runtime-container-defaults`. Quota ограничивает общее число pods, batch Jobs, PVC, суммарный requested storage и суммарные cpu/memory requests/limits. LimitRange задает cpu/memory defaults для containers без явных resources, чтобы quota admission не отклоняла agent Job.

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

Базовые templates создаются migration:

- `developer/developer_smoke`;
- `developer/implement_task`;
- `developer/fix_review`;
- `reviewer/review_pr`.

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
- `prompt render` позволяет проверить сохраненный или переданный inline template без запуска agent Job; строка `Language: {{.Locale.Language}} ...` должна меняться при `/agents locale set en|ru`.

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

Ожидаемый результат: channel-visible menu card с кнопками `Запуск flow`, `Pending`, `Репозитории`, `Аккаунты`, `Профили`, `Prompts`, `Runtime`, `System`, `Help`. Нажатия по кнопкам должны обновлять эту же карточку на выбранный раздел и показывать короткий ephemeral-статус. Кнопка `Назад` возвращает главное меню. В главном меню поля OpenAI и GitHub показывают счетчик готовых accounts в формате `готово/всего`.

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

Дополнительная проверка Codex developer agent:

Перед первой проверкой авторизовать OpenAI account из Mattermost:

```text
/agents openai auth primary
/agents openai status primary
```

После получения ссылки и кода открыть ссылку в браузере, ввести code, затем снова выполнить:

```text
/agents openai status primary
/agents openai list
```

Ожидаемый результат: account `primary` имеет status `authorized`.

```text
/agents token check
/agents dev smoke codex-k8s/matter-codex dev-manual
/agents dev status dev-manual
```

Ожидаемый результат:

- developer smoke возвращает run id, branch `matter-codex-dev-dev-manual`, Job и PVC;
- в ответе developer smoke указан OpenAI account `primary`;
- через некоторое время `dev status` показывает pod phase и artifact `pr-url`;
- в GitHub появляется draft PR с безопасным документационным изменением `docs/dogfood/codex-developer-smoke.md`;
- log tail не содержит значений OpenAI/GitHub/Mattermost секретов.

После проверки удалить Kubernetes resources:

```text
/agents dev cleanup dev-manual
```

Если smoke run создал draft PR, его надо закрыть/удалить вручную или оставить как проверочный артефакт до решения владельца. Cleanup удаляет только Kubernetes Job/PVC, а не GitHub branch/PR.

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

Дополнительная проверка developer-review flow:

Перед проверкой нужны authorized OpenAI account для профилей `developer` и `reviewer`, настроенные GitHub accounts `agent` и `primary`, а также доступ bot-service к Kubernetes runtime.

```text
/agents openai list
/agents profile list
/agents prompt render developer implement_task
/agents prompt render developer fix_review
/agents prompt render reviewer review_pr
/agents flow start codex-k8s/matter-codex flow-manual Update docs/dogfood/matter-codex-flow-smoke.md with a short Russian smoke note for developer-review flow
/agents flow status flow-manual
/agents flow card flow-manual
```

Ожидаемый результат:

- flow start возвращает branch `matter-codex-flow-flow-manual`, developer run `flow-manual-d1`, Job, PVC и строку `card`;
- если slash command был выполнен из Mattermost channel и bot token настроен, bot-service публикует flow card в текущий канал; иначе card можно создать/обновить вручную командой `/agents flow card flow-manual`;
- flow card содержит buttons `Approve`, `Reject`, `Rerun review`, `Stop`;
- после завершения developer Job повторный `flow status` показывает `pr-url` и автоматически стартует reviewer run `flow-manual-r1`;
- после завершения reviewer Job повторный `flow status` показывает один из финальных или промежуточных статусов: `approved_by_reviewer`, `waiting_owner`, `fix_running`, `reviewer_failed`, `blocked`;
- кнопка `Approve` переводит flow в `owner_approved` и не выполняет merge;
- кнопка `Reject` переводит flow в `owner_rejected` и не меняет PR;
- кнопка `Stop` переводит flow в `stopped` и не удаляет Kubernetes Job/PVC;
- кнопка `Rerun review` стартует новый reviewer run для текущего PR и обновляет card;
- если reviewer вернул `request_changes`, flow стартует developer fix run `flow-manual-d2` на той же ветке `matter-codex-flow-flow-manual`, затем следующий `flow status` снова запускает reviewer run `flow-manual-r2`;
- после трех попыток с `request_changes` flow переходит в `blocked`;
- log tail и ответы Mattermost не содержат значений OpenAI/GitHub/Mattermost секретов.

После проверки удалить Kubernetes resources всех run'ов flow:

```text
/agents flow cleanup flow-manual
```

Cleanup удаляет только Kubernetes Job/PVC, а не GitHub branch/PR/review comments.

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
- bot-service Deployment запускается non-root, с dropped Linux capabilities, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `seccompProfile: RuntimeDefault` и базовыми resource requests/limits.
- bot-service получает namespace-scoped Role на создание/чтение/удаление runtime Job/PVC, чтение pod/log, `pods/exec` для чтения готового `auth.json` из auth Job и create/update/delete Secret для account-specific Codex auth.
- Runtime namespace получает namespace-level ResourceQuota/LimitRange с conservative defaults и env overrides, потому что MVP namespace общий для Mattermost, bot-service и agent Job.
- ServiceAccount agent runner создается без automount token; smoke pod также явно отключает automount.
- Codex smoke/auth/developer/reviewer Job запускаются без automount service account token и с non-root securityContext.
- Codex developer/reviewer Job получает Codex `auth.json` выбранного OpenAI account и GitHub token/username/email выбранного GitHub account только через Kubernetes Secret volume mount.
- Developer/reviewer prompt templates хранятся в PostgreSQL, редактируются через Mattermost и передаются agent pod как отрендеренный Markdown через ConfigMap.
- Mattermost flow card buttons используют per-flow action token в Mattermost action context; token не выводится в card text, ответы, логи или PR.
- `CODEX_HOME/config.toml` задает `shell_environment_policy` с минимальным environment для команд, которые запускает Codex: `gh` получает только нужные GitHub env, без Mattermost/OpenAI/Kubernetes secret values.
- Codex agent внутри isolated Kubernetes Job запускается с `sandbox_mode = "danger-full-access"`, потому что `workspace-write` требует `bubblewrap`, который в текущем Kubernetes pod падает до выполнения shell-команд. Изоляционная граница MVP для agent run: отдельный pod, отдельный PVC, отключенный automount service account token и минимальные Secret volume mounts.
- Developer runner реализован отдельным Go binary в подготовленном image и сам выполняет push/PR после `codex exec`; prompt contract запрещает Codex агенту пушить branch или создавать PR напрямую, но разрешает отвечать на review threads через `gh` при соответствующей задаче.
- Reviewer runner реализован отдельным Go binary в подготовленном image и дает Codex reviewer доступ к `gh` для inline review comments; если Codex не отправил review сам, runner отправляет fallback summary review после `codex exec`.

## Production gaps после MVP

Актуальный список production gaps ведется в `docs/strategy/production-gaps.md`.
