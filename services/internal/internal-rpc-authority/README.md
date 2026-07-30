---
id: SVC-MC-003
title: Внутренний сервис internal-rpc-authority
type: service
status: approved
owner: developer
version: 1.1.0
updated: 2026-07-30
---

# Внутренний сервис internal-rpc-authority

`internal-rpc-authority` — общий security unit для короткоживущего
межсервисного authorization context. Он не владеет пользователями, ролями,
проектами и бизнесовыми permissions: authority приходит только в подписанном
proof от домена-владельца и связывается с заранее утверждённой machine policy.

## Runtime-сценарий

1. Владелец доменного состояния разрешает actor, tenant и project внутри своей
   boundary и выдаёт ES256 authority proof.
2. Приложение вызывает локальный
   `/internalrpcauthority.v1.AuthorizationIssuerService/IssueAuthorizationContext`
   через `/run/mattercodex/internal-rpc-authority/issuer.sock`.
3. Issuer проверяет Linux `SO_PEERCRED`, exact operation binding, proof,
   provenance и одноразовый `jti`, затем подписывает context текущим workload
   key.
4. Приложение передаёт context по mTLS downstream RPC. mTLS и context
   обязательны одновременно.
5. Target application передаёт локальному verifier фактически проверенные
   downstream SPIFFE и full RPC. Verifier повторно проверяет JWS, exact
   workload/RPC/audience/permission, TTL и persistent replay reservation.
6. PostgreSQL атомарно фиксирует snapshot watermark и одноразовый receipt.

Поля обычного request, произвольные identifiers клиента и сам mTLS не являются
authority.

## Состав

- `internal-rpc-authority-socket-init` создаёт private UDS root с
  `uid=29000`, `gid=29000`, mode `1770`;
- `internal-rpc-authority-issuer` слушает только именованный issuer UDS;
- `internal-rpc-authority-verifier` слушает только именованный verifier UDS;
- `internal-rpc-authority-publisher` публикует подписанные snapshot, normal
  readback credentials и role-specific restore credentials из versioned
  target registry;
- `internal-rpc-authority-readback-attestor` выдаёт persistent single-use
  challenge и атомарно сохраняет immutable attestation receipt;
- `internal-rpc-authority-restore-controller` координирует
  `OPEN → QUIESCING → PREPARED → RESTORING → COMPLETED`, а отдельные
  `restore-operator` и `restore-recovery` исполняют owner-triggered command и
  server-side fence recovery;
- `internal-rpc-authority-database-credential-reconciler` поддерживает
  server-derived `CURRENT`/`NEXT` PostgreSQL principals publisher и readback
  attestor через Vault Kubernetes auth;
- `internal-rpc-authority-cli migrate expand|contract|up|status|version`
  выполняет forward-only goose lifecycle без штатного rollback; полный `up`
  закрыто отклоняется, пока не завершён `expand`.

Issuer и verifier загружают подписанный canonical snapshot через независимый
manifest trust root. Обновление сначала проходит полную криптографическую
проверку, persistent forward-only CAS и served-state readback; только затем
atomic pointer переключает рабочий RPC на новую immutable модель. Отклонённая
проекция закрывает рабочий путь и readiness, не заменяя ранее проверенную
модель.

## Конфигурация

Обязательные runtime значения:

| Переменная | Назначение |
| --- | --- |
| `INTERNAL_RPC_AUTHORITY_WORKLOAD_ID` | Точный workload из capability registry |
| `INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE` | Файл DSN; значение не помещается в env |
| `INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME` | Exact TLS SNI/hostname |
| `INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER` | Exact login principal текущего поколения |
| `INTERNAL_RPC_AUTHORITY_SNAPSHOT_JWS_FILE` | Подписанный authority snapshot |
| `INTERNAL_RPC_AUTHORITY_MANIFEST_ROOT_PUBLIC_JWK_FILE` | Immutable bootstrap public key независимого manifest root |
| `INTERNAL_RPC_AUTHORITY_MANIFEST_TRUST_BUNDLE_JWS_FILE` | Forward-only подписанный manifest signer trust bundle |
| `INTERNAL_RPC_AUTHORITY_VAULT_AUTH_FILE` | Projected audience-bound Vault token; значение читается только из файла |
| `INTERNAL_RPC_AUTHORITY_WORKLOAD_CERTIFICATE_FILE` | Exact workload mTLS certificate для attestor/controller |

Issuer дополнительно требует
`INTERNAL_RPC_AUTHORITY_CONTEXT_PRIVATE_JWK_FILE` и
`INTERNAL_RPC_AUTHORITY_PROOF_TRUST_JWK_FILE`. Пути имеют безопасные defaults,
соответствующие Kustomize components. Значения DSN, token и private keys нельзя
передавать через env или выводить в лог.

Normal readback credential, possession key, restore role credential и restore
ACK key issuer/verifier читают непосредственно из exact Vault KV paths,
разрешённых только их workload role. Caller не передаёт path, workload,
role, generation, audience или TTL. Publisher signer material и attestor/
controller trust snapshots доставляются через namespaced Vault
`SecretProviderClass` с audience `vault`, exact TLS SNI и
`vaultSkipTLSVerify=false`.

Тайм-ауты и bounded polling задаются
`INTERNAL_RPC_AUTHORITY_STARTUP_TIMEOUT`,
`INTERNAL_RPC_AUTHORITY_READINESS_TIMEOUT`,
`INTERNAL_RPC_AUTHORITY_SHUTDOWN_TIMEOUT` и
`INTERNAL_RPC_AUTHORITY_SNAPSHOT_RELOAD_INTERVAL`. Очистка только истёкших
одноразовых reservations использует
`INTERNAL_RPC_AUTHORITY_REPLAY_CLEANUP_INTERVAL` и фиксированный retention:
issuer удаляет только authority-proof reservations, verifier — только
authorization-context reservations. Persistent high-watermark не удаляется.

Reconciler дополнительно использует exact mTLS certificate/client CA,
PostgreSQL identity, Vault HTTPS/SNI/CA, projected Vault audience token и
immutable source revision/digest capability registry. Kubernetes names и пути
зафиксированы в `deploy/k8s/base/internal-rpc-authority`. PostgreSQL DSN каждого
процесса обязан ссылаться на CA-файл из отдельного read-only ConfigMap mount и
использовать `sslmode=verify-full`.

## Локальная проверка

```bash
make test-contract-authority
make test-internal-rpc-authority
make test-internal-rpc-authority-deploy
```

PostgreSQL integration запускается отдельно только с disposable DSN:

```bash
make test-contract-authority-postgres
```

Для release-render необходим фактически опубликованный immutable image:

```bash
scripts/render-internal-rpc-authority.sh \
  --environment staging \
  --image-ref ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:<digest>
```

Zero digest в base является намеренным fail-closed placeholder источника и
никогда не проходит release-render.

Issuer/verifier подключаются к workload через Kustomize components. Component
добавляет sidecar, PodMonitor и exact UDS/Secret mounts, но не может задать
`NetworkPolicy` отдельному контейнеру: policy выбирает весь pod. Поэтому
consumer unit обязан включить issuer/verifier PostgreSQL, DNS и Prometheus
направления в свой итоговый default-deny render. Отсутствие этих exact правил
является блокирующей ошибкой consumer deploy, а не поводом расширять egress.

## Наблюдаемость и lifecycle

`/livez`, `/readyz` и `/metrics` доступны только на technical listener.
Readiness проверяет тот же persistent served snapshot/replay path, который
использует рабочий RPC. gRPC metric labels ограничены registry методов и
canonical codes; произвольные значения нормализуются.

Каждый runtime и восстановительная job создаёт OTel trace provider, включает
server/client gRPC spans и `otelpgx` hooks без SQL/connection details.
Структурные `slog` records получают `trace_id`/`span_id`. OTLP разрешён только
к `otel-collector.observability.svc:4317` с TLS 1.3, exact SNI и отдельной CA.
Sentry DSN читается только из read-only файла и допускает только
`sentry-relay.observability.svc:8443`; прямой internet egress запрещён.
Dashboard, alerts и абсолютный `runbook_url` входят в тот же deployable unit.

При `SIGTERM` readiness закрывается до остановки, затем независимо и в
ограниченные сроки завершаются gRPC, workers, technical HTTP и PostgreSQL.
Обновление mounted Secret само по себе не доказывает смену TLS/DSN:
credential или certificate rotation завершается rolling restart и повторной
проверкой фактически обслуживаемой identity/readiness. Порядок диагностики и
восстановления приведён в `RUN-MC-006`.
