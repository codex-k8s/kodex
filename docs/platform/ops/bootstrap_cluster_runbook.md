---
doc_id: RB-CK8S-BOOTSTRAP-CLUSTER-0001
type: runbook
title: "kodex — Runbook: локальный bootstrap кластера"
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

# Runbook: локальный bootstrap кластера

## TL;DR

- Preflight: `bash bootstrap/host/bootstrap_cluster.sh preflight --env-file <env>`.
- Strict deploy preflight после установки Kubernetes/foundation: `bash bootstrap/host/bootstrap_cluster.sh preflight --env-file <env> --require-kubernetes`.
- Dry-run: `bash bootstrap/host/bootstrap_cluster.sh install --env-file <env> --dry-run`.
- Install: `bash bootstrap/host/bootstrap_cluster.sh install --env-file <env>`.
- Registry/Kaniko smoke: `KODEX_SMOKE_ENV_FILE=<env> bash bootstrap/host/smoke_registry_kaniko.sh`.
- Backend smoke после появления образов: `KODEX_SMOKE_ENV_FILE=<env> bash bootstrap/host/smoke_backend_contour.sh`.

## Когда использовать

Runbook используется на сервере, где поднимается или используется single-node
k3s для backend MVP. Оператор заходит на сервер и запускает bootstrap локально.

## Предпосылки и доступы

- Реальный env хранится вне Git в `bootstrap/host/config.env` или отдельном защищённом файле.
- Домены, адреса, email, токены, ключи, пароли, DSN и kubeconfig не публикуются в рабочих логах, Issue, PR и документации.
- Нужен root или passwordless sudo.
- Docker daemon не требуется: registry/Kaniko smoke использует Kubernetes jobs.
- Корневой `services.yaml` является stack inventory платформы: источник версий, дефолтных образов и deploy inventory; env задаёт только локальные install-настройки и overrides.
- Это не проектный `services.yaml` пользовательского репозитория: project policy импортирует и валидирует `project-catalog`.
- Go tooling читает stack inventory через `libs/go/stackinventory`, чтобы renderer, bootstrap и будущие install/deploy tools не держали отдельные YAML parsers.
- Runtime defaults сервисов принадлежат Go config. Kubernetes templates задают только обязательные env, секреты, связи сервисов и явные deploy/runtime overrides.
- `cmd/bootstrap-preflight` выполняет безопасную проверку root stack inventory, dry-run render, `kubectl kustomize` и read-only Kubernetes checks. Он не запускает `kubectl apply`, jobs или install steps.

## Диагностика

1. Выполнить preflight.
2. Выполнить dry-run install, чтобы увидеть план без изменения системы.
3. Если k3s уже установлен, выполнить strict preflight с `--require-kubernetes`: он проверит `kubectl` context, `/readyz`, namespace и наличие `kodex-registry` Deployment/Service без изменения кластера.
4. Проверить dry-run render `deploy/base/bootstrap-foundation/**` и `deploy/base/bootstrap-builder-smoke/**`; `cmd/bootstrap-preflight` делает это через `libs/go/manifestrender`.
5. После install проверить rollout `deployment/kodex-registry` в production namespace.
6. Запустить registry/Kaniko smoke и проверить завершение jobs `kodex-registry-mirror-smoke`, `kodex-registry-pull-smoke`, `kodex-kaniko-build-smoke`.

## Митигирование

- Если preflight падает на DNS prerequisite, исправить привязку production domain к `KODEX_BOOTSTRAP_PUBLIC_HOST` или к текущему host. `KODEX_BOOTSTRAP_SKIP_DNS_CHECK=true` допустим только для изолированной проверки foundation без публикации внешнего ingress.
- Если stackinventory/render preflight падает на image ref, исправить `services.yaml/spec.images` или env override; значение override не должно попадать в логи.
- Если strict preflight падает на live Kubernetes check, сначала проверить kubeconfig/current context, затем namespace и registry foundation. Без `--require-kubernetes` эти проверки считаются deferred до install/foundation smoke.
- Если registry не готов, проверить PVC, port binding `127.0.0.1:<KODEX_INTERNAL_REGISTRY_PORT>` и readiness `/v2/`.
- Если Kaniko smoke не пушит образ, проверить доступ job к node loopback registry и image overrides в env.
- Если backend smoke падает на image pull, сначала выполнить mirror/build шаги для соответствующих backend images.
- Если включён firewall, `KODEX_SSH_PORT` должен соответствовать фактическому SSH-порту сервера.

## Проверка результата

- k3s активен и kubeconfig создан для `OPERATOR_USER`.
- `/etc/rancher/k3s/registries.yaml` указывает на internal registry profile.
- `/opt/kodex` содержит актуальный repository snapshot без локального `bootstrap/host/*.env`.
- `kodex-registry` готов в production namespace.
- Mirror smoke и Kaniko smoke завершаются успешно.
- Backend smoke запускается только после того, как нужные service images доступны во внутреннем registry.

## Границы

Этот runbook не разворачивает frontend, business services и full runtime build
orchestration. Ingress controller и cert-manager остаются отдельной
foundation-зависимостью, пока для них не добавлен активный контур.
