# Контекстный Kodex и редактируемые планы

## Источники

- Старый ориентир: `docs/design/mockups/02_assistant_desktop.dc.html`.
- Текущая реализация: `services/staff/control-center/src/pages/AssistantPage.vue`.
- `docs/product/user-scenarios.md`.
- `docs/decisions/0005-integrations-and-approvals.md`.

## Важное решение

Не сохраняй старую полноэкранную страницу помощника и не проектируй legacy
fallback. Новый Kodex существует как контекстный drawer на desktop и bottom
sheet на mobile, вызываемый FAB на любом экране. История диалогов доступна
внутри этого нового интерфейса.

## Результат

Создай:

- `kodex-drawer-project-desktop.html`;
- `kodex-drawer-agent-desktop.html`;
- `kodex-plan-editor-desktop.html`;
- `kodex-plan-conflict-desktop.html`;
- `kodex-bottom-sheet-mobile.html`.

Header drawer показывает, над каким экраном и объектом работает Kodex, что он
может сделать здесь и от чьего имени будут записаны изменения. Context из UI
не является полномочием, поэтому тексты не должны обещать доступ без серверной
проверки.

Composer находится внутри drawer: textarea, круглая кнопка с иконкой отправки
внутри поля и disabled-кнопка микрофона с tooltip «Голосовой ввод появится
позже». Названия диалогов предлагает Kodex после первого содержательного
сообщения; пользователь может переименовать их.

План изменений обязан быть полным и редактируемым:

- create/update/delete показаны раздельно;
- у каждой операции видны target, before, after, зависимости и последствия;
- prompt/instructions и TOML открываются в отдельных code-editor modal;
- secret values никогда не показываются, только refs и masked metadata;
- пользователь может изменить поля, удалить или добавить допустимую операцию;
- после редактирования создаётся новая revision и выполняется validation;
- stale/conflict не применяется частично, а требует пересобрать план;
- применение атомарно и оставляет понятный audit receipt.

Покажи примеры контекста списка сотрудников, конкретного сотрудника и Run.
Набор предлагаемых действий должен различаться по экрану и полномочиям.
