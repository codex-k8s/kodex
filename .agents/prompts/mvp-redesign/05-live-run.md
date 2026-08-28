# Live Run Workspace

## Источники

- `docs/design/kodex-live-run.html`.
- `docs/design/mockups/12_live_run_desktop.dc.html`.
- `docs/design/mockups/12_live_run_mobile.dc.html`.
- `services/staff/control-center/src/pages/RunPage.vue`.
- `services/staff/control-center/src/features/runs/RunGraphCanvas.vue`.
- `docs/domains/runtime-orchestration.md`.

## Результат

Создай:

- `live-run-desktop.html`;
- `live-run-activity-drawer-desktop.html`;
- `live-run-tool-details-desktop.html`;
- `live-run-mobile.html`.

Сохрани понравившиеся владельцу фон canvas и характер узлов, но полностью
перестрой workspace:

- около половины рабочей ширины занимает интерактивный граф;
- pointer drag выполняет pan, wheel - zoom, есть fit и minimap;
- работающий узел имеет спокойную анимацию, учитывающую reduced motion;
- ещё не начавшиеся этапы Процесса показаны приглушённо;
- выбранный узел имеет компактный inspector с сотрудником, способом запуска,
  родителем, задачей, попыткой, временем и текущим безопасным статусом;
- дочерние и родительские Run открываются по понятным ссылкам.

«Ход работы» не занимает постоянную колонку. Он открывается в правом drawer:

- сообщения пользователя или вызывающего сотрудника;
- промежуточные и конечные сообщения сотрудника;
- отдельные блоки tool call, command, file change и plan update;
- started/running/completed/error, duration и безопасный результат;
- большие результаты представлены ссылкой на проверенный Artifact;
- raw reasoning, env, secrets, полный stdout и provider JSON не показываются.

Покажи нормальный active Run, Human Gate, ошибку одного узла, отменённый Run и
realtime reconnect без потери текущего viewport. На mobile граф и activity
переключаются tabs, inspector открывается как bottom sheet.
