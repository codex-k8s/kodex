---
id: GUIDE-MC-005
title: Инфраструктурные изменения
type: guide
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Инфраструктурные изменения

## Source of truth

- Helm chart описывает packaged deployment.
- GitOps environment repository/overlays описывают desired deployment конкретной инсталляции.
- Runtime-created agent resources принадлежат runtime-controller и не коммитятся в Git.
- Secret values не хранятся в Helm values/Git; используются secret references.

## Kubernetes

- Stateless services имеют readiness/liveness/startup probes и минимум две replicas в production profile.
- Singleton work использует leader election/database claim.
- PDB, topology spread и graceful termination соответствуют topology инсталляции.
- Requests задаются по измерениям; CPU limits обычно не применяются к agent builds/runs без причины, memory limits защищают node от uncontrolled exhaustion.
- Namespace quotas не должны превращать ожидаемую очередь в permanent failure.
- ServiceAccounts разделены по platform services, builders и agent access profiles.

## Images

- Image build выполняется BuildKit.
- Immutable digest используется в manifests/runtime revisions.
- SBOM, scan, provenance и signature являются release gate.
- Kaniko и локальный single-PVC registry не являются production target.

## Scripts

Shell используется для короткого bootstrap/developer wrapper. YAML хранится в `deploy/**`. Go-код не содержит embedded shell workflows и Kubernetes manifests.

## Profiles

- `starter`: одна node/малый контур, ограниченная HA, bundled dependencies допустимы, но backup обязателен.
- `production`: external S3/OCI, HA PostgreSQL, две replicas stateless services, OIDC/RBAC, observability, tested restore.

## Change process

Infrastructure PR содержит rendered diff, migration order, rollout/rollback plan, capacity impact, security impact и ручную проверку. Deployment начинается только после проверки active agent turns по действующему operational rule.
