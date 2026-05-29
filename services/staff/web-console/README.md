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
- `VITE_KODEX_SCOPE_TYPE`, `VITE_KODEX_SCOPE_REF` — начальный scope context.
- `VITE_KODEX_LOCALE` — начальная локаль, default `ru`.

`web-console` не формирует доверенные `X-Kodex-Actor-*` в production-сборке:
проверенный actor context должен добавлять trusted edge или backend-session слой перед
`staff-gateway`. Для локальной разработки Vite можно явно включить
`VITE_ENABLE_LOCAL_DEV_ACTOR_HEADERS=true` и задать
`VITE_KODEX_LOCAL_DEV_ACTOR_TYPE`, `VITE_KODEX_LOCAL_DEV_ACTOR_ID`; этот режим работает
только при `import.meta.env.DEV`.

Если scope не задан, экраны показывают пустое состояние и не отправляют запросы к
`staff-gateway`. В local-dev режиме с actor headers также должен быть задан
local-dev actor.

## Доступные серверные ручки

- `GET /v1/owner-inbox/items`
- `GET /v1/owner-inbox/items/{request_id}`
- `POST /v1/owner-inbox/items/{request_id}/response`
- `GET /v1/agent-runs/{run_id}/runtime-status`
- `GET /v1/agent-runs/{run_id}/activities`

## Текущий UX-контур

- Shell адаптируется под мобильную ширину: навигация уходит в temporary drawer, а
  контекст scope остаётся в верхней полосе.
- Командный центр явно разделяет работающие зоны и зоны, которые ждут новый
  endpoint `staff-gateway`; быстрые действия и чат отключены без подмены
  production-данных.
- Входящие решения используют master-detail паттерн: список выбирает карточку,
  ответ фиксируется только через разрешённые `allowed_actions`, ошибки показываются
  безопасными кодами и request/correlation refs.
- Экран исполнений работает как поиск одного `Run` по safe id: показывает runtime
  summary, safe refs, activity timeline и понятное пустое состояние, но не строит
  список `Run` без backend-контракта.

Агрегированная витрина командного центра, список `Run`, проектные списки,
создание `Issue`, запуск flow и чат с `agent-manager` пока не имеют HTTP-контракта
в `staff-gateway`; UI отображает эти места как отключённые состояния без
подмены демо-данными.
