# Файлы, автоматизации и рабочие окружения

## Источники

- `docs/design/mockups/13_files_knowledge_desktop.dc.html`.
- `docs/design/mockups/14_automations_desktop.dc.html`.
- `docs/design/mockups/18_administration_desktop.dc.html`.
- `services/staff/control-center/src/pages/FilesPage.vue`.
- `services/staff/control-center/src/pages/AutomationsPage.vue`.
- `services/staff/control-center/src/pages/AdministrationPage.vue`.
- `docs/domains/artifacts-knowledge.md`.

## Результат

Создай:

- `files-list-desktop.html`;
- `files-grid-desktop.html`;
- `file-preview-desktop.html`;
- `automations-desktop.html`;
- `automation-editor-desktop.html`;
- `environments-desktop.html`;
- `environment-editor-desktop.html`;
- `files-automations-mobile.html`.

Файлы являются единым resource workspace: list/grid, MIME-иконки, серверный
поиск, фильтры, infinite scroll, безопасный preview, версии, provenance,
bindings и пакетное назначение сотрудникам. Покажи хранение как S3-compatible,
но не раскрывай bucket keys, credentials и внутренние object IDs.

«Знания» в текущем MVP означают только назначенные сотруднику проверенные
источники и их версии. Не рисуй chunks, embeddings, semantic search, reindex и
vector database: такой реализации пока нет.

Автоматизация имеет detail/edit UX, schedule, timezone, target, prompt,
input files, session policy, active revision и историю запусков. Редактирование
создаёт новую version для будущих запусков. Доступны pause, resume и archive.
Физическое удаление допускается только для никогда не запускавшегося draft.

Раздел Environments предоставляет CRUD/versioning recipes: назначение, base
image, packages, tools, installation block, build state, digest, совместимость
и связанные сотрудники. Покажи preview новой revision и понятный build status,
не показывая registry credentials.
