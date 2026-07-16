---
id: ROAD-MC-002
title: Эпики и волны
type: roadmap
status: proposed
owner: manager
version: 0.1.0
updated: 2026-07-16
---

# Эпики и волны

## Принципы поставки

- Волна завершается одним или несколькими принятыми типами результата.
- Кодовый PR должен быть deployable либо иметь изолированный manual acceptance path.
- 25-35 PR являются ориентиром, а не искусственным ограничением.
- Параллельные волны разрешены только при независимых data/contracts.
- Миграции live data имеют compatibility и rollback window.
- Deploy выполняется после merge и operational gate, а не для проверки непроверенного design.

## Зависимости

```text
Wave 0 Documentation
  -> Wave 1 Structural Foundation
      -> Wave 2 Runtime/Dogfooding Spine
          -> Wave 3 Attachments/Artifacts
          -> Wave 4 Universal Domain
              -> Wave 5 Integrations/Approvals
              -> Wave 7 Control Center/GitOps
          -> Wave 8 Instructions/Processes
              -> Wave 9 Automations
      -> Wave 6 Role Images/BuildKit
  -> Wave 10 Production Platform
      -> Wave 11 Public/Commercial Release
```

Wave 6 может идти параллельно Wave 4-5 после появления RuntimeRevision contract. Production work начинается частично раньше, но финальный production gate зависит от стабилизации основных доменов.

## Wave 0. Documentation baseline

Цель: согласовать продукт, домены, архитектуру, guides, ADR, operations и roadmap до масштабного кода.

Результат текущего PR:

- universal product model;
- human-gate delivery process;
- attachments/artifacts;
- schedules/playbooks;
- service/domain boundaries;
- production/backup/deploy/security baseline;
- dogfooding bootstrap.

Gate: owner comments -> documentation fixes -> consistency review -> owner OK -> merge.

## Wave 1. Structural foundation

Epic outcomes:

- characterization coverage critical existing behavior;
- enforceable domain packages;
- split repositories и application use cases;
- contracts/tooling skeleton;
- transactional outbox/idempotency foundation;
- documentation checks in CI.

Representative PRs:

1. Characterization tests и architecture fitness checks.
2. Repository interfaces и migrations ownership split.
3. Commands/queries/outbox и idempotent consumer foundation.
4. Mattermost transport extraction из domain service.
5. Runtime port extraction из Kubernetes adapter.

Result gates: repository/data boundaries; application/transport boundaries; outbox delivery contract.

## Wave 2. Runtime and dogfooding spine

Epic outcomes:

- RuntimeRevision resolution;
- immutable provider account affinity;
- session archives outside pod lifecycle;
- controller-style reconciliation/leases;
- durable delegation queue;
- cross-thread create/delegate/callback MCP;
- one start message with limits/stop action.

Representative PRs:

1. RuntimeRevision schema/effective resolver.
2. Session account affinity и new-session selection policy.
3. Session archive adapter и restore contract.
4. Runtime reconcile/lease/repair extraction.
5. Parent-child runs, create-thread, callbacks и merged delegation queue.

После этого manager может полноценно вести параллельные dogfooding threads.

## Wave 3. Attachments and artifacts

Epic outcomes:

- Artifact metadata/S3 adapter;
- Mattermost inbound file ingestion;
- workspace inbox/manifest;
- local `publish_artifact` bridge;
- Mattermost/S3 outbound delivery;
- scan/quarantine/retention;
- file/image E2E.

Representative PRs:

1. Artifact domain и object storage port.
2. Inbound Mattermost attachments.
3. Runtime materialization/prompt context.
4. Outbound files/images и delivery retry.
5. Security/limits/scan/retention E2E.

## Wave 4. Universal product model

Epic outcomes:

- Organization/Workspace/Room;
- RoleDefinition/Agent/Assignment;
- provider-neutral accounts/runtime profiles;
- staged migration current Project/Chat/Role;
- no-repository InstructionSet baseline.

Representative PRs:

1. Organization scope и compatibility IDs.
2. Workspace/Room migration и Mattermost bindings.
3. RoleDefinition/Agent split с сохранением bot identities.
4. Provider account abstraction/OpenAI adapter.
5. InstructionSet materialization без Git.

## Wave 5. Integrations and approvals

Epic outcomes:

- versioned IntegrationDefinition catalog;
- Connection/Capability/Grant;
- session-scoped MCP Integration Gateway;
- risk policies и ApprovalRequest;
- Mattermost/Control Center approval UX;
- GitHub/Kubernetes/email reference integrations;
- migration current env/account grants.

Representative PRs:

1. Catalog/schema/import/reconcile.
2. Connections и credential references.
3. Grants/effective tools.
4. MCP gateway и idempotent execution.
5. Approval lifecycle/UI.
6. GitHub direct/managed modes migration.

## Wave 6. Role images and supply chain

Epic outcomes:

- RoleImageRecipe и canonical hash;
- BuildKit builder/registry cache;
- tools manifest для prompt;
- SBOM/provenance/signing/scanning;
- RuntimeRevision image digest;
- migration from Kaniko.

Representative PRs:

1. Recipe/catalog/API.
2. BuildKit proof и cache.
3. Isolated build controller.
4. Supply-chain gates и runtime digest.

## Wave 7. Control Center and GitOps

Epic outcomes:

- Vue app shell/OIDC/generated API;
- Workspaces/Agents/Accounts;
- Integrations/Approvals;
- Schedules/Playbooks;
- Runtime/Artifacts/Audit;
- GitOps import/export/reconcile/drift;
- responsive/accessibility E2E.

Representative PRs группируются по законченным screens/use cases, а не по frontend/backend слоям.

## Wave 8. Instructions, processes and result gates

Epic outcomes:

- InstructionSet versions/sources;
- KnowledgeSpace/materialization;
- Playbook versions/input/result schemas;
- ProcessRun/ChildRun graph;
- human gate tied to result version;
- improver proposal workflow;
- standard presets для software, documents, analysis и business intake.

## Wave 9. Automations

Epic outcomes:

- AutomationSchedule CRUD/presets/timezone;
- durable scheduler occurrences;
- concurrency/misfire/session/delivery policies;
- headless sessions;
- run now/history/retry;
- scheduled attachments/integration results;
- daily improver и platform health schedules.

## Wave 10. Production platform

Epic outcomes:

- physical service extraction и HA;
- leader election;
- OIDC/RBAC/audit hardening;
- OpenTelemetry/Prometheus/Grafana/log backend;
- capacity/admission/load tests;
- CloudNativePG/S3/Velero backup;
- restore drills;
- Helm/GitOps/Argo Rollouts;
- security/threat model/incident runbooks.

## Wave 11. Public and commercial release

Epic outcomes:

- legal license decision/CLA/trademark;
- dependency/license inventory;
- security policy и vulnerability process;
- installation/upgrade/migration docs;
- starter/production release artifacts;
- public examples/catalog;
- clean-install, upgrade, rollback, restore и dogfooding E2E;
- release notes и support matrix.

## Definition of done программы

- Current live data успешно мигрируют.
- Fresh starter и production-like install проходят E2E.
- Accepted turn/session/schedule переживают service/pod restart.
- Опасные tools нельзя выполнить в обход policy выбранного integration mode.
- Attachments и output files работают end-to-end.
- Backup restore подтвержден drill evidence.
- Manager ведет параллельные processes с callbacks и owner gates.
- Нет обязательной зависимости от Git repository.
- Public artifacts не содержат secrets, private project references и instance-specific assumptions.
