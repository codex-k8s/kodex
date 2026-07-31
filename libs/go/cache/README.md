---
id: GO-LIB-DOC-008
title: Общий bounded cache Go
type: library
status: approved
owner: architect
version: 1.0.0
updated: 2026-07-31
---

# `libs/go/cache`

Модуль содержит provider-neutral read-through engine и TLS-only Redis adapter.
Сервис определяет tenant-scoped hash key, protobuf codec, TTL, invalidation и
domain constructors. Ошибка Redis приводит к authoritative read из
PostgreSQL; ошибка PostgreSQL никогда не маскируется stale cache.
