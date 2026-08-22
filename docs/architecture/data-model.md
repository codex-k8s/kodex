---
id: ARCH-MC-006
title: Логическая модель данных web-first платформы
type: architecture
status: approved
owner: architect
version: 1.0.0
updated: 2026-08-22
---

# Логическая модель данных web-first платформы

Нормативная fresh schema находится в
`services/internal/control-plane/cmd/cli/migrations/20260822000100_web_first_baseline.sql`.
Документ показывает aggregates и ownership, но не заменяет SQL contract.

## Installation, Organization и доступ

| Сущность | Назначение и ключевые связи |
| --- | --- |
| `installation` | stable installation identity и bootstrap revision |
| `organizations` | tenant boundary, slug, locale и lifecycle |
| `subjects` | server-resolved OIDC issuer/subject identity |
| `owner_claim_contracts` | initial owner bootstrap contract без статического owner UUID |
| `memberships` | organization platform role и status |
| `projects` | единственный пользовательский контейнер, version/OCC и lifecycle |

## Agents, instructions и role images

| Сущность | Назначение и ключевые связи |
| --- | --- |
| `platform_capabilities` | built-in closed capability catalog |
| `runtime_profiles` | provider/model/resource defaults без secret values |
| `role_definitions` | переиспользуемое назначение и role image policy |
| `agents` | Project Agent либо единственный system assistant по stable key |
| `instruction_versions` | immutable content/digest, draft/validated/published lifecycle |
| `role_image_recipes` | canonical source/build/toolchain spec и SHA-256 |
| `image_builds` | fenced build attempt и безопасный progress/verdict |
| `image_artifacts` | promoted image digest, runtime ABI, SBOM/provenance/signature receipts |

System assistant constraints запрещают delete/archive/disable и смену system
purpose. RuntimeRevision ссылается только на admitted promoted role image.

## Workflows, sessions и graph

| Сущность | Назначение и ключевые связи |
| --- | --- |
| `workflows` | Project aggregate и current published version |
| `workflow_versions` | immutable coordinator/agents/input/result/gate specification |
| `sessions` | Agent-owned durable FIFO context |
| `session_turns` | ordered tasks with source, attempt и lifecycle |
| `runs` | root/child execution, source, target, result, graph revision/sequence |
| `run_nodes` | root process, Agent, Human Gate или bounded external action |
| `run_edges` | delegation, callback, retry, continuation и waiting semantics |
| `run_events` | immutable ordered deltas в пределах root Run |
| `runtime_revisions` | exact immutable versions/digests/grants/input для attempt |
| `runtime_leases` | workload/method/attempt/input/fence-bound claim lifecycle |
| `callback_receipts` | exactly-once child-to-parent continuation effect |

Root lineage, parent/child route и actor назначает control-plane. Payload и
external locator не доказывают происхождение.

## Gates, artifacts и schedules

| Сущность | Назначение и ключевые связи |
| --- | --- |
| `owner_gates` | server-owned recipient policy, safe context, version и one-winner resolution |
| `artifacts` | organization/project/run metadata, version/digest/scan/result state |
| `artifact_bindings` | exact input/result/session/run/node relation |
| `artifact_content` | bounded MVP content под той же PostgreSQL tenant boundary |
| `schedules` | Agent/Workflow target, timezone, input/session/notification policy |
| `schedule_occurrences` | immutable due time, attempt/fence и materialized Run |

## Integrations

| Сущность | Назначение и ключевые связи |
| --- | --- |
| `integration_definitions` | built-in/catalog definition и typed capability schema |
| `integration_connections` | metadata, masked credential state и lifecycle |
| `integration_connection_tests` | asynchronous typed readiness test receipt |
| `integration_grants` | Agent/Workflow capability grant and policy revision |
| `integration_invocations` | fenced typed effect, gate relation и safe result/error |

Secret material не хранится в этих таблицах и не возвращается frontend.

## System assistant

| Сущность | Назначение и ключевые связи |
| --- | --- |
| `assistant_runtime` | desired/observed warm revision, heartbeat и readiness |
| `assistant_conversations` | durable system Session presentation per User/Project context |
| `assistant_plans` | safe typed configuration preview и apply receipt |

Каждая assistant operation сохраняет initiator User и assistant attribution.

## Сквозные таблицы

`idempotency_receipts` связывает organization, actor, operation, key и intent
digest. Один key с тем же intent возвращает receipt, а с другим — conflict.
`audit_events` хранит actor/assistant attribution и safe before/after metadata.
`outbox_events` публикует обязательные domain events после commit.
`worker_grant_high_watermarks` обеспечивает durable replay/rollback protection.

## Инварианты

- все owner data содержат `organization_id`; Project-scoped data разрешается
  через server-owned relation;
- version/OCC проверяется после owner resolution;
- published Instruction/Workflow и terminal attempt immutable;
- event sequence монотонен в пределах root Run;
- retry создаёт новую attempt/revision/lease и `RETRY_OF` edge;
- Human Gate и callback имеют одного доменного winner;
- external IDs и display values не являются authority;
- legacy table aliases, backfill, dual read/write и compatibility views отсутствуют.
