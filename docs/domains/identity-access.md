---
id: DOM-MC-002
title: Identity & Access
type: domain
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Identity & Access

## Назначение

Владеет Organization, user identity mapping, memberships, platform roles и policy decisions. Обеспечивает изоляцию даже при baseline «одна Organization на инсталляцию».

## В границах

- OIDC identity и mapping Mattermost user;
- organization membership;
- роли owner/admin/operator/auditor/member;
- authorization context для API/MCP;
- service/session identities;
- revocation и audit authorization decisions.

Не хранит OpenAI/GitHub/API credentials и не управляет Mattermost password/session.

## Инварианты

- Каждый бизнес-объект принадлежит Organization либо является system catalog object.
- Только owner может назначать platform administrator.
- Session token имеет organization, agent/session scope, expiration и capabilities.
- Disabled subject не создает новые actions; уже принятые dangerous approvals пересматриваются policy.

## Интерфейсы

Commands: create organization, invite/map member, change role, disable subject.

Queries: current actor, memberships, effective platform permissions.

Events: `OrganizationCreated`, `MembershipChanged`, `SubjectDisabled`, `PolicyChanged`.

## Observability

- denied actions по reason;
- invalid/expired session identities;
- administrative role changes;
- OIDC/Mattermost mapping failures.

## Acceptance

- API и MCP отвергают cross-organization IDs.
- UI не показывает недоступные entities.
- Audit содержит actor и policy result без token.
- Две stateless replicas принимают одинаковые authorization decisions.

## Открытое решение

Полноценный shared multi-tenant SaaS остается за пределами первого production profile; schema и contracts не должны блокировать его позже.
