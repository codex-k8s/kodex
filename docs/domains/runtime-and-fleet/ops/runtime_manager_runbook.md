---
doc_id: RB-CK8S-RUNTIME-MANAGER-0001
type: runbook
title: "runtime-manager — runbook: развёртывание и smoke-проверка"
status: active
owner_role: SRE
created_at: 2026-05-08
updated_at: 2026-05-08
related_issues: [661]
related_alerts: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-07-runtime-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-07
---

# Runbook: runtime-manager — развёртывание и smoke-проверка

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
- Для полной gRPC smoke-проверки локально нужен `grpcurl`.
- Перед запуском smoke-пути должен быть подготовлен локальный bootstrap env через `bootstrap/host/bootstrap_cluster.sh`.

## Сборка образов

```bash
KODEX_BUILD_ENV_FILE=/path/to/bootstrap.env \
  scripts/build-runtime-manager-images.sh
```

Скрипт собирает:
- `access-manager` и его миграции как обязательную зависимость проверки доступа;
- `runtime-manager` и его миграции;
- `platform-event-log` migrations image.

## Smoke-проверка

```bash
KODEX_SMOKE_ENV_FILE=/path/to/bootstrap.env \
  scripts/smoke-runtime-manager.sh
```

Путь проверки:
- применяет PostgreSQL stack и bootstrap database job;
- применяет `platform-event-log` migrations;
- применяет `access-manager` migrations и deployment;
- применяет `runtime-manager` migrations и deployment;
- проверяет `GET /health/readyz`;
- проверяет gRPC boundary через вызов `RuntimeManagerService/GetSlot` с ожидаемым application-level статусом.

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
