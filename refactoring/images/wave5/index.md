# Пакет макетов wave 5

Статус: `complete`

Назначение пакета:
- визуально уточнять wave 5 до начала реализации frontend;
- держать рядом с каждым макетом краткую спецификацию экрана;
- не фиксировать pixel-perfect реализацию, но фиксировать состав блоков, сущности и действия.
- для экранов с вкладками хранить один полноэкранный основной макет и отдельные `tab-*` изображения как вырезки блока активной вкладки с её содержимым.

Общий визуальный каркас пакета зафиксирован в [ui-style-guide.md](ui-style-guide.md).

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
   - статус: `approved`
   - экран: каталог плагинов и пакетов документации
   - спецификация: [screen.md](12-package-catalog/screen.md)
   - макеты:
     - [Основной экран](12-package-catalog/screen.png)
     - [Вкладка `Документация`](12-package-catalog/tab-documentation.png)
     - [Вкладка `Версии`](12-package-catalog/tab-versions.png)
     - [Вкладка `Права и секреты`](12-package-catalog/tab-permissions-secrets.png)
     - [Вкладка `Установка`](12-package-catalog/tab-installation.png)

13. `13-organizations-and-groups`
   - статус: `approved`
   - экран: организации и группы
   - спецификация: [screen.md](13-organizations-and-groups/screen.md)
   - макеты:
     - [Основной экран](13-organizations-and-groups/screen.png)
     - [Вкладка `Организации`](13-organizations-and-groups/tab-organizations.png)
     - [Вкладка `Группы`](13-organizations-and-groups/tab-groups.png)
     - [Вкладка `Модель доступа`](13-organizations-and-groups/tab-access-model.png)
     - [Вкладка `Наследование`](13-organizations-and-groups/tab-inheritance.png)
     - [Вкладка `Аудит`](13-organizations-and-groups/tab-audit.png)

14. `14-fleet-servers-clusters`
   - статус: `approved`
   - экран: серверы и кластеры
   - спецификация: [screen.md](14-fleet-servers-clusters/screen.md)
   - макеты:
     - [Основной экран](14-fleet-servers-clusters/screen.png)
     - [Вкладка `Серверы`](14-fleet-servers-clusters/tab-servers.png)
     - [Вкладка `Кластеры`](14-fleet-servers-clusters/tab-clusters.png)
     - [Вкладка `Размещение`](14-fleet-servers-clusters/tab-placement.png)
     - [Вкладка `Здоровье и ёмкость`](14-fleet-servers-clusters/tab-health-capacity.png)
     - [Вкладка `История`](14-fleet-servers-clusters/tab-history.png)

15. `15-billing-and-costs`
   - статус: `approved`
   - экран: биллинг и затраты
   - спецификация: [screen.md](15-billing-and-costs/screen.md)
   - макеты:
     - [Основной экран](15-billing-and-costs/screen.png)
     - [Вкладка `Расходы`](15-billing-and-costs/tab-expenses.png)
     - [Вкладка `Счета`](15-billing-and-costs/tab-invoices.png)
     - [Вкладка `Провайдеры и платежи`](15-billing-and-costs/tab-providers-payments.png)
     - [Вкладка `Выручка каталога`](15-billing-and-costs/tab-catalog-revenue.png)

16. `16-release-policy-automation`
   - статус: `approved`
   - экран: релизы и автоматизация
   - спецификация: [screen.md](16-release-policy-automation/screen.md)
   - макеты:
     - [Основной экран](16-release-policy-automation/screen.png)
     - [Вкладка `Релизные линии`](16-release-policy-automation/tab-release-lines.png)
     - [Вкладка `Правила веток`](16-release-policy-automation/tab-branch-rules.png)
     - [Вкладка `Расписания и триггеры`](16-release-policy-automation/tab-schedules-triggers.png)
     - [Вкладка `Risk gates`](16-release-policy-automation/tab-risk-gates.png)

## Wave 5.2

Wave 5.2 завершена по макетам: все полноэкранные экраны и отдельные вкладочные вырезки из [wave5-2-backlog.md](wave5-2-backlog.md) сгенерированы в 2K, просмотрены и описаны рядом с изображениями.
