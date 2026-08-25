---
id: RUN-MC-020
title: Диагностика internal RPC authority
type: runbook
status: approved
owner: sre
version: 2.0.0
updated: 2026-08-25
---

# Диагностика internal RPC authority

Runbook разрешает read-only диагностику publisher, issuer, verifier, readback
attestor и restore controller. Secret values, DSN и private keys не выводятся.

## Контракт

- installation material создаёт bootstrap roots, static PostgreSQL roles и
  начальные exact Kubernetes Secrets;
- `internal-rpc-authority-publisher` является единственным writer динамических
  key-delivery Secrets;
- publisher использует exact `resourceNames` RBAC, `resourceVersion` CAS,
  монотонную generation и readback после записи;
- issuer/verifier получают только собственные read-only Secret volumes;
- mTLS и signed authorization context обязательны одновременно;
- PostgreSQL сохраняет publication predecessor/history, readback receipts и
  replay high-watermark.

## Порядок fresh install

1. `kodex-postgresql-runtime-credentials` задаёт пароли закрытого списка LOGIN
   principals из `kodex-postgresql-runtime-credentials` Secret.
2. Завершаются `internal-rpc-authority-migrate` и `control-plane-migrate`.
3. Запускается publisher.
4. Installer ожидает все dynamic Secrets с generation больше нуля и точным
   набором keys из `tools/install/secret-projections.json`.
5. Только после этого запускаются остальные workloads.

## Read-only проверки

```bash
kubectl -n kodex-system rollout status deployment/internal-rpc-authority-publisher
kubectl -n kodex-system rollout status deployment/internal-rpc-authority-readback-attestor
kubectl -n kodex-system rollout status deployment/internal-rpc-authority-restore-controller
```

Для каждого динамического Secret проверяются только metadata и имена keys:

```bash
kubectl -n kodex-system get secret <name> -o json \
  | jq '{name:.metadata.name,generation:.metadata.annotations["kodex.dev/secret-generation"],keys:(.data|keys)}'
```

Дополнительно сверить:

- generation является положительным целым;
- labels указывают writer `internal-rpc-authority-publisher`;
- Role publisher не имеет wildcard/list/delete Secret permissions;
- consumer ServiceAccount не имеет update/patch Secret;
- Snapshot и bootstrap roots имеют ожидаемые immutable/digest annotations;
- NetworkPolicy разрешает publisher только PostgreSQL, Kubernetes API и
  telemetry exact destinations.

## Типовые отказы

### Dynamic Secret остался generation 0

Проверить publisher logs без env/volumes, PostgreSQL migration Job, publisher
DSN Secret shape, Kubernetes API egress и exact RBAC. Не заполнять Secret
вручную и не расширять Role wildcard-правами.

### Publisher готов, consumer не стартует

Сверить точные keys registry и `secret.items`, file mode, owner UID/GID и путь
mount. Исправлять registry/render, а не добавлять альтернативный env path.

### CAS conflict

Один publisher обязан повторно прочитать текущий `resourceVersion`, проверить
generation/digest и выполнить bounded retry. Несогласованный writer является
инцидентом; его нельзя лечить `--force-conflicts` в runtime.

### PostgreSQL identity rejected

Проверить `session_user`, членство `NOLOGIN` role и SCRAM reconciliation Job.
Runtime principal не должен получать superuser, `CREATEROLE` или bootstrap
password.

## Проверки кода

```bash
(cd services/internal/internal-rpc-authority && GOWORK=off go test ./...)
make test-internal-rpc-authority-postgres
make test-authority-policy-codegen
make test-web-only-release
buf format --diff --exit-code contracts/proto
buf build
git diff --check
```

Не запущенная проверка обозначается `NOT RUN`; наличие entrypoint не является
успешным результатом.
