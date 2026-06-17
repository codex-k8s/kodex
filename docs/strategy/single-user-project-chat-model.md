# Single-user Project/Chat Model

Этот документ фиксирует новую целевую модель `matter-codex` после отказа от flow-centric UX. Инстанс считается личным инструментом владельца, поэтому продукт оптимизируется под короткий путь настройки, а не под multi-tenant role model.

## Принятая модель

- `Project` = отдельная Mattermost team.
- `Chat` = Mattermost private channel внутри project team.
- `AgentRole` = проектная роль агента, которую можно добавить в chat.
- `Repository` = GitHub repository, подключенный глобально и затем привязанный к project/chat.
- `GitHub account` и `OpenAI account` = переиспользуемые credentials, назначаемые на role.
- `Flow` больше не является центральной сущностью продукта. Legacy flow/actions остаются только в `Advanced` для совместимости и диагностики.

Главный путь владельца:

1. `/agents -> Projects -> Create project`.
2. Добавить или выбрать accounts.
3. Подключить repositories.
4. Создать agent roles.
5. Создать private chat channel.
6. Писать задачу в chat/thread.

## Project

Project хранит:

- имя, slug и описание;
- Mattermost team id;
- project-level advanced/runtime settings;
- default repository bindings;
- доступные roles/chats через связанные таблицы.

Создание project должно создавать или привязывать Mattermost team. Slug используется как Mattermost team name, поэтому он должен быть DNS-safe.

## AgentRole

Role хранит:

- project id;
- name и role type (`manager`, `pm_delivery`, `worker`, `reviewer`, `analyst`, `architect`, `writer`, `sre`, `lexical_guard`, `custom`);
- optional prompt template;
- prompt mode;
- optional GitHub/OpenAI account bindings;
- Kubernetes access mode;
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
- selected repositories;
- optional root GitHub issue/epic;
- optional work policy и settings.

По умолчанию создается private channel. При выборе repository в chat creator repository автоматически привязывается к project, если еще не был привязан.

## Accounts And Repositories

GitHub accounts остаются PAT/fine-grained PAT bindings для MVP. Системный owner account используется платформой для поиска/добавления repository и может также назначаться role.

OpenAI accounts остаются Codex device-code accounts. В role выбирается account binding, а runner получает только ссылку на Kubernetes Secret с auth material.

Repository сначала onboard-ится глобально через GitHub account, branch selection и webhook setup, затем привязывается к project и chat.

## Advanced Settings

Расширенные Codex/runtime/provider settings сохраняются, но убираются из first-run UX:

- global defaults;
- project advanced settings;
- role `config.toml` overlay и advanced JSON;
- chat settings/overrides.

`sandbox_mode = "danger-full-access"` остается допустимым default для MVP в изолированном pod и может быть переопределен через role config overlay.

## Product Boundary For Current PR

Текущий PR вводит доменную модель, storage, Mattermost team/channel creation, project/role/chat dialogs и no-template prompt builder. Это foundation для chat-triggered agent execution.

Не считается готовым доработкой, если новая owner-facing операция требует помнить внутренний id. Существующие typed commands остаются только fallback/debug path.
