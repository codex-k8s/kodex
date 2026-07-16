---
id: DOM-MC-001
title: Домены MatterCodex
type: domain-index
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Домены MatterCodex

Доменная документация определяет ответственность, данные, интерфейсы, события, наблюдаемость и критерии приемки bounded contexts.

| Код | Домен | Файл |
| --- | --- | --- |
| `DOM-MC-002` | Identity & Access | `identity-access.md` |
| `DOM-MC-003` | Workspaces & Conversations | `workspaces-conversations.md` |
| `DOM-MC-004` | Agents & Instructions | `agents-instructions.md` |
| `DOM-MC-005` | Providers & Accounts | `providers-accounts.md` |
| `DOM-MC-006` | Runtime Orchestration | `runtime-orchestration.md` |
| `DOM-MC-007` | Integrations & Approvals | `integrations-approvals.md` |
| `DOM-MC-008` | Artifacts & Knowledge | `artifacts-knowledge.md` |
| `DOM-MC-009` | Automations & Processes | `automations-processes.md` |
| `DOM-MC-010` | Images & Supply Chain | `images-supply-chain.md` |
| `DOM-MC-011` | Operations & Observability | `operations-observability.md` |

## Обязательная структура доменного документа

- назначение и границы;
- роли и сценарии;
- данные и инварианты;
- commands/queries/events;
- внешние integrations;
- configuration и secrets;
- observability;
- acceptance criteria;
- открытые решения.

## Общие правила

- Таблицы и migrations имеют владельца-домен.
- Другой домен не читает их напрямую.
- External IDs не заменяют platform IDs.
- События имеют version, correlation, causation и idempotency keys.
- Secret values отсутствуют в DTO, event payload и documentation.
- Проекция/read model не становится источником истины для mutation.
