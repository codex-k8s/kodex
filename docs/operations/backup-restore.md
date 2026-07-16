---
id: OPS-MC-005
title: Backup и восстановление
type: operations
status: proposed
owner: sre
version: 0.1.0
updated: 2026-07-16
---

# Backup и восстановление

## Данные

Backup contract покрывает:

- MatterCodex PostgreSQL;
- Mattermost PostgreSQL;
- S3 buckets с attachments, artifacts, instructions, session archives и Mattermost files;
- GitOps/Helm desired configuration;
- Kubernetes resources, которые нельзя воспроизвести из Git/control plane;
- encrypted bootstrap/recovery material для secret backend;
- release/image metadata.

## PostgreSQL

Production profile использует CloudNativePG или эквивалентный оператор с base backups и WAL archive во внешнее object storage. PITR восстанавливает новый cluster, после чего выполняются integrity/application checks.

Reference: https://cloudnative-pg.io/documentation/current/recovery/

## Kubernetes

Velero может сохранять Kubernetes objects и нужные volume snapshots/filesystem backups. Он не заменяет application-aware PostgreSQL PITR и S3 versioning.

Reference: https://velero.io/docs/main/how-velero-works/

## S3

- Versioning и lifecycle policies включены согласно retention.
- Backup bucket/account отделен от runtime credentials.
- Object checksums и inventory сверяются с DB metadata.
- Удаление production objects защищено policy/MFA/immutability там, где доступно.

## Secrets

Обычный application backup не содержит raw secret dump. Secret backend имеет отдельный encrypted recovery process с минимальным кругом доступа и проверкой восстановления.

## Restore drill

Минимум раз в квартал для production profile:

1. развернуть изолированный target;
2. восстановить DB до выбранного времени;
3. восстановить/подключить S3 objects;
4. применить GitOps configuration;
5. проверить users/workspaces/agents/sessions/artifacts/schedules;
6. выполнить безопасный agent smoke без production mutations;
7. зафиксировать фактические RPO/RTO и замечания;
8. уничтожить drill environment по runbook.

Backup без успешного restore drill не считается проверенным.
