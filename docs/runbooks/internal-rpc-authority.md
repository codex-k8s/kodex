---
id: RUN-MC-020
title: Диагностика internal RPC authority
type: runbook
status: approved
owner: sre
version: 2.4.0
updated: 2026-09-07
---

# Диагностика internal RPC authority

Runbook разрешает read-only диагностику publisher, issuer, verifier, readback
attestor и restore controller. Secret values, DSN и private keys не выводятся.

## Контракт

### Безопасная startup диагностика issuer/verifier (#1121)

CLI выводит только закрытые `mode`, `stage`, `domain_kind`, `error_class`,
`sqlstate` и `context_state`. Поля исходных ошибок, DSN, SQL, receipt contents,
credential, private paths и адреса соединений не печатаются. Неизвестные
значения нормализуются в `UNKNOWN`; отсутствие domain/SQL/context — `NONE`.
SQLSTATE выводится только из явного списка `runtime_diagnostics.go`, а не по
проверке длины произвольной строки. Не включать raw causes ради диагностики.

| Stage/исход | Значение и следующий разрешённый разбор |
| --- | --- |
| CONFIGURATION, OBSERVABILITY, POSTGRES | До snapshot lifecycle: проверить code-first config/mounts и разрешённый доступ зависимостей. |
| SNAPSHOT_LOAD, AUTHORITY_CONSTRUCTION, READBACK_CLIENT, RESTORE_CLIENT | Конструирование exact snapshot/domain и mTLS clients; не подменять файлы/доверие вручную. |
| RESTORE_VERIFY | Startup restore barrier не пройден; socket/workers ещё не открываются. |
| SNAPSHOT_ACTIVATION + SNAPSHOT_REJECTED | Owner SQL не принял restore fence, attestation receipt или watermark/history. Это не доказательство конкретного из трёх отказов; нужен авторитетный readback. |
| SNAPSHOT_ACTIVATION + PERSISTENCE_UNAVAILABLE + POSTGRES | Ошибка PostgreSQL statement/driver; allowlisted SQLSTATE различает permission/schema/constraint/serialization отказ без содержимого SQL. |
| RESTORE_OPEN | После snapshot activation повторный restore poll не разрешил работу; startup остаётся закрытым. |
| LOCAL_SERVERS, RUNTIME | Listener либо последующий runtime/shutdown fan-in; существующие bounded cancel/join и cleanup не меняются. |
| context_state=DEADLINE/CANCELED | Исчерпан startup/retry budget либо отменён caller. Первичный POSTGRES/SNAPSHOT_REJECTED/network/gRPC class сохраняется отдельно; deadline не доказывает сетевую первопричину. |

Причина #1121: `history.signer_generation` назначает publisher из manifest
signer, а `SnapshotState.signer_generation` loader берёт из CURRENT ключа
workload AUTHORIZATION_CONTEXT. Эти независимые поколения могут различаться.
Прежний trigger сравнивал их числовое равенство и возвращал 42501 даже при
действительных identity/receipt. Forward migration
`20260907000100_snapshot_workload_signer_boundary.sql` сохраняет exact
history revision/digest/key-set/policy и identity/attestation проверки, но
получает workload generation из public payload того же owner-published
`snapshot_compact_jws`. Нужны ровно один exact workload issuer и один CURRENT
ключ с purpose AUTHORIZATION_CONTEXT и ожидаемым generation. Отсутствие,
duplicate, иной purpose/generation и ошибка parser закрыто отклоняются.
Это не самостоятельное принятие произвольного JWS: строка history принадлежит
publisher owner, а независимый attestation receipt остаётся обязательным.

Семантика workload watermark, claims и replay не меняется; сохранённые строки
не переписываются. Миграция заменяет только функцию guard через штатный goose
up, не отключает trigger/RLS и не меняет старые migration bytes. Локальный
PostgreSQL regression воспроизводит старый 42501 для manifest7/workload1,
применяет forward migration и принимает тот же fixture; повторный up и
negative controls проверяются той же публичной оснасткой
`scripts/tests/internal-rpc-authority-postgres-test.sh`.

Live активация после исправления остаётся NOT RUN до owner-approved exact
deploy/readback issuer/verifier и зависимых сервисов. Нельзя сбрасывать durable
watermark, переигрывать receipt, обходить external restore barrier или открывать
readiness независимо от результата.

Локальная проверка без PostgreSQL/Docker/live из модуля authority:
`timeout 90s go test -race ./internal/app ./cmd/internal-rpc-authority-issuer ./cmd/internal-rpc-authority-verifier`.
Sentinel cases покрывают вложенные pgx/network/domain/gRPC ошибки, joined
deadline, неизвестные Kind/SQLSTATE/stage/mode и private paths. Отдельно
проверяется реальный config-failure выход `Run`; CLI обоих режимов использует
тот же safe formatter. `go vet` и `go build` выполняются в той же области.

### Controller shutdown companion (#1073)

Только у runtime-controller issuer и platform-worker-grant-agent являются
native sidecars (`initContainers`, `restartPolicy: Always`). Порядок:
socket-init → issuer startup `/readyz` → grant-agent startup `/readyz` → main.
Issuer проверяет PostgreSQL/replay/snapshot/restore barrier без обращения к
main или grant-agent. Grant-agent подписывает и атомарно пишет начальный
application grant без обращения к main или issuer; startup cycle отсутствует.
UID, limits/requests, secret mounts и полномочия не изменяются.

После main exit Kubernetes останавливает grant-agent, затем issuer. Main не
ожидает SIGTERM этих процессов, поэтому порядок не создаёт shutdown deadlock.
Сторонние owner grants и leadership не продлеваются: signer продолжает прежнюю
ротацию application grant, а owner RPC сохраняет точную проверку lease/fence.
Это позволяет controller выполнить bounded callback drain перед закрытием
локального issuer; runtime Pod provider/relay остаются обычными контейнерами
со своим протоколом drain, их перевод в native sidecars не выполняется.

Native sidecars стабильны с Kubernetes1.33; installer lock содержит
K3s `v1.36.3+k3s1`. Поддержка и обратный порядок остановки сверены через Context7
`/websites/kubernetes_io` и
[официальный контракт Kubernetes](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/).
Ресурсы постоянно работающих init sidecars учитываются вместе с main;
requests/limits не уменьшаются и остаются прежними.

Локально оба profile ABI render с negative image/restartPolicy и полный
local-role-image-render contract прошли. Первый local render выявил старый
selector проверки authority-image только в containers; после включения
initContainers повтор прошёл. Исторический FAIL не является runtime PASS.

`make test-internal-rpc-authority-abi-render` проверяет обе группы containers,
точный startup order/readiness/resources и отклоняет подменённый native issuer
image. Local render применяет hot reload, authority image annotation и
PostgreSQL guard также к init sidecars. Реальный Kubernetes SIGTERM и live
проверки пока NOT RUN; локальный render не выдаётся за runtime acceptance.
Откат только согласованный с controller/runner companion: прежний обычный
issuer снова может закрыться до receipt.

- installation material создаёт bootstrap roots, static PostgreSQL roles и
  начальные exact Kubernetes Secrets;
- `internal-rpc-authority-publisher` является единственным writer динамических
  key-delivery Secrets;
- publisher использует exact `resourceNames` RBAC, `resourceVersion` CAS,
  монотонную generation и readback после записи;
- issuer/verifier получают только собственные read-only Secret volumes;
- mTLS и signed authorization context обязательны одновременно;
- PostgreSQL сохраняет publication predecessor/history, readback receipts и
  replay high-watermark;
- snapshot, issuer, verifier и client adapter используют один
  `authority_abi_version=2`; несовместимая ABI закрыто блокирует readiness и
  рабочий RPC;
- unary RPC связан с deterministic protobuf request digest, а streaming RPC —
  с server-owned session binding;
- STT continuation наследует root actor/tenant/project и их provenance только
  из проверенного parent context. Issuer одной PostgreSQL-командой проверяет
  durable-accepted parent и резервирует deterministic child JTI;
- worker grant high-watermark сравнивает пару
  `(credential_generation, revision)` и переживает замену Pod.
- signed snapshot действует 180 дней. Следующая ревизия registry выпускается
  forward-only до окончания этого окна; повторно подписывать или изменять
  durable publication с тем же `source_revision` запрещено.

## Порядок fresh install

1. `kodex-postgresql-runtime-credentials` задаёт пароли закрытого списка LOGIN
   principals из `kodex-postgresql-runtime-credentials` Secret.
2. Завершаются `internal-rpc-authority-migrate` и `control-plane-migrate`.
3. Запускается publisher.
4. Installer ожидает обычные dynamic Secrets с generation больше нуля и точным
   набором keys из `tools/install/secret-projections.json`. Event-scoped
   restore credential и ACK до появления подписанной restore directive остаются
   пустыми owner-managed placeholders с generation `0`.
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

- generation обычной dynamic projection является положительным целым;
- event-scoped restore projection находится только в одном из двух состояний:
  пустой placeholder с generation `0` либо полностью материализованный Secret
  с положительной generation и точным набором keys;
- labels указывают writer `internal-rpc-authority-publisher`;
- Role publisher не имеет wildcard/list/delete Secret permissions;
- consumer ServiceAccount не имеет update/patch Secret;
- Snapshot и bootstrap roots имеют ожидаемые immutable/digest annotations;
- NetworkPolicy разрешает publisher только PostgreSQL, Kubernetes API и
  telemetry exact destinations;
- Pod `stt-tts-service` содержит issuer и verifier sidecars, label
  `kodex.dev/internal-rpc-authority-abi: "2"` и local socket readiness;
- issuer `CheckReadiness` возвращает ABI 2 до активации STT protected path.

## Типовые отказы

### Dynamic Secret остался generation 0

Для event-scoped restore credential и ACK это штатное состояние, пока restore
controller не выдал подписанную directive. Placeholder обязан оставаться
пустым; частичные data keys при generation `0` являются инцидентом.

Для любой другой dynamic projection проверить publisher logs без env/volumes,
PostgreSQL migration Job, publisher DSN Secret shape, Kubernetes API egress и
exact RBAC. Не заполнять Secret вручную и не расширять Role wildcard-правами.

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

### Snapshot истёк или близок к истечению

Проверить `published_at`, `source_revision` и `expected_readback_count` в
`authority_snapshot_history`, не выводя signed payload. Истёкшую ревизию не
удалять и не переподписывать. В registry publisher нужно выпустить следующую
последовательную `source_revision`, сохранить predecessor/history и выполнить
обычный exact-SHA deploy. Увеличение срока не восстанавливает уже подписанный
истёкший snapshot без новой forward-only ревизии.

Если новая ревизия остаётся `PREPARED`, сравнить закрытый набор readback по
`workload_id` и `role` с registry. Для `AUTHORITY_PROOF_RESOLVER` проверить без
вывода private material, что `current_generation` resolver key Secret совпадает
с поколением `CURRENT` записи доставленного proof trust. Verifier перечитывает
это поколение при каждой активации; статический env поколения не используется.

### ABI mismatch или continuation rejected

Сверить без вывода JWS, что snapshot, оба sidecar image и application adapter
собраны из одного exact SHA и обслуживают ABI 2. Проверить metadata Secret,
PostgreSQL migration Jobs и local socket readiness. Не понижать ABI и не
разрешать старый snapshot запасным путём.

Для `continuation rejected` проверить operation profile parent/child, exact
full method, request binding mode и факт durable acceptance parent context.
Parent JWS, child JWS, request digest и authority provenance в логи не
выводить. Повтор continuation с тем же parent/operation/request/correlation
должен приводить к тому же child JTI; конфликт digest является инцидентом, а не
поводом очистить replay tables.

### Worker grant rejected after credential rotation

Сверить `credential_generation` и `revision` в control-plane state без вывода
grant. Новая generation может начинать собственную последовательность revision;
меньшая generation либо меньшая revision внутри текущей generation является
rollback/replay. Не исправлять это очисткой high-watermark: выпустить новый
server-owned grant от актуальной credential generation.

## Проверки кода

```bash
(cd services/internal/internal-rpc-authority && GOWORK=off go test ./...)
make test-internal-rpc-authority-postgres
make test-authority-policy-codegen
make test-platform-worker-grant-workloads-contract
make test-internal-rpc-authority-abi-render
make test-web-only-release
buf format --diff --exit-code contracts/proto
buf build
git diff --check
```

Не запущенная проверка обозначается `NOT RUN`; наличие entrypoint не является
успешным результатом.
