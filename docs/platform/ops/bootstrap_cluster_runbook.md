---
doc_id: RB-CK8S-BOOTSTRAP-CLUSTER-0001
type: runbook
title: "kodex — Runbook: bootstrap кластера"
status: active
owner_role: SRE
created_at: 2026-05-27
updated_at: 2026-05-27
related_alerts: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-27-bootstrap-cluster-slice"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-27
---

# Runbook: bootstrap кластера

## TL;DR

- Preflight: `bash bootstrap/host/bootstrap_cluster.sh preflight --mode local|remote --env-file <env>`.
- Install запускается только по явной команде владельца после merge: `bash bootstrap/host/bootstrap_cluster.sh install --mode local|remote --env-file <env>`.
- Registry/Kaniko smoke: `KODEX_SMOKE_ENV_FILE=<env> bash bootstrap/host/smoke_registry_kaniko.sh`.
- Backend smoke после появления образов: `KODEX_SMOKE_ENV_FILE=<env> bash bootstrap/host/smoke_backend_contour.sh`.

## Когда использовать

Runbook используется для первого Kubernetes-контура backend MVP на single-node k3s или для проверки удалённого target через `TARGET_*`.

## Предпосылки и доступы

- Реальный env хранится вне Git в `bootstrap/host/config.env` или отдельном защищённом файле.
- Значения `TARGET_*`, домены, адреса, email, токены, ключи и kubeconfig не публикуются в рабочих логах, Issue, PR и документации.
- Для удалённого режима нужен SSH-доступ к target host и operator public key.
- Docker daemon не требуется: registry/Kaniko smoke использует Kubernetes jobs.

## Диагностика

1. Выполнить preflight в нужном режиме.
2. Для remote dry-run убедиться, что preflight выполняется на target через `TARGET_*`; режим не запускает install, k3s, firewall или registry-шаги.
3. Если k3s уже установлен, проверить `kubectl get --raw=/readyz` с kubeconfig целевого кластера.
4. Проверить наличие `deploy/base/bootstrap-foundation/**` и `deploy/base/bootstrap-builder-smoke/**`.
5. После install проверить rollout `deployment/kodex-registry` в production namespace.
6. Запустить registry/Kaniko smoke и проверить завершение jobs `kodex-registry-mirror-smoke`, `kodex-registry-pull-smoke`, `kodex-kaniko-build-smoke`.

## Митигирование

- Если preflight падает на DNS/ingress prerequisites, исправить привязку production domain к target host. `KODEX_BOOTSTRAP_SKIP_DNS_CHECK=true` допустим только для изолированной проверки host/foundation без публикации внешнего ingress.
- Если registry не готов, проверить PVC, port binding `127.0.0.1:<KODEX_INTERNAL_REGISTRY_PORT>` и readiness `/v2/`.
- Если Kaniko smoke не пушит образ, проверить доступ job к node loopback registry и значение `KODEX_KANIKO_EXECUTOR_IMAGE`.
- Если backend smoke падает на image pull, сначала выполнить mirror/build шаги для соответствующих backend images.

## Проверка результата

- k3s активен и kubeconfig создан для operator user.
- `/etc/rancher/k3s/registries.yaml` указывает на internal registry profile.
- `kodex-registry` готов в production namespace.
- Mirror smoke и Kaniko smoke завершаются успешно.
- Backend smoke запускается только после того, как нужные service images доступны во внутреннем registry.

## Границы

Этот runbook не разворачивает frontend, не добавляет full runtime build orchestration и не заменяет будущий deploy pipeline. Ingress controller и cert-manager остаются отдельной foundation-зависимостью, пока для них не добавлен активный контур.
