---
id: OPS-MC-002
title: Профили развертывания
type: operations
status: proposed
owner: sre
version: 0.1.0
updated: 2026-07-16
---

# Профили развертывания

## Starter

Назначение: личный dogfooding, development, demo и малый single-node контур.

- один Kubernetes cluster/node допустим;
- bundled PostgreSQL/Mattermost допустимы;
- MinIO/local S3-compatible storage допустим;
- stateless services могут иметь одну replica;
- backup во внешнее хранилище обязателен;
- отсутствие HA и NetworkPolicy явно отображается как risk;
- upgrade и restore должны быть воспроизводимы.

Starter не называется HA/production-ready только потому, что работает в Kubernetes.

## Production

Назначение: коммерческая управляемая инсталляция.

- минимум две replicas stateless platform services;
- PostgreSQL HA/operator profile и PITR;
- external durable S3-compatible storage;
- external/HA OCI registry;
- OIDC/RBAC;
- leader election/database claims;
- PodDisruptionBudgets/topology policies;
- observability и alerts;
- регулярные backup/restore drills;
- signed immutable images;
- documented network/egress policy;
- tested upgrade/rollback.

## Managed nested cluster

Коммерческий вариант может выдавать клиенту отдельный nested/isolated cluster. Control plane deployment ownership, backup target, domain/TLS, registry и observability должны задаваться values/profile, а не кодом под одну инсталляцию.

## Configuration

Один Helm chart поддерживает profiles через schemas/values. Нельзя поддерживать starter и production разными неэквивалентными наборами ad-hoc manifests.
