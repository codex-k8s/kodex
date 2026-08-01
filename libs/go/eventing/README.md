---
id: GO-LIB-DOC-007
title: Общий контур доменных событий Go
type: library
status: approved
owner: architect
version: 1.0.0
updated: 2026-07-31
---

# `libs/go/eventing`

Модуль реализует утверждённые `GO-DOC-004` provider-neutral envelope,
PostgreSQL outbox relay boundary и NATS JetStream publisher. Он не содержит
service-specific event names, payload, SQL, migrations или consumer effects.

Relay получает durable claim/finalize port от сервиса, публикует исходный
envelope как минимум один раз и использует независимый bounded finalize
context. NATS adapter не создаёт stream: `Check` сверяет exact конфигурацию,
а `Publish` требует synchronous acknowledgement ожидаемого stream.
