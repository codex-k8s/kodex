# Vue Design Guidelines

Документы для frontend (Vue 3 + TypeScript).

- `docs/design-guidelines/vue/check_list.md` — чек-лист перед PR для Vue изменений.
- `docs/design-guidelines/vue/frontend_architecture.md` — размещение, структура приложения, границы ответственности.
- `docs/design-guidelines/vue/frontend_data_and_state.md` — axios/Pinia/router/i18n/cookies/PWA/WebSocket правила.
- `docs/design-guidelines/vue/frontend_error_handling.md` — модель и обработка ошибок на фронте.
- `docs/design-guidelines/vue/frontend_code_rules.md` — правила кодирования (TS/Vue/импорты/комментарии).
- `docs/design-guidelines/vue/libraries.md` — что выносить в `libs/{vue,ts,js}` и как.
- `docs/design-guidelines/common/external_dependencies_catalog.md` — согласованный список внешних библиотек и инструментов.

Проектный overlay `kodex`:
- целевой frontend приложения будет заново создан в каталоге, согласованном в новой архитектуре;
- вход в UI защищён платформенным SSO/OIDC-контуром; базовый самостоятельно управляемый IdP — Keycloak, GitHub/GitLab подключаются как внешние поставщики идентичности;
- пользовательские настройки и права приходят из backend API, где PostgreSQL является источником истины для платформенных состояний.
- Проектное планирование и документационная каноника задаются корневым `AGENTS.md` и актуальной проектной документацией, а не этим техническим гайдом.

Внешний источник: `github.com/codex-k8s/kodex-guidelines-vue-frontend-ru`, source submodule `docs/external/guidelines/vue`.
Если внешний источник импортирован в проект, его правила обязательны. Этот каталог задаёт `kodex`-специфичный overlay и имеет приоритет при конфликте с внешним Vue baseline.
