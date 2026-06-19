# Single-user Project/Chat Model

Этот документ фиксирует новую целевую модель `matter-codex` после отказа от flow-centric UX. Инстанс считается личным инструментом владельца, поэтому продукт оптимизируется под короткий путь настройки, а не под multi-tenant role model.

## Принятая модель

- `Project` = отдельная Mattermost team.
- `Chat` = Mattermost private channel внутри project team.
- `AgentRole` = проектная роль агента, которую можно добавить в chat.
- `Project GitHub owner` = GitHub organization или user namespace, к которому относится project.
- `Repository` = GitHub repository внутри GitHub owner проекта, подключенный глобально и затем привязанный к project/chat как доступный вариант.
- `ThreadContext` = привязка Mattermost thread к optional repository и agent session context.
- `GitHub account` и `OpenAI account` = переиспользуемые credentials, назначаемые на role.
- `Flow` больше не является центральной сущностью продукта. Legacy flow/actions остаются только в `Advanced` для совместимости и диагностики.

Главный путь владельца:

1. `/agents -> Projects -> Create project`.
2. Добавить или выбрать accounts.
3. Указать GitHub owner проекта и подключить repositories из этого owner.
4. Создать agent roles.
5. Создать private chat channel.
6. Писать задачу в chat/thread и выбирать repository для thread, если он нужен.

## Project

Project хранит:

- имя, slug и описание;
- Mattermost team id;
- platform GitHub account;
- GitHub owner и owner type (`org` или `user`);
- project-level advanced/runtime settings;
- repository bindings из GitHub owner проекта;
- доступные roles/chats через связанные таблицы.

Создание project должно создавать или привязывать Mattermost team. Slug используется как Mattermost team name, поэтому он должен быть DNS-safe.

Project не хранит один "главный repository". Он хранит GitHub namespace проекта. Например, project/team может быть привязан к `radar-auto`, а конкретные repositories `radar-auto/api`, `radar-auto/web` и `radar-auto/docs` выбираются позже как доступные project repositories.

## AgentRole

Role хранит:

- project id;
- name и role type (`manager`, `pm_delivery`, `worker`, `reviewer`, `analyst`, `architect`, `writer`, `sre`, `lexical_guard`, `custom`);
- optional prompt template;
- prompt mode;
- optional GitHub/OpenAI account bindings;
- Kubernetes access mode;
- список project runtime env variables, явно доступных этой role;
- Codex sandbox mode;
- Codex `config.toml` overlay;
- advanced settings JSON;
- enabled flag и optional bot identity.

Prompt template является nullable. Если template пустой, role работает в raw mode: пользовательское сообщение из chat/thread считается основной инструкцией. Это валидный режим, а не ошибка.

## Chat

Chat хранит:

- project id;
- Mattermost channel id;
- name, slug, description;
- chat type (`manager`, `pm_delivery`, `worker_reviewer`, `single_custom`, `multi_role_custom`, `custom`);
- selected roles;
- selected repositories как allowlist вариантов для thread;
- optional root GitHub issue/epic;
- optional work policy и settings.

По умолчанию создается private channel. При выборе repositories в chat creator они задают доступный набор для threads этого chat. Если chat-level allowlist пустой, thread может выбрать любой repository, уже привязанный к project.

## Thread Repository Context

Repository выбирается не как постоянная настройка всего chat, а как optional context конкретного Mattermost thread:

- при первом сообщении в новом thread система показывает карточку выбора repository, если у project/chat есть доступные repositories;
- owner может выбрать конкретный repository или режим `No repository`;
- выбранный repository сохраняется в `ThreadContext` и переиспользуется всеми последующими turns этого thread;
- если repository выбран, agent session pod получает checkout этого repository в `/workspace/repo`;
- если выбран `No repository` или repositories еще не подключены, агент стартует в PVC без checkout и работает только с prompt/chat/MCP context;
- отсутствие repository не считается ошибкой для manager, PM, analyst, ad-hoc и raw chat roles.

## Accounts And Repositories

GitHub accounts остаются PAT/fine-grained PAT bindings для MVP. Account metadata управляется глобально, но platform GitHub account выбирается на уровне project, потому что project/Mattermost team соответствует конкретной GitHub organization или user namespace. Этот project account используется для поиска repositories только внутри `Project.GitHubOwner`, загрузки branches, регистрации webhook и как default GitHub access для roles без собственного account override.

OpenAI accounts остаются Codex device-code accounts. В role выбирается account binding, а runner получает только ссылку на Kubernetes Secret с auth material.

Основной happy path: открыть project dashboard, указать GitHub owner и platform account, подключить repositories из этого owner через branch selection, затем в thread выбрать нужный repository или `No repository`. Глобальный `Repositories` раздел остается fallback/advanced path, где owner может выбрать GitHub account вручную.

## Project Runtime Env Variables

У project есть список runtime env variables для внешних credentials и окружений проекта. Это нужно для случаев, когда workload проекта живет не в том же Kubernetes cluster, где запущены MatterCodex, Mattermost и agent pods.

Модель:

- owner создает project runtime variable из project dashboard;
- имя переменной задается как обычное env name, например `RADAR_AUTO_KUBECONFIG` или `STAGING_DB_URL`;
- Kubernetes Secret name генерируется системой из project slug и env name;
- value сохраняется только в Kubernetes Secret и не рендерится в карточки, prompt или логи;
- description хранится в БД и рендерится в prompt, чтобы агент понимал назначение переменной;
- role получает variable только через явную binding-кнопку;
- agent pod получает env только для variables, привязанных к этой role.

Kubernetes access mode роли (`read-only` / `cluster-admin`) относится к MatterCodex/agent runtime cluster, то есть к кластеру, где запущены Mattermost, bot-service и agent pods. Этот доступ агент использует только если это прямо сказано в prompt, `AGENTS.md` или связанных инструкциях репозитория. Для других кластеров, например `radar-auto`, owner должен создать отдельную project runtime variable с kubeconfig/token/endpoint и явно выдать ее нужной role.

## Advanced Settings

Расширенные Codex/runtime/provider settings сохраняются, но убираются из first-run UX:

- global defaults;
- project advanced settings;
- role `config.toml` overlay и advanced JSON;
- chat settings/overrides.

`sandbox_mode = "danger-full-access"` остается допустимым default для MVP в изолированном pod и может быть переопределен через role config overlay.

## Product Boundary For Current PR

Текущий PR фиксирует project runtime env variables и role-level bindings. Это foundation для безопасной передачи агентам внешних cluster/database/API credentials без смешивания их с системным Kubernetes access MatterCodex runtime.

Не считается готовым доработкой, если новая owner-facing операция требует помнить внутренний id. Существующие typed commands остаются только fallback/debug path.
