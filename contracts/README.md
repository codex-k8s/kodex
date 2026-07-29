---
id: SVC-DOC-002
title: Контракты
type: contract-index
status: approved
owner: architect
version: 1.1.0
updated: 2026-07-28
---

# Контракты

Каталог содержит только версионированные источники правды:

| Граница                                | Формат             |
| -------------------------------------- | ------------------ |
| внешний клиент → gateway               | OpenAPI            |
| gateway/job/service → internal service | Proto/gRPC         |
| realtime client                        | AsyncAPI WebSocket |
| domain → consumers                     | AsyncAPI           |
| authorization/error/registry policy    | YAML               |

Generated code не редактируется вручную. Он создается рядом с потребителем или
во временном каталоге проверок.

`registry.yaml` фиксирует package, owner, версию, source и consumers.
Неизвестный пакет и расхождение владельца должны отклоняться закрыто.

Gateway не владеет чужим бизнес-состоянием. Событие принадлежит сервису,
который атомарно изменяет source of truth.

Нормативные правила:

- `CONTRACT-DOC-002` - Proto/gRPC package, authority, mutation и errors;
- `CONTRACT-DOC-003` - AsyncAPI envelope, NATS subject, ordering и consumer
  effect;
- `GO-DOC-005` - runtime-путь синхронной и асинхронной коммуникации.
