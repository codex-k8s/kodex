# PR Roadmap

Этот roadmap заменяет старую разбивку после merge PR #20. Система уже имеет bootstrap, bot-service, storage, GitHub adapter, Kubernetes runner, Codex developer/reviewer runs, developer-review flow, owner actions, retention cleanup, OAuth gate, i18n и первые `/agents` menu/dialog срезы.

Дальше цель не наращивать typed slash commands, а довести продукт до owner-first Mattermost UX: владелец открывает `/agents`, выбирает сущности кнопками и списками, вводит только содержательные данные и не помнит технические identifiers.

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
- единый confirmation flow без ввода `delete`;
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

## Code PR 4: Agent Profiles And Prompt Templates

Цель: owner собирает разные типы агентов и flow без ручного редактирования БД или знания template keys.

Содержимое:

- profile list/cards;
- create/edit profile из preset: developer, reviewer, technical reviewer, lexical reviewer, deployer;
- выбор OpenAI/GitHub accounts из списков;
- Kubernetes access mode: none, read-only, deployer, custom;
- sandbox/config overlay editor или безопасный config dialog;
- prompt template list per profile;
- prompt edit через Markdown submission;
- placeholders/functions help из i18n;
- test render before save.

Ручная проверка:

- создать reviewer-like profile из preset;
- выбрать accounts из UI;
- выбрать Kubernetes access mode;
- открыть template, изменить Markdown, увидеть test render и сохранить;
- проверить, что prompt language зависит от выбранной локали.

## Code PR 5: Flow Wizard And Pending Decisions

Цель: запуск и управление flow полностью из `/agents`.

Содержимое:

- wizard: repository -> flow preset -> profiles/accounts -> task -> confirm;
- system-generated flow id, run ids и branch names;
- flow card как главный статусный объект;
- pending decisions list;
- approve/reject/rerun/stop/hold actions;
- blocked escalation после лимита попыток;
- links на PR, logs/status и cleanup.

Ручная проверка:

- запустить developer-review flow, введя только текст задачи;
- увидеть generated ids только как read-only metadata;
- получить PR link в flow card;
- проверить pending decisions;
- нажать approve/reject/rerun/stop на тестовом flow.

## Code PR 6: Runtime Operations And Cleanup

Цель: owner управляет run resources без `run-id` и без риска удалить ожидающую задачу.

Содержимое:

- runtime lists: active, held, completed;
- run/flow status cards с log tail;
- cleanup конкретного run/flow из карточки;
- retention dry-run с skipped reasons;
- apply cleanup через confirmation UI;
- hold/unhold flow;
- правила: active jobs и waiting/held flows не удаляются без явного owner action.

Ручная проверка:

- открыть runtime menu;
- выбрать run из списка и посмотреть status/log tail;
- выполнить dry-run retention;
- подтвердить cleanup конкретного завершенного run;
- убедиться, что waiting/held flow пропускается cleanup.

## Code PR 7: E2E Dogfooding Polish

Цель: довести систему до полного ручного рабочего контура на реальном repository.

Содержимое:

- полный dogfooding run на `matter-codex` или выбранном repository;
- проверка developer -> PR -> reviewer -> fix-loop -> owner gate;
- финальный runbook без обязательных typed commands в product path;
- smoke/deploy evidence;
- список оставшихся production gaps.

Ручная проверка:

- пройти end-to-end task в Mattermost;
- получить PR и reviewer decision;
- проверить request changes -> fix attempt;
- принять owner decision;
- выполнить безопасный cleanup.

## Неизменные MVP-решения

- Mattermost, bot-service, agent pods и PVC остаются в одном runtime namespace.
- Single-server Mattermost/PostgreSQL остается осознанным MVP-риском.
- NetworkPolicy по умолчанию не включается.
- `sandbox_mode = "danger-full-access"` допустим как default MVP policy в изолированном pod и может быть переопределен через Codex `config.toml` overlay.
- GitHub PAT/account token остается MVP-механизмом; GitHub App остается upgrade path.
- Observability остается простой: Mattermost status, structured logs, Kubernetes status/log tail, audit events без Prometheus/Grafana.
