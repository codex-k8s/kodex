# staff web-console

`web-console` — первый Vue/Vite/TypeScript фронтенд для операторской консоли
сотрудников. Приложение вызывает только `staff-gateway` по OpenAPI и не обращается
к внутренним gRPC-сервисам, Kubernetes или БД доменных сервисов.

## Скрипты

- `npm run gen:openapi` — генерация typed клиента из
  `specs/openapi/staff-gateway.v1.yaml`.
- `npm run typecheck` — проверка TypeScript и Vue SFC.
- `npm run lint` — текущий lint-контур равен `vue-tsc --noEmit`.
- `npm run build` — production-сборка Vite.
- `npm run dev` — Vite dev server на `0.0.0.0:5174`.

## Runtime-переменные

- `VITE_STAFF_GATEWAY_BASE_URL` — базовый URL `staff-gateway`.
- `VITE_STAFF_GATEWAY_TIMEOUT_MS` — timeout HTTP-запросов, default `15000`.
- `VITE_KODEX_ACTOR_TYPE`, `VITE_KODEX_ACTOR_ID` — начальный actor context.
- `VITE_KODEX_SCOPE_TYPE`, `VITE_KODEX_SCOPE_REF` — начальный scope context.

Если actor или scope не заданы, экраны показывают пустое состояние и не отправляют
запросы к `staff-gateway`.

## Доступные серверные ручки

- `GET /v1/owner-inbox/items`
- `GET /v1/owner-inbox/items/{request_id}`
- `POST /v1/owner-inbox/items/{request_id}/response`
- `GET /v1/agent-runs/{run_id}/runtime-status`
- `GET /v1/agent-runs/{run_id}/activities`

Агрегированная витрина командного центра, список `Run`, проектные списки,
создание `Issue`, запуск flow и чат с `agent-manager` пока не имеют HTTP-контракта
в `staff-gateway`; UI отображает эти места как отключённые состояния без
подмены демо-данными.
