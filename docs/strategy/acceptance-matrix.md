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
- confirmation flow не требует ввода технических id; короткое подтверждение вроде `delete` допустимо только внутри dialog с видимым описанием сущности.

## Accounts UX PR

Проверка:

- OpenAI account создается из меню;
- device-code карточка дает ссылку, код и refresh/status кнопки;
- status/delete/cleanup работают с карточки account без ввода account name;
- несколько OpenAI accounts отображаются как `authorized/total`;
- GitHub account добавляется через token dialog;
- username/email/status подтягиваются из GitHub API;
- GitHub account назначается на project как platform account и может переопределяться на agent role;
- project хранит GitHub organization/user namespace отдельно от конкретных repositories;
- удаление account блокируется, если profile зависит от него.

## Repository Onboarding PR

Проверка:

- owner открывает project dashboard, где выбран platform GitHub account;
- repository onboarding использует platform GitHub account и GitHub owner проекта;
- owner выбирает repository из списка или через поиск;
- список и поиск repository ограничены GitHub owner проекта;
- default branch выбирается из GitHub API или заполняется системой;
- webhook создается при onboarding;
- Mattermost channel binding создается автоматически;
- repository card дает check/webhook/edit/delete actions без ввода `owner/name`;
- ошибка доступа к repository возвращается как карточка с next action.

## Project/Role/Chat Foundation PR

Проверка:

- project создается из `/agents -> Projects`;
- Mattermost team создается или привязывается автоматически;
- project form позволяет задать GitHub owner/org без выбора одного главного repository;
- repository можно привязать к project из UI;
- agent role создается из project dashboard;
- OpenAI/GitHub accounts выбираются из списков;
- Kubernetes access mode выбирается явно: `read-only` или `cluster-admin`;
- Codex sandbox/config overlay сохраняется в role;
- prompt template можно оставить пустым, и role переходит в raw chat instruction mode;
- chat создается как private Mattermost channel внутри project team;
- worker + reviewer или single custom role выбираются из списка;
- flow не является первым экраном и доступен только через Advanced.

## Chat-triggered Agent Sessions PR

Проверка:

- owner пишет сообщение в project chat/thread;
- role выбирается из chat participants или явной кнопкой;
- если prompt template пустой, сообщение owner становится основной инструкцией;
- prompt context содержит project, chat, selected repositories, role settings и task text;
- если у project/chat есть repositories, первый turn в thread предлагает выбрать repository или `No repository`;
- `No repository` запускает agent session без checkout и не считается ошибкой;
- выбранный repository сохраняется на thread и попадает в agent pod как checkout context;
- запускается agent pod с выбранными GitHub/OpenAI accounts;
- финальный ответ агента появляется в thread исходного сообщения;
- GitHub issue/PR links возвращаются в thread/card.

## Legacy Flow Wizard PR

Проверка:

- legacy flow запускается из `/agents -> Advanced -> Запуск flow`;
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

## Combined Owner MVP PR

Проверка:

- пройти checks для Project/Role/Chat Foundation, Chat-triggered Agent Sessions, Runtime And Cleanup и E2E Dogfooding;
- убедиться, что основные карточки `/agents` не показывают `/agents <command>` как инструкцию владельцу;
- убедиться, что fallback typed commands остались доступны только для debug/runbook;
- подтвердить deploy evidence: миграции применились, bot-service rolled out, smoke прошел.
