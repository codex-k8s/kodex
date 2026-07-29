---
id: ROAD-MC-002
title: Эпики и порядок реализации MatterCodex
type: roadmap
status: approved
owner: manager
version: 1.0.0
updated: 2026-07-29
---

# Эпики и порядок реализации MatterCodex

## Правила программы

- Источник правды backlog: umbrella #179 и GitHub sub-issues #180-#197.
- Один unit равен одному самостоятельно поставляемому компоненту, одному Issue,
  одному worktree, одной ветке и одному полному PR.
- Одновременно разрабатывается не более двух unit.
- Эпики выполняются по порядку. Следующий эпик не начинается без human gate
  предыдущего, кроме явно одобренной подготовки контрактов.
- Shared contract или runtime primitive имеет одного владельца и точный merge
  order; параллельные unit не создают копии.
- Каждый unit проходит процесс `GUIDE-DOC-004` и `ROAD-MC-003`.

## Epic 1. Trust and Control Core

GitHub: #180.

- #186 `internal-rpc-authority`;
- #187 `control-plane`.

`internal-rpc-authority` первым фиксирует Proto/UDS/JWKS contract. После
фиксации контракта обе реализации могут идти параллельно; merge order:
#186, затем #187.

## Epic 2. Runtime and Integrations

GitHub: #181.

- #188 `runtime-controller`;
- #189 `integration-gateway`.

Оба unit зависят от принятой authority boundary и control-plane contracts.

## Epic 3. External Boundaries

GitHub: #182.

- #190 `interaction-gateway`;
- #191 `control-api-gateway`.

Оба gateway используют generated clients и не читают PostgreSQL Control Plane.

## Epic 4. Execution and Automation

GitHub: #183.

- #192 `agent-runner`;
- #193 `automation-scheduler`.

Оба unit стартуют после принятия runtime и integration contracts. Runner не
владеет orchestration state; scheduler не исполняет AI process.

## Epic 5. Operations UX and Supply Chain

GitHub: #184.

- #194 `services/staff/control-center`;
- #195 `role-image-builder`.

Control Center зависит от принятого Control API. Image Builder выдаёт
immutable digest, который допускает Runtime Controller.

## Epic 6. Migration and Cutover

GitHub: #185.

- #196 проверяемая миграция данных legacy MatterCodex;
- #197 staging deployment, QA, backup/restore, cutover и rollback.

Эти задачи преимущественно последовательны. Production/cutover требует
отдельного owner OK после доказанного restore и rollback.

## Legacy freeze

Открытые до strategy reset PR закрыты без слияния. Их требования перенесены в
acceptance scope новых unit. Legacy bot-service остается работающим только для
dogfooding и не является базой новых реализаций.
