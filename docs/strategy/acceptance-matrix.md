# Acceptance Matrix

Этот документ задает проверяемую матрицу готовности после пересборки roadmap. Он нужен, чтобы следующий PR не выглядел готовым только потому, что команда работает в happy path.

## Общие критерии для каждого кодового PR

- Владелец может начать проверку с `/agents`.
- Основной путь проверяется кнопками, списками и dialog, а не ручным вводом технических id.
- Для каждой новой операции есть success path, validation error path и failure path.
- Результат действия виден в Mattermost без необходимости смотреть pod logs.
- Owner-facing тексты находятся в i18n.
- Секреты не выводятся в UI, logs, prompts, docs и PR.
- PR description содержит ручную проверку и список автоматических проверок.
- Если deploy нужен для проверки, bot-service раскатывается на кластер через SSH/deploy scripts.

## Product/UX Docs PR

Проверка:

- прочитать `owner-ux-contract.md`;
- убедиться, что roadmap больше не ориентируется на typed slash commands как основной UX;
- убедиться, что новые кодовые PR имеют owner-facing acceptance criteria;
- подтвердить, что single-server, no NetworkPolicy by default и `danger-full-access` как configurable MVP-риск отражены явно.

## UI Framework PR

Проверка:

- `/agents` открывает главное меню;
- каждое меню возвращается назад без ручного ввода;
- списки сущностей показывают cards/actions;
- карточки не показывают `/agents <command>` как основной путь;
- action context содержит hidden ids, а не просит owner ввести их;
- confirmation flow не требует ввода `delete`.

## Accounts UX PR

Проверка:

- OpenAI account создается из меню;
- device-code карточка дает ссылку, код и refresh/status кнопки;
- status/delete/cleanup работают с карточки account без ввода account name;
- несколько OpenAI accounts отображаются как `authorized/total`;
- GitHub account добавляется через token dialog;
- username/email/status подтягиваются из GitHub API;
- GitHub account выбирается в дальнейших сценариях из списка;
- удаление account блокируется, если profile зависит от него.

## Repository Onboarding PR

Проверка:

- owner выбирает GitHub account;
- owner выбирает repository из списка или через поиск;
- default branch выбирается из GitHub API или заполняется системой;
- webhook создается при onboarding;
- Mattermost channel binding создается автоматически;
- repository card дает check/webhook/edit/delete actions без ввода `owner/name`;
- ошибка доступа к repository возвращается как карточка с next action.

## Profiles And Prompts PR

Проверка:

- profile создается из preset;
- OpenAI/GitHub accounts выбираются из списков;
- Kubernetes access mode выбирается явно;
- prompt template выбирается из profile card;
- edit prompt принимает Markdown;
- test render показывает результат или ошибку до сохранения;
- prompt render учитывает локаль пользователя.

## Flow Wizard PR

Проверка:

- flow запускается из `/agents -> Запуск flow`;
- repository/profile/accounts выбираются из UI;
- owner вводит только текст задачи;
- system генерирует flow id, run id, branch name;
- flow card содержит PR/status/actions;
- pending decisions показывает waiting/blocked flows;
- approve/reject/rerun/stop работают кнопками.

## Runtime And Cleanup PR

Проверка:

- active/held/completed runs видны из runtime menu;
- status/log tail открывается с карточки run;
- cleanup конкретного run/flow запускается с его карточки;
- dry-run retention показывает skipped reasons;
- apply cleanup требует UI confirmation;
- held/waiting flows не удаляются без явного owner action.

## E2E Dogfooding PR

Проверка:

- реальный repository проходит путь task -> developer PR -> reviewer decision -> owner gate;
- request changes запускает fix attempt;
- лимит попыток переводит flow в blocked;
- PR и review оформляются через GitHub account агента;
- Mattermost thread остается источником статуса и решений;
- cleanup после завершения понятен и безопасен.
