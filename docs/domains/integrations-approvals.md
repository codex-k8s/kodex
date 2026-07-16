---
id: DOM-MC-007
title: Integrations & Approvals
type: domain
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Integrations & Approvals

## Назначение

Управляет декларативным integration catalog, connections, capabilities, grants, MCP execution и human approvals.

## Основные сущности

- IntegrationDefinition — immutable name/version/schema/tools.
- IntegrationConnection — organization-specific endpoint/config/credential refs.
- IntegrationCapability — именованная операция с input/output schema и risk.
- IntegrationGrant — право Agent на capability с constraints.
- ToolInvocation — идемпотентный вызов из конкретной session/turn.
- ApprovalRequest — durable human decision.

## Execution

1. Session-scoped token аутентифицирует AgentSession.
2. Gateway проверяет definition/version/connection health.
3. Evaluator проверяет grant и constraints.
4. Risk policy решает, нужен ли approval.
5. Approved action выполняется adapter/YAML-MCP executor.
6. Result возвращается агенту и записывается в audit.

## Approval lifecycle

States: `pending`, `approved`, `rejected`, `expired`, `cancelled`, `executing`, `succeeded`, `failed`.

Решение связывается с hash tool invocation. Изменение arguments после approve создает новый request.

## Dangerous actions

Credential для dangerous capability не materialize-ится в pod. Нельзя одновременно утверждать mandatory approval и выдавать агенту direct credential, способный выполнить ту же mutation в обход gateway.

## Acceptance

- Connection validation не раскрывает secret.
- Agent видит только granted tools.
- Approval card не требует command/ID.
- Двойной approve/retry не выполняет mutation дважды.
- Rejected/expired result понятен агенту.
- Tool, decision и execution имеют один correlation chain.
