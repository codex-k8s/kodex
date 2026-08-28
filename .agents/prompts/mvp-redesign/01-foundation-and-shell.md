# Foundation, AppShell и общие controls

## Источники

- `docs/design/mockups/Main.dc.html` и `docs/design/mockups/index.md`.
- `services/staff/control-center/src/app/AppShell.vue`.
- `services/staff/control-center/src/shared/ui/`.
- `services/staff/control-center/src/app/styles/base.css`.
- `docs/guides/frontend-vue.md`.

## Задание

Создай `docs/design/mvp-redesign/design-system.html` и используй его решения во
всех последующих макетах.

Оболочка desktop:

- стабильная левая навигация по глобальному и проектному контексту;
- верхняя строка с максимально растянутым глобальным поиском;
- справа от поиска: «Решения», realtime connection status и меню пользователя;
- выбранный Проект сохраняется при переходах к его сотруднику, Процессу,
  автоматизации или Run;
- активным остаётся тип открытой сущности, а клик возвращает к списку этого типа;
- FAB с иконкой бота закреплён справа снизу и не перекрывает содержимое.

Покажи общий набор production controls:

- `Menu/Popover`, закрывающийся по outside click и `Escape`;
- компактный `Select` для конечных enum;
- `Combobox` с фиксированной шириной и переносом текста максимум на две строки;
- `AsyncEntityPicker` и `AsyncMultiPicker` с серверным поиском, debounce,
  loading, empty, error, cursor pagination и infinite scroll;
- modal, desktop drawer и mobile bottom sheet с корректным focus trap;
- tabs, segmented list/grid control, buttons и icon-buttons с tooltip;
- status badges, progress, skeleton первого открытия и ненавязчивый background
  refresh без исчезновения текущего содержимого;
- textarea/code-editor shell, checkbox и radio-card без скачков размеров.

Все controls должны иметь стабильные размеры, keyboard navigation, видимый
focus, доступные названия и состояния disabled/read-only/error. Цвет не является
единственным носителем состояния. Не используй native `<details>` как меню.

Отдельно покажи поведение realtime: подтверждённые данные остаются на экране,
обновление обозначается небольшим индикатором; полный skeleton допустим только
при первом открытии. Переподключение WebSocket не выглядит как reload страницы.
