---
id: SVC-MC-005
title: Runtime controller
type: service
status: approved
owner: backend
version: 2.2.0
updated: 2026-08-07
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
- `runtime-credential-broker snapshot|s3-archive|s3-restore` — unprivileged
  mTLS client, передающий signed workload ticket отдельной authority;
- `runtime-credential-broker serve-snapshot|serve-s3-archive|serve-s3-restore`
  — раздельные trusted materializer/exchangers с server-derived spec,
  STS tags/policy, immutable one-time receipt и exact readback;
- `runtime-credential-broker copy-credential` — execution-scoped one-time Pod
  с 600-секундным Kubernetes token и Role только на exact source/destination;
- `runtime-workload-admission` — fail-closed TLS admission webhook с
  Ed25519 full-tuple verification, exact immutable Pod spec readback и
  PostgreSQL one-time replay receipt;
- `runtime-controller-cli migrate up|status|version` — forward-only схема
  durable inbox/projection.

Каждый worker имеет отдельные ServiceAccount, SPIFFE/application grant и
per-job `Role` только на exact journal. Controller, долговечный materializer и
routine broker Jobs не читают Secret и не создают/bind-ят `cluster-admin`
authority. Materializer может только создавать fail-closed
admission-ограниченные execution resources; Secret читает и создаёт one-time
copy identity, а full Pod/Secret spec и replay проверяет независимый webhook.
Vault/STS разделены между
archive/restore exchangers. Vault выдаёт broker только public keys с именем
`public-key.hex` (раздельные admission/archive/restore verifier),
`kms-key-arn`, `archive-role-arn` и
`restore-role-arn`; значения в manifests,
документацию и диагностику не выводятся.
Authority server Secrets `runtime-workload-materializer-tls`,
`runtime-s3-archive-exchanger-tls`, `runtime-s3-restore-exchanger-tls` и
client Secrets `<broker-service-account>-mtls` содержат только ключи
`ca.pem`, `tls.crt`, `tls.key`; server/client certificates имеют раздельные
SPIFFE identities и не переиспользуются между archive и restore.

## Reconcile и recovery

`ClaimRuntimeExecution` сначала фиксирует authoritative PostgreSQL `PENDING`.
Сразу после него создаётся journal с исходным tuple и idempotency keys — до
revision hydration, capacity и следующего Kubernetes effect. Если Create
journal не состоялся, новый server-owned claim key возвращает только тот же
exact session/turn/attempt/grant/revision/input `PENDING` без второго перехода.
После crash/defer controller повторно получает exact RuntimeRevision,
проверяет immutable organization/provider quota snapshot и replay-ит тот же
`AdmitRuntimeExecution` до материализации PVC, ConfigMap, credential broker или
Pod. Для restore эта owner transaction сверяет current operation/generation с
durable revoke watermark и одноразово потребляет generation. До journal/PVC/Pod
controller отдельно расходует durable `KUBERNETES_MATERIALIZATION` effect slot,
а `runtime-s3-restore-exchanger` непосредственно перед Vault/STS — отдельный
`S3_CREDENTIAL` slot через exact mTLS/application grant/issuer profile. Оба пути
при каждом replay снова сверяют current generation/digest и watermark; signed
restore ticket принимается только в `ADMITTED`, а Bind — только для уже consumed
current generation. Lease token восстанавливается только из owner receipt
и не сохраняется в Kubernetes Secret, ConfigMap, annotation, log или metric.

Для target Agent-графа Issue #234 поля `RoleID` и `PromptProfileID` указывают
не на изменяемые legacy `RoleSpec`/`PromptProfileSpec`, а на server-owned
immutable derived projection control-plane. Runtime-controller читает её тем же
`GetResource(kind,id,expected_version)` path; control-plane сверяет exact
source Agent/InstructionSet version и digest и не предоставляет projection
никакого mutation API. `ProviderCredentialBindingID` остаётся непустым и
указывает на выбранный из exact ProviderPool snapshot действующий
`CredentialBinding`; поэтому существующая materialization и credential
validation не получают caller fallback и не требуют второго авторитетного
источника.
Derived Prompt содержит exact CLEAN content Artifact ID/version/digest;
`ClaimRuntimeExecution` возвращает его тем же materialization contract как
`AGENTS.md`. `RuntimeRevision.Components` при этом ограничен уже принимаемыми
runtime-controller kinds, а target Agent/RoleDefinition/InstructionSet/Pool/
Assignment tuple остаётся top-level version-pinned readback и входит в
effective digest.

Этот consumer contract не означает готовность Mattermost producer #235:
runtime-controller не выполняет Team/bot effect, не подписывает
`ProviderEffectReadbackReceipt` и не вызывает mapping/bot RPC control-plane.
Эти signer/call-site/readiness обязанности остаются в interaction-gateway
Issue #235 после rebase на принятый #234.

Capacity учитывает durable pending/admitted/running journals, Pod requests,
organization limit, server-owned provider binding и свежую observation,
`ResourceQuota` и node pressure. Unknown/stale остаётся queued. Старейший
доказанно terminal idle Pod можно удалить один раз и пересчитать допуск; PVC
не затрагивается.

Role image запускает `agent-runner runtime-session`. Источник
`mattercodex.agent-runner-input.v2` содержит exact execution/revision/input,
Session/Turn/attempt, provider binding, Codex policy, materializations,
control-plane/interaction-gateway/MCP TLS bindings и пути immutable execution
credentials. Для повторной доставки частично опубликованного terminal owner
добавляет exact `codex_delivery_recovery_source_execution_id`; локальный
journal без этого server-owned marker либо marker без retained journal закрыто
останавливают запуск до provider effect. Kubernetes materialization начинается
только с `ADMITTED` owner readback и lease. Restore ticket дополнительно
проверяется независимым пересчётом полного private source tuple; digest из
snapshot не принимается как доказательство сам по себе. Identity
runtime-controller не подменяет runner.
Terminal Pod phase не завершает turn: runner обязан записать signed v2 envelope
в controller-owned ConfigMap. Exit без handoff создаёт incident.
Role Pod становится Ready только после materialization, Turn claim,
RuntimeExecution admission и MCP initialize по
тому же TLS 1.3/mTLS exact SNI/CA/client certificate + bearer пути, который
использует turn. Периодический readback и каждый successor повторяют
barrier; credential digest либо peer mismatch снимает readiness до claim.

Successor использует новый execution-scoped Pod и свежие immutable
RuntimeRevision, ConfigMap, authority/credential snapshots; retained session
PVC сохраняет Codex state. Старые mounted credentials и MCP client не
переиспользуются. Прямой Kubernetes access profile runner закрыто запрещён:
кластерные действия проходят только через специализированные MCP boundaries.
Каждый role Pod получает отдельный `runtime-access-*` ServiceAccount и exact
handoff Role/RoleBinding; общая identity между параллельными execution
запрещена. Vault role `internal-rpc-authority-agent-runner` допускает только
этот префикс в `mattercodex-system`. Удаление Pod проверяет exact lineage и с
UID/resourceVersion preconditions удаляет execution-scoped ServiceAccount и
handoff RBAC.

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
До успешного `Secret Create` durable credential effect отсутствует: crash
безопасно получает новый short-lived STS grant. `AlreadyExists` и lost response
проходят action-specific one-time readback Pod с exact `resourceNames`, UID,
`resourceVersion`, tuple, digest и expiry. Long-lived exchanger не имеет
`secrets/get`, а controller читает только immutable owner receipt.
Vault bootstrap role может только вызвать `AssumeRole`/`TagSession` для
закреплённой archive либо restore execution role. Только trusted action
exchanger выводит inline policy и session tags из owner-signed ticket и
передаёт их в exact STS `AssumeRole` через TLS endpoint S3 boundary;
unprivileged broker не получает generic STS authority.
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
scripts/render-runtime-controller.sh \
  --environment staging \
  --controller-image-ref mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/runtime-controller@sha256:<digest> \
  --authority-image-ref ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:<digest> \
  --registry-pull-host registry-pull.<environment-domain> \
  --kubernetes-api-cidrs "$KUBERNETES_API_CIDRS" \
  --kubernetes-api-ports "$KUBERNETES_API_PORTS" \
  > /tmp/runtime-controller-staging.yaml
```

Renderer fail-closed требует exact Service/EndpointSlice CIDR/ports и внешний
node-reachable pull DNS, материализует его как exact escaped repository в
`ValidatingAdmissionPolicy`, затем добавляет отдельные policy для controller,
workers и access profiles.
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
