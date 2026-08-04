---
id: SVC-MC-005
title: Runtime controller
type: service
status: approved
owner: backend
version: 1.3.0
updated: 2026-08-04
---

# Runtime controller

`runtime-controller` материализует назначенный `control-plane` immutable
`RuntimeRevision` в session PVC, immutable ConfigMap/credential projections и
role-image Pod. Он не запускает Codex, не меняет доменные настройки и не
выводит tenant, actor, access profile или credentials из payload.

## Исполняемые режимы

- `runtime-controller` — durable claim/admission recovery, capacity,
  Kubernetes reconcile, heartbeat, runner handoff, retention и inbox;
- `runtime-archive` — детерминированный versioned S3 archive от отдельной
  identity `runtime-archive`;
- `runtime-restore-verifier` — независимый exact-version restore proof;
- `runtime-rehydrate` — fail-closed восстановление очищенной session в новый
  PVC до создания Pod;
- `runtime-cleanup-authorizer` — owner-locked bounded cleanup claim;
- `runtime-credential-broker snapshot|s3-archive|s3-restore` — проверка
  signed workload ticket, execution-owned credentials/RBAC/Pod и раздельные
  Vault bootstrap leases с последующим exact execution `AssumeRole`;
- `runtime-workload-admission` — fail-closed TLS admission webhook с
  Ed25519 full-tuple verification, exact immutable Pod spec readback и
  PostgreSQL one-time replay receipt;
- `runtime-controller-cli migrate up|status|version` — forward-only схема
  durable inbox/projection.

Каждый worker имеет отдельные ServiceAccount, SPIFFE/application grant и
per-job `Role` только на exact journal. Controller не читает Secret, не
создаёт role Pod/ServiceAccount/RoleBinding и не создаёт/не bind-ит
`cluster-admin` authority. Vault выдаёт broker только public keys с именем
`public-key.hex` (раздельные admission/archive/restore verifier),
`kms-key-arn`, `archive-role-arn` и
`restore-role-arn`; значения в manifests,
документацию и диагностику не выводятся.

## Reconcile и recovery

`ClaimRuntimeExecution` сначала фиксирует authoritative PostgreSQL `PENDING`.
Сразу после него создаётся journal с исходным tuple и idempotency keys — до
revision hydration, capacity и следующего Kubernetes effect. Если Create
journal не состоялся, новый server-owned claim key возвращает только тот же
exact session/turn/attempt/grant/revision/input `PENDING` без второго перехода.
После crash/defer controller повторно получает exact RuntimeRevision,
проверяет immutable organization/provider quota snapshot и replay-ит тот же
`AdmitRuntimeExecution`. Lease token восстанавливается только из owner receipt
и не сохраняется в Kubernetes Secret, ConfigMap, annotation, log или metric.

Capacity учитывает durable pending/admitted/running journals, Pod requests,
organization limit, server-owned provider binding и свежую observation,
`ResourceQuota` и node pressure. Unknown/stale остаётся queued. Старейший
доказанно terminal idle Pod можно удалить один раз и пересчитать допуск; PVC
не затрагивается.

Role image запускает `agent-runner runtime-session`. Источник
`mattercodex.agent-runner-input.v1` содержит exact execution/revision/input
tuple, отдельные control-plane session/turn и server-owned bot
`AgentSession`/`AgentSessionTurn`/`RunID`, profile, HTTPS bot-service/MCP routes
с exact SNI/CA/client identity и пути immutable execution credential
snapshots. Control-plane UUID никогда не используется как bot `session_key`.
Bot-service сам разрешает `RunID` в локальные AgentSession/Turn и версии,
сохраняет durable binding outbox и доставляет generated
`BindRuntimeAgentSession` через mTLS+bearer; lost response повторяет тот же
idempotency intent.
Terminal Pod phase не завершает turn: runner обязан записать
exact handoff в собственный Pod. Exit без handoff создаёт incident.

Warm Pod допускается только `Running`+`Ready`, с тем же server-owned
`effective_runtime_sha256` и открытым archive gate. Successor получает свежие
immutable RuntimeRevision, ConfigMap и authority/credential snapshots; runner
через exact handoff Role читает новые execution-owned Secret в `0700` tmp
staging, проверяет tuple/snapshot/purpose и закрывает gate. Старые mounted
credentials не становятся authority successor. Несовместимый Pod заменяется.
`CLUSTER_ADMIN` Pod не прогревается.

## Session archive и cleanup

Archive замораживает quiesced PVC в неизменяемый CSI clone, открывает дерево
fd-relative с `openat2` `BENEATH|NO_SYMLINKS`, отклоняет symlink, hardlink и
device и только затем строит deterministic tar. S3 использует KMS encryption,
COMPLIANCE Object Lock не менее 90 суток, exact execution key
`archive.tar.gz`, SHA-256 и ненулевой `VersionId`;
exact `HeadObject` и `GetObjectRetention` подтверждают version/checksum/
provenance/mode/retain-until на новом и idempotent existing path.
Archive, restore и rehydrate используют разные short-lived execution/action
STS Secrets, выдаваемые после проверки подписанного workload ticket: immutable
Secret UID/resourceVersion, exact session tags, inline
policy/readback digests и срок не более 15 минут входят в credential snapshot.
Vault bootstrap role может только вызвать `AssumeRole`/`TagSession` для
закреплённой archive либо restore execution role; inline policy и session tags
передаются уже в exact STS `AssumeRole` через TLS endpoint S3 boundary.
IAM source запрещает List/Delete/Bypass, cross-tenant prefix и insecure
transport; startup проверяет versioning, Object Lock, KMS, public-access block
и фактический запрет List.
Archive и restore используют независимые Ed25519 issuer/audience/public trust,
ServiceAccount, Vault bootstrap role/config и разрешённый target role ARN;
bootstrap identity одной операции не может assume role другой.

PVC cleanup не использует journal `LastTransition` и не имеет собственного
TTL. Authoritative owner transaction pin-ит `ResourceRetentionPolicy` id,
version, durations, `pvc_cleanup_eligible_at` и `archive_retain_until` при
terminal transition. Authorizer под session/full graph lock проверяет именно
этот снимок, terminal graph, archive/proof и отсутствие work/hold, затем pin-ит
`ACTIVE` claim к exact PVC name/UID/resourceVersion на 15 минут и блокирует
новый claim. Controller удаляет PVC с UID/resourceVersion preconditions,
подтверждает `NotFound` и idempotent finalize передаёт exact timestamp и proof
digest. Client grace и delete-before-claim отсутствуют.

Следующий turn после `CONSUMED` cleanup получает одноразовое owner assignment
к exact source archive. `runtime-rehydrate` bind-ит assignment к новой пустой
PVC generation/name/UID/resourceVersion, восстанавливает во временное дерево
на том же filesystem, sync-ит regular files/marker/directories снизу вверх и
parent вокруг atomic rename `session/`, после чего
owner переводит assignment в `CONSUMED`. Повтор для live наполненной PVC или
другого UID закрыто отклоняется; crash оставляет только удаляемый staging, но
не частичное final tree. Role Pod до proof не создаётся.

## Быстрые проверки Prototype

```bash
cd services/internal/runtime-controller
gofmt -w .
go vet ./...
go test ./...
go build ./...
```

Итоговый environment render не сохраняет Kubernetes API маршруты в base:

```bash
KUBERNETES_API_CIDRS="$(scripts/resolve-kubernetes-api-endpoint-cidrs.sh)"
KUBERNETES_API_PORTS="$(scripts/resolve-kubernetes-api-endpoint-cidrs.sh --output ports)"
: "${RUNTIME_PROVIDER_EGRESS_CIDRS:?exact provider endpoints are required}"
scripts/render-runtime-controller.sh \
  --environment staging \
  --controller-image-ref mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/runtime-controller@sha256:<digest> \
  --authority-image-ref ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:<digest> \
  --kubernetes-api-cidrs "$KUBERNETES_API_CIDRS" \
  --kubernetes-api-ports "$KUBERNETES_API_PORTS" \
  --provider-egress-cidrs "$RUNTIME_PROVIDER_EGRESS_CIDRS" \
  > /tmp/runtime-controller-staging.yaml
```

Renderer fail-closed требует exact Service/EndpointSlice CIDR/ports и выданные
environment provider `/32|/128` endpoints, затем добавляет отдельные policy для
controller, workers, access profiles и provider TLS/443.
Render и deploy в этом unit не выполняются.

## Ручная проверка владельца

1. Сверить `PENDING` journal до revision/capacity и crash recovery через тот же
   idempotency tuple без lease Secret.
2. Проверить exact runner input, handoff, archive-gated warm reuse и incident
   при выходе Pod без handoff.
3. Проверить capacity defer для organization/provider stale/limit и oldest-idle
   eviction без PVC delete.
4. Завершить turn и проследить archive → restore proof → owner cleanup claim →
   exact delete/NotFound → consume.
5. После cleanup запустить новый turn и подтвердить rehydrate exact S3 version
   в новый PVC до Pod start.
6. Убедиться, что routine controller не создаёт admin binding/Secret, archive
   identity отделена, а S3 List/Delete/cross-tenant закрыты.
7. Проверить `/readyz`, exact StreamInfo/ConsumerInfo, inbox blockage/dead-letter
   metrics, dashboard и alerts с абсолютными `runbook_url`.

Миграция, render, применение Kubernetes ресурсов и deploy требуют отдельного
владельческого шлюза.
