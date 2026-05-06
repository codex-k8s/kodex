# Go: что выносить в `libs/go/*`

Цель: уменьшать дублирование между сервисами без “god-lib” и без протечки бизнес-логики конкретного домена.

Список согласованных внешних библиотек/инструментов:
- `docs/design-guidelines/common/external_dependencies_catalog.md`

## Когда выносить

- код нужен >= 2 сервисам;
- нужен единый стандарт поведения (логирование/метрики/otel, middleware, клиенты);
- API библиотеки можно сделать минимальным и стабильным.

## Что обычно выносим

- `libs/go/observability/*` — логгер, метрики, OTel helpers.
- `libs/go/auth/*` — OAuth/session helpers и безопасность.
- `libs/go/crypto/*` — шифрование/расшифровка секретов и токенов.
- `libs/go/db/*` — общие DB helpers (tx, pagination, jsonb/pgvector утилиты).
- `libs/go/postgres/*` — общие PostgreSQL helpers и pgxpool runtime.
- `libs/go/grpcserver/*` — общий runtime gRPC сервера, interceptors, auth и метрики.
- `libs/go/outbox/*` — общий runtime доставщика сервисного outbox.
- `libs/go/eventlog/*` — общий клиент `platform-event-log` и PostgreSQL publisher для outbox.
- `libs/go/platformevents/*` — сгенерированные из AsyncAPI контракты доменных событий.
- `libs/go/i18n/*` — общий backend runtime локализации системных message id.
- `libs/go/k8s/*` — клиентские адаптеры и шаблоны работы с Kubernetes API.
- `libs/go/repo/*` — общий слой provider интерфейсов для GitHub/GitLab.
- `libs/go/mcp/*` — общий слой MCP tool contracts и helpers.

## Что запрещено выносить

- доменные правила конкретного сервиса;
- транспортные DTO продукта (для этого есть `proto/` и OpenAPI/AsyncAPI контракты);
- service-owned domain enum конкретного сервиса: межсервисные enum-классификаторы берутся из protobuf/gRPC или generated AsyncAPI, а не из ручной shared enum-библиотеки;
- тяжёлые зависимости ради одной функции.

## Контракты транспорта

- gRPC правила см. `docs/design-guidelines/go/protobuf_grpc_contracts.md`.
- Ошибки см. `docs/design-guidelines/go/error_handling.md`.
