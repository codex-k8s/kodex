# Product Vision

## Назначение

`matter-codex` автоматизирует текущую ручную схему агентной разработки через Mattermost:

1. человек пишет задачу в Mattermost;
2. система создает run в thread;
3. developer agent выполняет работу в Kubernetes pod;
4. GitHub PR становится главным кодовым артефактом;
5. reviewer agent проверяет PR;
6. при request changes запускается fix-loop;
7. владелец принимает финальное решение.

Система сохраняет текущую рабочую модель: короткие PR, независимый review, GitHub как источник правды и human owner как финальный gate.

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

Внутренняя модель `matter-codex` строится вокруг собственных сущностей: repository, project, Mattermost channel binding, credential, OpenAI account, agent profile, prompt template, flow, run, step, artifact и audit log.

## MVP-сценарий

Минимальный сценарий, который должен заработать первым:

1. Администратор поднимает Mattermost в Kubernetes.
2. Администратор поднимает `matter-codex` bot-service в том же кластере.
3. Система создает дефолтные каналы управления.
4. Администратор авторизует один или несколько OpenAI accounts через device-code flow.
5. Администратор добавляет GitHub repository и GitHub bot token.
6. Система создает каналы проекта или репозитория.
7. Администратор создает developer и reviewer profiles с выбранным OpenAI account и `config.toml` overlay.
8. Пользователь открывает `/agents -> Запуск flow`.
9. Пользователь выбирает repository, flow preset и profiles из UI.
10. Пользователь вводит текст задачи и подтверждает run plan.
11. Bot-service генерирует flow/run ids, branch name и создает Mattermost thread с карточкой run.
12. Developer agent в отдельном pod создает branch, commit и PR.
13. Reviewer agent проверяет PR.
14. Если есть request changes, developer agent исправляет PR до трех попыток.
15. Если есть approval, run переходит в `waiting_owner`.
16. Владелец принимает, отклоняет, повторяет review/fix или удерживает результат кнопкой в Mattermost.

## Критерии успеха

- Один реальный репозиторий проходит путь от Mattermost-задачи до PR и reviewer decision.
- Каждый run имеет thread, status, ссылки на PR и audit trail.
- Секреты не выводятся в Mattermost, логи и prompt.
- Agent pod получает собственный workspace и PVC.
- Разные agent sessions могут использовать разные OpenAI accounts.
- Agent profile управляет Codex config overlay, включая MCP-настройки вроде Context7.
- Repository/project onboarding создает нужные Mattermost-каналы.
- Все основные операции owner path доступны из `/agents` без ручного ввода технических id.
- Все PR в разработке самой системы остаются ручно тестируемыми после deploy.
