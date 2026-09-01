---
id: OPS-MC-005
title: Резервное копирование и восстановление
type: operations
status: approved
owner: sre
version: 2.3.0
updated: 2026-08-29
---

# Резервное копирование и восстановление

## Текущий статус

`backup-controller` реализован отдельным deployable unit и включён в основной
`web-only` профиль. Controller выполняет ограниченный во времени
crash-consistent logical backup PostgreSQL, инвентаризацию immutable artifact
bodies, независимую проверку, защищённый retention и owner-gated restore drill по
[#1003](https://github.com/codex-k8s/kodex/issues/1003).

Независимые PostgreSQL и S3 не образуют общую атомарную snapshot-транзакцию.
Manifest schema v2 поэтому фиксирует модель `BOUNDED_CRASH_CONSISTENT`, точные
границы общего окна и время начала/завершения каждого database snapshot. Backup
принимается только после проверки, что все receipts входят в это окно, а все
объекты читаются обратно по exact version, ETag, размеру и SHA-256. Это не
является обещанием одной причинно-согласованной точки между разными БД и S3.

Production использует заранее подготовленный `backup-controller-credentials`
и exact external egress policy. Local hot-reload автоматически материализует
тот же schema-v1 Secret из существующих локальных PostgreSQL credentials и
`kodex-external-s3`, хранит backup в отдельном versioned bucket
`kodex-backups` и принимает readback только после появления verified backup.

## Авторитетные данные

Резервное копирование поддерживаемой установки покрывает:

- отдельные logical snapshots PostgreSQL control-plane и internal RPC authority
  с точными границами времени внутри одного bounded consistency window;
- долговечный NATS stream либо возможность безопасно восстановить его из
  PostgreSQL outbox/event store без двойного эффекта;
- immutable artifact bodies во внешнем S3 вместе с exact object
  keys/versions/ETags/digests из PostgreSQL;
- OCI role images вместе с provenance, SBOM, scan, signature и promotion
  receipts;
- platform configuration и release lock;
- PostgreSQL Keycloak с realm, users, roles и OIDC clients;
- зашифрованная owner-копия `.kodex-material`.

Отдельно от online backups владелец хранит checksum и минимум две зашифрованные
offline-копии `.kodex-material`. Ключ расшифрования не входит в backup этой же
установки. Backup Kubernetes etcd не заменяет owner material: для полноценного
восстановления нужны оба независимых источника.

Optional Mattermost принадлежит владельцу этой внешней системы и не является
источником истины Kodex. External message/post IDs восстанавливаются
только как locator metadata.

Session JSONL archive/restore не входит в backup controller и реализуется
отдельным unit [#1002](https://github.com/codex-k8s/kodex/issues/1002).

## Целевой контракт восстановления #1003

1. Создать изолированное новое окружение.
2. Восстановить точный набор PostgreSQL snapshots из одного immutable manifest,
   проверить их schema/version/digest и все S3 receipts. До завершения
   междоменного readback внешний трафик к восстановленному контуру не открывать.
3. Восстановить Keycloak PostgreSQL и проверить exact realm/client/role
   readback до открытия административных UI.
4. Восстановить `.kodex-material`, проверить checksum и материализовать
   Kubernetes Secrets без вывода значений.
5. Восстановить immutable S3 artifact bodies и OCI artifacts, проверить
   version/digest/readback.
6. Развернуть exact release lock сначала для инфраструктуры, затем migrations,
   domain services, gateways/jobs и Control Center.
7. Выполнить bootstrap readback: Organization, owner claim, system assistant,
   capabilities и definitions не должны дублироваться.
8. Проверить warm assistant, web-only Run, event sequence, artifact download и
   только затем optional integrations.

Restore не редактирует baseline migration и не запускает legacy backfill.
Повреждение signature, rollback revision, digest mismatch или истечение key
закрывают авторизацию немедленно.

Проверенной считается только резервная копия, для которой выполнено отдельное
restore drill с зафиксированными RPO/RTO и безопасным уничтожением тестовой цели.
