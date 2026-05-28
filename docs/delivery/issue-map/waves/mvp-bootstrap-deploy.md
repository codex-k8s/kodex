---
doc_id: IM-CK8S-MVP-BOOTSTRAP-DEPLOY-0001
type: issue-map
title: "kodex — MVP bootstrap/deploy"
status: active
owner_role: EM
created_at: 2026-05-27
updated_at: 2026-05-27
---

# MVP bootstrap/deploy

| Issue/PR | Документы и артефакты | Срез | Статус | Связь |
|---|---|---|---|---|
| #888 / #890 | `bootstrap/**`, `deploy/base/bootstrap-foundation/**`, `deploy/base/bootstrap-builder-smoke/**`, `services.yaml`, `docs/platform/ops/bootstrap_cluster_runbook.md` | Первый bootstrap-контур кластера | готово | Локальный preflight/install path, internal registry foundation, Kaniko/mirror smoke и backend smoke orchestration. |
| #910 / #913 | `libs/go/stackinventory/**`, `cmd/manifest-render/**`, `bootstrap/**`, `scripts/lib/inventory.sh`, `docs/design-guidelines/**`, `docs/platform/ops/bootstrap_cluster_runbook.md` | Общий root stack inventory parser | готово | Корневой `services.yaml` читается через общий Go-пакет для render/install/deploy tooling; shell fallback ограничен legacy smoke/build wrappers. |
| #920 / #922 | `cmd/bootstrap-preflight/**`, `libs/go/manifestrender/**`, `libs/go/stackinventory/**`, `bootstrap/**`, `docs/platform/ops/bootstrap_cluster_runbook.md` | Safe preflight перед server deploy | готово | Local bootstrap preflight проверяет env names, stack inventory, dry-run render, kustomize и read-only Kubernetes prerequisites без установки и без раскрытия env values. |
