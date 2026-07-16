---
id: ADR-MC-004
title: RuntimeRevision и account affinity
type: decision
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# ADR-MC-004. RuntimeRevision и account affinity

## Решение

Перед каждым turn строится immutable RuntimeRevision. Изменения env/auth/image/mount/permissions приводят к пересозданию idle session pod перед следующим turn. Provider config materialize-ится заново перед каждым `exec/resume`.

AIProviderAccount выбирается при создании session и после первого запуска immutable. Автоматическая балансировка разрешена только для новых sessions. Resume другим account запрещен.

## Последствия

- Изменения конфигурации предсказуемо применяются без вмешательства пользователя.
- Running turn не меняется на лету.
- Недоступный account требует reauthorization или новой session с context handoff.
- Session archive должен быть независим от pod lifecycle.
