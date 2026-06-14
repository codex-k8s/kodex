# Owner UX Contract

Этот документ фиксирует целевой продуктовый контракт для Mattermost-интерфейса `matter-codex`. Он важнее текущих технических команд: новые кодовые PR должны приводить поведение к этому контракту, а не расширять набор ручных slash command.

## Главный принцип

Владелец работает через `/agents` как через меню управления. После входа в Mattermost owner открывает `/agents`, видит карточку с разделами и дальше нажимает кнопки, выбирает элементы из списков и заполняет только содержательные поля.

Owner не должен помнить и вручную вводить:

- repository id или `owner/name`, если repository уже подключен;
- OpenAI account name, если account уже создан;
- GitHub account name, если account уже создан;
- agent profile name, если profile уже создан;
- prompt template key, если template уже есть в profile;
- run id, flow id, Kubernetes Job/PVC name;
- Kubernetes Secret name, если Secret создан или известен системе;
- технические callback/action ids.

Допустимый ручной ввод:

- текст задачи для агента;
- Markdown prompt template;
- человекочитаемый label новой сущности, если owner хочет задать его сам;
- GitHub token при добавлении GitHub account;
- фильтр поиска по репозиториям или PR, если список слишком большой;
- явное значение настройки, которое по смыслу является настройкой, а не внутренним id.

## Правила Mattermost UI

- `/agents` без аргументов открывает главное меню.
- Основной интерфейс строится из карточек, кнопок, списков, message menus и interactive dialogs.
- Typed slash commands остаются debug/fallback API для разработчика и runbook, но не показываются как основной путь в owner-facing карточках.
- Если action работает с существующей сущностью, ее id передается в hidden callback state или action context.
- Если action требует выбора существующей сущности, UI сначала показывает список/карточки или dialog `select`, а не просит ввести имя.
- Если сущностей больше, чем удобно показать в одной карточке, используется пагинация, фильтр или отдельный список с кнопками действий на каждой строке.
- Подтверждение удаления выполняется через явную confirmation-карточку или confirmation-dialog с видимым описанием сущности. Ввод `delete` не является целевым UX.
- Результат action возвращается в том же контексте: обновленная карточка, result-карточка или thread event. Пользователь не должен гадать, сработала кнопка или нет.
- Ошибка должна содержать ближайшее действие кнопкой: авторизовать account, проверить token, выбрать другой profile, открыть status, повторить cleanup.
- Все тексты owner-facing UI берутся из i18n и рендерятся в выбранной владельцем локали.
- Секреты, token values, device auth internals и kubeconfig никогда не показываются в карточках, логах, prompt или PR.

## Главное меню

Главное меню `/agents` должно давать быстрый доступ к разделам:

- запуск flow;
- pending decisions;
- repositories/projects;
- accounts;
- agent profiles;
- prompt templates;
- runtime/runs;
- system/status.

Главная карточка показывает безопасную сводку:

- storage ready/not ready;
- runtime configured/not configured;
- OpenAI accounts `authorized/total`;
- GitHub accounts `configured/total`;
- количество waiting/blocked flows;
- текущую локаль.

## Accounts

### OpenAI accounts

Owner может иметь несколько OpenAI accounts. Целевой сценарий:

1. Нажать `Accounts -> OpenAI -> Add account`.
2. Указать только label или принять предложенный label.
3. Получить карточку device-code authorization с ссылкой, кодом и кнопкой `Refresh status`.
4. После авторизации увидеть account card со статусом `authorized`.
5. Использовать account в agent profiles через выбор из списка.
6. Удалить account через карточку account, если он не используется профилями.

UI не должен требовать вручную повторять account name для status, cleanup или delete.

### GitHub accounts

Owner может добавить несколько GitHub accounts с разными scopes и назначать их разным agent profiles. Целевой сценарий:

1. Нажать `Accounts -> GitHub -> Add account`.
2. Вставить token в secure dialog.
3. Система проверяет token через GitHub API и сохраняет Kubernetes Secret.
4. Система показывает username, email, safe scopes/status и account card.
5. Owner выбирает account в profile или repository onboarding из списка.
6. Удаление GitHub account удаляет metadata и, если Secret создан системой, предлагает отдельное подтверждение удаления Secret.

Secret name не должен быть обязательным полем для owner. Если нужен bring-your-own Secret, это отдельный advanced path.

## Repositories And Projects

Repository onboarding должен начинаться с выбора GitHub account. После выбора account система показывает доступные organization/repository варианты или дает поиск по GitHub API.

Целевой сценарий:

1. `Repositories -> Add repository`.
2. Выбрать GitHub account.
3. Выбрать organization/repository из списка или найти по строке поиска.
4. Выбрать default branch из GitHub API или принять default.
5. Подтвердить создание Mattermost channel bindings и GitHub webhook.
6. Получить repository card со статусами: access, webhook, Mattermost channel, default branch.

Для уже подключенного repository действия выполняются с его карточки:

- check access;
- ensure webhook;
- open channel;
- edit settings;
- disable/delete metadata.

Owner не должен вводить `codex-k8s/matter-codex` повторно после onboarding.

## Agent Profiles

Agent profile связывает роль, accounts, prompts, Codex config overlay и runtime permissions.

Целевые profile actions:

- создать profile из preset: developer, reviewer, technical reviewer, lexical reviewer, deployer;
- выбрать OpenAI account из списка;
- выбрать GitHub account из списка;
- выбрать Kubernetes access mode;
- выбрать sandbox/config overlay;
- включить/выключить profile;
- открыть связанные prompt templates.

Kubernetes access modes:

- `none` - агент не получает Kubernetes API access;
- `read-only` - агент может читать logs/status в разрешенных namespaces;
- `deployer` - агент получает явно выданные deploy-права в выбранных namespaces;
- `custom` - owner выбирает заранее подготовленную Role/ClusterRole binding policy.

NetworkPolicy по умолчанию не включается: это осознанное MVP-решение владельца, потому что агентам может потребоваться ходить во внешние сервисы проекта.

## Prompt Templates

Prompt templates хранятся в БД и редактируются через Mattermost. Целевой сценарий:

1. Выбрать profile.
2. Выбрать template из списка.
3. Открыть карточку template: description, last updated, available placeholders/functions.
4. Нажать `Edit`.
5. Отправить Markdown.
6. Система делает test render на безопасном sample context.
7. Если render успешен, owner нажимает `Save`; если нет, видит ошибку и может исправить Markdown.

Язык prompt должен рендериться с учетом выбранной локали пользователя. UI-подсказки и справка по placeholders также идут через i18n.

## Flow Wizard

Запуск agent flow должен быть wizard-ом:

1. Выбрать repository/project.
2. Выбрать flow preset: developer-review, dev + product review, dev + technical review, dev + lexical guard, deploy.
3. Выбрать или подтвердить profiles/accounts.
4. Ввести текст задачи.
5. Подтвердить run plan.

`flow-id`, branch name, run ids и Kubernetes resource names генерируются системой. Owner видит человекочитаемый title, repository, branch, PR link, statuses и decisions.

## Pending Decisions

Раздел pending decisions показывает waiting/blocked flows и действия владельца:

- approve result;
- reject result;
- request another fix/review;
- stop flow;
- hold flow;
- cleanup resources;
- open PR;
- open run logs/status.

Каждая кнопка работает с конкретным flow из hidden state. Owner не вводит `flow-id`.

## Runtime And Cleanup

Runtime раздел показывает active, held и completed runs. Cleanup должен уважать рабочий процесс, где задача может ждать решения владельца несколько рабочих дней.

Целевые правила:

- active jobs не удаляются автоматическим cleanup;
- held/waiting flows не удаляются retention cleanup без owner action;
- dry-run показывает, какие ресурсы были бы затронуты и почему часть ресурсов пропущена;
- apply требует явного подтверждения через UI;
- cleanup конкретного run/flow запускается из его карточки.

## Acceptance Rule For New PR

Каждый следующий кодовый PR должен отвечать на вопросы:

1. Как owner проверяет это через `/agents` без знания внутренних id?
2. Какие кнопки и dialogs добавлены?
3. Какие typed commands остались только fallback/debug?
4. Какие owner-facing сообщения покрыты i18n?
5. Как вручную проверить success, validation error и failure path?

Если PR добавляет функциональность, доступную только через typed slash command с ручным вводом id, это считается незавершенным UX для product path.
