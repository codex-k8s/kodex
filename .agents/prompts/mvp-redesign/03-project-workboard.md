# Обзор Проекта: три варианта

## Источники

- `docs/design/mockups/05_project_overview_desktop.dc.html`.
- `docs/design/mockups/05_project_overview_mobile.dc.html`.
- `services/staff/control-center/src/pages/ProjectOverviewPage.vue`.
- `docs/product/requirements.md`.

## Результат

Создай:

- `project-a-workboard-desktop.html`;
- `project-b-operational-dashboard-desktop.html`;
- `project-c-run-kanban-desktop.html`;
- `project-recommended-mobile.html`.

### A. Workboard, рекомендуемый

Секции «Требует внимания», «Выполняется», «Недавние результаты» и компактные
быстрые действия. Сотрудники, Процессы и автоматизации показываются как ресурсы
Проекта, а не как одинаковые KPI-карточки.

### B. Operational Dashboard

Две основные колонки: работа и решения. Показатели Проекта используются только
как компактный summary.

### C. Run Kanban

Запуски сгруппированы по состояниям «В очереди / Работает / Ждёт решения /
Завершён». Это вариант для проектов с большим числом параллельных работ.

Во всех вариантах:

- status badge закреплён справа и не сдвигает название;
- вместо неясного правого столбца используется «Источник запуска» с понятным
  значением «Control Center», «Kodex» или «Автоматизация», иконкой и tooltip;
- автор/инициатор и исполнитель показаны отдельно от источника;
- переход к любой сущности сохраняет project-scoped route и выбранный пункт
  навигации;
- быстрые действия не дублируются в нескольких местах.
