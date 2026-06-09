# Go: что выносить в `libs/go/*`

Цель: уменьшать дублирование между сервисами без “god-lib” и без протечки бизнес-логики конкретного домена.

Список согласованных внешних библиотек/инструментов:
- `docs/design-guidelines/common/external_dependencies_catalog.md`

## Когда выносить

- код нужен >= 2 сервисам;
- нужен единый стандарт поведения (логирование/метрики/otel, middleware, клиенты);
- API библиотеки можно сделать минимальным и стабильным.

## Связь с версиями сервисов

- Если сервис начинает импортировать библиотеку из `libs/go/*`, этот путь нужно синхронно добавить в `services.yaml` в `spec.versions.<service>.bumpOn`.
- Если новая или изменённая библиотека из `libs/go/*` зависит от других локальных контрактов (`proto/**`, `specs/**`, `libs/go/accesscatalog`, `libs/go/platformevents/**` и т.п.), каждый deployable-сервис должен иметь в `bumpOn` как саму библиотеку, так и прямые локальные контракты, которые сервис импортирует или использует через неё как часть runtime/build-контракта.
- Перед push нужно проверить все потребители новой библиотеки через поиск импортов и убедиться, что изменение библиотеки приведёт к пересборке всех затронутых образов.

## Что обычно выносим

- `libs/go/observability/*` — логгер, метрики, OTel helpers.
- `libs/go/auth/*` — OAuth/session helpers и безопасность.
- `libs/go/crypto/*` — шифрование/расшифровка секретов и токенов.
- `libs/go/db/*` — общие DB helpers (tx, pagination, jsonb/pgvector утилиты).
- `libs/go/postgres/*` — общие PostgreSQL helpers и pgxpool runtime.
- `libs/go/grpcserver/*` — общий runtime gRPC сервера: gRPC-перехватчики, auth, Prometheus-метрики, OpenTelemetry tracing, проброс trace context и лог-корреляция.
- `libs/go/outbox/*` — общий runtime доставщика сервисного outbox.
- `libs/go/eventlog/*` — общий клиент `platform-event-log` и PostgreSQL publisher для outbox.
- `libs/go/secretresolver/*` — общий контракт безопасного получения значения секрета по разрешённой ссылке и проверки доступности без раскрытия значения; детали реализации хранилища (`kubernetes_mounted_secret`, `env`, `vault`) не должны протекать в домены-потребители.
- `libs/go/accesscheck/*` — общий клиент `access-manager` для `CheckAccess` и сервисных адаптеров авторизации.
- `libs/go/platformevents/*` — сгенерированные из AsyncAPI контракты доменных событий.
- `libs/go/i18n/*` — общий backend runtime локализации системных message id.
- `libs/go/k8s/*` — клиентские адаптеры и шаблоны работы с Kubernetes API.
- `libs/go/repo/*` — общий слой provider интерфейсов для GitHub/GitLab.
- `libs/go/mcp/*` — общий слой MCP tool contracts и helpers.

## Межсервисный plumbing

- Для gRPC error boundary использовать `grpcserver.DomainRule` и `grpcserver.NewDomainErrorMapper`, не копировать вручную одинаковые `DomainErrorRule` literals между сервисами.
- Для безопасных metadata-полей из protobuf использовать `grpcserver.ActorParts` и `grpcserver.RequestContextParts`, затем приводить к service-owned value object.
- Для `access-manager` checks использовать `accesscheck.NewConnectedAuthorizer` и `accesscheck.NewRequestFromValues`/`NewRequest`, оставляя в сервисе только маппинг собственных domain request fields.
- Для PostgreSQL pagination/query/idempotency plumbing использовать `postgres.AddOffsetPageArgs`, `postgres.QueryRows`, `postgres.ScanCommandResultRow` и `postgres.CRUDSentinels`, если форма строк совпадает между сервисами.
- Shared helper не должен скрывать доменное решение: service-owned enum casting, lifecycle state transitions и payload semantics остаются в сервисе.

## Что запрещено выносить

- доменные правила конкретного сервиса;
- транспортные DTO продукта (для этого есть `proto/` и OpenAPI/AsyncAPI контракты);
- service-owned domain enum конкретного сервиса: межсервисные enum-классификаторы берутся из protobuf/gRPC или generated AsyncAPI, а не из ручной shared enum-библиотеки;
- тяжёлые зависимости ради одной функции.

## Контракты транспорта

- gRPC правила см. `docs/design-guidelines/go/protobuf_grpc_contracts.md`.
- Ошибки см. `docs/design-guidelines/go/error_handling.md`.
