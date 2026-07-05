# Owner UX Contract

Этот документ фиксирует целевой продуктовый контракт для Mattermost-интерфейса `matter-codex`. Он важнее текущих технических команд: новые кодовые PR должны приводить поведение к этому контракту, а не расширять набор ручных slash command.

## Главный принцип

Владелец работает через `/agents` как через меню управления. После входа в Mattermost owner открывает `/agents`, видит карточку с разделами и дальше нажимает кнопки, выбирает элементы из списков и заполняет только содержательные поля.

Owner не должен помнить и вручную вводить:

- repository id или `owner/name`, если repository уже подключен;
- GitHub organization/user проекта после настройки project;
- OpenAI account name, если account уже создан;
- GitHub account name, если account уже создан;
- agent role/profile name, если role/profile уже создана;
- prompt template key, если template уже есть в profile;
- run id, Kubernetes Job/PVC name;
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
- Подтверждение удаления или применения опасной операции выполняется через явную confirmation-карточку или confirmation-dialog с видимым описанием сущности. Для MVP допустимо короткое слово подтверждения вроде `delete` или `apply`, если пользователь не вводит технический id и видит, какая сущность будет затронута.
- Результат action возвращается в том же контексте: обновленная карточка, result-карточка или thread event. Пользователь не должен гадать, сработала кнопка или нет.
- Ошибка должна содержать ближайшее действие кнопкой: авторизовать account, проверить token, выбрать другой profile, открыть status, повторить cleanup.
- Все тексты owner-facing UI берутся из i18n и рендерятся в выбранной владельцем локали.
- Секреты, token values, device auth internals и kubeconfig никогда не показываются в карточках, логах, prompt или PR.

## Главное меню

Главное меню `/agents` должно давать быстрый доступ к разделам:

- projects;
- accounts;
- repositories;
- agent roles;
- chats;
- advanced tools;
- runtime/status.

Старые profiles и prompt templates доступны в `Advanced`. Flow wizard и pending decisions не являются частью owner-facing UX.

Главная карточка показывает безопасную сводку:

- storage ready/not ready;
- runtime configured/not configured;
- OpenAI accounts `authorized/total`;
- GitHub accounts `configured/total`;
- количество projects;
- текущую локаль.

## Projects And Chats

Project является первичным контейнером конфигурации и соответствует отдельной Mattermost team.

Целевой сценарий:

1. `Projects -> Create project`.
2. Ввести человекочитаемое имя, optional slug, выбрать platform GitHub account и указать GitHub organization/user проекта.
3. Система создает или привязывает Mattermost team.
4. Открыть project dashboard.
5. Из dashboard добавить repositories, roles, accounts и chats.

GitHub organization/user на project является namespace проекта. Например, если project работает над `radar-auto`, repositories ищутся и добавляются только из `radar-auto/*`. Project не требует выбирать один главный repository.

Chat соответствует private Mattermost channel внутри project team.

Целевой сценарий:

1. Открыть project dashboard.
2. Нажать `Create chat`.
3. Выбрать mode: manager, pm/delivery, worker + reviewer, single custom или multi-role custom.
4. Выбрать roles из списка.
5. Optional: выбрать repositories из project repository list как allowlist для этого chat.
6. Optional: указать GitHub issue/epic и work policy.
7. Система создает private channel и сохраняет role/repository bindings.

Owner не вводит Mattermost team/channel id. Они создаются и передаются через action context/storage.

## Accounts

### OpenAI accounts

Owner может иметь несколько OpenAI accounts. Целевой сценарий:

1. Нажать `Accounts -> OpenAI -> Add account`.
2. Указать только label или принять предложенный label.
3. Получить карточку device-code authorization с ссылкой, кодом и кнопкой `Refresh status`.
4. После авторизации увидеть account card со статусом `authorized`.
5. Использовать account в agent roles через выбор из списка.
6. Удалить account через карточку account, если он не используется roles.

UI не должен требовать вручную повторять account name для status, cleanup или delete.

### GitHub accounts

Owner может добавить несколько GitHub accounts с разными scopes. Account можно назначить project как platform GitHub account, а также назначать отдельным agent roles, если role должна работать от другого GitHub identity. Целевой сценарий:

1. Нажать `Accounts -> GitHub -> Add account`.
2. Вставить token в secure dialog.
3. Система проверяет token через GitHub API и сохраняет Kubernetes Secret.
4. Система показывает username, email, safe scopes/status и account card.
5. Owner выбирает account в project как platform account или в role как explicit override.
6. Удаление GitHub account удаляет metadata и, если Secret создан системой, предлагает отдельное подтверждение удаления Secret.

Secret name не должен быть обязательным полем для owner. Если нужен bring-your-own Secret, это отдельный advanced path.

## Repositories

Repository onboarding в основном сценарии начинается из project dashboard и использует platform GitHub account выбранного project. После этого система показывает repositories только из GitHub organization/user, указанного в project, или дает поиск по GitHub API внутри этого namespace.

Целевой сценарий:

1. `Projects -> Open project -> Add repository`.
2. Если у project не выбран platform GitHub account или GitHub owner, нажать `Edit project` и заполнить их.
3. Выбрать repository из списка project owner или найти по строке поиска.
4. Выбрать default branch из GitHub API или принять default.
5. Подтвердить создание Mattermost channel bindings и GitHub webhook.
6. Получить repository card со статусами: access, webhook, Mattermost channel, default branch; repository автоматически привязан к project.

Глобальный `Repositories -> Add repository` остается advanced/fallback path и может явно выбрать GitHub account вручную.

Для уже подключенного repository действия выполняются с его карточки:

- check access;
- ensure webhook;
- open channel;
- edit settings;
- disable/delete metadata.

Owner не должен вводить `codex-k8s/matter-codex` повторно после onboarding.

## Thread Repository Choice

Repository для фактического checkout выбирается на уровне Mattermost thread, а не жестко на уровне всего project/chat.

Целевой сценарий:

1. Owner пишет первое сообщение в chat или thread.
2. Если у chat/project есть repositories, bot-service публикует приватную thread-card с кнопками `No repository` и доступными repositories.
3. Owner выбирает repository или `No repository`.
4. Выбор сохраняется в hidden `ThreadContext`, исходное сообщение ставится в обработку, а дальше owner просто продолжает thread.
5. Если выбран repository, agent pod делает checkout перед запуском Codex.
6. Если выбран `No repository`, agent pod стартует в PVC без checkout; это валидно для manager/PM/analyst/ad-hoc задач.

Owner не вводит repository id или `owner/name` при выборе repository для thread.

## Agent Roles

Agent role связывает project, accounts, optional prompt template, Codex config overlay и runtime permissions.

Целевые role actions:

- создать role: manager, pm/delivery, worker, reviewer, analyst, architect, writer, sre, lexical guard или custom;
- выбрать OpenAI account из списка;
- выбрать GitHub account из списка;
- выбрать Kubernetes access mode;
- выбрать sandbox/config overlay;
- оставить prompt template пустым для raw chat instruction mode;
- открыть связанные advanced settings.

Если prompt template пустой, это валидный режим: текст пользователя из chat/thread становится основной инструкцией агента.

Kubernetes access modes MVP:

- `read-only` - default для обычных developer/reviewer roles, агент может читать logs/status при наличии выданных runtime credentials;
- `cluster-admin` - осознанный owner-selected риск для deploy/ops/service roles, которым нужно менять ресурсы в любых namespaces и чинить сам MatterCodex через MatterCodex.

Runtime behavior: `cluster-admin` role запускается с отдельным Kubernetes service account, привязанным к встроенному `cluster-admin` ClusterRole. `read-only` role остается на обычном namespace-scoped read-only service account.

Будущий upgrade path: заменить `cluster-admin` на заранее подготовленные role policies per project/namespace без изменения owner-facing role model.

NetworkPolicy по умолчанию не включается: это осознанное MVP-решение владельца, потому что агентам может потребоваться ходить во внешние сервисы проекта.

## Advanced Tools

Legacy agent profiles, prompt template editor, runtime diagnostics и system/status остаются в `Advanced` до полной миграции на project roles. Они не должны появляться как первый экран или основной happy path.

- Advanced включает:

- legacy profiles;
- prompt templates;
- accounts;
- runtime/runs;
- system/status.

## Legacy Prompt Templates

Prompt templates хранятся в БД и редактируются через Mattermost. Целевой сценарий:

1. Выбрать profile.
2. Выбрать template из списка.
3. Открыть карточку template: description, last updated, available placeholders/functions.
4. Нажать `Edit`.
5. Отправить Markdown.
6. Система делает test render на безопасном sample context.
7. Если render успешен, owner нажимает `Save`; если нет, видит ошибку и может исправить Markdown.

Язык prompt должен рендериться с учетом выбранной локали пользователя. UI-подсказки и справка по placeholders также идут через i18n.

## Runtime And Cleanup

Runtime раздел показывает active, held и completed runs. Cleanup должен уважать рабочий процесс, где задача может ждать решения владельца несколько рабочих дней.

Целевые правила:

- active jobs не удаляются автоматическим cleanup;
- активные jobs и live thread sessions не удаляются retention cleanup без owner action или TTL;
- dry-run показывает, какие ресурсы были бы затронуты и почему часть ресурсов пропущена;
- apply требует явного подтверждения через UI;
- cleanup конкретного run запускается из его карточки.

## Acceptance Rule For New PR

Каждый следующий кодовый PR должен отвечать на вопросы:

1. Как owner проверяет это через `/agents` без знания внутренних id?
2. Какие кнопки и dialogs добавлены?
3. Какие typed commands остались только fallback/debug?
4. Какие owner-facing сообщения покрыты i18n?
5. Как вручную проверить success, validation error и failure path?

Если PR добавляет функциональность, доступную только через typed slash command с ручным вводом id, это считается незавершенным UX для product path.
