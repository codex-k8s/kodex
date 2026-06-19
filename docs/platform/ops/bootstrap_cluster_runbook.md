---
doc_id: RB-CK8S-BOOTSTRAP-CLUSTER-0001
type: runbook
title: "kodex — Runbook: локальный bootstrap кластера"
status: active
owner_role: SRE
created_at: 2026-05-27
updated_at: 2026-06-19
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
- Финальный self-deploy preflight: `go run ./cmd/bootstrap-deploy-plan --env-file <env> --require-kubernetes --self-deploy-readiness`.
- Install: `bash bootstrap/host/bootstrap_cluster.sh install --env-file <env>`.
- Registry/Kaniko smoke: `KODEX_SMOKE_ENV_FILE=<env> bash bootstrap/host/smoke_registry_kaniko.sh`.
- Deploy первого backend-кольца: `bash bootstrap/host/deploy_backend_ring.sh --env-file <env>`.
- Deploy второго backend-кольца: `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring second`.
- Deploy `staff-gateway`: `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring staff`.
- Deploy `platform-mcp-server`: `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring mcp`.
- Deploy `web-console`: `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring web`.
- Deploy публичного HTTPS web-contour: `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring web-public`.
- Self-deploy chain acceptance: `go run ./cmd/self-deploy-chain-acceptance --project-ref <project_id> --staff-gateway-url <staff-gateway-url>`.
- Post-deploy operational acceptance: `go run ./cmd/bootstrap-operational-acceptance --env-file <env>`.
- Проверка первого кольца после deploy: `KODEX_SMOKE_ENV_FILE=<env> bash bootstrap/host/smoke_backend_contour.sh`.

## Когда использовать

Runbook используется на сервере, где поднимается или используется single-node
k3s для backend MVP. Оператор заходит на сервер и запускает bootstrap локально.

## Предпосылки и доступы

- Реальный env хранится вне Git в `bootstrap/host/config.env` или отдельном защищённом файле.
- Домены, адреса, email, токены, ключи, пароли, DSN и kubeconfig не публикуются в рабочих логах, Issue, PR и документации.
- Нужен root или passwordless sudo.
- Docker daemon не требуется: registry/Kaniko smoke использует Kubernetes jobs.
- Live-кластер уже существует: повторный deploy должен идти последовательными шагами, без
  переписывания применённых миграций, ручного production SQL и внезапного удаления обязательных env,
  secret key или manifest refs.
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
- `cmd/bootstrap-deploy-plan` по умолчанию проверяет ручной MVP deploy:
  инвентарь, рендер, Kubernetes foundation, runtime `Secret` и согласованность
  derived runtime keys без вывода значений. `self-deploy` не входит в критический
  путь ручной выкладки.
- Готовность `self-deploy` проверяется только при явном
  `--self-deploy-readiness`: `KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID`
  должен быть заполнен UUID, signal consumer, governance gate, build dispatch и
  gate decision consumer должны быть включены, runtime executor должен быть
  настроен, а rendered `agent-manager`/`runtime-manager` manifests должны
  содержать обязательные env/Secret refs. При `--require-kubernetes` команда
  дополнительно проверяет нужные ключи в `kodex-platform-runtime` без вывода
  значений.
- `cmd/bootstrap-backend-deploy` выполняет реальный deploy выбранного backend-кольца:
  применяет registry foundation, подготавливает Kubernetes `Secret`, запускает
  Kaniko build jobs, PostgreSQL, базы, migrations и deployments. Первое кольцо
  содержит `access-manager`, `project-catalog`, `package-hub`, `provider-hub`;
  второе кольцо содержит `fleet-manager`, `runtime-manager`, `interaction-hub`,
  `governance-manager`, `agent-manager`, `integration-gateway`,
  `codex-hook-ingress`. Отдельный режим `--ring staff` разворачивает
  `staff-gateway` после готовности `agent-manager` и `interaction-hub`.
  Отдельный режим `--ring mcp` разворачивает `platform-mcp-server` после
  готовности сервисов-владельцев MCP-инструментов. Отдельный режим `--ring web`
  разворачивает `web-console` после готовности `staff-gateway`; для этого ring
  не запускаются PostgreSQL foundation и backend migrations. Отдельный режим
  `--ring web-public` готовит публичный HTTPS-доступ к `web-console`: применяет
  `cert-manager`, Traefik `IngressClass` `kodex-public`, `ClusterIssuer`
  Let’s Encrypt, `oauth2-proxy`, `Certificate` для `platform.kodex.works` и
  публичный `Ingress`, который ведёт только на `oauth2-proxy`, а также
  отдельный HMAC-only `Ingress` для GitHub webhook route
  `/v1/provider-webhooks/github` в `integration-gateway`. Значения env, DSN,
  токены, домены, адреса и kubeconfig не печатаются.
- `cmd/bootstrap-operational-acceptance` выполняет read-only приёмку уже
  развёрнутого backend/UI-контура: проверяет Kubernetes API, deployments,
  PostgreSQL, migration/bootstrap jobs, отсутствие активных или failed build
  jobs, Service type, обязательные Kubernetes `Secret` keys, loopback registry,
  операторскую HTTP-поверхность через локальный `kubectl port-forward`, public
  Ingress через `oauth2-proxy`, public GitHub webhook `Ingress` через
  `integration-gateway`, `Certificate`/`ClusterIssuer`, HTTPS redirect на
  GitHub OAuth и HMAC reject неподписанного webhook. Команда не выполняет
  `kubectl apply`, не запускает jobs, не собирает образы и не печатает значения
  env.
- `cmd/self-deploy-chain-acceptance` выполняет read-only приёмку живой
  self-deploy цепочки по safe refs. Команда читает только публичные gRPC/HTTP
  поверхности `agent-manager`, `project-catalog`, `governance-manager`,
  `runtime-manager` и `staff-gateway`: provider/project signal,
  `SelfDeployPlan`, governance gate, gate decision, build context, build job,
  deploy job и owner-facing summary. Результат печатается как JSON со
  `status`, `current_stage`, typed `blocker` и списком `stages`. Команда не
  выполняет ручной SQL, `kubectl patch`, redelivery, checkpoint mutation,
  owner decision, build или deploy, не читает raw webhook body, provider
  response, полный diff/YAML/manifests, kubeconfig, логи или значения секретов.
  До owner approval ожидаемый итог — `status=waiting` на gate/build/deploy
  стадиях; после approval команда показывает точный runtime stage и safe error
  code, если цепочка остановилась.

## Диагностика

1. Выполнить preflight.
2. Выполнить dry-run install, чтобы увидеть план без изменения системы.
3. Выполнить план backend deploy:
   `bash bootstrap/host/plan_backend_deploy.sh --env-file <env>`.
   Этот шаг является основным preflight ручной MVP-выкладки и не требует
   настроенного `self-deploy`.
4. Если нужен только render/inventory без чтения Kubernetes, добавить
   `--skip-live-kubernetes`.
5. Если k3s и foundation уже установлены, выполнить план backend deploy с
   `--require-kubernetes`: он проверит `kubectl` context, `/readyz`, namespace,
   registry Deployment/Service, PostgreSQL StatefulSet/Service, runtime `Secret`,
   service accounts, Kaniko builder image из `services.yaml` и согласованность
   derived runtime keys. Команда печатает строки `READINESS` только со
   статусами, safe codes и следующим действием; значения env, `Secret`, DSN,
   kubeconfig и manifest payload не выводятся.
6. Для финального live `self-deploy` rollout дополнительно выполнить:
   `go run ./cmd/bootstrap-deploy-plan --env-file <env> --require-kubernetes --self-deploy-readiness`.
   Если не заполнен project id, сначала выполнить штатный
   onboarding/adoption self-repo и записать полученный project id в защищённый
   env-контур. Этот режим также проверяет self-deploy service tokens,
   `agent-manager` flags, runtime Kubernetes executor refs, source auth
   SecretRef, rendered surface `agent-manager`/`runtime-manager` и наличие
   read-only команды `cmd/self-deploy-chain-acceptance`.
7. Для готовности live `self-deploy` после изменения access bootstrap выполнить
   штатный `cmd/onboarding-runner --apply` для self-repo. Повторный запуск
   должен переиспользовать binding, checked `services.yaml` policy и
   идемпотентно создать или подтвердить service grants: project policy,
   governance, runtime build context/job orchestration для `service/agent-manager`,
   build context report grant для `service/runtime-manager-build-context-materializer`
   и lifecycle grants для `service/runtime-manager-kubernetes-executor`.
   Команда проверяет каждый grant через `CheckAccess` и не печатает значения
   токенов, `Secret`, DSN, raw webhook payload или полный YAML.
8. Перед финальным live self-deploy прогоном выполнить read-only проверку
   цепочки:
   `go run ./cmd/self-deploy-chain-acceptance --project-ref <project_id> --staff-gateway-url <staff-gateway-url>`.
   Если известен точный plan, использовать
   `--self-deploy-plan-id <plan_id>`; если известен provider signal identity,
   добавить `--provider-signal-id` или `--provider-signal-key`. Агент проверяет
   JSON-ответ: `provider_signal`, `project_signal`, `self_deploy_plan`,
   `governance_gate`, `gate_decision`, `build_context`, `build_job`,
   `deploy_job` и `staff_summary`. До owner approval допустим только
   `status=waiting` с `current_stage` на approval/build/deploy ожидании. После
   owner approval допустимы `ok` или понятный typed blocker вроде
   `permission_denied`, `dependency_unavailable`, `policy_stale`,
   `deploy_executor_unavailable` или другой safe code из доменных API.
   Нельзя компенсировать blocker ручным SQL, redelivery, `kubectl patch`,
   повторным checkpoint или ручной записью owner decision.
9. Финальная live-проверка `self-deploy` выполняется одной последовательностью:
   strict deploy plan с `READINESS=ready`, один rollout нужных rings, один
   controlled deploy-relevant trigger после rollout, одна owner decision через
   операторскую поверхность и один финальный запуск `self-deploy-chain-acceptance`.
   Если любой этап возвращает typed blocker, следующий шаг — исправить этот
   blocker в сервисе-владельце или bootstrap/config контуре; ручной SQL,
   checkpoint mutation, redelivery, `kubectl patch` и повторный owner decision
   не используются.
10. После явного разрешения владельца выполнить install, если registry
   foundation ещё не применён.
11. Для первого backend-кольца выполнить:
   `bash bootstrap/host/deploy_backend_ring.sh --env-file <env>`.
12. Для второго backend-кольца выполнить:
   `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring second`.
   Для новой установки можно применить оба кольца одной командой:
   `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring all`.
   `all` не включает `staff-gateway`, `platform-mcp-server` и `web-console`.
13. После готовности второго кольца выполнить:
   `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring staff`.
14. После готовности сервисов-владельцев выполнить:
    `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring mcp`.
15. После готовности `staff-gateway` выполнить:
    `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring web`.
16. После готовности `web-console` и наличия GitHub OAuth seed-полей выполнить:
    `bash bootstrap/host/deploy_backend_ring.sh --env-file <env> --ring web-public`.
17. Проверить, что завершились jobs `kodex-build-*`,
   `kodex-postgres-bootstrap-databases`, `platform-event-log-migrations` и
   migrations выбранного backend/staff/MCP-кольца. Для `web` ring проверить
   `kodex-build-web-console`, rollout deployment и `/health/readyz`.
   Для `web-public` проверить `cert-manager`, `kodex-public-ingress`,
   `ClusterIssuer`, `Certificate`, rollout `oauth2-proxy` и HTTPS redirect на
   GitHub OAuth, а также `integration-gateway-public-webhook` ingress без OAuth.
18. После применения всех нужных rings выполнить операционную приёмку:
   `go run ./cmd/bootstrap-operational-acceptance --env-file <env>`.
   Успешный результат `OK: operational acceptance completed` означает, что
   кластер готов к ручной UI-проверке владельца. Интерактивная авторизация
   owner-аккаунтом не входит в автоматическую приёмку, потому что агент не
   должен печатать cookie, OAuth `state`, токены или callback-параметры.
19. При необходимости повторить проверку первого кольца без пересборки:
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
- Если strict ручной deploy plan падает с `runtime_secret_source_key_missing`,
  `runtime_secret_derived_key_missing` или `runtime_secret_derived_key_drift`,
  проверить защищённый env-файл на наличие исходного ключа и запустить штатный
  `deploy_backend_ring.sh` для нужного ring. Deploy path должен согласовать
  derived keys в `kodex-platform-runtime`; `kubectl patch` и ручное
  редактирование `Secret` не используются.
- Если финальный self-deploy preflight падает с `self_deploy_*` или
  `runtime_executor_*` safe code, сначала выполнить указанное `next_action`:
  обычно это idempotent `onboarding-runner --apply`, заполнение защищённого
  env-файла, подготовка `kodex-platform-runtime` Secret key или применение
  runtime manifest/RBAC штатным deploy path. Значения Secret и полный manifest
  не выводить в отчётах.
- Если plan сообщает, что registry Secret ref для build ещё не проверен,
  дождаться checked build plan после controlled trigger и читать typed blocker
  через `self-deploy-chain-acceptance` или runtime safe summary. Registry
  credentials должны оставаться только Kubernetes `Secret` ref, а не env-дубли.
- Если `deploy_backend_ring.sh` сообщает о сгенерированных `Secret`, это
  означает, что отсутствовали ключи или локальные PostgreSQL DSN были
  нормализованы под текущий `kodex-postgres`. Уже существующие значения
  токенов и паролей не перегенерируются.
- Если `--ring web-public` падает до создания `Ingress`, проверить наличие
  `KODEX_GITHUB_OAUTH_CLIENT_ID`, `KODEX_GITHUB_OAUTH_CLIENT_SECRET`,
  `KODEX_PUBLIC_BASE_URL`, `KODEX_PRODUCTION_DOMAIN` и
  `KODEX_LETSENCRYPT_EMAIL` в защищённом env-файле. Значения не выводить.
- Если `Certificate` не становится `Ready`, проверить DNS на
  `platform.kodex.works`, доступность портов `80`/`443`, состояние
  `ClusterIssuer` и `Challenge`/`Order` cert-manager без публикации токенов,
  kubeconfig и приватных адресов.
- Если OAuth redirect не происходит, проверить rollout `oauth2-proxy`,
  `Secret` `kodex-web-oauth2-proxy`, public `Ingress` и GitHub OAuth callback
  `https://platform.kodex.works/oauth2/callback`. Публичный `Ingress` не должен
  указывать прямо на `web-console`.
- Если GitHub webhook endpoint редиректит в OAuth или возвращает не
  `401/signature_invalid` на неподписанный запрос, проверить `Ingress`
  `integration-gateway-public-webhook`, path `/v1/provider-webhooks/github`,
  `IngressClass` `kodex-public`, rollout `integration-gateway` и наличие key
  `KODEX_GITHUB_WEBHOOK_SECRET` в `kodex-platform-runtime` без вывода значения.
- GitHub webhook для `codex-k8s/kodex` регистрируется на
  `https://platform.kodex.works/v1/provider-webhooks/github` с событиями
  `push` и `pull_request`. Secret берётся из Kubernetes `Secret` и передаётся в
  GitHub API только как значение процесса; в логи, документы и Issue его не
  записывать.
- Если Kaniko build job не завершился, проверить pod events и logs конкретного
  `kodex-build-*` job. Не публиковать вывод, если в нём есть адреса registry,
  домены или значения env.
- Если migration job не завершился, проверить состояние PostgreSQL,
  соответствующий DSN key в `kodex-platform-runtime` и logs job без публикации
  значений DSN.
- Если живая схема отличается от текущего текста уже применённой миграции,
  не менять history row, не править базу вручную и не редактировать уже
  применённый migration-файл. Такое расхождение закрывается новой добавочной
  миграцией владельца схемы; migration job должен применить её штатно и
  сохранить существующие данные.
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
- `staff-gateway` развёрнут после явного `--ring staff`, имеет готовый
  deployment, отвечает на `/health/readyz` и отдаёт
  `/openapi/staff-gateway.v1.yaml`.
- `platform-mcp-server` развёрнут после явного `--ring mcp`, имеет готовый
  deployment, отвечает на `/health/readyz`, отдаёт `/metrics` и принимает
  запросы на MCP endpoint `/mcp` только через настроенный bearer-токен.
- `web-console` развёрнут после явного `--ring web`, имеет готовый deployment,
  отвечает на `/health/readyz`, отдаёт статический HTML на `/` и проксирует
  `/v1/**` к `staff-gateway` внутри кластера.
- Публичный web-contour развёрнут после явного `--ring web-public`:
  `cert-manager` готов, `IngressClass` `kodex-public` есть, `ClusterIssuer`
  готов, `Certificate` для `platform.kodex.works` готов, `oauth2-proxy`
  готов, публичный web `Ingress` ведёт только на `oauth2-proxy`, а GitHub
  webhook `Ingress` ведёт только на `integration-gateway` по точному path
  `/v1/provider-webhooks/github`.
- Post-deploy operational acceptance завершается `OK: operational acceptance
  completed`: workloads готовы, migration/bootstrap jobs завершены, build jobs
  не висят и не failed, обязательные secret keys присутствуют без вывода
  значений, registry доступен локально, `staff-gateway` OpenAPI, `web-console`
  root и `oauth2-proxy` ping отвечают через локальный port-forward, а публичный
  root редиректит на GitHub OAuth, и публичный GitHub webhook endpoint
  отклоняет неподписанный запрос без OAuth redirect.
- Проверка первого кольца запускается после `deploy_backend_ring.sh`; штатный режим
  проверяет первый backend-набор через идемпотентный deploy без пересборки
  образов.
- Доменные проверки, provider live-сценарии и end-to-end проверки оформляются
  как Go tests или отдельные Go integration runners. Shell в этом контуре
  остаётся только тонкой обвязкой bootstrap/deploy или Make targets.
- Strict ручной deploy plan завершается `READINESS:
  component=manual-mvp-deploy-preflight status=ready`; self-deploy readiness
  проверяется только отдельным явным режимом и не блокирует обычный MVP rollout.

## Границы

Фронтенд разворачивается как внутренний `web-console` Service через явный
`--ring web`, а публичная HTTPS-поверхность включается отдельным
`--ring web-public` только через `oauth2-proxy`. `staff-gateway` не входит во
второе backend-кольцо и не
входит в `--ring all`: он разворачивается только явным режимом `--ring staff`.
`platform-mcp-server` также не входит в `--ring all` и разворачивается только
явным режимом `--ring mcp`, чтобы MCP-поверхность не менялась при обычном
повторном backend deploy. Прямой публичный `Ingress` на `web-console` не
создаётся: внешний трафик идёт через `oauth2-proxy`, GitHub OAuth callback
закреплён за `https://platform.kodex.works/oauth2/callback`, а allowlist
задаётся отдельным файлом `authenticated-emails.txt`, не общей bootstrap
allowlist. Публичный webhook route `integration-gateway` не находится за OAuth:
он принимает только `POST /v1/provider-webhooks/github` и защищён HMAC SHA-256
по `X-Hub-Signature-256`, body limit, route limits и `provider_slug=github`.
