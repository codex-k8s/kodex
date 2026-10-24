---
id: RUN-MC-024
title: Backup, retention и restore drill
type: runbook
status: approved
owner: sre
version: 1.5.2
updated: 2026-10-25
---

# Backup, retention и restore drill

Runbook не разрешает deploy, удаление backup, создание production database или
запуск restore без отдельного owner OK. Secret values, DSN, passwords, access
keys, session tokens, TLS private keys и содержимое dump не выводить.

## Контракт Secret

Deployment читает только key `credentials.json` Kubernetes Secret
`backup-controller-credentials`. JSON schema version 1 содержит
`destination`, непустые `databases` и `objectStores`. S3 entry содержит имена
полей `name`, `endpoint`, `region`, `bucket`, `prefix`, `accessKeyId`,
`secretAccessKey`, optional `sessionToken`, `usePathStyle`; database entry —
`name`, `host`, `port`, `database`, `user`, `password`, optional `role`,
`tlsMode`, `tlsServerName`, `caFile`, optional client certificate paths,
`schemaKind` и optional `declaredSchemaVersion`. Значения хранятся только в
Secret и здесь не документируются.

One-shot restore дополнительно читает:

- `repository.json` Secret `backup-controller-repository`, содержащий schema
  version 1 и только backup `destination`;
- `targets.json` owner-created Secret `backup-controller-restore-targets` с
  новыми database names и отдельным пустым versioned S3 target;
- `approval.json` owner-created Secret `backup-controller-restore-approval` с
  schema version 1, `approvalId`, `restoreId`, exact `backupId`,
  `targetSetSha256` и `expiresAt` не дальше 24 часов.

CA и client TLS material также монтируются из Kubernetes Secret. Production
конфигурация с HTTP S3, plaintext PostgreSQL, неточным SNI или отсутствующей CA
закрыто отклоняется.

Base NetworkPolicy разрешает только cluster-local PostgreSQL и SeaweedFS
fixture. Production overlay обязан материализовать
`external-egress-networkpolicy.template.yaml` с exact destination CIDR для S3
и, при необходимости, отдельной restore database; wildcard egress запрещён.

Основной `web-only` профиль включает production overlay controller. Для
локального hot-reload `dev.sh up` собирает exact OCI image из штатного
Dockerfile, материализует `backup-controller-credentials` без вывода значений и
использует `kodex-artifacts` и `kodex-session-archives` как независимые source
stores, а `kodex-backups` — как repository. Оба source сохраняются в одном
immutable manifest с исходными store name, bucket, key, version и digest;
отсутствие любого из них делает backup неполным.
`tools/dev/deploy-local.sh` проверяет точное содержимое Secret по digest,
rollout Deployment и появление verified backup через `/status`.

## Read-only проверка

1. Проверить `/healthz`, `/readyz`, `/status` и `up{service="backup-controller"}`.
2. Сверить возраст `kodex_backup_controller_last_successful_backup_timestamp_seconds`,
   счётчики `backup_runs_total`, `database_actions_total`,
   `object_actions_total` и `retention_runs_total`.
3. В versioned repository проверить наличие exact `manifest.json` и
   `verification.json` одного `backupId`. Manifest допустим только со
   `state=complete`, совпадающими counts и exact receipts всех dump/schema/S3
   copies. Не считать `attempt.json` или `failure.json` успешным backup.
4. Проверить, что source bucket и backup bucket имеют versioning `Enabled`, а
   platform-owned source objects содержат metadata `kodex-sha256`.
5. Для retention убедиться, что хотя бы один сохранённый backup имеет валидный
   `restore-drills/<restoreId>.json`. При его отсутствии outcome `protected` —
   ожидаемый закрытый отказ удаления.

## Owner-gated restore drill

1. Владелец выбирает exact verified `backupId` и готовит новые отсутствующие
   target database names. Использовать source database, существующую database
   или непустой S3 target prefix запрещено.
2. SRE создаёт Secret targets без вывода значений и запускает image командой
   `fingerprint-targets`. Владелец независимо сверяет target set и выпускает
   отдельный approval Secret с полученным digest и коротким expiry.
3. После отдельного owner OK SRE материализует
   `deploy/k8s/base/backup-controller/restore-drill-job.template.yaml`, заменяет
   только exact image/release placeholders и уникальное имя Job, затем запускает
   один Job. Параллельный backup/retention закрыто блокируется S3 operation lock.
4. Успех принимать только при `Complete` Job и immutable restore drill receipt,
   где совпадают approval, request digest, target digest, database schema
   versions, object counts, sizes и SHA-256. Проверить рост
   `restore_drills_total{outcome="success"}` при доступном scrape one-shot job.
5. Восстановленные database и S3 objects остаются отдельными. Controller не
   переключает traffic, не меняет source и не очищает target; cleanup требует
   отдельного owner-approved кодового действия.

Partial restore создаёт terminal `failure.json`; повтор с тем же `restoreId`
запрещён. После устранения причины владелец выдаёт новый approval, новый
`restoreId` и новый пустой target.

## Disposable local restore drill

В каноническом `./dev.sh full-e2e --context <exact-local-context>` фаза
`backup-and-disposable-restore-drill` использует только уже verified backup из
`/status`. Wrapper `scripts/tests/local-backup-restore-e2e.sh` создаёт уникальные
новые PostgreSQL database names и пустой prefix bucket
`kodex-restore-fixture`, вычисляет digest штатной командой
`fingerprint-targets`, выпускает короткоживущий local approval и запускает
существующий restore-drill Job с exact digest уже развернутого image.

Успех требует `Complete` Job, существования обеих восстановленных БД,
immutable restore receipt, совпадения числа exact object versions и receipt.
После readback wrapper удаляет созданные exact object versions и disposable
database, не меняя source backup. Все credential material находится во
временных файлах без group/world permissions (`0400` для immutable restore
config и `0600` для runtime credential files) и удаляется trap; stdout не
содержит значений.

Entrypoint отклоняет production-like context, неверные namespace labels,
неприватный kubeconfig/state directory и запуск без явного
`KODEX_E2E_CONFIRM_DISPOSABLE`. Нет verified backup, API path, receipt или
авторитетного readback — фаза `FAIL`; mandatory local профиль не подменяет это
conditional skip.

## Отказы

- `backup repository operation is already locked`: до `expiresAt` это закрытый
  отказ параллельной операции. При наступившем `expiresAt` следующий contender
  читает и валидирует immutable lock, удаляет только его exact `VersionId`,
  подтверждает отсутствие этой версии и повторяет conditional put. Если между
  delete и put другой contender уже получил lock, операция остаётся
  заблокированной. Невалидный документ, ошибка exact delete или readback не
  разрешают ручное или автоматическое снятие lock: требуется расследование.
- `immutable S3 object already exists`: не перезаписывать key и не отключать
  version checks; выяснить повтор operation ID или постороннюю запись.
- `backup verification receipt is unavailable`: backup не является restore
  point. Исправить dependency и запустить новый backup либо explicit `verify`
  для того же immutable manifest.
- `restore target database already exists` или `restore S3 target is not empty`:
  не очищать target автоматически; подготовить новый target и новый approval.
- PostgreSQL tool failure: сверить server major 17/18, TLS, роль backup,
  свободное ephemeral storage и timeout. stderr намеренно не содержит payload.

## Rollback

Вернуть Deployment на прежний exact image digest. Backup objects, manifests,
verification, restore receipts и operation locks не переписывать. Retention
остановить до подтверждения хотя бы одного сохранённого verified restore point.


## Одноразовый data drill #1003/#1830

Это отдельный путь без `serve`, только для текущего disposable `k3d-kodex`.
Оператор — root с уже выданным разрешением full QA + backup/restore; запуск
агентом-ревьюером запрещён. Программа фиксирует source namespace UID и labels
`staging/hot-reload`, не принимает `.env`, PAT, пароль или произвольный context.
Это проверка data/schema restore, не готовности disaster recovery платформы.
Контур с 1GiB RAM не подходит: здесь одновременно возможны два Pod с limits
1GiB RAM/2CPU/16GiB ephemeral каждый; первый backup ограничен 8GiB на dump и
15 минутами. До запуска нужны размер исходных БД/максимального объекта и
свободное место с запасом для target data/WAL, а не только размер сжатого dump.

### Custody и границы полномочий

`tools/dev/scoped_backup_drill.py` читает у точного source S3 Secret только
`endpoint`, `region`, `bucket`, `access-key`, `secret-key`; роль `backup`
дополнительно читает `kodex-postgresql-runtime-credentials/kodex_backup_reader`.
Роль `repository` пароль БД не читает. Exact decoded UTF-8 bytes сохраняются,
включая LF. Credentials передаются единственному дочернему kubectl через stdin;
в файлах plan/INTENT/receipt и stdout их нет. Secret создаётся immutable,
create-only, без `apply`, last-applied annotation, overwrite или усыновления
существующего объекта. Source UID/RV повторно проверяются до эффекта; INTENT
fsync предшествует create, POST UID сравнивается с readback UID.

**S3 credential-level isolation не доказана.** Историческая fixture identity
имеет shared Admin/Read/List/Tagging/Write; новая проекция не сужает её права по
bucket/prefix. Пустой уникальный prefix, exact receipts и NetworkPolicy —
ограничения инструмента, не least-privilege principal. До live root проверяет
фактический scope и решает допустимость текущего разрешения. Этот unit не
изменяет shared SeaweedFS identities/global auth и не выдаёт ключи ИИ-моделям.
Если требуется scoped principal, prerequisite — отдельная repo-owned
материализация существующего SeaweedFS identity config с read-only двумя
source prefixes и read/write/exact-delete QA repository/target prefixes,
проверками запрета записи source и доступа вне prefix; до её review/readback
операции BLOCKED. Подменять отсутствующие права shared admin key нельзя.

HTTP используется только для exact существующей локальной fixture
`seaweedfs-s3.kodex-system.svc.cluster.local:8333`, как в `deploy-local.sh`;
другой endpoint отклоняется, HTTPS не понижается. PostgreSQL — verify-full с
exact SNI/CA; новый target server разрешает TLS1.3. Runtime SA не получает
Kubernetes API token/RBAC. Оператору нужны get названных Secrets (Kubernetes
RBAC не ограничивает отдельные keys), create named Secrets/Jobs и собственных
namespace/PG/Certificate/NetworkPolicy, read metadata/Pod status и UID delete.

### Матрица жизненного цикла

| Этап | Владелец / проверка | Результат / отказ |
| --- | --- | --- |
| Plan | Root transport, exact namespace UID, source Secret UID/RV | Только метаданные, SHA плана; без credential read |
| Custody apply | Approved plan SHA, source drift, target absence | Один immutable Secret и exact receipt; нет доменного события |
| Lost ACK / crash | Durable INTENT, прежний target name | Только readback exact UID; новый пароль/create не повторяются |
| Expired approval после cleanup ACK loss | Действующие storage read credentials и полные immutable backup/drill/approval/target/request pins | Исторический read-only inspection разрешён; cleanup/restore требуют fresh approval |
| Backup | Один Job, новый QA repository prefix, backoff0 | Manifest complete + verification либо FAIL; завершение Pod недостаточно |
| Verify | Exact backup ID, versioned repository | Полный payload readback; repeated verification не создаёт новую S3 version |
| Target create | Новый namespace UID/emptyDir/credentials/TLS | Собственный PG, без source admin или installation private key |
| Target preflight | DNS/TLS/auth, все DB отсутствуют, bucket versioned/prefix empty | Read-only EMPTY_TARGETS_READY, без lock/intent/DB writes |
| Fingerprint / approval | Owner exact target digest, backup/restore/approval IDs, expiry<=2h | Immutable approval, не источник новых runtime grants |
| Restore | Existing approved runner, immutable manifest/target, operation lock | Exact drill receipt после schema/object checks; partial failure terminal |
| Full target readback | Exact expected complete object/version set | Missing-one/all → FAIL, не measured zero успеха |
| Cleanup / unknown delete | Только receipt-owned versions; explicit cleanup mode | Exact conditional delete, unknown → отдельный cleanup-only readback |
| Resource cleanup | Terminal consumers, owner labels и server UID precondition | Удалён только собственный ресурс; NotFound отдельно от ERROR/чужой замены |

Границы domain events, существующих backup/restore алгоритмов и migration
schema не меняются. Новые команды — операторский CLI, не public API и не
расширение роли помощника.

### Порядок выполнения root

Все файлы ниже — в owned каталоге0700 с umask077; ID/пути/образ — несекретные
операторские параметры. `QA_SOURCE` должен указывать на чистый принятый SHA;
`BACKUP_IMAGE` — на digest его заново собранного/опубликованного image.
Старый образ, в котором ещё нет новых CLI-команд, не подходит.

```sh
umask 077
: "${QA_SOURCE:?clean accepted QA worktree required}"
: "${QA_STATE:?owned 0700 directory required}"
: "${DRILL_ID:?fresh lowercase id, at most 32 characters}"
: "${QA_PREFIX:?fresh qa-1830- prefix required}"
: "${BACKUP_IMAGE:?registry.local.kodex/kodex/backup-controller@sha256 digest required}"
: "${S3_SOURCE_SECRET:?exact CP object-storage volume Secret ref required}"
CLI="$QA_SOURCE/tools/dev/scoped_backup_drill.py"
```

1. Сверить actual serving source namespace UID/labels; получить S3 source ref
   только из `deployment/control-plane` volume `object-storage`. Проверить
   фактический S3 scope по правилу выше, grants backup reader на **обе** БД,
   versioning источников/repository/target и размеры. В trusted-cluster IRA
   grants reconciler условны; отсутствие прав не заменять исключением БД.
   Публичный CA ConfigMap `backup-controller-postgresql-ca/ca.pem` должен быть
   непустым и соответствовать текущему PostgreSQL CA; сверить public CA hash
   с рабочим `control-plane-postgresql-ca`, не читать installation private key.
   При mismatch остановиться и штатно исправить только этот Bundle/CA path.
2. Publication — только stable build/verify/JS plan/apply/readback и реальный
   `publish-stable-image-worker.sh`. После registry-only PASS проверить **node
   pull** новым Job через `render-pull`. Он использует default SA без token,
   не имеет volumes/env/envFrom, запускает только проверку UID10001 и наличия
   исполняемого backup-controller. Pull всегда выполняется для exact digest;
   timeout120s/backoff0, root filesystem read-only, capabilities сняты.

```sh
python3 "$CLI" render-pull --run "$DRILL_ID" --image "$BACKUP_IMAGE" \
  --output "$QA_STATE/pull-job.json"
PULL_UID=$(kubectl --context k3d-kodex create -f "$QA_STATE/pull-job.json" -o 'jsonpath={.metadata.uid}')
python3 "$CLI" capture --manifest "$QA_STATE/pull-job.json" --run "$DRILL_ID" \
  --expected-uid "$PULL_UID" --output "$QA_STATE/pull-job-receipt.json"
kubectl --context k3d-kodex -n kodex-system wait --for=condition=complete \
  "job/qa-pull-$DRILL_ID" --timeout=150s
```

   Сохранить Job/Pod UID и exact imageID; этот успех ещё не backup PASS.
   После terminal Job удалить его через `cleanup-resource --receipt
   "$QA_STATE/pull-job-receipt.json"`, подтвердить `readback-absence` с тем же
   receipt. При UNKNOWN create сначала только metadata readback/capture UID,
   без повторного create. Новые SA, NetworkPolicy и глобальные ресурсы не нужны.
3. Подготовить обе custody роли. Пример для `backup`; повторить с
   `--role repository` и отдельными файлами plan/result. SHA берётся только из
   результата plan, не пересчитывается после редактирования.

```sh
python3 "$CLI" custody-plan --role backup --run "$DRILL_ID" \
  --object-storage-secret "$S3_SOURCE_SECRET" --output "$QA_STATE/backup-plan.json"
python3 "$CLI" custody-apply --plan "$QA_STATE/backup-plan.json" \
  --approved-plan-sha "$BACKUP_PLAN_SHA" --output "$QA_STATE/backup-custody.json"
```

При UNKNOWN не повторять `custody-apply`. Root получает UID только metadata
read и вызывает `custody-readback --plan ... --approved-plan-sha ...
--expected-uid ... --output ...`. Foreign UID/labels/source drift закрывают
результат. Для неизвестного restore-input create использовать отдельный
`restore-inputs-readback --intent <receipt>.intent --expected-uid ...`:
пароль не генерируется повторно, exact bytes проверяются по durable digest.

4. Материализовать/сверить namespace-owned SA backup-controller и две штатные
   NetworkPolicy из base; не применять весь kustomization с Deployment/serve.
   Render первого Job:

```sh
python3 "$CLI" render-backup --run "$DRILL_ID" --image "$BACKUP_IMAGE" \
  --prefix "$QA_PREFIX" --output "$QA_STATE/backup-job.json"
JOB_UID=$(kubectl --context k3d-kodex create -f "$QA_STATE/backup-job.json" -o 'jsonpath={.metadata.uid}')
python3 "$CLI" capture --manifest "$QA_STATE/backup-job.json" --run "$DRILL_ID" \
  --expected-uid "$JOB_UID" --output "$QA_STATE/backup-job-receipt.json"
kubectl --context k3d-kodex -n kodex-system wait --for=condition=complete \
  "job/backup-controller-$DRILL_ID" --timeout=1000s
```

Новый prefix обязателен: команда backup вызывает Retain после Backup; его
scope не должен включить прежний каталог. Keep1000/minimumAge3650d дополнительно
защищают свежие копии. Job не имеет HTTP/status и не включает periodic serve.

5. Для каждой команды создать отдельный Job через `render-readback --operation
list-verified|inspect-backup|verify|inspect-restore-drill`, с теми же image/prefix
и `--backup-id`, а для inspect-restore-drill также `--restore-id --approval-id
--target-sha`. Create/capture/wait выполнять как выше. Логи этих CLI содержат
только allowlisted JSON, сохранять private; raw stderr failure bundle не выводить.
`list-verified` обязан завершить EOF (8 pages×512 /16 manifests maximum),
вернуть complete=true и ровно один backup ID нового prefix. Empty/partial/FAIL
не считаются backup. `inspect-backup` сверяет manifest/verification и выдаёт
counts/sizes/schema/provenance; controllerVersion/releaseRevision должны
совпасть с принятым source/image. Затем отдельный verify обязан PASS. Replay
EnsureVerification использует conditional Put и проверяет прежнюю receipt,
не перезаписывает verification.json новой версией.

6. Локально render изолированного контура:

```sh
python3 "$CLI" render-restore --run "$DRILL_ID" --image "$BACKUP_IMAGE" \
  --prefix "$QA_PREFIX" --output "$QA_STATE/restore.json"
```

До create сохранить отсутствие namespace `kodex-qa-restore-$DRILL_ID`.
`.resources` создаются **по одному в указанном порядке**, каждая create
возвращает UID и немедленно получает `capture --manifest <этот-object.json>
--run ... --expected-uid <POST UID> --output <отдельный-receipt.json>`.
Не усыновлять AlreadyExists и не продолжать после неизвестного create.
Порядок: Namespace → default-deny → SA → fresh SelfSigned Issuer/Certificate →
role-stub ConfigMap → exact network policies → Service → disposable PG.
Source namespace получает только additive exact SeaweedFS ingress для этого
namespace/run. Ни source PostgreSQL ingress, ни installation CA private key
не копируются. Stubs покрывают policy TO refs обеих repo migration chains,
все NOLOGIN/без членств и прав; unknown role останавливает render preflight.
Это **не** восстановление authority/RLS runtime actors.

7. Сохранить namespace POST UID. Затем один атомарный target input Secret:

```sh
python3 "$CLI" restore-inputs --plan "$QA_STATE/repository-plan.json" \
  --approved-plan-sha "$REPOSITORY_PLAN_SHA" --render "$QA_STATE/restore.json" \
  --namespace-uid "$RESTORE_NAMESPACE_UID" --output "$QA_STATE/restore-inputs.json"
```

Он содержит новый случайный PG password и только repository/targets; source
PG admin не читается. Дождаться Certificate Ready и PG Available. На **новом
PG Pod** root проверяет image pin/platform imageID, `id -u`=70 и только
`stat -Lc '%u:%g:%a' /var/run/tls/tls.key`=0:70:440. Ключ не читать.
PG18 разрешает root-owned group-readable key; readonly fsGroup70 сохраняет
0440. Source pin/UID70 дополнительно проверен офлайн по слою actual OCI с тем
же PostgreSQL18.3-alpine3.23 base; это не заменяет node readback.

8. Из `.preflightJob` создать/capture/wait один read-only Job. Он проверяет
   DNS/TLS/auth обоих target DB servers, supported PG version, отсутствие всех
   target DB, S3 target versioning и пустой prefix **до** restore effects.
   Только EMPTY_TARGETS_READY разрешает продолжить. Из `.fingerprintJob`
   создать/capture/wait Job; его JSON содержит только targetSetSha256.
   Не печатать targets/password files.
9. Root сверяет exact backup/target и выпускает local approval максимум2h:

```sh
python3 "$CLI" render-approval --run "$DRILL_ID" --backup-id "$BACKUP_ID" \
  --target-sha "$TARGET_SHA" --expires-at "$APPROVAL_EXPIRES_UTC" \
  --output "$QA_STATE/approval.json"
```

Approval Secret create-only/capture; затем `.restoreJob` create/capture/wait.
Прежний restore runner повторяет свои checks и требует immutable success
receipt. Partial failure — FAIL и сохранение диагностики; исходный namespace,
DB и S3 objects не переключаются/не изменяются.
10. `.targetInspectionJob` должен PASS с RESTORED_CONTENT_PRESENT и полным
    exact set/count. Отдельный `inspect-restore-drill` сверяет expected
    approval/target/request/manifest digests/schema/counts. Missing-one/all —
    FAIL. Сохранить safe evidence/RPO/RTO до cleanup.
11. `.cleanupJob` удаляет только exact receipt-owned versions в фиксированном
    kodex-restore-fixture/e2e-restore-* и подтверждает пустой prefix. Foreign
    version → отказ до первого delete; UNKNOWN → только
    `.cleanupReadbackJob` (inspect-restore-cleanup). EMPTY здесь означает
    отсутствие после cleanup, **не** восстановление данных; REMAINING требует
    решения root по уже прочитанному состоянию, не blind retry.
    Истечение approval не блокирует `inspect-restore-targets` и
    `inspect-restore-cleanup`: это чтение только при совпадении полных
    исторических immutable pins и текущем доступе к storage. Неверные
    approval/backup/restore/target/request pins по-прежнему отклоняются.
    Исторический decoder не разрешает повтор delete, restore или deployment;
    изменение требует действующего approval и повторной проверки freshness
    на границе runner до lock или внешнего эффекта.
12. После terminal Jobs удалить/capture-owned PG Deployment с server UID
    precondition, дождаться исчезновения PG Pod, затем input Secrets, target
    namespace, source additive ingress и source one-shot/readback Jobs/Secrets
    через `cleanup-resource --receipt ...`; каждый подтверждается
    `readback-absence --receipt ...`. Namespace с чужими consumers и активные
    secret consumers закрывают cleanup. Backup manifests/verification/drill
    receipts сохраняются. Shared foundation SA/policies не удалять как
    временную инфраструктуру. Operation lock вручную не снимать.

### Локальные проверки и пределы

```sh
make test-scoped-backup-drill
GOTOOLCHAIN=go1.26.6 GOMAXPROCS=2 \
  go -C services/jobs/backup-controller test -p 1 ./...
```

Первый target — hermetic custody/unknown/UID/no-leak/render/release-pin,
без Docker/кластера. Go unit включает bounded cursor/EOF, immutable verify
replay, owner metadata, missing target и separate cleanup semantics. Это не
live PASS. После нового SHA нужен полный environment render, node pull и
описанный фактический drill. Миграции/Proto/OpenAPI не меняются.

Target PostgreSQL имеет только data/schema и NOLOGIN role stubs; owners/ACL,
runtime identities, Keycloak, OCI/provenance, installation material,
bootstrap/services/PWA и пользовательские сценарии этим не восстановлены.
Полный DR остаётся отдельным acceptance #1003/#1031; успешный scoped drill
не позволяет закрыть его целиком.

Проверены официальные [PG18 SSL rules](https://www.postgresql.org/docs/18/ssl-tcp.html),
[SelfSigned issuer](https://cert-manager.io/docs/configuration/selfsigned/) и
[kubectl UID DELETE body](https://github.com/kubernetes/kubectl/blob/v0.35.0/pkg/cmd/delete/delete.go).
Context7 недоступен (Invalid API key); credential не искался и не менялся.
