---
id: ADR-MC-002
title: Универсальная продуктовая модель
type: decision
status: proposed
owner: product
version: 0.1.0
updated: 2026-07-16
---

# ADR-MC-002. Универсальная продуктовая модель

## Решение

Домен строится вокруг Organization, Workspace, Room, RoleDefinition и Agent. GitHub/repository является optional integration. Один Organization на инсталляцию — первый deployment profile, но organization scope присутствует в schema/contracts с начала migration.

Mattermost mappings:

- Workspace = team;
- Room = channel;
- Conversation = thread либо headless execution binding;
- Agent = отдельная bot identity.

`Project`, `Chat` и current `AgentRole` поддерживаются как migration/UI aliases для IT preset.

## Последствия

- Не-IT workspace работает без repository.
- Role definition переиспользуется; Agent хранит concrete identity/config bindings.
- Требуется staged data migration и compatibility UI.
- Provider-specific термины не используются в universal entities.
