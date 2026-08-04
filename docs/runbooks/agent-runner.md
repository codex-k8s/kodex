---
id: RUNBOOK-MC-AGENT-RUNNER
title: Agent runner runbook
type: runbook
status: approved
owner: sre
version: 1.1.0
updated: 2026-08-04
---

# Agent runner runbook

## Сигналы

- `AgentRunnerTurnIncidents`: Pod завершился без принятого signed handoff;
- `AgentRunnerCapacityRetries`: provider capacity повторяется чаще нормального;
- `/readyz` неуспешен до exact Turn claim и RuntimeExecution admission;
- `mattercodex_agent_runner_turns_total{outcome="incident"}` не содержит
  provider payload или персональные данные.

## Read-only preflight

1. Сначала получить environment manifest через `scripts/render-agent-runner.sh`
   с утверждёнными handoff key ID/public key, exact Kubernetes API endpoints и
   provider `/32|/128`; прямой `kubectl kustomize` оставляет fail-closed
   placeholder и не является deploy input.
2. Найти execution по server-owned ID из alert и прочитать RuntimeExecution в
   control-plane; не использовать Pod annotation как authority.
3. Сверить journal, Pod UID, version/fence/generation, Turn attempt и текущую
   lease. Не выводить lease token, application grant, Codex auth или rollout JSONL.
   При resume дополнительно сверить только факт совпадения закреплённых
   app-server rollout relative path/SHA-256/provenance; содержимое archive не
   публиковать.
4. Проверить причины init/runtime container и readiness sidecar только по
   закрытым кодам. Raw stderr/provider response не публиковать.
5. Проверить наличие exact handoff ConfigMap и только факт валидности подписи,
   key generation и digest. Убедиться, что Pod использует собственный
   `runtime-access-*` ServiceAccount, а RoleBinding ссылается только на handoff
   и credential resources этого execution. Содержимое пользовательских files
   не выводить.
6. Проверить durable interaction delivery и provider receipt у owner readback.

## Диагностика

| Симптом | Вероятная граница | Действие |
|---|---|---|
| init failed | materialization TLS/bearer/version/digest/path | сверить owner Artifact и gateway working path; не копировать object вручную |
| authority issuer not ready | policy snapshot/Vault/PostgreSQL/readback | проверить exact sidecar readiness и mounted key names |
| runner waiting admission | Turn lease ещё не видна controller либо reconcile задержан | проверить FIFO head и следующий reconcile; не подменять identity |
| MCP unavailable | SNI/CA/certificate/bearer или required server | восстановить тот же рабочий endpoint; fallback запрещён |
| incident after exit | отсутствующий/invalid signed handoff | сохранить Pod/PVC, проверить key trust overlap и ConfigMap RBAC |
| resume отклонён | app-server rollout path/type/link-count/digest/provenance или provider binding не совпал | сохранить PVC, проверить owner RuntimeExecution и restore evidence; не заменять archive вручную |
| delivery pending | interaction-gateway временно недоступен | оставить owner row для reclaim; не публиковать в Mattermost вручную |
| capacity retries | exact `CodexErrorInfo=serverOverloaded` | дождаться 1/3/5; после исчерпания owner получит terminal result |
| blocked без retry | `usageLimitExceeded`, `unauthorized`, `cyberPolicy` либо invalid/missing `codexErrorInfo` | проверить закреплённый provider account/policy; не классифицировать diagnostic `message` и не запускать retry вручную |
| app-server request rejected | approval, user input, token refresh, attestation или dynamic tool request | сохранить incident evidence без payload; runner не должен отвечать approval и расширять authority |

## Recovery

Recovery выполняется только кодом runtime-controller. Не patch-ить Pod,
handoff ConfigMap, journal, lease или provider binding вручную. При crash до
handoff owner watchdog фиксирует incident и выбирает expiry/retry. При crash
после owner terminal transaction interaction delivery восстанавливается по
durable row и provider receipt. Session PVC удаляется только после archive,
retention и cleanup authorization.

При cancel/SIGTERM runner отправляет `turn/interrupt` для exact
`(threadId, turnId)`, ждёт bounded grace и затем завершает process group.
Подменять этот путь ручным `codex` вызовом или правкой rollout запрещено.

Ротация handoff keys выполняется вперёд: сначала новый public key добавляется в
controller trust set, затем выдаётся private key новым revisions, после
исчерпания старых executions старый key удаляется. Пропуск overlap или
placeholder key блокирует readiness/environment activation. Имя public key в
`agent-runner-handoff-trust` имеет вид `sha256-<первые 16 hex>` от pinned
`CredentialBinding.contentSha256`; оно должно совпадать с private-key binding
новой `RuntimeRevision`.

Vault Kubernetes role `internal-rpc-authority-agent-runner` настраивается на
namespace `mattercodex-system` и только имена ServiceAccount
`runtime-access-*`. Общий статический ServiceAccount для role Pod запрещён.
После terminal cleanup должны отсутствовать execution-scoped ServiceAccount,
`runtime-handoff-*` Role и RoleBinding; наличие любого из них после удаления
Pod считается incident и не исправляется ручным расширением RBAC.

## Эскалация

Нужен ручной шлюз владельца при повторяющемся invalid signature, provider
account mismatch, подозрении на credential disclosure либо необходимости
изменить owner lifecycle. Production restart, retry, cleanup или deploy без
отдельного подтверждения владельца запрещены.
