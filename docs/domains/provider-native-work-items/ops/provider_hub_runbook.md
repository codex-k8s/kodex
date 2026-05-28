---
doc_id: RB-CK8S-PROVIDER-HUB-0001
type: runbook
title: "provider-hub — runbook: развёртывание и smoke-проверка"
status: active
owner_role: SRE
created_at: 2026-05-14
updated_at: 2026-05-27
related_issues: [754, 770, 840, 895, 908, 909]
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
- При сбоях webhook inbox, пакетной сверки, provider write pipeline, bootstrap/adoption PR, safe merge signal, outbox-доставки или gRPC auth boundary.

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

### Smoke producer path для merge signal

```bash
scripts/smoke-provider-merge-signal.sh
```

Этот staged smoke использует синтетические safe fixtures GitHub `pull_request closed + merged` для bootstrap и adoption из `fixtures/provider-webhooks/**` и не требует live webhook secret, реального домена, provider API или Kubernetes. Проверка состоит из двух частей:

- `integration-gateway` route test принимает подписанные тестовым секретом fixtures и передаёт envelope в owner client без хранения gateway state;
- `provider-hub` domain test обрабатывает fixtures через GitHub normalizer, создаёт safe `RepositoryMergeSignal`, читает его через `GetRepositoryMergeSignal`, проверяет локальные outbox events `provider.repository.bootstrap_merged` / `provider.repository.adoption_merged`, replay без дубля merge event и conflict diagnostic без raw payload.

В fixture mode проверяется граница хранения: canonical webhook payload нужен только во внутреннем retryable inbox до terminal статуса; после обработки `provider_hub_webhook_events.payload_json` содержит safe envelope, а `RepositoryMergeSignal`, read surface, normalized provider event payload и outbox/event-log payload не содержат raw/canonical webhook body, body PR, provider response, diff, checked artifact payload или checked `services.yaml`.

Live HTTP режим запускается отдельно:

```bash
KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_MODE=live-http \
KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_GATEWAY_URL=http://127.0.0.1:18086 \
KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_PROVIDER_HUB_GRPC_ADDR=127.0.0.1:19095 \
scripts/smoke-provider-merge-signal.sh
```

Для live HTTP режима нужны настроенный webhook secret, доступный `integration-gateway`, доступный gRPC `provider-hub` и уже существующая bootstrap/adoption PR-проекция с `project_repository_binding` в `provider-hub`. Без этой provider-side precondition один webhook корректно обновит PR-проекцию, но не создаст onboarding merge signal. Для adoption live check вместе переопределяются `KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_FIXTURE`, `KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_SIGNAL_KEY` и `KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_DELIVERY_ID`. Если требуется проверить публикацию в `platform-event-log`, дополнительно задаётся `KODEX_PROVIDER_MERGE_SIGNAL_SMOKE_CHECK_EVENT_LOG=true` и DSN event-log через безопасный локальный env; значение DSN не выводится.

### Граница webhook inbox и safe diagnostics

`provider-hub` хранит canonical provider webhook payload в `provider_hub_webhook_events.payload_json` только пока webhook остаётся `pending` или `failed` и может быть повторно нормализован через PRV-4 retry/reprocess. После терминального состояния `processed` или `ignored` storage payload заменяется safe envelope с `payload_storage`, `payload_sha256`, delivery/source refs и retention metadata без raw provider body. Это внутренний inbox сервиса и не safe read surface. Соседние сервисы не должны читать storage payload и не должны использовать его как checked artifact input.

Миграция privacy-hardening backfill-ит уже существующие `processed`/`ignored` строки: сначала фиксирует digest текущего canonical payload через стабильную `public.digest`, затем заменяет storage payload safe envelope. Для таких migrated terminal rows envelope содержит `payload_digest_source=postgres_jsonb_text`; поздний duplicate delivery обрабатывается как replay по provider/delivery identity, потому что исходный body уже удалён и повторно сверить runtime compact digest невозможно. `pending`/`failed` строки остаются с canonical payload для retry/reprocess.

Safe outputs `provider-hub` для bootstrap/adoption merge содержат только provider-owned refs/facts/digests/status/timestamps/version: repository refs, PR refs, branches, merge commit sha, source ref, provider operation ref и watermark digest. Raw/canonical webhook payload, подписи, provider response, body PR, diff, checked artifact payload и checked `services.yaml` не должны попадать в `RepositoryMergeSignal`, gRPC read responses, outbox payload, `platform-event-log`, ошибки или safe diagnostics.

`GetWebhookEvent`, `ListWebhookEvents` и retry response возвращают в `WebhookEvent.payload_json` только safe envelope, даже если полный payload временно удерживается внутри retryable inbox-записи. `payload_sha256` можно использовать для диагностики replay/conflict без вывода тела.

Оставшийся privacy backlog: короткий TTL cleanup для retryable payload, encryption-at-rest/KMS policy и re-fetch/reprocess strategy для отказа от полного payload при долгих failed-сценариях.

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
| merge signal conflict | Повтор bootstrap/adoption PR merge пришёл с тем же signal key, но другим commit/source ref | provider target, PR number/url, safe signal key, merge commit sha; raw payload не выгружать |
| reconciliation errors | Ошибка provider API, secret resolver или конфликт курсора | sync cursor, lease, last_error code, rate budget |
| bootstrap PR error | Непустой base branch, совпадающие base/bootstrap refs, нет прав на branch/PR | короткий код операции, refs, права внешнего аккаунта |
| adoption PR error | Совпадающие base/adoption refs, нет прав на branch/PR, конфликт provider validation | короткий код операции, refs, права внешнего аккаунта |

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
