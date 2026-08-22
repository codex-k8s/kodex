---
id: RUN-MC-008
title: Диагностика runtime-controller
type: runbook
status: approved
owner: sre
version: 2.0.0
updated: 2026-08-23
---

# Диагностика runtime-controller

## Role Pod path

Для каждой обычной attempt controller создаёт новый Pod exact promoted role
image. Проверить authoritative execution, RuntimeRevision digest, attempt,
fence/generation, image `repository@sha256`, runtime ABI, ServiceAccount,
resources, PVC и callback ticket. Display role name, prompt или caller-supplied
Kubernetes locator не являются authority.

`mattercodex.agent-runner-input.v4` должен пройти schema validation. Mutable
tag, image вне promoted repository, ABI mismatch, stale fence, extra container,
broad ServiceAccount или host access закрыто отклоняются admission.

## Always-hot assistant

Проверить одну desired system revision, один warm Pod, heartbeat, resource
limits и observed `READY`. Idle Pod не имеет active Turn. При restart или
revision change controller заменяет materialization; readiness помощника не
может быть положительной до фактического callback/provider warm path.

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
