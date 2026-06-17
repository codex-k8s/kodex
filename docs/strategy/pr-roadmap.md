# PR Roadmap

Этот roadmap заменяет старую разбивку после merge PR #20 и после перехода на single-user model фиксирует новый основной путь: Project = Mattermost team, Chat = private channel, AgentRole = агентная роль в project.

Система уже имеет bootstrap, bot-service, storage, GitHub adapter, Kubernetes runner, Codex developer/reviewer runs, legacy developer-review flow, owner actions, retention cleanup, OAuth gate, i18n и первые `/agents` menu/dialog срезы.

Дальше цель не наращивать typed slash commands и не развивать flow-builder, а довести продукт до owner-first Mattermost UX: владелец открывает `/agents`, создает project, accounts, repositories, roles и chats кнопками/формами, вводит только содержательные данные и не помнит технические identifiers.

## Current Code PR: Project/Role/Chat Foundation

Цель: убрать flow из главного UX и ввести новую доменную основу.

Содержимое:

- таблицы `projects`, `project_repositories`, `agent_roles`, `chats`, `chat_participants`, `chat_repositories`;
- Mattermost team creation для project;
- private Mattermost channel creation для chat;
- `/agents` main menu: Projects, Accounts, Repositories, Roles, Chats, Advanced;
- project dashboard с быстрыми действиями;
- role editor с optional prompt template, GitHub/OpenAI bindings, Kubernetes access, sandbox и Codex config overlay;
- chat creator с выбором project, roles, repository, issue и work policy;
- prompt builder, где пустой template означает raw chat instruction mode;
- legacy flow/profiles/prompts перенесены в Advanced.

Ручная проверка:

- открыть `/agents` и увидеть Projects первым путем;
- создать project и проверить Mattermost team;
- создать role без prompt template и увидеть raw mode;
- создать chat с worker + reviewer и проверить private channel;
- убедиться, что flow находится в Advanced, а не в главном happy path.

## Next Code PR: Chat-triggered Agent Sessions

Цель: сообщения в project chat запускают role-bound Codex sessions.

Содержимое:

- обработчик Mattermost post/thread events или polling fallback;
- binding thread -> role -> Codex session;
- prompt context builder: project, chat, selected repos, role settings, user message;
- финальный ответ агента в thread исходного сообщения;
- базовая диагностика session status из chat card;
- запрет падения при пустом prompt template.

Ручная проверка:

- написать сообщение в chat с role без prompt template;
- увидеть запуск agent pod;
- получить финальный ответ в thread;
- проверить, что выбранные GitHub/OpenAI accounts и config overlay попали в runtime без вывода секретов.

## Docs PR: Product/UX Contract

Содержимое:

- `owner-ux-contract.md`;
- `acceptance-matrix.md`;
- обновление product vision, architecture и roadmap;
- фиксация правила: typed commands - fallback/debug, основной path - Mattermost cards/buttons/dialogs.

Ручная проверка:

- прочитать документы;
- подтвердить, что будущие PR имеют проверяемые owner-facing критерии;
- подтвердить, что no NetworkPolicy by default, single-server MVP и configurable `danger-full-access` остаются осознанными решениями.

## Code PR 1: `/agents` UI Framework

Цель: убрать повторяющиеся ad hoc карточки и дать основу для entity-first UX.

Содержимое:

- общий слой entity cards/list pages;
- pagination/filter actions для длинных списков;
- hidden action state для repository/account/profile/template/run/flow ids;
- единый confirmation flow без ввода технических ids;
- result/error cards с next actions;
- тесты, запрещающие `/agents <command>` в owner-facing карточках основного меню.

Ручная проверка:

- открыть `/agents`;
- пройти по всем разделам туда-обратно кнопками;
- увидеть списки сущностей как карточки/actions;
- выполнить безопасную action и получить result-карточку;
- проверить validation error и failure path без просмотра pod logs.

## Code PR 2: Accounts UX

Цель: OpenAI и GitHub accounts полностью управляются из UI.

Содержимое:

- OpenAI account cards;
- `Add OpenAI account` с device-code карточкой;
- refresh/status/cleanup/delete без ручного account name;
- GitHub account add через token dialog;
- GitHub token introspection: username, email, safe status/scopes;
- автоматическое создание Kubernetes Secret для GitHub account;
- блокировка удаления account, если он используется profile;
- multiple accounts summary `ready/total`.

Ручная проверка:

- создать OpenAI account, авторизовать device-code и обновить status кнопкой;
- создать второй OpenAI account и увидеть `authorized/total`;
- добавить GitHub account через token dialog;
- увидеть username/email/status без вывода token;
- удалить неиспользуемый account из его карточки;
- получить понятный blocker при удалении account, связанного с profile.

## Code PR 3: Repository/Project Onboarding

Цель: owner подключает repository без ручного `owner/name` после выбора GitHub account.

Содержимое:

- выбор GitHub account перед onboarding;
- список/search GitHub organizations/repositories через GitHub API;
- выбор default branch из GitHub API;
- repository card;
- check access / ensure webhook / open channel / edit / disable-delete actions;
- автоматическое создание Mattermost channel binding;
- webhook registration при onboarding;
- понятные error cards для permission/webhook ошибок.

Ручная проверка:

- выбрать GitHub account;
- найти и подключить repository;
- убедиться, что channel и webhook созданы;
- выполнить check/webhook actions с repository card;
- удалить/disable metadata через confirmation UI.

## Combined Code PR: Owner Flow MVP

Цель: одним PR закрыть оставшиеся owner-facing MVP-срезы так, чтобы владелец управлял profiles, prompts, flow, pending decisions и runtime cleanup из `/agents`, без ручного ввода flow/run/profile/template ids для уже созданных сущностей.

Содержимое:

- profile list/cards;
- create/edit profile из preset: developer, reviewer, technical reviewer, lexical reviewer, deployer;
- выбор OpenAI/GitHub accounts из списков;
- Kubernetes access mode MVP: read-only или cluster-admin;
- sandbox/config overlay editor или безопасный config dialog;
- prompt template list per profile;
- prompt edit через Markdown submission;
- placeholders/functions help из i18n;
- test render before save.
- wizard: repository -> flow preset -> profiles/accounts -> task -> confirm;
- system-generated flow id, run ids и branch names;
- flow card как главный статусный объект;
- pending decisions list;
- approve/reject/rerun/stop/hold actions;
- blocked escalation после лимита попыток;
- links на PR, logs/status и cleanup.
- runtime lists: active, held, completed;
- run/flow status cards с log tail;
- cleanup конкретного run/flow из карточки;
- retention dry-run с skipped reasons;
- apply cleanup через confirmation UI;
- hold/unhold flow;
- правила: active jobs и waiting/held flows не удаляются без явного owner action.
- полный dogfooding run на `matter-codex` или выбранном repository;
- проверка developer -> PR -> reviewer -> fix-loop -> owner gate;
- финальный runbook без обязательных typed commands в product path;
- smoke/deploy evidence;
- список оставшихся production gaps.

Ручная проверка:

- создать reviewer-like profile из preset;
- выбрать accounts из UI;
- выбрать Kubernetes access mode;
- открыть template, изменить Markdown, увидеть test render и сохранить;
- проверить, что prompt language зависит от выбранной локали;
- запустить developer-review flow, введя только текст задачи;
- увидеть generated ids только как read-only metadata;
- получить PR link в flow card;
- проверить pending decisions;
- нажать approve/reject/rerun/stop/hold на тестовом flow;
- открыть runtime menu;
- выбрать run из списка и посмотреть status/log tail;
- выполнить dry-run retention;
- подтвердить cleanup конкретного завершенного run;
- убедиться, что waiting/held flow пропускается cleanup без явного owner action;
- пройти end-to-end task в Mattermost;
- получить PR и reviewer decision;
- проверить request changes -> fix attempt;
- принять owner decision;
- выполнить безопасный cleanup.

## Оставшиеся production gaps после MVP

- Fine-grained Kubernetes role policies вместо MVP `cluster-admin` для deploy/ops profiles.
- Более богатая пагинация/поиск для больших списков profiles, flows и runs.
- Полная история audit trail и log tail прямо в карточках без fallback typed commands.
- GitHub App вместо PAT/account token.
- HA Mattermost/PostgreSQL и managed storage вместо single-server setup.

## Неизменные MVP-решения

- Mattermost, bot-service, agent pods и PVC остаются в одном runtime namespace.
- Single-server Mattermost/PostgreSQL остается осознанным MVP-риском.
- NetworkPolicy по умолчанию не включается.
- `sandbox_mode = "danger-full-access"` допустим как default MVP policy в изолированном pod и может быть переопределен через Codex `config.toml` overlay.
- GitHub PAT/account token остается MVP-механизмом; GitHub App остается upgrade path.
- Observability остается простой: Mattermost status, structured logs, Kubernetes status/log tail, audit events без Prometheus/Grafana.
