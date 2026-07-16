---
id: PRD-MC-005
title: Требования production baseline
type: product-requirements
status: proposed
owner: product
version: 0.1.0
updated: 2026-07-16
---

# Требования production baseline

## Функциональные требования

- `FR-001`: система поддерживает Organization и Workspace, связанный с Mattermost team.
- `FR-002`: система поддерживает reusable RoleDefinition и concrete Agent с отдельной bot identity.
- `FR-003`: Agent получает provider account, integrations, instructions и runtime settings через versioned bindings.
- `FR-004`: пользователь может создать Room и работать с агентами через posts/threads.
- `FR-005`: сообщения, callbacks и scheduled runs используют одну durable turn queue.
- `FR-006`: агент может запускать другого агента и создавать новый thread только через MCP tools.
- `FR-007`: human gate поддерживает approve, reject, request changes, expiration и audit.
- `FR-008`: prompt-driven Playbook описывает coordinator, allowed agents, inputs, callbacks и completion policy.
- `FR-009`: AutomationSchedule поддерживает interval/cron, timezone, pause, run now, concurrency, misfire и delivery policies.
- `FR-010`: входные attachments материализуются в workspace и описываются в prompt manifest.
- `FR-011`: агент публикует files/images через controlled `publish_artifact` tool.
- `FR-012`: Git и repositories не обязательны для работы Workspace.
- `FR-013`: InstructionSet версионируется и может управляться через UI либо GitOps.
- `FR-014`: RuntimeRevision пересчитывается перед каждым turn.
- `FR-015`: AI account закрепляется за session и не заменяется при resume.
- `FR-016`: новые sessions поддерживают manual и policy-based account selection.
- `FR-017`: role image recipe имеет immutable hash и переиспользует готовый image digest.
- `FR-018`: Control Center предоставляет CRUD, search, diagnostics и audit без ручного ввода внутренних ID.
- `FR-019`: Mattermost предоставляет разговорный UX, approvals, progress и быстрые действия.
- `FR-020`: UI-managed и Git-managed configuration не изменяют одну сущность одновременно.

## Надежность

- `NFR-REL-001`: принятый в очередь turn не теряется после рестарта любого stateless service.
- `NFR-REL-002`: один occurrence расписания не создает два run при нескольких scheduler replicas.
- `NFR-REL-003`: pod recreation не уничтожает session archive, workspace state и queued turns.
- `NFR-REL-004`: integration mutations и callbacks идемпотентны.
- `NFR-REL-005`: application rollback совместим с expand/migrate/contract schema lifecycle.
- `NFR-REL-006`: backup имеет документированные RPO/RTO и регулярно проверяемый restore.

## Безопасность

- `NFR-SEC-001`: raw secrets не хранятся в prompt, logs, audit payload и Git-managed YAML.
- `NFR-SEC-002`: dangerous integration credential не передается agent pod.
- `NFR-SEC-003`: artifacts изолированы по Organization/Workspace/Session и проверяются до публикации.
- `NFR-SEC-004`: произвольный install script выполняется только в isolated image builder.
- `NFR-SEC-005`: permissions вычисляются из явных grants и отображаются пользователю.
- `NFR-SEC-006`: все administrative, approval и external mutation actions аудируются.

## UX

- `NFR-UX-001`: основной path не требует typed commands или внутренних IDs.
- `NFR-UX-002`: формы используют presets, selects, toggles и validation; cron доступен только в advanced mode.
- `NFR-UX-003`: error card объясняет причину, состояние и безопасное следующее действие.
- `NFR-UX-004`: `no_action` scheduled runs не засоряют Mattermost.
- `NFR-UX-005`: пользователь видит agent identity, status, account label, limits и stop action.

## Эксплуатация

- `NFR-OPS-001`: production profile поддерживает минимум две replicas stateless services.
- `NFR-OPS-002`: singleton loops используют leader election либо database claim.
- `NFR-OPS-003`: metrics, logs и traces связаны correlation identifiers.
- `NFR-OPS-004`: runtime admission не запускает pod при недостаточной capacity и оставляет run в очереди.
- `NFR-OPS-005`: build, deploy, backup и restore имеют наблюдаемый статус и audit evidence.

## Качество поставки

- `NFR-QLT-001`: каждый тип результата проходит 2-3 review cycles до первого owner gate, если owner не сократил процесс явно.
- `NFR-QLT-002`: после owner feedback выполняются fix и повторный review.
- `NFR-QLT-003`: merge разрешен только после финального owner OK.
- `NFR-QLT-004`: после merge запускается improver для обновления instructions/guides.
- `NFR-QLT-005`: каждый PR имеет автоматические проверки и ручной acceptance path.
