# Wave 5.2 — макеты и следующие шаги

Статус: `complete`

## Что сгенерировано и проверено
- `03-flow-editor/screen.png` — редактор flow с публикацией версии в БД и ссылкой на установочную fixture-конфигурацию.
- `04-role-catalog/screen.png` и вкладки `Шаблоны`, `Инструменты (MCP)`, `Аккаунты`, `Использование`, `История версий`.
- `08-projects-and-repositories/screen.png` и вкладки `Репозитории`, `Документация`, `Рабочая область`, `Релизная политика`.
- `09-integrations-and-accounts/tab-accounts.png`, `tab-limits.png`, `tab-history.png`.
- `12-package-catalog/screen.png`, `tab-documentation.png`, `tab-versions.png`, `tab-permissions-secrets.png`, `tab-installation.png`.
- `13-organizations-and-groups/screen.png` и вкладки `Организации`, `Группы`, `Модель доступа`, `Наследование`, `Аудит`.
- `14-fleet-servers-clusters/screen.png` и вкладки `Серверы`, `Кластеры`, `Размещение`, `Здоровье и ёмкость`, `История`.
- `15-billing-and-costs/screen.png` и вкладки `Расходы`, `Счета`, `Провайдеры и платежи`, `Выручка каталога`.
- `16-release-policy-automation/screen.png` и вкладки `Релизные линии`, `Правила веток`, `Расписания и триггеры`, `Risk gates`.

## Что было догенерировано в завершающем проходе
- недостающая вкладка `12-package-catalog/tab-installation.png`;
- новый экран `13-organizations-and-groups` и все его вкладки;
- новый экран `14-fleet-servers-clusters` и все его вкладки;
- новый экран `15-billing-and-costs` и все его вкладки;
- новый экран `16-release-policy-automation` и все его вкладки.

## Финальная проверка перед PR wave 5.2
- Все новые PNG просмотрены после генерации.
- Вкладки являются отдельными блоками содержимого, а не повтором всего экрана.
- Секреты и токены нигде не показаны значениями.
- `index.md` и `screen.md` новых папок обновлены.

## Следующие волны после wave 5.2
- Волна 6: runtime, deploy и bootstrap — серверы, кластеры, slots, platform jobs, сборка, выкладка, cleanup и первичный production-контур.
- Backlog checkpoint после wave 6: сверить старые GitHub Issues с новой каноникой, закрыть поглощённые и переписать те, что остаются актуальными.
- Волна 7+: реализация по доменам небольшими PR, начиная с foundation-слоя и контрактов, затем сервисные домены, затем frontend-срезы поверх утверждённых экранных моделей.
