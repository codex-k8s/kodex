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

## Что не является целью MVP

- Полный UI администрирования.
- Multi-tenant SaaS.
- Поддержка GitLab.
- Marketplace агентов.
- Billing.
- Visual flow editor.
- Auto-merge без явного решения владельца.
- Замена основной платформы `kodex`.

## Граница с kodex

`matter-codex` должен быть быстрым standalone-продуктом для dogfooding идеи. При этом модель не должна ломать будущую интеграцию с `kodex`:

- `matter-codex` использует те же понятия: repository, credential, agent profile, prompt template, flow, run, step, artifact, audit log.
- Доменные границы внутри сервиса повторяют будущие owner-контуры `kodex`.
- Provider-native артефакты остаются в GitHub.
- Runtime выполняется в Kubernetes.
- Flow, profiles и prompt templates хранятся как versioned records, а не как hardcoded prompt в коде.

## MVP-сценарий

Минимальный сценарий, который должен заработать первым:

1. Администратор поднимает Mattermost в Kubernetes.
2. Администратор поднимает `matter-codex` bot-service в том же кластере.
3. Администратор добавляет GitHub repository, GitHub bot token и OpenAI API key.
4. Администратор создает developer и reviewer profiles.
5. Пользователь запускает `/agents run developer-review-loop <repo> <task>`.
6. Bot-service создает Mattermost thread с карточкой run.
7. Developer agent в отдельном pod создает branch, commit и PR.
8. Reviewer agent проверяет PR.
9. Если есть request changes, developer agent исправляет PR до трех попыток.
10. Если есть approval, run переходит в `waiting_owner`.
11. Владелец принимает или отклоняет результат кнопкой в Mattermost.

## Критерии успеха

- Один реальный репозиторий проходит путь от Mattermost-задачи до PR и reviewer decision.
- Каждый run имеет thread, status, ссылки на PR и audit trail.
- Секреты не выводятся в Mattermost, логи и prompt.
- Agent pod получает собственный workspace и PVC.
- Все PR в разработке самой системы остаются ручно тестируемыми после deploy.
