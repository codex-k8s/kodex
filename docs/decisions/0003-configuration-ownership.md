---
id: ADR-MC-003
title: Владение конфигурацией UI и GitOps
type: decision
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# ADR-MC-003. Владение конфигурацией UI и GitOps

## Решение

Control Center и versioned YAML catalog поддерживаются одновременно. Каждая управляемая сущность имеет `managed_by: ui|git` и source reference/revision.

- Git-managed object read-only в UI, кроме explicit detach/fork.
- UI-managed object можно экспортировать в YAML и передать под Git ownership.
- Secret values отсутствуют в YAML; используются credential references.
- Reconciler применяет Git desired state идемпотентно и показывает drift/status.

## Причина

YAML-only исключает бизнес-пользователей, UI-only не дает воспроизводимость системным интеграторам. Bidirectional write без ownership создает конфликты и потерю изменений.
