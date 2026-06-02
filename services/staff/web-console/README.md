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

## Production deploy

Production-контур собирает статический Vite bundle в образ `web-console` и
отдаёт его через unprivileged nginx на порту `8080`.

В Kubernetes активный путь:

```bash
bash bootstrap/host/deploy_backend_ring.sh \
  --env-file bootstrap/host/config.env \
  --ring web
```

Kubernetes manifest создаёт внутренний `Service` и nginx `ConfigMap`:
`/health/livez` и `/health/readyz` отвечают без обращения к backend, а `/v1/**`
проксируется на `staff-gateway:8080` внутри кластера. Поэтому production-сборка
оставляет `VITE_STAFF_GATEWAY_BASE_URL` пустым и использует same-origin API
path.

Публичный HTTPS-доступ включается отдельным deploy-контуром:

```bash
bash bootstrap/host/deploy_backend_ring.sh \
  --env-file bootstrap/host/config.env \
  --ring web-public
```

Этот контур готовит `cert-manager`, Traefik `IngressClass` `kodex-public`,
`ClusterIssuer` Let’s Encrypt, `oauth2-proxy`, `Certificate` для
`platform.kodex.works` и публичный `Ingress` только на `oauth2-proxy`. Прямого
публичного `Ingress` на `web-console` нет. GitHub OAuth callback закреплён за
`https://platform.kodex.works/oauth2/callback`, а доступ ограничен отдельным
allowlist-файлом для owner email, не общей bootstrap allowlist.

## Доступные серверные ручки

- `GET /v1/owner-inbox/items`
- `GET /v1/owner-inbox/items/{request_id}`
- `POST /v1/owner-inbox/items/{request_id}/response`
- `GET /v1/agent-sessions`
- `GET /v1/agent-runs`
- `GET /v1/agent-runs/{run_id}/runtime-status`
- `GET /v1/agent-runs/{run_id}/activities`

## Текущий UX-контур

- Shell адаптируется под мобильную ширину: навигация уходит в temporary drawer, а
  контекст scope остаётся в верхней полосе.
- Командный центр явно разделяет работающие зоны и зоны, которые ждут новый
  endpoint `staff-gateway`; быстрые действия и чат отключены без подмены
  production-данных.
- Верхние карточки командного центра показывают текущую страницу входящих
  решений и первые страницы списков `AgentSession`/`AgentRun` из
  `staff-gateway`; они не заменяют агрегированную производственную витрину.
- Входящие решения используют master-detail паттерн: список выбирает карточку,
  ответ фиксируется только через разрешённые `allowed_actions`, ошибки показываются
  безопасными кодами и request/correlation refs.
- Экран исполнений загружает списки `AgentSession` и `AgentRun`, поддерживает
  фильтры по safe status, открывает выбранный `Run` и показывает runtime summary,
  safe refs, activity timeline и понятные пустые состояния.

Агрегированная витрина командного центра, проектные списки, создание `Issue`,
запуск flow и чат с `agent-manager` пока не имеют HTTP-контракта в
`staff-gateway`; UI отображает эти места как отключённые состояния без подмены
демо-данными.

Списки `AgentSession`/`AgentRun` используют scope types, которые поддерживает
текущий `agent-manager` contract: `platform`, `organization`, `project`,
`repository`. Если пользователь выбирает `service` scope, `web-console` честно
показывает неподдержанное состояние для списков исполнений; owner inbox и другие
поверхности продолжают работать в рамках своих контрактов.
