# Product Vision

## Назначение

`matter-codex` автоматизирует личную агентную работу владельца через Mattermost:

1. владелец создает project, который соответствует Mattermost team;
2. владелец подключает accounts, GitHub owner проекта, repositories и agent roles;
3. владелец создает chat, который соответствует private Mattermost channel;
4. в chat добавляются один или несколько agents по ролям;
5. пользовательское сообщение в thread становится задачей агента, а repository для checkout выбирается опционально на уровне thread;
6. агент работает в Kubernetes pod, а GitHub остается источником issues, branches, PR, review и progress;
7. финальный ответ агент пишет в thread исходного сообщения.

Система сохраняет текущую рабочую модель: короткие PR, независимый review, GitHub как источник правды и human owner как финальный gate. Flow больше не является центральной сущностью продукта и остается в Advanced/deprecated path.

## Продуктовый UX-контракт

Основной интерфейс продукта - `/agents` в Mattermost. Owner открывает меню, выбирает разделы кнопками, выбирает существующие сущности из списков и вводит только содержательные данные: текст задачи, Markdown prompt, token при добавлении GitHub account или человекочитаемый label новой сущности.

Owner не должен помнить и вручную вводить внутренние идентификаторы: repository full name после onboarding, account name, profile name, template key, run id, flow id, Kubernetes Job/PVC/Secret name. Эти значения передаются через hidden callback state, action context или выбираются из UI.

Typed slash commands остаются fallback/debug интерфейсом для runbook и разработки. Они не считаются готовым product path, если та же операция не доступна через owner-facing карточку, кнопку или dialog.

Подробный контракт зафиксирован в `docs/strategy/owner-ux-contract.md`.

## Что не является целью MVP

- Отдельный полноценный web UI администрирования вне Mattermost.
- Multi-tenant SaaS.
- Поддержка GitLab.
- Marketplace агентов.
- Billing.
- Visual flow editor.
- Auto-merge без явного решения владельца.
- Замена основной платформы `kodex`.
- Встраивание в `kodex` как обязательная часть его runtime.

## Независимость от kodex

`matter-codex` - отдельный продукт для управления агентами через Mattermost. Он будет использоваться для разработки большой платформы `kodex`, но между проектами не должно быть жесткой связи:

- `matter-codex` не зависит от кода, БД, API, событий или deploy-контуров `kodex`.
- `kodex` не зависит от `matter-codex` для своей работы.
- `kodex` может быть первым подключенным repository/project для dogfooding.
- Та же система должна уметь подключить любой другой GitHub-репозиторий без специальных правил `kodex`.

Внутренняя модель `matter-codex` строится вокруг собственных сущностей: project, project repository binding, credential/account, agent role, chat, chat participant, chat repository binding, runtime settings, run/step/artifact и audit log.

## MVP-сценарий

Минимальный сценарий, который должен заработать первым:

1. Администратор поднимает Mattermost в Kubernetes.
2. Администратор поднимает `matter-codex` bot-service в том же кластере.
3. Система создает дефолтные каналы управления.
4. Владелец открывает `/agents -> Projects`.
5. Владелец создает project; система создает или привязывает Mattermost team.
6. Владелец добавляет GitHub и OpenAI accounts.
7. Владелец выбирает platform GitHub account и GitHub owner на project, затем подключает repositories из dashboard этого project.
8. Владелец создает roles: manager, pm/delivery, worker, reviewer, analyst, sre или custom.
9. Role получает GitHub account, OpenAI account, optional prompt template и advanced/Codex settings.
10. Владелец создает chat; система создает private Mattermost channel внутри project team.
11. В chat добавляются roles и optional selected repositories как allowlist для threads.
12. Владелец пишет задачу в channel/thread.
13. Если у project/chat есть repositories, owner выбирает repository для thread или `No repository`.
14. Если у role нет prompt template, агент использует текст сообщения как основную инструкцию.
15. Worker agent работает через GitHub issue/branch/PR, когда thread repository выбран.
16. Reviewer agent проверяет результат через PR/diff/comments.
17. PM/Delivery agent по запросу собирает status/weekly summary из GitHub issues/PR.

## Критерии успеха

- Owner может создать project/team, role и chat/channel из `/agents` без ручного ввода внутренних id.
- Project хранит GitHub organization/user namespace, а repository onboarding/search ограничены этим namespace.
- Thread может быть запущен с выбранным repository или без repository checkout.
- Один реальный репозиторий проходит путь от Mattermost chat task до PR и reviewer decision.
- Каждый agent session привязан к Mattermost thread, имеет status, ссылки на GitHub artifacts и audit trail.
- Секреты не выводятся в Mattermost, логи и prompt.
- Agent pod получает собственный workspace и PVC.
- Разные agent sessions могут использовать разные OpenAI accounts.
- Agent role управляет Codex config overlay, включая MCP-настройки вроде Context7.
- Project/chat onboarding создает нужные Mattermost team/channel bindings.
- Все основные операции owner path доступны из `/agents` без ручного ввода технических id.
- Все PR в разработке самой системы остаются ручно тестируемыми после deploy.
