---
id: RUN-MC-021
title: Диагностика authoritative owner readbacks control-plane
type: runbook
status: approved
owner: sre
version: 1.0.0
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
  прошёл content-addressed object write/readback;
- OCC version текущая, а persistent session policy не пытается обойти graph
  locks.

Не создавать Artifact через generic gateway mutation. Повтор с тем же
idempotency key и тем же semantic intent должен вернуть тот же Schedule;
другой intent с тем же ключом закрыто конфликтует.

### `nextActions` не совпадает с состоянием

Сверить exact committed graph:

- Run: nonterminal → `CANCEL`; eligible terminal
  `FAILED|EXPIRED|CANCELLED` с закрытым runtime predecessor → `RETRY`; terminal
  без retry eligibility → пусто;
- Incident: action зависит одновременно от incident state, current execution,
  runtime state и fence; `CLOSED` всегда возвращает пусто;
- Restore: `QUEUED|RUNNING` → `CANCEL`; terminal retryable state → `RETRY`
  только пока exact backup `AVAILABLE`, membership совпадает и retention ещё
  действует по PostgreSQL time.

Не вычислять actions в gateway и не исправлять состояние отдельными SQL.

### Configuration continuation отклонён

Continuation связан HMAC с comparison digest, обеими versions, content
digests и offset. Любое изменение пары versions/digests требует начать compare
с первой страницы. Подпись и payload не логировать. Secret-like строки должны
возвращаться только как `[REDACTED]`.

## Rollback

Схема таблиц и новые события Issue #263 отсутствуют. Совместимый rollback
приложения возвращает прежний бинарь и policy только одним code-first PR после
подтверждения владельца. Уже сохранённые JSONB поля preset/default/prompt
остаются неизменяемыми pins; удалять либо переписывать их вручную запрещено.
