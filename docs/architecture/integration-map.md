---
id: ARCH-MC-005
title: Карта интеграций
type: architecture
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Карта интеграций

## Типы интеграций

### Runtime tool integration

Утилита и credential предоставляются agent pod напрямую. Подходит для доверенных сценариев, где human approval не должен быть технически обязательным.

Примеры:

- `gh` с ограниченным GitHub token;
- read-only `kubectl` с выделенным kubeconfig;
- локальные language/build tools без credential.

Direct mode нельзя использовать, если платформа обязана гарантировать approval перед mutation: агент уже имеет credential и способен обойти gateway.

### Managed MCP integration

Agent получает только session-scoped MCP endpoint. Integration Gateway владеет credential и выполняет tool после grants/risk/approval checks.

Примеры:

- запись/проведение документа в 1С;
- отправка договора или письма клиенту;
- изменение CRM opportunity;
- финансовая операция;
- production Kubernetes mutation;
- изменение настроек MatterCodex.

### Read/context integration

Интеграция предоставляет ограниченный поиск и чтение данных. Выдача ограничивается scope, pagination и context budget.

## Integration package

Версионируемый YAML package содержит:

```yaml
apiVersion: mattercodex.io/v1alpha1
kind: IntegrationDefinition
metadata:
  name: example-crm
spec:
  connectionSchema: {}
  capabilities: []
  runtime:
    mode: managed-mcp
  tools: []
  riskPolicies: []
  promptDocumentation: "..."
  healthCheck: {}
```

Package не содержит secret values. Поля проходят JSON Schema validation. Произвольные MCP actions используют декларативный подход, совместимый по идее с `yaml-mcp-server`, но production gateway дополнительно обеспечивает tenancy, auth, grants, audit и Mattermost approvals.

## Карта внешних систем

| Система | Роль | Mode baseline |
| --- | --- | --- |
| Mattermost | Conversations, identities, files, approvals | Native adapter |
| OpenAI/Codex | Первый agent runtime provider | Provider adapter + device code auth |
| GitHub | Repositories, issues, PR, review | Direct `gh` или managed MCP по policy |
| Kubernetes | Platform/target runtime | Read-only direct; mutations managed MCP либо explicit admin profile |
| S3 | Artifacts/session archives/backups | Platform adapter |
| Email | Intake and outbound communication | Managed MCP |
| CRM/1С | Business operations | Managed MCP with approvals |
| OCI registry | Platform and role images | Supply-chain adapter |

## Approval contract

Каждый capability имеет risk class:

- `read` — approval обычно не нужен;
- `write` — policy-dependent;
- `external_communication` — approval по шаблону/получателю;
- `destructive` — обязательный approval;
- `financial` — обязательный approval и дополнительная audit metadata;
- `platform_admin` — обязательный approval либо явно разрешенный emergency profile.

Approval показывает инициатора, target, tool, безопасные arguments, risk, expiration и expected effect. Secret arguments маскируются до сохранения request.
