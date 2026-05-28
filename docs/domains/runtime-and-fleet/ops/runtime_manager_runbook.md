---
doc_id: RB-CK8S-RUNTIME-MANAGER-0001
type: runbook
title: "runtime-manager — runbook: развёртывание и проверка готовности"
status: active
owner_role: SRE
created_at: 2026-05-08
updated_at: 2026-05-08
related_issues: [661, 966]
related_alerts: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-07-runtime-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-07
---

# Runbook: runtime-manager — развёртывание и проверка готовности

## TL;DR
- Симптом: `runtime-manager` не стартует, не проходит readiness или не отвечает по gRPC.
- Быстрая диагностика: проверить миграции, секреты DSN/auth, доступность `postgres`, `platform-event-log` и `access-manager`.
- Быстрое восстановление: повторить migration job, перезапустить deployment, проверить значения в `kodex-platform-runtime`.

## Когда использовать

- После сборки и публикации образов `runtime-manager` и `runtime-manager-migrations`.
- После изменения миграций, deploy-манифестов, runtime env или shared gRPC runtime.
- При сбоях readiness, gRPC auth boundary, outbox-доставки runtime-событий.

## Предпосылки/доступы

- Доступ к Kubernetes-кластеру целевой установки.
- Секреты и адреса берутся из локального bootstrap-профиля и не публикуются в Issue/PR.
- Для полной gRPC проверки готовности локально нужен `grpcurl`.
- Перед запуском проверки готовности должен быть подготовлен локальный bootstrap env через `bootstrap/host/bootstrap_cluster.sh`.

## Сборка образов

```bash
KODEX_BUILD_ENV_FILE=/path/to/bootstrap.env \
  scripts/build-runtime-manager-images.sh
```

Скрипт собирает:
- `access-manager` и его миграции как обязательную зависимость проверки доступа;
- `runtime-manager` и его миграции;
- `platform-event-log` migrations image.

## Проверки

Для `runtime-manager` нет активного shell smoke-сценария. Проверки Kubernetes
executor, job lifecycle и gRPC boundary должны жить в Go tests или отдельном Go
integration runner. Shell допускается только как тонкая обвязка общего
deploy/diagnostic tooling.

## Диагностика

1. Проверить migration job:

```bash
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" logs job/runtime-manager-migrations
```

2. Проверить readiness и последние события pod:

```bash
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get pods -l app.kubernetes.io/name=runtime-manager
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" describe deploy/runtime-manager
```

3. Проверить runtime-секреты без вывода значений:

```bash
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get secret kodex-platform-runtime -o jsonpath='{.data}' | jq 'keys'
```

4. Проверить связи:
- `KODEX_RUNTIME_MANAGER_DATABASE_DSN` указывает на БД `kodex_runtime_manager`;
- `KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_DSN` указывает на общий `platform-event-log`;
- `KODEX_RUNTIME_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN` совпадает с токеном доступа к `access-manager`;
- `KODEX_RUNTIME_MANAGER_OUTBOX_PUBLISHER_KIND=postgres-event-log`.

## Исполнитель Kubernetes

По умолчанию исполнитель Kubernetes выключен: `KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_ENABLED=false`. При таком режиме `runtime-manager` хранит и выдаёт platform jobs, но не создаёт Kubernetes workloads.

Для включения первого безопасного пути нужны:

- `KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_ENABLED=true`;
- `KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_NAMESPACE` с namespace, где разрешено создавать проверочные Job;
- `KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_IMAGE` с образом, который содержит `/bin/sh`;
- policy в `access-manager` для service actor `runtime-manager-kubernetes-executor` на `runtime.job.claim`, `runtime.job.step.report`, `runtime.job.complete` и `runtime.job.fail`;
- cluster record в `fleet-manager` с `secret_store_type`/`secret_store_ref`, доступным `runtime-manager` через настроенный `secretresolver`.

Базовый manifest выдаёт `runtime-manager` права на создание Job в production namespace. Если `KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_NAMESPACE` указывает другой namespace, оператор должен выдать аналогичные RBAC-права для service account `runtime-manager` в этом namespace.

Поддержанные типы этого исполнителя — `health_check` и `agent_run` с валидным `AgentRunExecutionSpec`. Канонический тип `agent_run` можно создавать и читать через runtime job lifecycle; Kubernetes-исполнитель забирает его только при наличии spec. Задание без spec остаётся ожидающим с диагностикой `agent_run_execution_spec_required` и не попадает в claim.

Для `agent_run` обязательны safe refs на Run/slot/materialization/workspace/context, `workspace_pvc_ref`, `runner_profile_ref`, `runner_image_ref`, фиксированный `runner_mode`, secret refs без значений и reporting target refs. Первый executor принимает `workspace_pvc_ref` как `pvc://<namespace>/<claim>` или `k8s://pvc/<claim>`. Runner image ref может быть прямой ссылкой на контейнерный образ или typed ref с префиксом `image://`; в Kubernetes Job используется образ без этого префикса. Контейнер запускается фиксированной командой `/kodex/bin/agent-runner run`, workspace монтируется в `/workspace`, automount service account token выключен.

Исполнитель создаёт только ограниченный Kubernetes Job, не вызывает `kubectl`, не читает GitHub/GitLab, не хранит kubeconfig и не сохраняет полный лог. В БД попадают статус job, шаг `kubernetes_health_check` или `kubernetes_agent_run`, короткий хвост лога, ссылка на Kubernetes Job, ссылка на namespace и для `agent_run` ссылка на runner image.

Если задание падает с `cluster_secret_unavailable`, `cluster_ref_unavailable`, `kubernetes_client_init_failed` или `kubernetes_job_create_failed`, проверять нужно secret ref в `fleet-manager`, настройки `KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_*`, RBAC service account `runtime-manager` и наличие default namespace/image. Значения kubeconfig, токенов, DSN и содержимое Secret не выводить в Issue/PR и не прикладывать к отчётам.

## Митигирование

- Если миграции не прошли, исправить причину и пересоздать `runtime-manager-migrations`.
- Если readiness падает из-за БД, проверить `postgres`, `kodex-postgres-bootstrap-databases` и DSN.
- Если readiness падает из-за event log, проверить `platform-event-log-migrations` и `KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_DSN`.
- Если gRPC возвращает unexpected transport error, проверить service port `grpc`, NetworkPolicy и shared gRPC настройки.
- Если outbox не доставляет события, проверить `runtime_manager_outbox_events` и доступность БД `kodex_platform_event_log`.

## План отката

- Вернуть предыдущий образ `runtime-manager` через image tag или предыдущее rendered manifest.
- Не откатывать миграции вручную без отдельного решения: goose down допустим только после проверки совместимости данных.
- При невозможности быстрого восстановления временно остановить новые runtime-команды на стороне вызывающего сервиса.

## Проверка результата

- `deployment/runtime-manager` в состоянии available.
- `GET /health/readyz` возвращает успешный ответ.
- gRPC boundary отвечает application-level статусом, а не сетевой ошибкой.
- В БД `runtime-manager` доступны таблицы слотов, workspace materialization, job, job step, artifact refs и outbox.

## Пост-действия

- Если сбой был неразовым, завести Issue с root cause и ссылками на безопасные логи.
- Не прикладывать к Issue/PR значения DSN, токенов, адресов целевого сервера или доменов из локального `config.env`.

## Апрув
- request_id: `owner-2026-05-07-runtime-manager-kickoff`
- Решение: approved
- Комментарий: runbook входит в эксплуатационный контур RTM-6.
