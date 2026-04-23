# Пакет макетов wave 5

Статус: `in-progress`

Назначение пакета:
- визуально уточнять wave 5 до начала реализации frontend;
- держать рядом с каждым макетом краткую спецификацию экрана;
- не фиксировать pixel-perfect реализацию, но фиксировать состав блоков, сущности и действия.
- для экранов с вкладками хранить один полноэкранный основной макет и отдельные `tab-*` изображения как вырезки блока активной вкладки с её содержимым.

## Текущие экраны

1. `01-command-center`
   - статус: `approved`
   - экран: командный центр
   - спецификация: [screen.md](01-command-center/screen.md)
   - макеты:
     - [Основной экран](01-command-center/screen.png)

2. `02-issue-workspace`
   - статус: `approved`
   - экран: рабочее пространство `Issue`
   - спецификация: [screen.md](02-issue-workspace/screen.md)
   - макеты:
     - [Основной экран](02-issue-workspace/screen.png)
     - [Вкладка `Проверки`](02-issue-workspace/tab-checks.png)
     - [Вкладка `История переходов`](02-issue-workspace/tab-stage-history.png)
     - [Вкладка `Связи`](02-issue-workspace/tab-links.png)
     - [Вкладка `Журнал активности`](02-issue-workspace/tab-activity.png)

3. `03-flow-editor`
   - статус: `approved`
   - экран: редактор flow
   - спецификация: [screen.md](03-flow-editor/screen.md)
   - макеты:
     - [Основной экран](03-flow-editor/screen.png)

4. `04-role-catalog`
   - статус: `approved`
   - экран: каталог ролей и шаблонов
   - спецификация: [screen.md](04-role-catalog/screen.md)
   - макеты:
     - [Основной экран](04-role-catalog/screen.png)
     - [Вкладка `Шаблоны`](04-role-catalog/tab-templates.png)
     - [Вкладка `Инструменты (MCP)`](04-role-catalog/tab-mcp-tools.png)
     - [Вкладка `Аккаунты`](04-role-catalog/tab-accounts.png)
     - [Вкладка `Использование`](04-role-catalog/tab-usage.png)
     - [Вкладка `История версий`](04-role-catalog/tab-version-history.png)

5. `05-pr-workspace`
   - статус: `approved`
   - экран: рабочее пространство `PR/MR`
   - спецификация: [screen.md](05-pr-workspace/screen.md)
   - макеты:
     - [Основной экран](05-pr-workspace/screen.png)
     - [Вкладка `Acceptance`](05-pr-workspace/tab-acceptance.png)
     - [Вкладка `Связи`](05-pr-workspace/tab-links.png)
     - [Вкладка `Журнал активности`](05-pr-workspace/tab-activity.png)

6. `06-inbox-and-approvals`
   - статус: `approved`
   - экран: входящие, approvals и уведомления
   - спецификация: [screen.md](06-inbox-and-approvals/screen.md)
   - макеты:
     - [Основной экран](06-inbox-and-approvals/screen.png)

7. `07-executions-jobs-slots`
   - статус: `approved`
   - экран: исполнения, platform `job` и slot
   - спецификация: [screen.md](07-executions-jobs-slots/screen.md)
   - макеты:
     - [Основной экран](07-executions-jobs-slots/screen.png)

8. `08-projects-and-repositories`
   - статус: `approved`
   - экран: проекты и репозитории
   - спецификация: [screen.md](08-projects-and-repositories/screen.md)
   - макеты:
     - [Основной экран](08-projects-and-repositories/screen.png)
     - [Вкладка `Репозитории`](08-projects-and-repositories/tab-repositories.png)
     - [Вкладка `Документация`](08-projects-and-repositories/tab-documentation.png)
     - [Вкладка `Рабочая область`](08-projects-and-repositories/tab-workspace.png)
     - [Вкладка `Релизная политика`](08-projects-and-repositories/tab-release-policy.png)

9. `09-integrations-and-accounts`
   - статус: `approved`
   - экран: внешние аккаунты, интеграции и MCP
   - спецификация: [screen.md](09-integrations-and-accounts/screen.md)
   - макеты:
     - [Основной экран (`Права`)](09-integrations-and-accounts/screen.png)
     - [Вкладка `Аккаунты`](09-integrations-and-accounts/tab-accounts.png)
     - [Вкладка `Ограничения`](09-integrations-and-accounts/tab-limits.png)
     - [Вкладка `История`](09-integrations-and-accounts/tab-history.png)

10. `10-users-and-access`
   - статус: `approved`
   - экран: пользователи и доступы
   - спецификация: [screen.md](10-users-and-access/screen.md)
   - макеты:
     - [Основной экран (`Проекты`)](10-users-and-access/screen.png)
     - [Вкладка `Репозитории`](10-users-and-access/tab-repositories.png)
     - [Вкладка `Доступы`](10-users-and-access/tab-accesses.png)
     - [Вкладка `История`](10-users-and-access/tab-history.png)

11. `11-onboarding-and-empty-states`
   - статус: `approved`
   - экран: первый запуск и empty states
   - спецификация: [screen.md](11-onboarding-and-empty-states/screen.md)
   - макеты:
     - [Основной экран](11-onboarding-and-empty-states/screen.png)

12. `12-package-catalog`
   - статус: `partial`
   - экран: каталог плагинов и пакетов документации
   - спецификация: [screen.md](12-package-catalog/screen.md)
   - макеты:
     - [Основной экран](12-package-catalog/screen.png)
     - [Вкладка `Документация`](12-package-catalog/tab-documentation.png)
     - [Вкладка `Версии`](12-package-catalog/tab-versions.png)

## Незавершённая часть wave 5.2

Текущий пакет зафиксирован частично из-за сбоя генерации вкладки `Права и секреты` для каталога пакетов. Подробный список того, что нужно догенерировать и проверить, лежит в [wave5-2-backlog.md](wave5-2-backlog.md).
