---
id: RUN-MC-006
title: Диагностика и восстановление internal-rpc-authority
type: runbook
status: approved
owner: sre
version: 1.1.0
updated: 2026-07-30
---

# Диагностика и восстановление internal-rpc-authority

## Когда применять

Runbook используется при отказе issuer/verifier readiness, отклонении snapshot,
ошибках replay persistence, недоступности reconciler, сбое Vault/PostgreSQL
credential lifecycle или перед контролируемой ротацией. Он не разрешает
production deploy: для применения production manifest требуется отдельный
owner gate.

Не выводить DSN, JWT/JWS payload целиком, projected token, password, private
JWK, certificate private key или содержимое Kubernetes Secret.

## Read-only preflight

1. Зафиксировать exact Git SHA и image digest.
2. Получить render той же версией кода:

```bash
scripts/render-internal-rpc-authority.sh \
  --environment staging \
  --image-ref ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:<digest> \
  > /tmp/internal-rpc-authority-staging.yaml
```

3. Проверить без значений Secret:

```bash
kubectl -n mattercodex-system get deploy,pod,job,svc,networkpolicy \
  -l app.kubernetes.io/name=internal-rpc-authority
kubectl -n mattercodex-system get endpoints \
  internal-rpc-authority-database-credential-reconciler
```

4. Сверить service account, pod UID/GID, image digest, volume names,
   `NetworkPolicy` selectors и exact destinations с render.
5. Проверить `/readyz` и bounded метрики через локальный port-forward. Не
   публиковать technical endpoint наружу.

## Классы отказа

### Telemetry не готова

Все runtime и восстановительные job закрыто отказываются стартовать без
доверенного OTLP TLS endpoint и файловой доставки Sentry DSN. Проверить:

- `OTEL_EXPORTER_OTLP_ENDPOINT` указывает ровно на
  `otel-collector.observability.svc:4317`;
- TLS SNI равен
  `otel-collector.observability.svc.cluster.local`, CA читается из
  `internal-rpc-authority-otel-ca`;
- host Sentry DSN равен `sentry-relay.observability.svc:8443`, DSN
  доставлен файлом из `internal-rpc-authority-sentry`;
- `NetworkPolicy` разрешает только соответствующие pod и namespace selectors,
  а не произвольный destination на портах `4317` или `8443`.

Не выводить Sentry DSN при диагностике. Проверять только имя Secret, file mode,
размер файла и совпадение ожидаемого host. Dashboard
`mattercodex-internal-rpc-authority` показывает served-state readiness,
ограниченные gRPC outcomes и p99 latency. Alerts
`InternalRPCAuthorityServedStateUnavailable`,
`InternalRPCAuthorityUnexpectedGRPCFailures` и
`InternalRPCAuthorityGRPCLatencyHigh` ведут в этот runbook.

При остановке OTel trace provider и Sentry flush получают независимые
ограниченные context. Исчерпание одного бюджета не отменяет второй cleanup.

### UDS не готов

- root обязан быть реальным каталогом `uid=29000`, `gid=29000`, mode `1770`;
- `issuer.sock` и `verifier.sock` должны быть socket, не symlink;
- listener UID должны быть соответственно `29001` и `29002`;
- application peer обязан иметь exact зарегистрированные UID/GID;
- volume — private pod-local `emptyDir`, не общий PVC/hostPath.

Удалять stale socket вручную внутри running pod запрещено. Перезапустить pod:
socket-init повторно проверит тип, владельца и mode, а runtime выполнит atomic
bind/rename.

### Snapshot отклонён

Сверить только metadata: source revision/digest, predecessor revision/digest,
key-set revision, policy revision, signer generation, `kid` и validity.
Проверить:

- manifest JWS подписан независимым root и canonical;
- exact workload имеет один `CURRENT`, bounded `NEXT`/`PREVIOUS`;
- issuer private key соответствует served public JWK;
- verifier не получает issuer private key;
- proof trust и role-specific readback possession key доставлены отдельно;
- PostgreSQL high-watermark не выше предлагаемой revision.

Same-revision mutation и rollback не обходить. При пропущенной revision
publisher обязан дать корректную predecessor/history chain либо workload
остаётся not ready.

### Replay/persistence недоступны

Проверить TLS `verify-full`, exact server name, `session_user`, `SET ROLE` и
доступность таблиц:

- `authority_snapshot_watermarks`;
- `authority_replay_reservations`;
- `authority_proof_watermarks`;
- `authority_proof_reservations`.

Не очищать watermark. Expired reservation удаляет только role-specific worker
после retention: issuer не имеет `DELETE` к verifier reservations, verifier не
имеет `DELETE` к issuer reservations. In-memory/`emptyDir` fallback запрещён.

### Reconciler не готов

Проверить:

- projected service-account token имеет audience `vault` и TTL 600 секунд;
- Vault доступен только по HTTPS с exact SNI и CA;
- PostgreSQL доступен только по TLS `verify-full`;
- active fenced lease принадлежит одной реплике;
- server-derived readback содержит ровно publisher/attestor
  `CURRENT`+`NEXT`;
- `session_user` совпадает с reconciler principal, capability активирована
  только через exact `SET ROLE`.

Secret value не копировать в env и не сравнивать в shell output.

## Контролируемая ротация

1. Подтвердить готовность `CURRENT` и доставку `NEXT`.
2. Проверить cryptographic readback всех фактически обслуживаемых workload.
3. Атомарно опубликовать следующий snapshot с predecessor digest.
4. Дождаться served-state readiness каждой реплики.
5. Перевести прежний `CURRENT` в bounded `PREVIOUS`.
6. После overlap и отсутствия active sessions установить runtime principal
   `RETIRED`, затем `NOLOGIN`, отозвать exact membership, выполнить Vault
   rotation и bounded drain.
7. Повтор старого JTI, старого signer generation и прежнего password обязан
   закрыто отклоняться.

После обновления TLS certificate, PostgreSQL DSN либо login principal выполнить
rolling restart соответствующего deployable. Обновлённый Kubernetes Secret не
считается readback: проверить фактически обслуживаемый certificate,
`session_user`, `current_user` после exact `SET ROLE` и readiness на каждой
новой реплике до удаления overlap.

Crash до publish не меняет served state. Crash после publish использует
PostgreSQL high-watermark и повторный exact readback. PITR/rollback,
понизивший state ниже внешнего anchor, оставляет unit not ready до
owner-authorized recovery.

## Миграции

Миграционный Job запускается до rollout и использует отдельный ServiceAccount и
DSN Secret. Перед staging выполнить:

```bash
make test-contract-authority-postgres
```

Если disposable PostgreSQL/DSN отсутствует, проверка считается `NOT RUN`, а не
успешной. Production CLI не предоставляет `down`: откат схемы выполняется
только новой компенсирующей forward migration после отдельного Issue,
проверенной резервной копии и owner gate.

## Откат

Application image можно вернуть на предыдущий проверенный digest только если
он понимает уже опубликованную contract version и его signer/policy revisions
не ниже persistent high-watermark. Snapshot, watermark, replay reservations и
credential generation назад не откатываются.

При несовместимости оставить workload not ready, остановить rollout и
восстановить новую совместимую версию. Удаление таблиц, сброс watermark,
повторное использование retired key/principal и plaintext/TLS-skip fallback
запрещены.
