---
id: GO-LIB-DOC-007
title: Общий контур доменных событий Go
type: library
status: approved
owner: architect
version: 1.1.0
updated: 2026-08-02
---

# `libs/go/eventing`

Модуль реализует утверждённые `GO-DOC-004` provider-neutral envelope,
PostgreSQL outbox relay boundary, NATS JetStream publisher и общий durable
PostgreSQL inbox. Он не содержит service-specific event names, payload,
migrations или consumer effects.

Relay получает durable claim/finalize port от сервиса, публикует исходный
envelope как минимум один раз и использует независимый bounded finalize
context. NATS adapter не создаёт stream: `Check` сверяет exact конфигурацию,
а `Publish` требует synchronous acknowledgement ожидаемого stream.

Подпакет [`postgresinbox`](postgresinbox/README.md) фиксирует provider-neutral
consumer API, exact schema contract, receive/claim/effect lifecycle,
transaction/ACK ownership, generation/fence, retry/dead-letter/repair,
bounded operator read/recovery, узкую transaction-bound effect capability,
readiness и retention. Нормативный `schema.sql` не применяется библиотекой:
каждый сервис включает эквивалентный DDL в собственную forward-only goose
migration.
