---
id: RUN-MC-024
title: Backup, retention и restore drill
type: runbook
status: approved
owner: sre
version: 1.4.0
updated: 2026-09-01
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
