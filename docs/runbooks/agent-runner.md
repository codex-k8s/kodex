---
id: RUNBOOK-MC-AGENT-RUNNER
title: Agent runner runbook
type: runbook
status: approved
owner: sre
version: 1.3.0
updated: 2026-08-09
---

# Agent runner runbook

## Сборка direct-production prototype

Wave A собирает `agent-runner` на repository-scoped ephemeral ARC runner через
rootless BuildKit. `tools/release/build-release.sh` обязательно проходит через
защищённый `scripts/build-agent-runner-image.sh`; узкий shim переводит только его
проверенный argv в локальный BuildKit UDS и отклоняет другой Dockerfile,
destination либо option. Результатом является immutable digest в exact release
lock. Legacy Kaniko path bot-service не используется для новых сборок и в этой
волне не удаляется.

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
   `runtime-access-*` ServiceAccount, а Role разрешает только exact runtime
   ConfigMap read и handoff ConfigMap read/update, без `secrets/get`. Убедиться,
   что `provider-runtime` UID 10002 не имеет authority/credential/Kubernetes
   mounts и общается с runner только через protected UDS. Он видит session PVC
   для app-server, но model shell получает закрытый профиль: `read-only`
   соответствует exact `:read-only`, а `workspace-write` — exact `:workspace`;
   оба добавляют deny на `CODEX_HOME`, `/proc`, authority paths и не наследуют
   authority env. Неизвестный режим и `danger-full-access` отклоняются.
   Содержимое пользовательских files не выводить.
6. Проверить, что Session MCP binding перед текущим Turn получил свежий
   bot-service `TokenSecretRef` readback с exact execution/turn/attempt и
   монотонной binding revision, вошёл в RuntimeRevision и был скопирован
   credential broker только в trusted container. Новый binding атомарно
   становится current в AgentSession; predecessor bearer немедленно перестаёт
   проходить consumer fence, а удаление Secret повторяет reconciler. URL/SNI должны указывать на
   `matter-codex-bot-service`, а readiness проходить через тот же UDS
   `SO_PEERCRED` proxy и TLS 1.3/mTLS/bearer transport.
7. Проверить durable interaction delivery и provider receipt у owner readback.
   Для крупного результата сверить только Artifact ID/version/size/SHA-256 и
   private storage ref; runner не должен иметь S3 credential.

## Диагностика

| Симптом | Вероятная граница | Действие |
|---|---|---|
| init failed | materialization TLS/bearer/version/digest/path | сверить owner Artifact и gateway working path; не копировать object вручную |
| authority issuer not ready | policy snapshot/Vault/PostgreSQL/readback | проверить exact sidecar readiness и mounted key names |
| runner waiting admission | Turn lease ещё не видна controller либо reconcile задержан | проверить FIFO head и следующий reconcile; не подменять identity |
| MCP unavailable | exact `matter-codex-bot-service` SNI/CA/certificate/session bearer или required path | восстановить тот же рабочий endpoint; fallback запрещён |
| provider broker unavailable | UDS peer UID, socket mode либо provider container | не переносить Codex обратно в trusted runner; проверить разделение mounts |
| incident after exit | отсутствующий/invalid signed handoff | сохранить Pod/PVC, проверить key trust overlap и ConfigMap RBAC |
| resume отклонён | app-server rollout path/type/link-count/digest/provenance или provider binding не совпал | сохранить PVC, проверить owner RuntimeExecution и restore evidence; не заменять archive вручную |
| delivery pending | interaction-gateway временно недоступен | оставить owner row для reclaim; не публиковать в Mattermost вручную |
| отдельный output не сохранён | owner staging, S3 readback или Artifact registration не завершены | terminal `FAILED` сохраняет protected recovery journal с предыдущим delivery execution и отдельным исходным provider execution для archive provenance; card Retry запускает новую attempt только для доставки immutable bytes, без повторного вызова модели; если journal утрачен, новая attempt также фиксирует bounded `FAILED`, сохраняет исходный server marker и не читает outbox/не запускает модель — вернуть retained PVC либо разбирать owner recovery, не загружать файл вручную |
| capacity retries | exact `CodexErrorInfo=serverOverloaded` | дождаться 1/3/5; после исчерпания owner получит terminal result |
| blocked без retry | `unauthorized`, `cyberPolicy` либо invalid/missing `codexErrorInfo` | для auth выполнить `/agents openai auth <account-name>`, где публичное имя разрешено owner из stable binding; device-code обновляет credential revision той же logical account и не меняет lineage; policy/unknown вручную не повторять |
| quota без capacity retry | `usageLimitExceeded`, terminal `BLOCKED` | проверить лимит учётной записи; не смешивать с `serverOverloaded` и не использовать Retry до обновления account binding |
| app-server request rejected | approval, user input, token refresh, attestation или dynamic tool request | сохранить incident evidence без payload; runner не должен отвечать approval и расширять authority |

## Recovery

Recovery выполняется только кодом runtime-controller. Не patch-ить Pod,
handoff ConfigMap, journal, lease или provider binding вручную. При crash до
handoff owner watchdog фиксирует incident и выбирает expiry/retry. При crash
после owner terminal transaction interaction delivery восстанавливается по
durable row и provider receipt. Session PVC удаляется только после archive,
retention и cleanup authorization.

Stop допускается только из actor-verified текущей Mattermost run card для
`QUEUED|CLAIMED|ADMITTED|RUNNING`; Retry — только для утверждённых
`FAILED|EXPIRED`. Interaction-gateway проверяет card/channel/root/actor и
callback replay, control-plane повторно разрешает owner graph, а
runtime-controller останавливает Pod лишь после authoritative cancel readback.
Terminal Pod удаляется сразу, но retained PVC сохраняется для нового successor
Pod. Stale card и cancel/complete race не исправлять повторным прямым RPC.

Если provider outcome уже получен, но хотя бы один output не прошёл staging,
runner фиксирует `FAILED` и protected checksum journal на retained PVC. Retry
создаёт новую owner attempt/revision/grant, повторно читает exact digest исходного
файла либо полного Markdown и выполняет только staging/handoff. Успешные refs
переиспользуются, provider/app-server не запускается, а журнал удаляется только
после принятого handoff. Control-plane переносит в новый execution только
server-owned `codex_delivery_recovery_source_execution_id`; без exact marker
локальный journal не может назначить recovery, а без exact journal marker
закрыто останавливает attempt до provider. Повреждение журнала, mismatch
Turn/attempt или исчезновение immutable source также закрыто останавливают recovery.

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
