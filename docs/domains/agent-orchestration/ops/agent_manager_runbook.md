---
doc_id: RB-CK8S-AGENT-MANAGER-0001
type: runbook
title: "agent-manager — runbook: развёртывание и диагностика"
status: active
owner_role: SRE
created_at: 2026-05-27
updated_at: 2026-05-27
related_issues: [897]
related_alerts: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-27-agent-manager-deploy"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-27
---

# Runbook: agent-manager — развёртывание и диагностика

## TL;DR

- Симптом: `agent-manager` не стартует, не проходит readiness, не отвечает по gRPC или не публикует `agent.*` события.
- Быстрая диагностика: проверить migration job, `Deployment`, `/health/readyz`, `/metrics`, БД `agent-manager`, БД `platform-event-log` и доступность owner-сервисов `package-hub`, `project-catalog`, `runtime-manager`, `provider-hub`.
- Быстрое восстановление: исправить env/secret/image, повторить migration job, перезапустить `Deployment/agent-manager`, выполнить Go checks или общий deploy/diagnostic runner.

## Когда использовать

- После сборки и публикации образов `agent-manager` и `agent-manager-migrations`.
- После изменения миграций, deploy-манифестов, runtime env, gRPC контрактов или shared Go-библиотек.
- При сбоях session/run, activity timeline, acceptance, follow-up dispatch, Human gate wait/result, guidance resolution, runtime preparation или outbox-доставки.

## Предпосылки и доступы

- Доступ к Kubernetes namespace платформы.
- Доступ к логам `agent-manager`, `agent-manager-migrations`, `postgres` и owner-сервисов.
- Нормализованный `bootstrap.env`, подготовленный bootstrap-процессом.
- Локально для проверки готовности нужны `kubectl`, `curl`, `grpcurl` и `go`.
- Значения секретов, DSN, приватные домены, адреса серверов, raw prompt, transcript, workspace paths и provider payload не выводить в логи, Issue, PR и сообщения.

## Сборка образов

```bash
KODEX_BUILD_ENV_FILE=/path/to/bootstrap.env \
  scripts/build-agent-manager-images.sh
```

Скрипт собирает `agent-manager`, его миграции и минимальные backend-зависимости проверки готовности: `access-manager`, `project-catalog`, `package-hub`, `provider-hub`, `fleet-manager`, `runtime-manager` и migrations image общего event log.

## Проверки

Для `agent-manager` нет активного shell smoke-сценария. Проверки доменного
gRPC boundary, runtime job и Human gate связок должны жить в Go tests или
отдельном Go integration runner. Shell допускается только как тонкая обвязка
общего deploy/diagnostic tooling.

## Диагностика миграций

```bash
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get job/agent-manager-migrations
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" logs job/agent-manager-migrations
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" describe job/agent-manager-migrations
```

Проверить:

- `KODEX_AGENT_MANAGER_DATABASE_DSN` указывает на БД `kodex_agent_manager`;
- БД создана `kodex-postgres-bootstrap-databases`;
- образ `agent-manager-migrations` соответствует версии сервиса;
- migration job не требует доступа к workspace files, GitHub/GitLab, prompt templates или raw provider payload.

## Диагностика rollout и health

```bash
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get deployment/agent-manager service/agent-manager
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" rollout status deployment/agent-manager
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" describe deployment/agent-manager
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" logs deploy/agent-manager
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" port-forward svc/agent-manager 18087:8080
curl -fsS http://127.0.0.1:18087/health/livez
curl -fsS http://127.0.0.1:18087/health/readyz
curl -fsS http://127.0.0.1:18087/metrics
```

Readiness должна видеть:

- БД `agent-manager`;
- общую БД `platform-event-log`, если outbox dispatch включён и publisher kind равен `postgres-event-log`.

## Диагностика зависимостей

### package-hub

- Проверить `KODEX_AGENT_MANAGER_PACKAGE_HUB_ENABLED`.
- Проверить доступность `package-hub` по `KODEX_AGENT_MANAGER_PACKAGE_HUB_GRPC_ADDR`.
- Проверить, что `KODEX_AGENT_MANAGER_PACKAGE_HUB_GRPC_AUTH_TOKEN` соответствует boundary token `package-hub`.
- `agent-manager` не хранит manifest payload, `SKILL.md`, scripts, assets или package source.

### runtime preparation

- Проверить `KODEX_AGENT_MANAGER_RUNTIME_PREPARATION_ENABLED`.
- Если после подготовки workspace нужно ставить задание агента, проверить `KODEX_AGENT_MANAGER_RUNTIME_JOB_DISPATCH_ENABLED`; этот switch требует включённой runtime preparation.
- Проверить доступность `project-catalog` и `runtime-manager`.
- Проверить `KODEX_AGENT_MANAGER_PROJECT_CATALOG_GRPC_AUTH_TOKEN` и `KODEX_AGENT_MANAGER_RUNTIME_MANAGER_GRPC_AUTH_TOKEN`.
- Checkout, workspace paths, `.kodex/guidance/*`, `.kodex/context/agent-run.json`, runtime job state и будущий executor остаются у `runtime-manager`; `agent-manager` хранит только safe refs/status/fingerprint/diagnostic summary и `runtime_job_ref`.
- Если `Run` перешёл в `waiting` с reason `runtime_job_retryable`, проверить доступность `runtime-manager` и повторить orchestration-команду с тем же idempotency/command context. Если `Run` перешёл в `failed` с `runtime_job_failed`, смотреть безопасную summary в `Run` и состояние job/slot в `runtime-manager`.

### self-deploy signal consumer

- Проверить `KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_ENABLED`.
- Перед rollout consumer получить `KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID` через `cmd/onboarding-runner`: runner должен найти или создать project `kodex` по `organization_id`, найти или привязать repository binding `codex-k8s/kodex` через публичный `project-catalog` API и напечатать safe `project_id`. Ручные SQL-вставки, ручной UUID и прямой GitHub/GitLab не используются.
- Проверить, что `KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID` задан и указывает на active self-project scope в `project-catalog`.
- Для provider-owned `provider.repository.changed` без `project_id` consumer использует этот project id как область вызова `project-catalog.GetSelfDeploySignal`; `agent-manager` не вычисляет `services_yaml_digest` или affected service keys сам.
- Если `project-catalog.GetSelfDeploySignal` возвращает `ready` с governance policy key без namespace, consumer сохраняет его как typed ref `governance:gate_policy/<key>`; bare policy key не должен попадать в `SelfDeployPlan` как `governance_gate_policy_ref`. Project-side `risk_profile` key не передаётся в `governance-manager` как local `risk_profile_ref`, пока это не UUID-совместимая ссылка; self-deploy gate использует built-in governance path.
- Если consumer получает non-ready status от `project-catalog`, plan не создаётся; нужно устранить причину вроде `needs_services_policy_reconcile` или `needs_repository_change_summary`.
- Если `SelfDeployPlan` уже существует в `pending_approval`, но `governance_risk_assessment_ref` и `governance_gate_request_ref` пустые, стартовая сверка `agent-manager` повторно применяет штатный `EnsureSelfDeployPlanGovernanceGate` для настроенного self-project. Этот путь не меняет checkpoint event-log и не требует ручного SQL; после успешного вызова `governance-manager` в plan появляются safe governance refs. Если risk assessment и gate request уже созданы для target `self_deploy_plan`, `agent-manager` находит active risk assessment через safe `governance-manager` read API по target/project/fingerprint, затем читает gate requests по найденному `risk_assessment_id` и локально сверяет target/status перед записью refs. Такой порядок использует project-scoped `governance.risk.read` и не требует отдельного target-wide gate read для recovery. При ошибке в лог попадает только stage code: `plan_list_failed`, `plan_lookup_failed`, `gate_replay_failed`, `gate_prepare_failed`, `existing_gate_lookup_failed`, `gate_response_invalid` или `plan_governance_refs_update_failed`.
- После owner/governance approval build jobs создаются только если `project-catalog.GetSelfDeployBuildPlan` вернул `ready` для affected service keys и ожидаемой `ServicesPolicy` digest/fingerprint/version. `agent-manager` не парсит `services.yaml`, не подбирает Dockerfile/image refs сам и не передаёт `runtime-manager` значения секретов; non-ready build plan блокирует `JOB_TYPE_BUILD` безопасной причиной. Статус `build_context_unavailable` означает, что image/Dockerfile policy уже найдена в checked `ServicesPolicy`, но checked build context PVC/ref и digest ещё не подготовлены.

### provider-hub follow-up dispatch

- Проверить `KODEX_AGENT_MANAGER_PROVIDER_HUB_WRITE_ENABLED`.
- Проверить доступность `provider-hub` по `KODEX_AGENT_MANAGER_PROVIDER_HUB_GRPC_ADDR`.
- Проверить `KODEX_AGENT_MANAGER_PROVIDER_HUB_GRPC_AUTH_TOKEN`.
- `agent-manager` вызывает только typed provider-hub operations и не ходит напрямую в GitHub/GitLab.

### platform-event-log

- Проверить `platform-event-log-migrations`.
- Проверить `KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_DSN`.
- Если события не доходят, проверить локальную outbox-таблицу `agent-manager` и короткую причину последней ошибки публикации.

### PostgreSQL

- Проверить доступность `postgres`.
- Проверить, что database bootstrap job создаёт `kodex_agent_manager`.
- Проверить лимиты пула: `KODEX_AGENT_MANAGER_DATABASE_MAX_CONNS` и `KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS` должны учитывать число replicas и не создавать connection storm.

## Частые отказы

| Симптом | Вероятная причина | Что проверить |
|---|---|---|
| `agent-manager-migrations` падает | БД не создана, неверный DSN или не тот образ миграций | bootstrap job, `KODEX_AGENT_MANAGER_DATABASE_DSN`, image tag |
| `/health/readyz` не проходит | Недоступна БД `agent-manager` или `platform-event-log` | DSN, PostgreSQL, event-log migrations |
| gRPC возвращает `Unauthenticated` | Неверный runtime gRPC token | `KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN` в `kodex-platform-runtime` |
| `StartAgentRun` получает dependency unavailable | Недоступен `package-hub`, `project-catalog` или `runtime-manager` | соответствующий gRPC addr/token и rollout owner-сервиса |
| `DispatchFollowUpIntent` остаётся failed | Ошибка typed provider write через `provider-hub` | provider operation ref, safe status/error, provider-hub logs без raw payload |
| Outbox backlog растёт | Event log DSN недоступен или publisher не успевает | локальная outbox-таблица, event-log БД, outbox лимиты |

## Митигирование

- Если миграции упали из-за временной недоступности БД, удалить failed job и применить migration manifest повторно.
- Если readiness падает из-за БД, проверить `postgres`, database bootstrap и DSN.
- Если readiness падает из-за event log, проверить `platform-event-log-migrations` и event-log DSN.
- Если gRPC transport не отвечает, проверить service port `grpc`, NetworkPolicy и shared gRPC настройки.
- Если owner-сервис недоступен, исправить его rollout или временно выключить соответствующую интеграцию только осознанным env-переключателем.

## План отката

- Вернуть предыдущий образ `agent-manager` через image tag или предыдущий rendered manifest.
- Не откатывать миграции вручную без отдельного плана восстановления данных.
- Если новый сервис блокирует rollout платформы, временно не применять `agent-manager` manifests, но оставить БД и общий event log в согласованном состоянии.
- Не удалять session/run/activity/acceptance/follow-up/Human gate state вручную: это нарушит идемпотентность orchestration state.

## Проверка результата

- `Job/agent-manager-migrations` завершён успешно.
- `Deployment/agent-manager` доступен.
- `/health/readyz` возвращает успешный ответ.
- `/metrics` доступен.
- Go tests доменного и транспортного слоя проходят в `make test-go`; будущая
  end-to-end проверка запускается через Go integration runner.

## Пост-действия

- Если была авария, создать Issue с причиной и корректирующими действиями.
- Если обнаружен пробел в манифестах, env или проверке готовности, обновить этот runbook в том же изменении, где исправляется поведение.
- В Issue/PR не прикладывать значения DSN, токенов, адресов целевого сервера, приватных доменов или raw prompt/transcript/provider payload.

## Апрув

- request_id: `owner-2026-05-27-agent-manager-deploy`
- Решение: approved
- Комментарий: runbook входит в эксплуатационный контур AGO-10.
