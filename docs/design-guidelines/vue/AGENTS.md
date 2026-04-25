# Vue Design Guidelines

Документы для frontend (Vue 3 + TypeScript).

- `docs/design-guidelines/vue/check_list.md` — чек-лист перед PR для Vue изменений.
- `docs/design-guidelines/vue/frontend_architecture.md` — размещение, структура приложения, границы ответственности.
- `docs/design-guidelines/vue/frontend_data_and_state.md` — axios/Pinia/router/i18n/cookies/PWA/WebSocket правила.
- `docs/design-guidelines/vue/frontend_error_handling.md` — модель и обработка ошибок на фронте.
- `docs/design-guidelines/vue/frontend_code_rules.md` — правила кодирования (TS/Vue/импорты/комментарии).
- `docs/design-guidelines/vue/libraries.md` — что выносить в `libs/{vue,ts,js}` и как.
- `docs/design-guidelines/common/external_dependencies_catalog.md` — согласованный список внешних библиотек и инструментов.

Специфика `kodex`:
- целевой frontend приложения будет заново создан в каталоге, согласованном в новой архитектуре;
- вход в UI защищен GitHub OAuth;
- пользовательские настройки и права приходят из backend API (PostgreSQL как source of truth).
- Wave-планирование и doc-governance на время reset-итерации — по
  `refactoring/task.md`, `refactoring/02-doc-governance.md` и `refactoring/24-pre-wave7-documentation-rebuild-plan.md`.
