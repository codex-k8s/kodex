---
doc_id: RB-CK8S-PROVIDER-HUB-0001
type: runbook
title: "provider-hub — runbook: развёртывание и smoke-проверка"
status: active
owner_role: SRE
created_at: 2026-05-14
updated_at: 2026-05-14
related_issues: [754]
related_alerts: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-14-provider-hub-deploy"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-14
---

# Runbook: provider-hub — развёртывание и smoke-проверка

## TL;DR

- Симптом: `provider-hub` не стартует, не проходит readiness, не отвечает по gRPC или не публикует `provider.*` события.
- Быстрая диагностика: проверить migration job, `Deployment`, `/health/readyz`, `/metrics`, БД `provider-hub`, БД `platform-event-log`, доступность `access-manager` и параметры secret resolver.
- Быстрое восстановление: исправить env/secret/image, повторить migration job, перезапустить `Deployment/provider-hub`, выполнить smoke-скрипт.

## Когда использовать

- После сборки и публикации образов `provider-hub` и `provider-hub-migrations`.
- После изменения миграций, deploy-манифестов, runtime env или shared Go-библиотек.
- При сбоях webhook inbox, пакетной сверки, provider write pipeline, bootstrap PR, outbox-доставки или gRPC auth boundary.

## Предпосылки и доступы

- Доступ к Kubernetes namespace платформы.
- Доступ к логам `provider-hub`, `provider-hub-migrations`, `access-manager` и `postgres`.
- Нормализованный `bootstrap.env`, подготовленный bootstrap-процессом.
- Локально для smoke-проверки нужны `kubectl`, `curl`, `grpcurl` и `go`.
- Значения секретов, DSN, приватные домены, адреса серверов и provider payload не выводить в логи, Issue, PR и сообщения.

## Сборка образов

```bash
KODEX_BUILD_ENV_FILE=/path/to/bootstrap.env \
  scripts/build-provider-hub-images.sh
```

Скрипт собирает:

- `access-manager` и его миграции как обязательную зависимость проверки доступа;
- `provider-hub` и его миграции;
- `platform-event-log` migrations image.

## Smoke-проверка

```bash
KODEX_SMOKE_ENV_FILE=/path/to/bootstrap.env \
  scripts/smoke-provider-hub.sh
```

Путь проверки:

- рендерит манифесты во временный каталог;
- применяет PostgreSQL stack и bootstrap database job;
- применяет `platform-event-log` migrations;
- применяет `access-manager` migrations и deployment;
- применяет `provider-hub` migrations и deployment;
- проверяет `GET /health/readyz`;
- проверяет gRPC boundary через `ProviderHubService/ListProviderOperations`.

Smoke не выполняет реальные операции GitHub/GitLab и не читает значения provider-секретов. Проверка gRPC допускает прикладной `PermissionDenied`, если токен и transport boundary корректны, но у smoke-актора нет доменных прав.

## Диагностика миграций

```bash
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get job/provider-hub-migrations
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" logs job/provider-hub-migrations
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" describe job/provider-hub-migrations
```

Проверить:

- `KODEX_PROVIDER_HUB_DATABASE_DSN` указывает на БД `kodex_provider_hub`;
- БД создана `kodex-postgres-bootstrap-databases`;
- образ `provider-hub-migrations` соответствует версии сервиса;
- migration job не использует секреты провайдера и не требует доступа к GitHub/GitLab.

## Диагностика rollout и health

```bash
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get deployment/provider-hub service/provider-hub
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" rollout status deployment/provider-hub
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" describe deployment/provider-hub
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" logs deploy/provider-hub
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" port-forward svc/provider-hub 18085:8080
curl -fsS http://127.0.0.1:18085/health/livez
curl -fsS http://127.0.0.1:18085/health/readyz
curl -fsS http://127.0.0.1:18085/metrics
```

Readiness должна видеть:

- БД `provider-hub`;
- общую БД `platform-event-log`, если outbox dispatch включён и publisher kind равен `postgres-event-log`.

## Диагностика зависимостей

### access-manager

- Проверить, что `access-manager` доступен по `KODEX_PROVIDER_HUB_ACCESS_MANAGER_GRPC_ADDR`.
- Проверить, что `KODEX_PROVIDER_HUB_ACCESS_MANAGER_GRPC_AUTH_TOKEN` совпадает с токеном доступа к `access-manager`.
- Не обходить `ResolveExternalAccountUsage`: `provider-hub` не должен сам выбирать и читать provider-токен без подтверждения доступа.

### secret resolver

- Для backend `env` проверять только наличие переменной, не выводя значение.
- Для backend `kubernetes_mounted_secret` проверять корневой путь и права чтения mounted secret.
- Для Vault проверять адрес, namespace и наличие токена без вывода токена.
- Значение provider-секрета не должно попадать в БД, outbox, operation log, логи, ошибки, traces или тестовые снапшоты.

### platform-event-log

- Проверить `platform-event-log-migrations`.
- Проверить `KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_DSN`.
- Если события не доходят, проверить локальную outbox-таблицу `provider-hub` и короткую причину последней ошибки публикации.

### PostgreSQL

- Проверить доступность `postgres`.
- Проверить, что database bootstrap job создаёт `kodex_provider_hub`.
- Проверить лимиты пула: `KODEX_PROVIDER_HUB_DATABASE_MAX_CONNS` и `KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_MAX_CONNS` должны учитывать число replicas и не создавать connection storm.

## Частые отказы

| Симптом | Вероятная причина | Что проверить |
|---|---|---|
| `provider-hub-migrations` падает | БД не создана, неверный DSN или не тот образ миграций | bootstrap job, `KODEX_PROVIDER_HUB_DATABASE_DSN`, image tag |
| `/health/readyz` не проходит | Недоступна БД `provider-hub` или `platform-event-log` | DSN, PostgreSQL, event-log migrations |
| gRPC возвращает `Unauthenticated` | Неверный runtime gRPC token | `KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN` в `kodex-platform-runtime` |
| операции провайдера получают `Forbidden` | Нет права во внешнем аккаунте или отказ `access-manager` | action, scope, `external_account_id`, access-manager logs без токена |
| `reauthorization_required` | Provider-токен недействителен или отозван | состояние внешнего аккаунта и ссылку на секрет без значения |
| rate limit или abuse limit | Исчерпан лимит GitHub/GitLab | лимитный budget, retry-after, число фоновых курсоров |
| webhook backlog растёт | Не успевает нормализация или повторная обработка | статусы inbox, oldest pending/failed, ошибки нормализации |
| reconciliation errors | Ошибка provider API, secret resolver или конфликт курсора | sync cursor, lease, last_error code, rate budget |
| bootstrap PR error | Непустой base branch, совпадающие base/bootstrap refs, нет прав на branch/PR | короткий код операции, refs, права внешнего аккаунта |

## Митигирование

- Если миграции упали из-за временной недоступности БД, удалить failed job и применить migration manifest повторно.
- Если readiness падает из-за БД, проверить `postgres`, database bootstrap и DSN.
- Если readiness падает из-за event log, проверить `platform-event-log-migrations` и event-log DSN.
- Если gRPC transport не отвечает, проверить service port `grpc`, NetworkPolicy и shared gRPC настройки.
- Если outbox не доставляет события, проверить `KODEX_PROVIDER_HUB_OUTBOX_*`, `KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_DSN` и доступность event-log БД.
- Если provider-токен недоступен, не подставлять его вручную в `provider-hub`; исправить ссылку на секрет и права через `access-manager`.

## План отката

- Вернуть предыдущий образ `provider-hub` через image tag или предыдущий rendered manifest.
- Не откатывать миграции вручную без отдельного плана восстановления данных.
- Если новый сервис блокирует rollout платформы, временно не применять `provider-hub` manifests, но оставить БД и общий event log в согласованном состоянии.
- Не удалять provider projections, webhook inbox или operation log вручную: это нарушит идемпотентность сверки и provider-операций.

## Проверка результата

- `Job/provider-hub-migrations` завершён успешно.
- `Deployment/provider-hub` доступен.
- `/health/readyz` возвращает успешный ответ.
- `/metrics` доступен.
- `scripts/smoke-provider-hub.sh` проходит до сообщения `gRPC boundary OK`.

## Пост-действия

- Если была авария, создать Issue с причиной и корректирующими действиями.
- Если обнаружен пробел в манифестах, env или smoke-проверке, обновить этот runbook в том же PR, где исправляется поведение.
- В Issue/PR не прикладывать значения DSN, токенов, адресов целевого сервера, приватных доменов или сырые provider payload.

## Апрув

- request_id: `owner-2026-05-14-provider-hub-deploy`
- Решение: approved
- Комментарий: runbook входит в эксплуатационный контур PRV-9.
