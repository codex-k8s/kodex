---
doc_id: RB-CK8S-FLEET-MANAGER-0001
type: runbook
title: "fleet-manager — runbook: развёртывание и smoke-проверка"
status: active
owner_role: SRE
created_at: 2026-05-13
updated_at: 2026-05-13
related_issues: [738]
related_alerts: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-11-fleet-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-11
---

# Runbook: fleet-manager — развёртывание и smoke-проверка

## TL;DR

- Симптом: `fleet-manager` не стартует, не проходит readiness или не отвечает по gRPC.
- Быстрая диагностика: проверить migration job, секреты DSN/auth, доступность `postgres`, `platform-event-log`, `access-manager` и backend secret resolver.
- Быстрое восстановление: повторить migration job, перезапустить deployment, проверить значения в `kodex-platform-runtime` без вывода самих значений.

## Когда использовать

- После сборки и публикации образов `fleet-manager` и `fleet-manager-migrations`.
- После изменения миграций, deploy-манифестов, fleet env или shared runtime-библиотек.
- При сбоях readiness, gRPC auth boundary, outbox-доставки fleet-событий или проверок связности Kubernetes API.

## Предпосылки и доступы

- Доступ к Kubernetes-кластеру целевой установки.
- Секреты, DSN, адреса и токены берутся из локального bootstrap-профиля и не публикуются в Issue/PR.
- Для gRPC smoke-проверки локально нужен `grpcurl`.
- Перед smoke-путём должен быть подготовлен локальный bootstrap env через `bootstrap/host/bootstrap_cluster.sh`.

## Сборка образов

```bash
KODEX_BUILD_ENV_FILE=/path/to/bootstrap.env \
  scripts/build-fleet-manager-images.sh
```

Скрипт собирает:

- `access-manager` и его миграции как обязательную зависимость проверки доступа;
- `fleet-manager` и его миграции;
- `platform-event-log` migrations image.

## Smoke-проверка

```bash
KODEX_SMOKE_ENV_FILE=/path/to/bootstrap.env \
  scripts/smoke-fleet-manager.sh
```

Путь проверки:

- рендерит манифесты во временный каталог;
- применяет PostgreSQL stack и bootstrap database job;
- применяет `platform-event-log` migrations;
- применяет `access-manager` migrations и deployment;
- применяет `fleet-manager` migrations и deployment;
- проверяет `GET /health/readyz`;
- проверяет gRPC boundary через `FleetManagerService/ListFleetScopes` с ожидаемым application-level статусом.

Smoke не обращается к приватному внешнему серверу напрямую: он работает через переданный kubeconfig и локальный port-forward к сервису в выбранном кластере.

## Диагностика миграций

```bash
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" logs job/fleet-manager-migrations
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get job/fleet-manager-migrations
```

Проверить:

- `KODEX_FLEET_MANAGER_DATABASE_DSN` указывает на БД `kodex_fleet_manager`;
- БД создана `kodex-postgres-bootstrap-databases`;
- образ `fleet-manager-migrations` соответствует версии сервиса.

## Диагностика readiness/liveness/metrics

```bash
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get pods -l app.kubernetes.io/name=fleet-manager
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" describe deploy/fleet-manager
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" port-forward svc/fleet-manager 18084:8080
curl -fsS http://127.0.0.1:18084/health/livez
curl -fsS http://127.0.0.1:18084/health/readyz
curl -fsS http://127.0.0.1:18084/metrics
```

Readiness должна видеть:

- БД `fleet-manager`;
- общую БД `platform-event-log`, если outbox dispatch включён и publisher kind равен `postgres-event-log`.

## Диагностика зависимостей

### access-manager

- Проверить, что `access-manager` доступен по `KODEX_FLEET_MANAGER_ACCESS_MANAGER_GRPC_ADDR`.
- Проверить, что `KODEX_FLEET_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN` совпадает с токеном доступа к `access-manager`.
- Если access checks временно отключены, это должно быть явным операторским решением через `KODEX_FLEET_MANAGER_ACCESS_CHECK_ENABLED=false`, а не скрытым обходом.

### secretresolver

- Для backend `env` проверить только наличие нужной env-переменной, не выводя значение.
- Для backend `kubernetes_mounted_secret` проверить путь root и права чтения mounted secret.
- Для Vault проверить адрес, namespace и наличие токена без вывода токена.
- Значения kubeconfig/service account нельзя писать в логи, события, ошибки или Issue.

### platform-event-log

- Проверить `platform-event-log-migrations`.
- Проверить `KODEX_FLEET_MANAGER_EVENT_LOG_DATABASE_DSN`.
- Проверить backlog в локальной outbox-таблице `fleet-manager`, если события не попадают в общий event log.

### PostgreSQL

- Проверить доступность `postgres`.
- Проверить, что database bootstrap job создаёт `kodex_fleet_manager`.
- Проверить ограничения пула: `KODEX_FLEET_MANAGER_DATABASE_MAX_CONNS` и `KODEX_FLEET_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS` не должны создавать connection storm при увеличении replicas.

## Диагностика Kubernetes connectivity checks

Connectivity check выполняется только по безопасной ссылке на kubeconfig/service account:

- в БД хранится `secret_store_type` и `secret_store_ref`, а не значение секрета;
- snapshot хранит итоговый статус, короткую безопасную причину, latency и время проверки;
- сырые Kubernetes objects, events, logs и kubeconfig не сохраняются в PostgreSQL.

Если проверки падают:

- сначала проверить доступность secret resolver backend;
- затем проверить валидность ссылки на секрет;
- затем проверить сетевую доступность Kubernetes API из pod `fleet-manager`;
- не публиковать реальные endpoint, токены и kubeconfig в Issue/PR.

## Митигирование

- Если миграции не прошли, исправить причину и пересоздать `fleet-manager-migrations`.
- Если readiness падает из-за БД, проверить `postgres`, database bootstrap и DSN.
- Если readiness падает из-за event log, проверить `platform-event-log-migrations` и event-log DSN.
- Если gRPC возвращает transport error, проверить service port `grpc`, NetworkPolicy и shared gRPC настройки.
- Если outbox не доставляет события, проверить локальную outbox-таблицу, `KODEX_FLEET_MANAGER_OUTBOX_*` и доступность event-log БД.

## План отката

- Вернуть предыдущий образ `fleet-manager` через image tag или предыдущий rendered manifest.
- Не откатывать миграции вручную без отдельного решения: `goose down` допустим только после проверки совместимости данных.
- При невозможности быстрого восстановления временно остановить операции регистрации cluster/scope/server или новые runtime-команды на вызывающей стороне.
- Не удалять scope/cluster/server записи вручную из БД: это нарушит журнал placement decisions и события.

## Безопасные ограничения

- `fleet-manager` не запускает workloads и не управляет slot/job/workspace: это зона `runtime-manager`.
- `fleet-manager` не хранит kubeconfig, Kubernetes objects, events и logs.
- `fleet-manager` не выполняет SSH bootstrap, установку Kubernetes, join-node или upgrade cluster в MVP.
- При диагностике запрещено выводить реальные адреса приватных серверов, домены, DSN, токены и содержимое секретов.

## Проверка результата

- `deployment/fleet-manager` в состоянии available.
- `GET /health/readyz` возвращает успешный ответ.
- gRPC boundary отвечает application-level статусом, а не сетевой ошибкой.
- В БД `fleet-manager` доступны таблицы scope, server, cluster, health snapshot, placement rule, placement decision, command result и outbox.

## Пост-действия

- Если сбой был неразовым, завести Issue с root cause и ссылками на безопасные логи.
- Не прикладывать к Issue/PR значения DSN, токенов, адресов целевого сервера или доменов из локального `config.env`.

## Апрув

- request_id: `owner-2026-05-11-fleet-manager-kickoff`
- Решение: approved
- Комментарий: runbook входит в эксплуатационный контур FLEET-6.
