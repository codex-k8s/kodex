---
id: ADR-MC-002
title: Универсальная web-first продуктовая модель
type: decision
status: approved
owner: product
version: 1.0.0
updated: 2026-08-22
---

# ADR-MC-002. Универсальная web-first продуктовая модель

## Решение

Домен строится вокруг `Organization`, `Project`, `Agent`, versioned
`Instruction`, `Workflow`, `Session`, `Turn`, `Run`, `RunNode`, `RunEdge`,
`RunEvent`, `HumanGate`, `Artifact`, `Schedule`, `IntegrationDefinition`,
`IntegrationConnection`, `IntegrationCapability`, `IntegrationGrant` и `Audit`.

`Project` — единственный пользовательский контейнер. Workspace, Room, Team,
Channel, Thread, repository, cluster и provider-specific identity не являются
core aggregates или compatibility aliases.

Control Center обслуживает весь основной путь. Mattermost, GitHub, GitLab,
Kubernetes, CRM, ERP, email и storage являются необязательными typed
integrations. Ноль connections — штатный web-only режим.

Agent ссылается на exact promoted role image: его Docker image задаёт окружение,
инструменты и ПО, а protected `agent-runner` обеспечивает общий runtime ABI.
Агентские tools предоставляются через MCP с типизированными schemas,
capabilities и grants.

## Последствия

- существующие Mattermost-first DTO, bindings, migrations, routes и dual-write
  удаляются, а не поддерживаются facade;
- Organization присутствует в каждой owner boundary, хотя первый профиль
  материализует одну;
- software development остаётся одним из сценариев, но не влияет на core schema;
- external IDs являются locator metadata adapter-а и не доказывают authority;
- UI не предлагает два конкурирующих понятия «проект/рабочая область».
