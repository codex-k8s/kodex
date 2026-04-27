# Frontend: архитектура и размещение

## Где живёт фронтенд

- Frontend-приложение размещается в целевом frontend-сервисе, определённом актуальной архитектурной документацией проекта.
- Устаревшие frontend-каталоги не используются как база новой реализации.
- Дополнительные публичные страницы (если появятся) размещаются в целевой зоне `services/external/*`.
- Dev-only frontend размещается в целевой зоне `services/dev/*` и не деплоится в production.

## Container и deploy требования

- Каждый frontend-сервис обязан иметь `Dockerfile` в корне сервиса.
- В Dockerfile обязательно поддерживаются target:
  - `dev` — для production/dev (например, Vite dev server);
  - `prod` — для production runtime со статическим бандлом (например, `nginx`).
- Для каждого frontend-сервиса обязателен отдельный Kubernetes manifest-шаблон
  в целевом deploy-каталоге сервиса.

## Технологический стек

- Vue 3
- TypeScript
- Pinia (state)
- Axios (HTTP)
- Vite + `@vitejs/plugin-vue` + `vite-plugin-pwa`
- `vue-i18n`
- `vue-router`
- `vue3-cookies`

## Границы ответственности

- Frontend не реализует backend бизнес-логику.
- Контракт с backend: HTTP/OpenAPI (`specs/openapi/<service-name>.v<major>.yaml`) и стабильный формат ошибок.
- Контракт async сообщений (если есть): AsyncAPI (`specs/asyncapi/<service-name>.v<major>.yaml`).
- OAuth state и permissions приходят из backend; frontend не хранит доверенные полномочия локально как источник истины.

## Рекомендуемая структура приложения

Внутри целевого frontend-сервиса:
- `index.html`, `package.json`, `vite.config.ts`.
- `public/` — статические файлы.
- `src/`:
  - `src/app/` — composition root: app/router/pinia/i18n/PWA.
  - `src/router/` — маршруты и guards.
  - `src/i18n/` — словари и ключи.
  - `src/shared/`:
    - `src/shared/api/` — axios client и нормализация ошибок.
    - `src/shared/api/generated/` — codegen артефакты.
    - `src/shared/ws/` — WS клиент (если используется).
    - `src/shared/ui/` — базовые UI-компоненты.
    - `src/shared/lib/` — утилиты.
  - `src/features/` — фичи и Pinia stores.
  - `src/pages/` — route-level страницы.

## Запрещено

- Прямые вызовы `axios` из компонентов/страниц (только через слой `shared/api`/feature services).
- Хранить секреты/token material в локальном хранилище как источник истины.
- Дублировать права доступа в UI без серверной валидации.
