---
id: OPS-MC-007
title: Security и secrets
type: operations
status: proposed
owner: security
version: 0.1.0
updated: 2026-07-16
---

# Security и secrets

## Threat boundaries

- пользователь и Mattermost content считаются недоверенными;
- agent-generated commands и files считаются недоверенными;
- integration responses могут содержать prompt injection;
- role install script и dependency supply chain являются привилегированным риском;
- direct runtime credentials позволяют обойти MCP approval и выдаются осознанно.

## Secret lifecycle

- Secret value принимается по защищенному UI/API и сохраняется в secret backend.
- DB хранит stable credential reference, metadata и revision.
- UI после создания показывает только masked identity/status.
- Rotation увеличивает revision и применяется к следующему turn.
- Revocation блокирует новые tool calls/sessions.
- Secrets не попадают в Git, prompt, logs, metrics, traces, artifacts и support bundles.

## Agent isolation

- ServiceAccount/access profile выбирается явно.
- Session pod получает только grants текущего RuntimeRevision.
- Dangerous connection credentials остаются в Integration Gateway.
- Artifact paths и storage authorization изолированы.
- Builder не получает runtime secrets.
- Production profile документирует network/egress policy; отключение контроля является видимым risk acceptance.

## Approval

Human approval связывается с immutable invocation hash. Approver видит безопасное описание effect. Expired/changed request не выполняется.

## Public release

До публикации обязательны threat model review, dependency/license inventory, secret-history scan, security policy, vulnerability reporting channel, supported versions и incident disclosure process.
