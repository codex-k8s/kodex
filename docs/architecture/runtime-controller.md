---
id: ARCH-MC-010
title: Runtime-controller и role Pod
type: architecture
status: approved
owner: architect
version: 1.1.0
updated: 2026-08-28
---

# Runtime-controller и role Pod

## Граница ответственности

Runtime-controller claim-ит server-owned execution tasks и materialize-ит
Kubernetes resources exact attempt. Он не запускает provider process внутри
себя и не владеет Project, Agent, Session, Turn, Run lifecycle, permissions,
callback route или Human Gate.

Control-plane выдаёт immutable `RuntimeExecution` с exact:

- organization/project/agent/session/turn/run/node/attempt refs;
- RuntimeRevision version и SHA-256;
- runtime configuration, provider policy, published overlay, runtime
  environment и Agent binding refs/versions/SHA-256;
- promoted role image `repository@sha256` и runtime ABI digest;
- input/result bounds, capabilities и credential bindings;
- claim generation, fence и expiry.

Caller-provided owner/root/parent, role name, prompt и external conversation IDs
не используются как authority.

## Обычный turn

Каждый turn, retry и continuation создаёт новый execution-scoped Pod из exact
promoted role image. Role image содержит собственное окружение, пакеты,
инструменты и ПО конкретной роли. Supply chain после недоверенного installation
step добавляет защищённые `kodex-init` и `kodex-agent-runner` из
trusted base и подтверждает runtime ABI перед promotion.

Pod использует отдельный ServiceAccount, immutable Secret input, bounded
workspace и exact Secret projections. Он не получает namespace-wide access,
control-plane database DSN, integration/provider master credentials, registry
push/admin credential или external channel token.

Runtime-controller проверяет claimed revision/fence, создаёт immutable input и
execution-scoped opaque ticket в отдельном Secret и materialize-ит Pod.
`agent-runner` не claim-ит Turn повторно: он подтверждает уже выданную attempt
через exact mTLS callback + ticket, запускает provider runtime, передаёт bounded
progress, обслуживает разрешённые MCP servers/tools и завершает attempt через
typed RPC. Provider process работает отдельным UID без Kubernetes token и
authority credential.

Wire contract `kodex.agent-runner-input.v6` содержит non-secret environment
values и только Secret descriptors. Для каждого descriptor runtime-controller
читает exact immutable source Secret, сверяет name/key, UID,
`resourceVersion`, UTF-8/size и SHA-256, затем копирует проверенные байты в
immutable execution ticket под непрозрачным ключом `environment-<hash>`.
`runtime.json` сохраняет descriptor, но не value. Только `provider-runtime`
получает `env.secretKeyRef` на exact execution ticket/key; `role-runtime` не
получает Secret projection. Любой stale UID/resourceVersion, отсутствующий key
или digest mismatch закрывает materialization до создания Pod.

Agent-runner не объединяет TOML как текст. Он повторно разбирает published
overlay strict parser-ом и кодирует один typed `config.toml`: model,
approval/sandbox, permissions, auth store, MCP и shell boundary назначает
сервер; overlay заполняет только закрытый non-authority allowlist; environment
set управляет только разрешёнными process environment names. Ни overlay, ни
environment set не могут переопределить server-owned поля.

Terminal Pod не переиспользуется для следующего turn. Retry получает новую
attempt, RuntimeRevision, claim, credentials и Pod; прежний execution остаётся
read-only lineage.

## MCP boundary

RuntimeRevision содержит только разрешённые bindings. Для managed integration
role Pod получает session-scoped MCP endpoint/credential, а provider secret и
external effect остаются в integration-gateway. Платформенные MCP tools
делегирования, callback, sync и owner attention вызывают специализированные
control-plane commands через тот же authorization context.

Raw provider response, stdout/stderr, Codex JSONL, arbitrary tool payload и
secret value не передаются в domain event или browser stream.

## Always-hot помощник

System assistant имеет отдельный warm materialization, потому что обычный
one-Pod-per-turn путь не обеспечивает hot-first-request. Reconciler поддерживает
ровно один ready system role Pod требуемой revision, resource limits, heartbeat
и durable system Session. Idle не является active Turn; turns выполняются FIFO.

После Pod/process restart controller materialize-ит warm runtime заново. Positive
assistant readiness означает, что exact desired prompt/runtime revision реально
обслуживается и может принять turn. Warm state не даёт database, Kubernetes или
secret-store authority; typed MCP operation повторно проверяет текущего User.
`Failed`/`Succeeded` warm Pod и immutable ticket другого controller instance или
revision являются stale materialization и заменяются reconciler-ом.

Повторный bootstrap сверяет digest текущего immutable core prompt. Более новая
поставляемая revision создаёт следующую published instruction version,
forward-only меняет desired runtime revision, переводит assistant в
`RECOVERING` и фиксирует системный audit. Rollback revision и совпавшая revision
с другим digest закрывают startup.

Warm report связан с exact desired revision и controller instance. Повторный
report того же state обновляет только `last_heartbeat_at`, не создаёт новые
idempotency receipts, audit и outbox events и не увеличивает aggregate version.
Смена state/revision/instance атомарно увеличивает version и публикует одно
событие. Поэтому heartbeat действительно поддерживает readiness и не создаёт
периодический поток ложных configuration changes.

## Health/readiness

- process `/healthz` проверяет только собственную жизнь;
- `/readyz` читает локальный рассчитанный snapshot и не выполняет network call
  на probe;
- direct readiness dependencies controller-а: local authority sidecar,
  Kubernetes API observation и NATS/claim consumer, если они используются;
- control-plane, integration-gateway, provider и optional adapters не входят в
  Pod readiness; рабочий отказ возвращает typed `Unavailable`;
- Kubernetes observation допускает bounded LKG только при transport failure;
  signature/digest/revision rollback/conflict/expiry fail closed немедленно;
- outage/recovery логируются один раз как state transition.

## Состояния materialization

`CLAIMED -> MATERIALIZING -> POD_READY -> RUNNING -> TERMINAL -> CLEANED`.
Cancel может перевести любой non-terminal execution в `CANCELLING -> CANCELLED`.
Lease expiry возвращает task в bounded retry либо создаёт terminal incident по
server policy. Controller cleanup не определяет terminal Run самостоятельно.

## Network и admission

Base deny-all. Role Pod получает DNS, exact provider egress proxy, exact MCP
service и только разрешённый project access profile. Admission проверяет exact
image digest, runtime ABI, container layout, ServiceAccount, volumes, commands,
resources, immutable input binding и execution ticket Secret. Admission также
требует exact revision/config/environment digests, запрещает `stringData` и
лишние ticket keys, а Secret projections разрешает только из этого ticket в
`provider-runtime`. Mutable image, extra container, broad token, host access и
privileged fallback запрещены.

## Критерии приёмки

- разные роли действительно запускаются в своих Docker images;
- новый turn создаёт новый Pod, а system assistant использует отдельный warm Pod;
- stale claim/fence/revision не запускает workload;
- stale Secret revision или digest не запускает workload, а runner input не
  содержит Secret values;
- Mattermost и любая optional integration не участвуют в materialization;
- cancel/retry/restart не создают две активные attempt одного Turn.
