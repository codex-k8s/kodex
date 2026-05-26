# Машинно-проверяемые спецификации

## Назначение

`specs/**` хранит машинно-проверяемые API-спецификации платформы. Документы в `docs/**` объясняют решения и контекст, а спецификации в `specs/**` являются источником истины для валидации, генерации клиентов и проверки совместимости.

## Разделы

| Раздел | Назначение |
|---|---|
| `openapi/` | HTTP/OpenAPI контракты пограничных gateway API. Внутренние сервисы не публикуют прямые OpenAPI-контракты. |
| `asyncapi/` | AsyncAPI контракты событий, webhook и асинхронных сообщений. |
| `jsonschema/` | Standalone JSON Schema контракты для машинно-проверяемых payload-моделей, которые не являются transport contract. |

## Правила именования

- Имя OpenAPI/AsyncAPI файла строится по gateway-поверхности или сервису-владельцу событий и версии: `<surface-or-service>.v<major>.yaml`.
- Standalone JSON Schema размещается в каталоге `jsonschema/<domain-or-service>.v<major>/` и именуется по payload-модели: `<payload-name>.v<major>.schema.json`.
- Для внутренних доменных сервисов целевые файлы: AsyncAPI событий и gRPC proto; HTTP-контракты появляются только в OpenAPI-спецификациях соответствующих gateway.
- gRPC-контракты остаются в `proto/kodex/<domain_or_service>/v<major>/**`.
- Сгенерированный код не является источником истины и не правится руками.

## Статус и полнота контрактов

- Стабильный контракт `v1` создаётся сразу на весь согласованный объём доменного API, если доменный пакет уже принят к реализации.
- Нельзя выпускать частичную спецификацию как стабильный `v1`, если в архитектурном API-контракте уже согласованы дополнительные команды, чтения или события.
- Если нужен неполный контракт для раннего прототипа, он явно помечается как предварительный: отдельным статусом, описанием ограничений и без заявления, что это источник истины стабильного `v1`.
- Стабильный контракт может опережать реализацию. В таком случае документ поставки сервиса обязан содержать таблицу: что уже реализовано, что остаётся в бэклоге, и через какую задачу или рабочий срез это будет закрыто.
- Запрещено оставлять нереализованные части стабильного контракта в неопределённом состоянии “когда-нибудь”: у каждой такой части должен быть владелец в бэклоге и критерий, когда её нужно реализовать до появления потребителя.

## Активные спецификации

| Сервис | OpenAPI | AsyncAPI | gRPC | JSON Schema |
|---|---|---|---|---|
| `access-manager` | нет прямого OpenAPI; HTTP принадлежит gateway-спецификациям | `asyncapi/access-manager.v1.yaml` стабильный `v1` | `../proto/kodex/access_accounts/v1/access_manager.proto` стабильный `v1` | нет |
| `project-catalog` | нет прямого OpenAPI; HTTP принадлежит gateway-спецификациям | `asyncapi/project-catalog.v1.yaml` стабильный `v1` | `../proto/kodex/projects/v1/project_catalog.proto` стабильный `v1` | нет |
| `package-hub` | нет прямого OpenAPI; HTTP принадлежит gateway-спецификациям | `asyncapi/package-hub.v1.yaml` стабильный `v1` | `../proto/kodex/packages/v1/package_hub.proto` стабильный `v1` | нет |
| `runtime-manager` | нет прямого OpenAPI; HTTP принадлежит gateway-спецификациям | `asyncapi/runtime-manager.v1.yaml` стабильный `v1` | `../proto/kodex/runtime/v1/runtime_manager.proto` стабильный `v1` | нет |
| `interaction-hub` | нет прямого OpenAPI; HTTP принадлежит gateway-спецификациям | `asyncapi/interaction-hub.v1.yaml` стабильный `v1` | `../proto/kodex/interactions/v1/interaction_hub.proto` стабильный `v1` | нет |
| `codex-hook-ingress` | нет прямого OpenAPI; transport contract не выбран | transport events не создаются в CHI-1/CHI-2 | transport contract не выбран | `jsonschema/codex-hook-ingress.v1/**` для normalized envelope, sanitizer contract и hook emitter/local sidecar runtime config |
| `integration-gateway` | `openapi/integration-gateway.v1.yaml` предварительный каркас HTTP-поверхности (`x-kodex-contract-status: mvp-skeleton`), Go-модели генерируются в `services/external/integration-gateway/internal/transport/http/generated` | нет; доменные события публикуют сервисы-владельцы | нет собственного gRPC; вызывает сервисы-владельцы, первый client interface — `provider-hub.IngestWebhookEvent` | нет |
