---
doc_id: RB-CK8S-GOVERNANCE-MANAGER-0001
type: runbook
title: "governance-manager — runbook: развёртывание и smoke-проверка"
status: active
owner_role: SRE
created_at: 2026-05-27
updated_at: 2026-05-27
related_issues: []
related_alerts: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-27-governance-manager-ops"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-27
---

# Runbook: governance-manager — развёртывание и smoke-проверка

## TL;DR

- Симптом: `governance-manager` не стартует, не проходит readiness, не отвечает по gRPC или не публикует `governance.*` события.
- Быстрая диагностика: проверить migration job, `Deployment`, `/health/readyz`, `/metrics`, БД `governance-manager`, БД `platform-event-log`, доступность `access-manager` и outbox-настройки.
- Быстрое восстановление: исправить env/secret/image, повторить migration job, перезапустить `Deployment/governance-manager`, выполнить smoke-скрипт.

## Когда использовать

- После сборки и публикации образов `governance-manager` и `governance-manager-migrations`.
- После изменения миграций, deploy-манифестов, runtime env, outbox/event-log настроек или shared Go-библиотек.
- При сбоях risk assessment, review signal refs, gate lifecycle, release decision package, release decisions, safety-loop state или outbox-доставки.

## Предпосылки и доступы

- Доступ к Kubernetes namespace платформы.
- Доступ к логам `governance-manager`, `governance-manager-migrations`, `access-manager` и `postgres`.
- Нормализованный `bootstrap.env`, подготовленный bootstrap-процессом.
- Локально для smoke-проверки нужны `kubectl`, `curl` и `go`.
- Значения секретов, DSN, приватные домены, адреса серверов, raw provider payload, prompt/transcript, stdout/stderr и runtime logs не выводить в логи, Issue, PR и сообщения.

## Образы

`governance-manager` использует один Dockerfile с отдельными стадиями:

- `prod` — сервисный процесс с HTTP health/metrics и gRPC boundary;
- `migrations` — goose runner с миграциями `services/internal/governance-manager/cmd/cli/migrations`;
- `dev` — hot reload для локальной разработки.

Серверный контур не требует Docker daemon: builder может собирать стадии `prod` и `migrations` через Kaniko или совместимый registry workflow по данным `services.yaml`.

## Smoke-проверка

```bash
KODEX_SMOKE_ENV_FILE=/path/to/bootstrap.env \
  scripts/smoke-governance-manager.sh
```

Путь проверки:

- рендерит манифесты во временный каталог;
- применяет PostgreSQL stack и bootstrap database job;
- применяет `platform-event-log` migrations;
- применяет `access-manager` migrations и deployment;
- применяет `governance-manager` migrations и deployment;
- проверяет `GET /health/readyz`.

Smoke не вызывает risk/gate/release business-команды и не передаёт provider, agent, interaction или runtime payload. Проверка подтверждает миграции, runtime env, доступность БД и готовность HTTP health boundary.

## Диагностика миграций

```bash
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get job/governance-manager-migrations
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" logs job/governance-manager-migrations
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" describe job/governance-manager-migrations
```

Проверить:

- `KODEX_GOVERNANCE_MANAGER_DATABASE_DSN` указывает на БД `kodex_governance_manager`;
- БД создана `kodex-postgres-bootstrap-databases`;
- образ `governance-manager-migrations` соответствует версии сервиса;
- migration job не требует доступа к provider API, agent state, runtime execution или interaction delivery.

## Диагностика rollout и health

```bash
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get deployment/governance-manager service/governance-manager
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" rollout status deployment/governance-manager
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" describe deployment/governance-manager
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" logs deploy/governance-manager
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" port-forward svc/governance-manager 18086:8080
curl -fsS http://127.0.0.1:18086/health/livez
curl -fsS http://127.0.0.1:18086/health/readyz
curl -fsS http://127.0.0.1:18086/metrics
```

Readiness должна видеть:

- БД `governance-manager`;
- общую БД `platform-event-log`, если outbox dispatch включён и publisher kind равен `postgres-event-log`.

## Диагностика зависимостей

### access-manager

- Проверить, что `access-manager` доступен по `KODEX_GOVERNANCE_MANAGER_ACCESS_MANAGER_GRPC_ADDR`.
- Проверить, что `KODEX_GOVERNANCE_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN` совпадает с токеном доступа к `access-manager`.
- Не дублировать membership, roles или access policy в `governance-manager`: сервис только вызывает access boundary.

### platform-event-log

- Проверить `platform-event-log-migrations`.
- Проверить `KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_DSN`.
- Если события не доходят, проверить локальную outbox-таблицу `governance-manager` и короткую причину последней ошибки публикации.
- Входящий потребитель `provider.comment.synced` выключен по умолчанию; включать его только явным `KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_ENABLED=true` после готовности access policy и плана выкладки.
- Входящий потребитель `interaction.request.response_recorded` для gate decision выключен по умолчанию; включать его только явным `KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_ENABLED=true` после готовности access policy для service actor `interaction-hub` и согласованного потока Human gate в `interaction-hub`.

### PostgreSQL

- Проверить доступность `postgres`.
- Проверить, что database bootstrap job создаёт `kodex_governance_manager`.
- Проверить лимиты пула: `KODEX_GOVERNANCE_MANAGER_DATABASE_MAX_CONNS` и `KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS` должны учитывать число replicas и не создавать connection storm.

## Частые отказы

| Симптом | Вероятная причина | Что проверить |
|---|---|---|
| `governance-manager-migrations` падает | БД не создана, неверный DSN или не тот образ миграций | bootstrap job, `KODEX_GOVERNANCE_MANAGER_DATABASE_DSN`, image tag |
| `/health/readyz` не проходит | Недоступна БД `governance-manager` или `platform-event-log` | DSN, PostgreSQL, event-log migrations |
| gRPC возвращает `Unauthenticated` | Неверный runtime gRPC token | `KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN` в `kodex-platform-runtime` |
| команды возвращают `PermissionDenied` | Отказ `access-manager` или неверный access token | action key, actor/scope refs, access-manager logs без токена |
| outbox backlog растёт | Event-log БД недоступна или publisher не успевает | outbox config, event-log DSN, PostgreSQL pool |
| release package refs rejected | Вход содержит конфликтующие snapshots или unsafe refs | canonical `domain/kind/ref`, bounded summary, отсутствие raw payload |

## Митигирование

- Если миграции упали из-за временной недоступности БД, удалить failed job и применить migration manifest повторно.
- Если readiness падает из-за БД, проверить `postgres`, database bootstrap и DSN.
- Если readiness падает из-за event log, проверить `platform-event-log-migrations` и event-log DSN.
- Если gRPC transport не отвечает, проверить service port `grpc`, NetworkPolicy и shared gRPC настройки.
- Если outbox не доставляет события, проверить `KODEX_GOVERNANCE_MANAGER_OUTBOX_*`, `KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_DSN` и доступность event-log БД.
- Если access checks недоступны, не отключать их для production без отдельного risk decision; восстановить `access-manager` или service token.

## План отката

- Вернуть предыдущий образ `governance-manager` через image tag или предыдущий rendered manifest.
- Не откатывать миграции вручную без отдельного плана восстановления данных.
- Если новый сервис блокирует rollout платформы, временно не применять `governance-manager` manifests, но оставить БД и общий event log в согласованном состоянии.
- Не удалять risk assessments, review signals, gate decisions, release decisions, safety-loop state или outbox вручную: это нарушит audit trail и идемпотентность.

## Проверка результата

- `Job/governance-manager-migrations` завершён успешно.
- `Deployment/governance-manager` доступен.
- `/health/readyz` возвращает успешный ответ.
- `/metrics` доступен.
- `scripts/smoke-governance-manager.sh` проходит до сообщения `readyz OK`.

## Пост-действия

- Если была авария, создать Issue с безопасными симптомами, root cause и корректирующими действиями.
- Если обнаружен пробел в манифестах, env или smoke-проверке, обновить этот runbook в том же PR, где исправляется поведение.
- В Issue/PR не прикладывать значения DSN, токенов, адресов целевого сервера, приватных доменов, raw provider payload, prompt/transcript, stdout/stderr или runtime logs.

## Апрув

- request_id: `owner-2026-05-27-governance-manager-ops`
- Решение: approved
- Комментарий: runbook фиксирует эксплуатационный контур `governance-manager` для первого backend deploy.
