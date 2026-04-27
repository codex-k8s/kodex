# Кодогенерация (контракты -> код)

Цель: после изменения контрактов (OpenAPI/proto/AsyncAPI) артефакты регенерируются через `make`, коммитятся и не правятся руками.

## Общие правила

- Сгенерированный код хранится только в заранее закреплённых каталогах кодогенерации. Для protobuf/gRPC канонический каталог — `proto/gen/go/**`; для транспортного слоя и frontend используются профильные `**/generated/**`.
- Сгенерированное руками не правим.
- Изменение перечня сервисов/приложений, участвующих в codegen, должно синхронно обновлять:
  - цели `gen-openapi-*` в `Makefile`;
  - конфиги codegen в целевом `tools/codegen/**`;
  - CI/job-проверку codegen в целевом deploy-контуре.
- Источник правды транспорта:
  - REST: `specs/openapi/<service-name>.v<major>.yaml` (OpenAPI YAML)
  - gRPC: `proto/**/*.proto`
  - async/webhook: `specs/asyncapi/<service-name>.v<major>.yaml` (если используется)
- Контракт и реализация:
  - стабильные контракты `v1` создаются на весь согласованный объём доменного API, а не только на уже написанные обработчики;
  - если обработчики, репозиторные методы или доставщики событий будут реализованы позже, это фиксируется в документе поставки и карте связей;
  - кодогенерация выполняется по контракту, а не по текущему неполному набору обработчиков, чтобы клиенты и серверные адаптеры не расходились с принятой архитектурой.

## OpenAPI (REST) -> Go

Инструмент:
- `github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest`

Выход:
- `internal/transport/http/generated/openapi.gen.go`

Запуск:
```bash
make gen-openapi-go SVC=services/<zone>/<service>
```

Текущий охват backend-кодогенерации определяется актуальной архитектурной документацией проекта и активным инвентарём сервисов.
Устаревшие сервисы не включаются в активное покрытие кодогенерации.

Обязательный make-блок:
- `gen-openapi-go` — генерация Go transport-артефактов;
- `gen-openapi-ts` — генерация TS-клиента для frontend;
- `gen-openapi` — агрегирующая цель для CI и локальной проверки.

## Protobuf/gRPC -> Go

Инструменты:
- `google.golang.org/protobuf/cmd/protoc-gen-go@latest`
- `google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest`

Выход:
- `proto/gen/go/**`

Запуск:
```bash
make gen-proto-go
```

## AsyncAPI (webhook/event payloads)

Контракт:
- `specs/asyncapi/<service-name>.v<major>.yaml`

Применение в `kodex`:
- описание webhook payloads и внутренних async-событий,
- опциональная генерация моделей для transport-слоя.

Валидация:
```bash
make validate-asyncapi SVC=<service-name>
```

## Frontend codegen по OpenAPI (TypeScript + Axios)

Рекомендуемый инструмент:
- `@hey-api/openapi-ts` (клиенты `@hey-api/client-*` встроены начиная с `v0.73.0`; отдельная установка `@hey-api/client-axios` не требуется)

Выход:
- `src/shared/api/generated/**`

Запуск:
```bash
make gen-openapi-ts APP=services/<zone>/<app> SPEC=specs/openapi/<service-name>.v<major>.yaml
```

## Проверка консистентности generated-кода в CI

- Обязательная проверка размещается в целевом deploy-контуре новой архитектуры.
- Codegen-check job должен выполнять:
  - установку зависимостей frontend;
  - `make gen-openapi`;
  - `git diff --exit-code` по OpenAPI-generated артефактам.
- Любое расширение/изменение codegen-охвата (новый backend service или frontend app) сопровождается правкой этого job-манифеста.
