---
id: GUIDE-MC-007
title: Качество API и event contracts
type: guide
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Качество API и event contracts

## OpenAPI

- Source-first YAML/JSON contract в `specs/openapi`.
- Stable operation IDs и reusable schemas/errors.
- Pagination/filter/sort задаются явно.
- Secret write-only поля не возвращаются read API.
- Breaking change проверяется CI.
- Server/client types генерируются; ручные DTO допустимы только на adapter boundary с mapping tests.

## AsyncAPI

Каждый command/event envelope содержит:

- `event_id`;
- `event_type` и version;
- `occurred_at`;
- `organization_id`;
- `correlation_id`;
- `causation_id`;
- `idempotency_key`;
- typed payload.

Consumer обязан выдерживать duplicate delivery и неизвестные optional fields.

## Protobuf/gRPC

- Используется только при обоснованном internal contract.
- Package/version отражены в path и namespace.
- Field numbers не переиспользуются; удаленные поля reserved.
- Deadlines и status mapping документированы.
- Buf lint/breaking/generate выполняются CI.

## MCP tools

- Tool name стабилен и versioned через IntegrationDefinition.
- Input/output описаны JSON Schema.
- Risk, required grant, idempotency и approval semantics обязательны.
- Tool result структурирован; человекочитаемый текст не является единственным status channel.

## Review gate

Контрактный результат проходит отдельные review purposes: domain correctness, compatibility, security/PII, idempotency/retries и usability generated clients.
