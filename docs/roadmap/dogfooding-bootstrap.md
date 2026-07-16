---
id: ROAD-MC-004
title: Bootstrap dogfooding Workspace MatterCodex
type: roadmap
status: proposed
owner: manager
version: 0.1.0
updated: 2026-07-16
---

# Bootstrap dogfooding Workspace MatterCodex

Bootstrap выполняется после merge documentation baseline. Операция сначала читает live state и переиспользует существующие accounts/integrations без duplicate entities.

## Workspace

- Name: `MatterCodex`.
- Slug/team: `matter-codex` либо свободный согласованный вариант.
- Mattermost: отдельная team.
- GitHub owner: `codex-k8s`.
- Repository: `codex-k8s/matter-codex` как optional IT integration context.
- Locale: Russian.
- Configuration owner: UI на первом этапе; переход к Git после GitOps wave.

## Agents

- `manager` — координация waves/result gates.
- `product-manager` — product requirements, backlog, reporting.
- `architect` — domains/contracts/service boundaries/ADR.
- `developer` — Go/backend/runtime implementation.
- `frontend` — Vue Control Center.
- `reviewer` — technical/product review и merge после owner OK.
- `docs` — documentation consistency.
- `qa-bot` — E2E/regression/evidence.
- `sre` — deployment/operations/backup/observability.
- `security` — threat model/integration/supply-chain review.
- `ui-designer` — UX flows и visual mockups/artifacts.
- `improver` — feedback-to-instructions cycle.
- `mattercodex-admin` — emergency/platform configuration profile с отдельным risk policy.

Каждый Agent получает отдельную Mattermost bot identity. Existing AI/GitHub accounts выбираются по purpose и permissions; секретные значения не копируются в prompts или docs.

## Rooms

- `management` — manager/owner coordination и roadmap.
- `product` — personas/processes/requirements.
- `architecture` — boundaries/data/contracts/ADR.
- `runtime` — sessions/turns/providers/Kubernetes.
- `integrations` — MCP/connections/approvals.
- `attachments-artifacts` — files/S3/knowledge.
- `automations` — schedules/playbooks/callbacks.
- `control-center` — Vue UX.
- `operations` — deploy/observability/backup/security.
- `release` — public/commercial readiness.

Rooms private по умолчанию. Owner и релевантные Agents добавляются автоматически.

## Initial GitHub epics

Создаются parent issues по Wave 1-11. Массовое создание выполняется только после dry-run списка titles и owner confirmation. Issue содержит result type, dependencies, acceptance, roles и human-gate policy.

## Initial schedules

После реализации scheduling:

- daily improver по merged/review feedback;
- platform health check от `mattercodex-admin`;
- backup age/restore evidence check от `sre`;
- weekly manager summary по active epics.

## Bootstrap acceptance

- Owner состоит в team/rooms.
- Все role bots видимы и имеют correct usernames.
- Accounts/integrations показывают effective grants без secret.
- Repository доступен через выбранную GitHub integration.
- Manager отвечает в management room.
- Agent delegation smoke не использует mention trigger.
- Ничего не запускается автоматически до owner kickoff.
