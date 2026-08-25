---
id: OPS-MC-005
title: Резервное копирование и восстановление
type: operations
status: approved
owner: sre
version: 2.0.0
updated: 2026-08-25
---

# Резервное копирование и восстановление

## Авторитетные данные

Резервное копирование поддерживаемой установки покрывает:

- PostgreSQL control-plane и internal RPC authority с согласованной точкой WAL;
- долговечный NATS stream либо возможность безопасно восстановить его из
  PostgreSQL outbox/event store без двойного эффекта;
- bounded artifact content и, при подключении внешнего object storage, exact
  object versions/digests;
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

## Восстановление

1. Создать изолированное новое окружение.
2. Восстановить PostgreSQL до единой согласованной точки и проверить baseline.
3. Восстановить Keycloak PostgreSQL и проверить exact realm/client/role
   readback до открытия административных UI.
4. Восстановить `.kodex-material`, проверить checksum и материализовать
   Kubernetes Secrets без вывода значений.
5. Восстановить immutable OCI artifacts и проверить digest/readback.
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
