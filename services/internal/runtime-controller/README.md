---
id: SVC-MC-005
title: runtime-controller
type: service
status: approved
owner: developer
version: 2.0.0
updated: 2026-08-23
---

# runtime-controller

`runtime-controller` материализует server-owned execution attempts в
Kubernetes. Он не владеет Project, Agent, Session, Turn, Run lifecycle,
Human Gate, integration grant или terminal result.

## Кто запускает агентов

1. `control-plane` создаёт immutable `RuntimeRevision` и выдаёт exact attempt.
2. `runtime-controller` claim-ит работу и проверяет fence/generation.
3. Для обычного turn создаётся новый Pod из exact promoted role image.
4. Защищённый `agent-runner` внутри image запускает provider runtime и MCP.
5. Terminal handoff проверяется control-plane; только после readback Pod
   удаляется. Retry/continuation получают новую attempt и новый Pod.

Каждая роль может использовать свой Docker image с собственными утилитами,
пакетами и программным окружением. `role-image-builder` собирает этот image через
rootless BuildKit, а image admission проверяет provenance, SBOM, vulnerability
policy, signature, promotion и runtime ABI. Controller допускает только
`repository@sha256` из настроенного promoted repository.

## Runtime contract

Канонический input — `mattercodex.agent-runner-input.v4`, схема находится в
`contracts/runtime-controller/v4/agent-runner-input.schema.json`, типы — в
`libs/go/runtimecontract`. Input связывает organization/project/agent/session/
turn/run/node/attempt, revision digest, role image digest, bounded input,
capabilities и credential references. Payload не назначает owner или lineage.

Protected init/runner входят в trusted runtime ABI role image. Provider process
работает без Kubernetes token и authority credential. Role Pod не получает
control-plane DSN, registry writer/admin, secret-store authority, Mattermost
token или managed integration credentials.

## MCP

MCP не заменяется generic RPC. RuntimeRevision материализует только разрешённые
типизированные MCP servers/tools. Platform MCP tools отображаются в
специализированные control-plane commands; managed integration MCP выполняется
`integration-gateway`. Secret values и raw provider/tool payload не входят в
domain events или browser stream.

## System assistant

Системный помощник использует отдельный always-hot Pod. Reconciler поддерживает
exact desired prompt/runtime revision, heartbeat и resource limits. Idle не
является активным Turn; turns идут FIFO. После process/Pod restart warm runtime
восстанавливается до положительной assistant readiness. Этот Pod не получает
DB, Kubernetes или secret-store authority.

## Health и readiness

- `/healthz` проверяет только жизнь процесса;
- `/readyz` читает локальный snapshot;
- control-plane, provider, integration и interaction gateways не входят в
  Kubernetes readiness;
- недоступный рабочий сосед возвращает typed `Unavailable`;
- Kubernetes observation может использовать bounded LKG только при transport
  failure; digest/signature/revision conflict или expiry закрывают путь сразу;
- отказ и восстановление логируются один раз как переход состояния.

## Локальная проверка

```bash
cd services/internal/runtime-controller
GOWORK=off go test ./...
```

Deployment: `deploy/k8s/base/runtime-controller`. Нормативная архитектура:
`docs/architecture/runtime-controller.md`.
