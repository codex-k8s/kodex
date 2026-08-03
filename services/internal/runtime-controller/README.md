---
id: SVC-MC-005
title: Runtime controller
type: service
status: approved
owner: backend
version: 1.0.0
updated: 2026-08-03
---

# Runtime controller

`runtime-controller` — внутренний controller, который материализует уже
назначенный `control-plane` immutable `RuntimeRevision` в Pod, PVC, Secret и
ConfigMap. Он не запускает Codex, не меняет доменные настройки и не выводит
tenant, actor, profile доступа либо credentials из payload.

## Исполняемые режимы

- `runtime-controller` сверяет Kubernetes, capacity, heartbeat, terminal и
  retention chain;
- `runtime-archive` создаёт детерминированный versioned S3 archive и вызывает
  только `RecordRuntimeArchive` от identity `runtime-controller`;
- `runtime-restore-verifier` независимо скачивает exact S3 version, безопасно
  восстанавливает дерево и вызывает только `VerifyRuntimeRestore`;
- `runtime-cleanup-authorizer` запрашивает отдельную 15-минутную destructive
  authorization;
- `runtime-controller-cli migrate up|status|version` владеет forward-only
  миграцией durable inbox и локальной проекции.

Restore verifier и cleanup authorizer используют отдельные ServiceAccount,
SPIFFE, application grant и локальный authority issuer. Archive worker является
ограниченным дочерним процессом identity `runtime-controller`, поскольку
утверждённая `RecordRuntimeArchive` принадлежит controller; его executable не
содержит других lifecycle вызовов. Lease token хранится только в отдельном
Kubernetes Secret, не монтируется в role Pod и удаляется при terminal.

## Сверка

До `AdmitRuntimeExecution` создаётся устойчивый journal с server-owned
idempotency keys. Повтор после сбоя воспроизводит exact admission; другой
revision/input/fence закрыто отклоняется. Имя role Pod стабильно для
`role+thread+session`, а каждый turn получает новый Pod и immutable ConfigMap.
Session PVC сохраняется при replacement.
Bounded version-pinned rejoin `v..v+3` подхватывает cancel/retry/expiry и
удалённый commit, потерянный до локального journal patch; lineage и
version/fence delta сверяются закрыто.
Только лидер по Kubernetes Lease запускает durable consumer и mutation loops;
потеря лидерства отменяет и join-ит workers до закрытия клиентов.

Для `PROJECT_READ_ONLY` controller проверяет server-managed project namespace
по exact organization/project annotations и создаёт execution-scoped
ServiceAccount/RoleBinding. `CLUSTER_ADMIN` использует такой же отдельный
ServiceAccount и отдельный ClusterRoleBinding; terminal transition отзывает их
до удаления Pod.

Warm Pod удаляется после 4 часов простоя, не затрагивая PVC. Удаление PVC
допустимо не ранее 7 суток простоя и только после versioned S3 upload с `ChecksumSHA256`,
успешного `HeadObject`, независимого restore proof и актуальной cleanup
authorization. Pod/PVC/Secret удаляются с precondition `UID+resourceVersion`.
PVC ограничен 30 GiB; archive/restore Jobs получают явный ephemeral-storage
request/limit, достаточный для compressed archive и восстановленного дерева.
Vault предоставляет только ключи `access-key-id` и `secret-access-key` без
значений в manifests: обе identity имеют bucket/versioning read; archive
identity — Put/Head/Get своего archive prefix, restore identity — Get archive
и Put/Head своего proof prefix. Bucket обязан иметь versioning `Enabled`;
`VersionId=null` отклоняется.
Terminal journals без archive/proof автоматически продолжаются после restart;
неавторитетный legacy PVC без execution/journal остаётся `inventory-only`.
Для общего session PVC archive/cleanup выполняет только journal, указанный в
server-owned retention-owner annotation; новый admitted turn переносит owner
optimistic update, поэтому старый turn не может удалить более новое состояние.

## Быстрая локальная проверка

```bash
cd services/internal/runtime-controller
gofmt -w .
go vet ./...
go test ./...
go build ./...
```

Итоговый environment render создаётся без сохранённых Kubernetes API адресов:

```bash
KUBERNETES_API_CIDRS="$(scripts/resolve-kubernetes-api-endpoint-cidrs.sh)"
KUBERNETES_API_PORTS="$(scripts/resolve-kubernetes-api-endpoint-cidrs.sh --output ports)"
scripts/render-runtime-controller.sh \
  --environment staging \
  --controller-image-ref mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/runtime-controller@sha256:<digest> \
  --authority-image-ref ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:<digest> \
  --kubernetes-api-cidrs "$KUBERNETES_API_CIDRS" \
  --kubernetes-api-ports "$KUBERNETES_API_PORTS" \
  > /tmp/runtime-controller-staging.yaml
```

Render-команда fail-closed требует exact Service/EndpointSlice CIDR и заменяет
оба нулевых digest. Выполнение render не является deploy.

## Ручная проверка владельца

1. Сверить `RuntimeExecution`/`RuntimeRevision` и immutable journal без чтения
   значений Secret.
2. Создать turn и убедиться, что Pod, PVC и ConfigMap несут один exact
   execution/revision/input/fence tuple, а lease Secret не смонтирован в Pod.
3. Завершить Pod и проверить последовательность archive → restore proof →
   cleanup authorization → guarded delete → consume.
4. Имитировать crash между journal/admit/materialize и подтвердить rejoin без
   второго Pod.
5. Проверить capacity defer, LRU idle eviction, node pressure, 4-часовой TTL
   Pod и отдельную семисуточную отсрочку PVC.
6. Проверить `/readyz`, метрики, dashboard, alerts и отсутствие произвольных
   metric labels.

Применение migration, render или Kubernetes ресурсов требует отдельного
владельческого шлюза; этот unit сам deploy не выполняет.
