# Архитектура MVP

## Выбранный стартовый подход

MVP реализуется как один backend-сервис `matter-codex`, но с явными внутренними модулями:

- `mattermost` - slash commands, bot posts, interactive actions, thread updates.
- `projects` - Project = Mattermost team, project repository bindings и project-level settings.
- `chats` - Chat = private Mattermost channel, participants, repository bindings и thread/session context.
- `orchestrator` - state machine run/step и переходы flow.
- `runtime` - создание Kubernetes pod/job/PVC для agent run.
- `github` - операции repository, branch, PR, comments, review status.
- `credentials` - безопасные metadata и ссылки на Kubernetes Secrets.
- `openai` - OpenAI account profiles, device-code authorization sessions и routing аккаунтов по agent session.
- `agents` - agent roles, optional prompt templates, `config.toml` overlays, MCP bindings и render контекста.
- `audit` - журнал действий и безопасных событий.

Такой старт быстрее отдельного набора микросервисов, но не смешивает доменные ответственности.

## Mattermost integration

На первом этапе используется внешний bot-service:

- slash command `/agents`;
- Mattermost bot account token для REST API;
- posts в channel/thread через `/api/v4/posts`;
- interactive message actions/buttons с callback URL в bot-service;
- delayed responses через `response_url` для долгих команд;
- interactive dialogs, message menus и карточки сущностей для owner-facing UX;
- создание дефолтных каналов после установки;
- создание Mattermost teams под projects;
- создание private channels под chats;
- создание каналов под legacy repository onboarding.

Mattermost server plugin не является обязательным для MVP, потому что нужный первый UX можно закрыть внешним сервисом через REST API и slash/interactions. Plugin остается вариантом расширения, если после первых ручных проверок окажется, что без него неудобно управлять каналами, меню или системными настройками.

## Owner-facing control surface

`/agents` без аргументов открывает главное меню. Дальнейшие действия должны быть доступны через кнопки, списки, message menus и dialogs. Typed slash commands остаются fallback/debug API и не должны быть основным способом ручной проверки продуктового сценария.

UI state хранит технические identifiers в action context или signed/encoded dialog state:

- project id/team id;
- chat id/channel id;
- role id;
- repository id/full name;
- account name;
- profile name;
- template key;
- flow id;
- run id;
- Kubernetes resource references.

Если owner работает с уже существующей сущностью, он выбирает ее из UI. Ввод технического id допустим только для debug/fallback command path.

## Mattermost teams and channels

После установки система должна подготовить базовый control surface:

- `agents-control` - административные команды, onboarding repo/project, tokens, OpenAI accounts, profiles и flow.
- `agents-runs` - общая лента run и переходов статусов.
- `agent-alerts` - ошибки, блокеры, лимиты, падения runner и запросы решения.
- `agents-audit` - безопасные audit summaries без секретов.

Основная продуктовая модель:

- project создает или привязывает Mattermost team;
- chat создает private channel внутри project team;
- thread внутри chat привязывается к конкретной agent session;
- в одном thread работает один агент и одна session;
- thread может иметь optional repository context; если repository не выбран, session стартует без checkout;
- если в chat несколько roles, они должны быть визуально различимы через bot identity или явное имя role в ответе.

Legacy repository channels остаются для совместимости с текущим onboarding, но не являются главным контейнером будущего UX.

## State machine

Минимальные состояния run:

- `created`
- `queued`
- `developer_running`
- `developer_failed`
- `pr_opened`
- `review_running`
- `changes_requested`
- `fix_running`
- `approved_by_reviewer`
- `waiting_owner`
- `owner_approved`
- `owner_rejected`
- `merged`
- `blocked`
- `cancelled`

Run хранит текущий статус, а step хранит конкретную попытку agent execution. Переходы выполняет orchestrator, а не runner pod.

## Agent runner

Runner запускает `codex exec --json` в Kubernetes pod:

- рабочая директория монтируется из PVC;
- repository checkout выполняется внутри workspace только если thread/run получил repository context;
- если repository context отсутствует, Codex запускается в пустом `/workspace` без ошибки;
- `CODEX_HOME` указывает на отдельный путь в PVC;
- OpenAI account выбирается из agent profile или явно из run/session;
- runner материализует auth/config выбранного OpenAI account в runtime `CODEX_HOME`;
- GitHub token передается только агентам, которым он разрешен;
- MCP credentials передаются только через разрешенные config bindings;
- stdout JSONL парсится runtime-модулем и превращается в step events;
- финальное сообщение и ссылки на PR сохраняются как artifact summary.

Базовая команда исполнения должна быть параметризована профилем агента:

```bash
CODEX_HOME=/workspace/.codex \
codex exec --json \
  --cd /workspace/repo \
  --profile "${CODEX_PROFILE}" \
  "${PROMPT}"
```

`CODEX_PROFILE` рендерится системой из agent profile. В него входят модель, sandbox policy, approval policy, MCP servers, env bindings, project instruction discovery и другие безопасные позиции `config.toml`.

Для no-repository sessions `--cd` указывает на `/workspace`, а GitHub token может оставаться доступным роли для `gh`/GitHub API операций, если он назначен через project или role.

`sandbox_mode = "danger-full-access"` остается допустимым default для MVP внутри изолированного pod. Это осознанный риск владельца. Значение должно быть переопределяемым через Codex `config.toml` overlay в agent profile.

## OpenAI accounts

OpenAI-доступ настраивается отдельными account profiles:

- администратор запускает device-code authorization flow из Mattermost UI;
- bot-service показывает безопасную карточку авторизации без вывода токенов;
- после подтверждения account сохраняется как credential reference в Kubernetes Secret;
- account получает имя, статус, допустимые модели, лимиты и разрешенные agent profiles;
- run/session выбирает account явно или наследует его из agent profile.

Один общий raw API key не является моделью доступа для agent sessions. Для текущего MVP runner получает Codex `auth.json`, сохраненный как Kubernetes Secret после device-code авторизации.

## Agent roles and config overlays

Agent role хранит не только optional prompt template, но и runtime-настройки Codex:

- default OpenAI account;
- model policy;
- `codex exec` profile name;
- sandbox и approval policy;
- разрешенные GitHub credentials;
- разрешенные MCP servers;
- `config.toml` overlay;
- env bindings для MCP/API keys без вывода значений;
- stop rules, retry limits и финальный report contract.

Если prompt template пустой, prompt builder не подставляет агрессивный business prompt и использует пользовательское сообщение из Mattermost thread как основную инструкцию.

Пример: Context7 задается как MCP binding в agent role. Система хранит только ссылку на credential, а runner рендерит `config.toml` и env для конкретного pod.

## Kubernetes runtime

Для каждого agent step создаются:

- Kubernetes Job или Pod;
- PVC с workspace;
- ServiceAccount с минимальными правами;
- env/secret refs только для разрешенных credentials;
- labels `matter-codex.dev/run-id`, `step-id`, `agent-role`;
- cleanup policy после завершения run.

PVC живет дольше pod, чтобы developer/reviewer/fix шаги могли работать с одной веткой и логами. Retention задается политикой run.

## GitHub integration

MVP использует bot PAT из secret:

- проверить доступ к GitHub owner и repo;
- искать repositories внутри project GitHub owner;
- создать branch;
- push commit;
- открыть PR;
- читать reviews и comments;
- оставить review или comment;
- получить merge status.

GitHub App остается целевым вариантом после MVP, потому что дает лучшую модель permissions, installations и audit.

Owner-facing GitHub account path не должен требовать Kubernetes Secret name как обязательный ввод. Базовый сценарий: owner вставляет token в secure dialog, bot-service проверяет его через GitHub API, создает Kubernetes Secret и сохраняет metadata. Bring-your-own Secret остается advanced path.

## Credential policy

Система хранит:

- stable credential id;
- тип секрета;
- scope и разрешенные agent profiles;
- ссылку на Kubernetes Secret;
- время последней проверки;
- безопасный статус проверки.

Система не хранит и не показывает значения секретов в Mattermost thread, логах и prompt.

## База данных

Для скорости MVP допустима одна PostgreSQL database с отдельными таблицами и префиксами доменных модулей. Доменные модули не должны полагаться на cross-domain SQL как на бизнес-контракт: связи между ними должны оставаться явными на уровне кода и событий.

Минимальные таблицы:

- projects;
- project_repository_bindings;
- repositories;
- credentials;
- openai_accounts;
- openai_authorization_sessions;
- agent_roles;
- chats;
- chat_participants;
- chat_repository_bindings;
- agent_profiles;
- agent_config_overlays;
- prompt_templates;
- mattermost_channel_bindings;
- flows;
- runs;
- steps;
- artifacts;
- audit_events;

## Observability

Первый уровень:

- structured application logs;
- Mattermost thread status;
- Kubernetes pod/job status;
- short tail logs в step artifact;
- audit events в БД.

Полные pod logs остаются в Kubernetes/runtime и не копируются без retention policy.
