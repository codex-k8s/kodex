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
| #910 / #913 | `libs/go/stackinventory/**`, `cmd/manifest-render/**`, `bootstrap/**`, `scripts/lib/inventory.sh`, `docs/design-guidelines/**`, `docs/platform/ops/bootstrap_cluster_runbook.md` | Общий root stack inventory parser | готово | Корневой `services.yaml` читается через общий Go-пакет для render/install/deploy tooling; shell fallback ограничен legacy local Docker build wrappers. |
| #920 / #922 | `cmd/bootstrap-preflight/**`, `libs/go/manifestrender/**`, `libs/go/stackinventory/**`, `bootstrap/**`, `docs/platform/ops/bootstrap_cluster_runbook.md` | Safe preflight перед server deploy | готово | Local bootstrap preflight проверяет имена env, stack inventory, dry-run render, kustomize и Kubernetes prerequisites только на чтение без установки и раскрытия значений env. |
| #929 | `cmd/bootstrap-deploy-plan/**`, `bootstrap/host/plan_backend_deploy.sh`, `bootstrap/host/smoke_backend_contour.sh`, `libs/go/manifestrender/**`, `libs/go/stackinventory/**`, `docs/platform/ops/bootstrap_cluster_runbook.md` | Dry-run план первого backend deploy | готово | План только на чтение проверяет MVP deploy inventory, рендерит PostgreSQL/event-log/foundation/backend manifests, выполняет `kubectl kustomize` и live foundation checks без `kubectl apply`, jobs, push образов и раскрытия значений env. |
| #941 | `cmd/bootstrap-backend-deploy/**`, `bootstrap/host/deploy_backend_ring.sh`, `bootstrap/host/smoke_backend_contour.sh`, `bootstrap/README.md`, `docs/platform/ops/bootstrap_cluster_runbook.md` | Реальный deploy первого backend-кольца | готово | Локальный deploy применяет registry foundation, подготавливает Kubernetes `Secret`, собирает first-ring images через Kaniko, применяет PostgreSQL, platform-event-log migrations, `access-manager`, `project-catalog`, `package-hub`, `provider-hub` и проверяет rollout/readyz без раскрытия значений env. |
| без отдельного Issue | `scripts/**`, `bootstrap/host/smoke_backend_contour.sh`, `docs/design-guidelines/**`, `docs/platform/ops/bootstrap_cluster_runbook.md`, `docs/domains/**/ops/**`, `docs/delivery/issue-map/**` | Очистка shell-проверок | готово | Доменные shell-проверки удалены из активного пути; `smoke_backend_contour.sh` остался тонкой обвязкой `deploy_backend_ring.sh --skip-build`, PostgreSQL shell runners сохранены как Make target runners, а доменные/live проверки переведены в политику Go tests или Go integration runner. |
