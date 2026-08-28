---
id: RUN-MC-008
title: Диагностика runtime-controller
type: runbook
status: approved
owner: sre
version: 2.1.0
updated: 2026-08-28
---

# Диагностика runtime-controller

## Role Pod path

Для каждой обычной attempt controller создаёт новый Pod exact promoted role
image. Проверить authoritative execution, RuntimeRevision digest, attempt,
fence/generation, image `repository@sha256`, runtime ABI, ServiceAccount,
resources, PVC и callback ticket. Display role name, prompt или caller-supplied
Kubernetes locator не являются authority.

`kodex.agent-runner-input.v4` должен пройти schema validation. Mutable
tag, image вне promoted repository, ABI mismatch, stale fence, extra container,
broad ServiceAccount или host access закрыто отклоняются admission.

## Always-hot assistant

Проверить одну desired system revision, один warm Pod, heartbeat, resource
limits и observed `READY`. Idle Pod не имеет active Turn. При restart или
revision change controller заменяет materialization; readiness помощника не
может быть положительной до фактического callback/provider warm path.

Assistant runtime получает contextual descriptor и только закрытые tools,
соответствующие server-owned allowed operations. `propose_assistant_metadata`
может предложить bounded title, а `propose_configuration_plan` передаёт только
explicit operations с target/parameters/before/after. Ни один из этих tools не
применяет план и не выдаёт runtime новые полномочия.

Обычный и assistant runtime могут вызвать `propose_run_metadata`. После каждого
terminal MCP call controller отправляет одну bounded проекцию через
`RecordRunToolCall`: tool, safe parameters, exact capability/grant, outcome,
duration и safe result. Ошибка проекции считается ошибкой рабочего path и может
быть безопасно повторена по тому же idempotency key; raw arguments/result в
control-plane не отправляются.

## Probes

- `/healthz` — controller process;
- `/readyz` — локальный рассчитанный snapshot Kubernetes observation,
  authority sidecar и worker loop;
- control-plane/provider/integration/interaction service не вызываются на probe;
- working-path outage возвращает typed `Unavailable` и bounded retry.

Kubernetes transport failure допускает bounded LKG. Signature/digest mismatch,
revision rollback/conflict, expired ticket или grace period немедленно закрывают
materialization. Один и тот же отказ/restore логируется только как transition.

## Cancel, retry, cleanup

Controller не делает Run terminal по состоянию Pod. Cancel приходит как
server-owned graph command, закрывает attempt/grants/leases и затем Pod. Retry
имеет новую attempt/RuntimeRevision/Pod; старый Pod не переиспользуется.
Cleanup разрешён только после signed handoff и authoritative terminal readback;
PVC следует отдельной retention policy.

## Локальная проверка

```bash
cd services/internal/runtime-controller
GOWORK=off go test ./...
```

При диагностике tool-call projection дополнительно сверить соответствие tool и
capability, наличие grant в immutable RuntimeRevision и отсутствие ключей
`secret`, `token`, `password`, `credential`, `payload` или `raw` в safe
parameters.
