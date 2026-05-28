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
- План backend deploy: `bash bootstrap/host/plan_backend_deploy.sh --env-file <env>`.
- Install: `bash bootstrap/host/bootstrap_cluster.sh install --env-file <env>`.
- Registry/Kaniko smoke: `KODEX_SMOKE_ENV_FILE=<env> bash bootstrap/host/smoke_registry_kaniko.sh`.
- Deploy первого backend-кольца: `bash bootstrap/host/deploy_backend_ring.sh --env-file <env>`.
- Deploy второго backend-кольца: `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring second`.
- Проверка первого кольца после deploy: `KODEX_SMOKE_ENV_FILE=<env> bash bootstrap/host/smoke_backend_contour.sh`.

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
- `cmd/bootstrap-preflight` выполняет безопасную проверку root stack inventory, dry-run render, `kubectl kustomize` и проверки Kubernetes только на чтение. Он не запускает `kubectl apply`, jobs или install steps.
- `cmd/bootstrap-deploy-plan` выполняет безопасный план первого backend deploy:
  проверяет MVP deploy inventory из `services.yaml`, рендерит PostgreSQL,
  platform event-log migrations, registry/Kaniko manifests и manifests
  текущих backend-сервисов, выполняет `kubectl kustomize` и только проверки
  Kubernetes на чтение. Он не запускает `kubectl apply`, jobs, push образов или
  доменные интеграционные проверки.
- `cmd/bootstrap-backend-deploy` выполняет реальный deploy выбранного backend-кольца:
  применяет registry foundation, подготавливает Kubernetes `Secret`, запускает
  Kaniko build jobs, PostgreSQL, базы, migrations и deployments. Первое кольцо
  содержит `access-manager`, `project-catalog`, `package-hub`, `provider-hub`;
  второе кольцо содержит `fleet-manager`, `runtime-manager`, `interaction-hub`,
  `governance-manager`, `agent-manager`, `integration-gateway`,
  `codex-hook-ingress`. Значения
  env, DSN, токены, домены, адреса и kubeconfig не печатаются.

## Диагностика

1. Выполнить preflight.
2. Выполнить dry-run install, чтобы увидеть план без изменения системы.
3. Выполнить план backend deploy:
   `bash bootstrap/host/plan_backend_deploy.sh --env-file <env>`.
4. Если нужен только render/inventory без чтения Kubernetes, добавить
   `--skip-live-kubernetes`.
5. Если k3s и foundation уже установлены, выполнить план backend deploy с
   `--require-kubernetes`: он проверит `kubectl` context, `/readyz`, namespace,
   registry Deployment/Service, PostgreSQL StatefulSet/Service и runtime Secret
   refs без изменения кластера.
6. После явного разрешения владельца выполнить install, если registry
   foundation ещё не применён.
7. Для первого backend-кольца выполнить:
   `bash bootstrap/host/deploy_backend_ring.sh --env-file <env>`.
8. Для второго backend-кольца выполнить:
   `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring second`.
   Для новой установки можно применить оба кольца одной командой:
   `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring all`.
9. Проверить, что завершились jobs `kodex-build-*`,
   `kodex-postgres-bootstrap-databases`, `platform-event-log-migrations` и
   migrations выбранного кольца.
10. При необходимости повторить проверку первого кольца без пересборки:
   `KODEX_SMOKE_ENV_FILE=<env> bash bootstrap/host/smoke_backend_contour.sh`.
   Обвязка повторяет `deploy_backend_ring.sh --skip-build`, чтобы не
   перезаписать Kubernetes `Secret` устаревшими значениями из env-файла.

## Митигирование

- Если preflight падает на DNS prerequisite, исправить привязку production domain к `KODEX_BOOTSTRAP_PUBLIC_HOST` или к текущему host. `KODEX_BOOTSTRAP_SKIP_DNS_CHECK=true` допустим только для изолированной проверки foundation без публикации внешнего ingress.
- Если stackinventory/render preflight падает на image ref, исправить `services.yaml/spec.images` или env override; значение override не должно попадать в логи.
- Если strict preflight падает на live Kubernetes check, сначала проверить kubeconfig/current context, затем namespace и registry foundation. Без `--require-kubernetes` эти проверки откладываются до install/foundation.
- Если план backend deploy падает на deploy inventory, исправить `services.yaml`
  или соответствующий `deploy/base/<service>/**`: сервис должен иметь Dockerfile,
  service manifest, kustomization, а при наличии БД — migration manifest и
  migrations image ref.
- Если план backend deploy падает на `kubectl kustomize`, сначала проверить
  отрендеренный manifest set через `--render-dir <empty-dir>`. Непустой каталог
  команда не очищает.
- Если план backend deploy в строгом режиме падает на PostgreSQL, runtime Secret
  refs или registry resources, foundation ещё не готов к backend deploy; это не
  повод запускать доменные shell-проверки.
- Если `deploy_backend_ring.sh` сообщает о сгенерированных `Secret`, это
  означает, что отсутствовали ключи или локальные PostgreSQL DSN были
  нормализованы под текущий `kodex-postgres`. Уже существующие значения
  токенов и паролей не перегенерируются.
- Если Kaniko build job не завершился, проверить pod events и logs конкретного
  `kodex-build-*` job. Не публиковать вывод, если в нём есть адреса registry,
  домены или значения env.
- Если migration job не завершился, проверить состояние PostgreSQL,
  соответствующий DSN key в `kodex-platform-runtime` и logs job без публикации
  значений DSN.
- Если registry не готов, проверить PVC, port binding `127.0.0.1:<KODEX_INTERNAL_REGISTRY_PORT>` и readiness `/v2/`.
- Если Kaniko smoke не пушит образ, проверить доступ job к node loopback registry и image overrides в env.
- Если backend smoke падает на image pull, сначала выполнить deploy первого
  кольца с Kaniko build jobs. Сервисные shell smoke scripts не являются штатным
  путём диагностики.
- Если включён firewall, `KODEX_SSH_PORT` должен соответствовать фактическому SSH-порту сервера.

## Проверка результата

- k3s активен и kubeconfig создан для `OPERATOR_USER`.
- `/etc/rancher/k3s/registries.yaml` указывает на internal registry profile.
- `/opt/kodex` содержит актуальный repository snapshot без локального `bootstrap/host/*.env`.
- `kodex-registry` готов в production namespace.
- План backend deploy проходит без раскрытия значений env и показывает текущий
  MVP backend-набор, готовые manifests, migrations и зависимости.
- Mirror smoke и Kaniko smoke завершаются успешно.
- Первое backend-кольцо развёрнуто: `access-manager`, `project-catalog`,
  `package-hub`, `provider-hub` имеют готовые deployments, завершённые
  migrations и отвечают на `/health/readyz`.
- Второе backend-кольцо развёрнуто после явного `--ring second`:
  `fleet-manager`, `runtime-manager`, `interaction-hub`, `governance-manager`,
  `agent-manager`, `integration-gateway`, `codex-hook-ingress` имеют готовые
  deployments, завершённые migrations там, где они есть, и отвечают на
  `/health/readyz`.
- Проверка первого кольца запускается после `deploy_backend_ring.sh`; штатный режим
  проверяет первый backend-набор через идемпотентный deploy без пересборки
  образов.
- Доменные проверки, provider live-сценарии и end-to-end проверки оформляются
  как Go tests или отдельные Go integration runners. Shell в этом контуре
  остаётся только тонкой обвязкой bootstrap/deploy или Make targets.

## Границы

Этот runbook не разворачивает frontend. `staff-gateway` не входит во второе
backend-кольцо и разворачивается отдельным контуром после готовности
API-среза. Ingress controller и cert-manager остаются отдельной
foundation-зависимостью, пока для них не добавлен активный контур.
