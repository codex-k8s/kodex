---
id: RUN-MC-021
title: Диагностика authoritative owner readbacks control-plane
type: runbook
status: approved
owner: sre
version: 1.1.0
updated: 2026-08-09
---

# Диагностика authoritative owner readbacks control-plane

## Назначение и ограничения

Runbook относится к Agent/runtime catalog, Schedule basic intent, Run,
RuntimeIncident, WorkspaceRestore и configuration diff Issue #263. Он не
разрешает deploy, production change, ручное исправление PostgreSQL, повторный
provider effect или вывод content/receipt/evidence/credential values.

Не выводить raw provider receipt, object `StorageRef`, instruction content,
actor/workload IDs, grants, JTI, credential refs, DSN и TLS/private key data.

## Read-only диагностика

1. Зафиксировать exact application SHA и operation ID без bearer token либо
   authority proof payload.
2. Проверить startup/readiness `control-plane`: exact policy должна содержать
   `control.owner-configuration.catalog`,
   `control.owner-schedule.manage|get|list` и `control.run.list` с workload
   `control-api-gateway`, точным SPIFFE ID и full method.
3. Сверить, что generated descriptor содержит те же методы. Отсутствующий
   operation или method снимает readiness; добавлять generic fallback
   запрещено.
4. По безопасному owner RPC проверить только typed status/version/digest и
   correlation code. Raw legacy поля owner responses должны быть пустыми.

## Типовые отказы

### Agent runtime имеет `STALE` или `INELIGIBLE`

Проверить owner-visible RoleDefinition state и совпадение закреплённых
RoleDefinition/RoleImageRecipe version+digest. Не подставлять recipe ID из UI и
не создавать альтернативный Agent lifecycle. После исправления авторитетной
конфигурации следующий owner read должен вернуть `PRESENT`.

### Schedule basic create/update возвращает conflict

Проверить:

- preset/default revision и digest существуют в server catalog;
- Agent, published InstructionSet, ProviderPool, AgentAssignment и optional
  Room активны в одной owner boundary;
- prompt selector указывает CLEAN Markdown input Artifact либо inline prompt
  прошёл durable preparation и exact version-pinned object write/readback;
- OCC version текущая, а persistent session policy не пытается обойти graph
  locks.

Не создавать Artifact через generic gateway mutation. Повтор с тем же
idempotency key и тем же semantic intent должен вернуть тот же Schedule;
другой intent с тем же ключом закрыто конфликтует.

Для inline prompt проверить только safe state preparation:

- live `WRITING` имеет lease и единственного generation winner;
- `READY|CONSUMED` хранит exact VersionID/readback и не выполняет второй PUT;
- expired `WRITING|AMBIGUOUS` перед recovery повторно проверяет owner, target
  OCC и bindings;
- S3 RPC завершается раньше durable lease; private object locator не выводить.

### `nextActions` не совпадает с состоянием

Сверить exact committed graph:

- Run: `CANCEL` доступен только для единственного nonterminal current Turn без
  active child и с незавершённой exact attempt/lease fence; `RETRY` — только
  для `FAILED|BLOCKED|WAITING_OWNER|CANCELLED` predecessor, который принимает
  тот же locked command decision; `EXPIRED`, live runtime, stale fence и
  unknown state возвращают пустой набор;
- Incident: action зависит одновременно от incident state, current execution,
  runtime state и fence; `CLOSED` всегда возвращает пусто;
- Restore: `QUEUED|RUNNING` → `CANCEL`; terminal retryable state → `RETRY`
  только пока exact backup `AVAILABLE`, membership совпадает и retention ещё
  действует по PostgreSQL time.

Не вычислять actions в gateway и не исправлять состояние отдельными SQL.

### Configuration continuation отклонён

Continuation связан HMAC с comparison digest, обеими versions, content
digests и offset. Любое изменение пары versions/digests требует начать compare
с первой страницы. Подпись и payload не логировать. Любой произвольный
InstructionSet content, включая неизвестные ключи, PII, rename/add/remove и
secret-like строки, возвращается только как `[REDACTED]`: в v1 нет
schema-owned allowlist безопасных typed values.

### Owner list возвращает version conflict

List continuation подписан и связан с видом списка, последним UUID и digest
owner mutation fence. Это штатный fail-closed результат изменения любого
участвующего Resource/RuntimeIncident между страницами. Не повторять token и
не собирать mixed page: начать list заново с пустым continuation.

### Run lineage или incident page отклонена

- lineage page size не превышает 100; полный traversal hard cap — 1000 nodes;
  `truncated=true` всегда сопровождается continuation;
- tampered token, другой Process version или overflow требуют нового read;
- incident query обязан содержать exact execution ID до cursor/limit;
- history/detail/list/manage одного RuntimeIncident обязаны показывать его
  сохранённый non-zero `ExecutionFence`; silent zero является дефектом.

## Rollback

Новых событий Issue #263 нет. Миграции durable prompt preparation и bounded
lineage function/index forward-only: автоматический downgrade запрещён.
Rollback приложения возвращает прежний бинарь и policy только code-first PR
после подтверждения владельца; новые таблица/function/index и сохранённые
JSONB selectors/preset/default/prompt pins остаются на месте. Удалять object
versions, preparation rows либо переписывать pins вручную запрещено.
