---
id: OPS-DOC-1119
title: Явное ожидание отсутствия managed consumer binding в HTTP
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-07
---

# Явное ожидание отсутствия managed consumer binding

Refs #1119, #1118, #1117, #1031. HTTP потребляет additive Proto
`ManagedConfigurationConsumer.expected_absent=5` из producer checkpoint
`3ca19aa376a1867e6c3e18193b8baebacdbc6a4c`.

## Сквозная карта

| Инициатор и authority | HTTP → RPC | Owner effect и ответ | Consumer |
| --- | --- | --- | --- |
| Owner web session, проверенные CSRF/context; kind/ref не выдают полномочия | POST `/api/v1/prompt-template-configurations/{configurationRef}/revisions/{revisionRef}/consumer-bindings` → `RebindPromptTemplateConsumers` | CP проверяет target authority/impact, global consumer key и прежнюю связь атомарно; конфликт → Aborted → HTTP412 | Managed Configuration Editor |
| Та же transport authority | POST `/api/v1/integration-definition-configurations/{configurationRef}/revisions/{revisionRef}/consumer-bindings` → `RebindIntegrationDefinitionConsumers` | Тот же producer CAS; batch stale полностью откатывается | Managed Configuration Editor |
| Та же transport authority | POST `/api/v1/system-stt-configurations/{configurationRef}/revisions/{revisionRef}/consumer-bindings` → `RebindSystemSTTConsumers` | INSERT-only либо MATCH по прежним pins; readback CP конфигурации/ревизии | STT setup #1117 и Managed Configuration Editor |

У каждого endpoint сохраняются прежние `If-Match` конфигурации,
`Idempotency-Key`, CSRF и `impactDigest`. Это не замена owner eligibility.
HTTP не выполняет повторный POST после412, не меняет события или транзакции
producer. Read path/configuration impact продолжает возвращать прежний
`ManagedConfigurationConsumer` с обязательными revisionRef/version.

## Input

`ManagedConfigurationConsumerInput` — oneOf двух закрытых объектов:

- ABSENT: kind/ref и обязательный `expectedAbsent:true`; revisionRef/version
  отсутствуют полностью, включая запрет null/пустых/нулевых pins. Mapper
  передаёт ExpectedAbsent=true, RevisionRef="", Version=0; CP выполняет
  INSERT-only и отвечает412, если связь уже существует.
- MATCH: kind/ref, обязательные прежние revisionRef/version>=1;
  expectedAbsent отсутствует либо false. Старые корректные запросы сохраняют
  семантику. Target revision/version1 не обозначают отсутствие.

Generated Go union хранит raw JSON, поэтому HTTP проверяет закрытые keys,
тип boolean, presence/null, kind/ref, revision format и safe integer до RPC.
Duplicate consumer key в batch остаётся HTTP400. Generated TypeScript union
различает true/false на уровне типов; read DTO не расширяется write-полем.

## Проверки и потребители

`make test-managed-consumer-contract` проверяет исходную OpenAPI через Ajv
и generated TypeScript через tsc: обе valid формы, contradictory/missing/null
pins, закрытые properties и прежний read DTO. Focused Go
`go test -race ./internal/transport/http -run '^TestManaged'` проверяет все три
HTTP маршрута, exact Proto selection/OCC/impact и owner412 без преобразования
в успешный ответ. Генерация выполняется только `make gen-openapi` и каноникой
Proto; повторная генерация должна сохранять tree.

Handwritten PWA здесь не меняется. Существующие MATCH consumers:
`features/managed-configurations/api.ts` (rebind), `ConfigurationEditor.vue`
(selection), `model.ts` (selectedConsumers) и его test. Новый первый bind
STT выполняет setup #1117 через generated SDK с explicit expectedAbsent.

Context7 resolve/query проверены для oapi-codegen и Ajv; официальные источники:
[oapi-codegen unions](https://github.com/oapi-codegen/oapi-codegen/blob/main/README.md),
[Ajv oneOf/additionalProperties](https://ajv.js.org/faq.html).
Synthetic HTTP/SDK не доказывает PostgreSQL CAS: его проверяет producer #1118.
Live до общего deploy NOT RUN. Ручная проверка: первый ABSENT успешен, второй
configuration не захватывает existing binding (412), fresh MATCH меняет его.
Rollback требует согласованного producer/client checkpoint, чтобы новый
setup не потерял explicit-absence контракт. Секреты не раскрываются.
