# Новый запуск

## Источники

- `docs/design/mockups/10_new_run_desktop.dc.html`.
- `docs/design/mockups/10_new_run_mobile.dc.html`.
- `services/staff/control-center/src/pages/NewRunPage.vue`.
- `docs/domains/runtime-orchestration.md`.
- `docs/architecture/runtime-and-sessions.md`.

## Результат

Создай:

- `new-run-desktop.html`;
- `new-run-file-picker-desktop.html`;
- `new-run-session-picker-desktop.html`;
- `new-run-mobile.html`.

Используй одностраничный конструктор со sticky summary. Если пользователь вошёл
из Проекта, Проект уже выбран и не теряется. Глобальный запуск сначала требует
выбрать Проект.

Основные поля: цель запуска, необязательное пользовательское название, задача,
входные файлы и продолжение работы. Если название не введено, интерфейс ясно
объясняет, что Kodex создаст понятное имя автоматически и пользователь сможет
его изменить.

Файлы выбираются в большом modal picker:

- переключатель list/grid;
- MIME-иконки, имя, размер, версия, статус проверки и время;
- серверный поиск и фильтры;
- cursor infinite scroll с loading footer;
- selected tray и счётчик выбранных файлов;
- preview без скачивания потенциально опасного содержимого;
- duplicate names различаются контекстом, а не внутренним UUID.

Продолжение сессии выбирается отдельным async picker с названием последней
работы, сотрудником, временем, состоянием и кратким контекстом. Длинный текст не
растягивает dropdown и занимает максимум две строки.

Кнопки «Запустить» и «Отмена» имеют одинаковую высоту. Radio для новой и
существующей сессии выглядит как полноценный стабильный control. На mobile
summary становится нижней секцией, а primary action остаётся доступной.
