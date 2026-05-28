---
doc_id: MON-CK8S-PROVIDER-HUB-0001
type: monitoring
title: "provider-hub — наблюдаемость"
status: active
owner_role: SRE
created_at: 2026-05-14
updated_at: 2026-05-28
related_issues: [754, 770, 840, 908]
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-14-provider-hub-deploy"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-14
---

# Наблюдаемость: provider-hub

## TL;DR

- Дашборды: readiness, gRPC, PostgreSQL, outbox, webhook inbox, reconciliation cursors, provider operations, лимиты, bootstrap/adoption PR и safe merge signals.
- Метрики: ошибки доступа, ошибки secret resolver, rate limit, backlog webhook/outbox/reconciliation, latency provider API и частота retryable ошибок.
- Логи: только безопасные идентификаторы `request_id`, `provider_slug`, `external_account_id`, `work_item_id`, `operation_id`, `sync_cursor_id`.
- Алерты: недоступность readiness, падение migration job, outbox backlog, рост webhook backlog, stuck cursors, auth failure, rate limit exhaustion, bootstrap/adoption PR failures и конфликтующие merge signals.

## Источники данных

- HTTP health: `/health/livez`, `/health/readyz`.
- HTTP metrics: `/metrics`.
- gRPC server metrics: общий runtime из `libs/go/grpcserver`.
- PostgreSQL: БД `kodex_provider_hub` и общая БД `kodex_platform_event_log`.
- Kubernetes: deployment, migration job, pod status и events.
- Логи приложения: structured logs без секретов, raw provider payload, DSN, токенов, email и приватных endpoint.

## Дашборды

| Название | Ссылка | Для чего | Owner |
|---|---|---|---|
| Provider hub overview | TBD | Общий статус сервиса, readiness, gRPC, БД и outbox. | SRE |
| Provider webhook inbox | TBD | Pending/failed webhook backlog, дедупликация, возраст старейшей записи. | SRE |
| Provider reconciliation | TBD | Hot/warm/cold cursors, lease, retry, drift status и ошибки сверки. | SRE |
| Provider operations | TBD | Write pipeline, статусы операций, retryable/permanent ошибки, bootstrap/adoption PR. | SRE |
| Provider limits | TBD | Rate limit budget по provider/account, auth failure и reauthorization required. | SRE |

## Golden signals

- Latency: длительность gRPC-команд, PostgreSQL-запросов, provider API-вызовов и обработки webhook.
- Traffic: количество gRPC-запросов, webhook deliveries, reconciliation batches и provider write operations.
- Errors: gRPC-коды, ошибки БД, ошибки `access-manager`, ошибки secret resolver, provider error kind и ошибки outbox.
- Saturation: `MaxInFlight`, active PostgreSQL connections, размер outbox backlog, размер webhook backlog, количество active leases и остаток provider rate limit.

## Доменный мониторинг

- Количество webhook inbox записей по статусам `pending`, `processing`, `processed`, `failed`.
- Возраст самой старой `pending` и `failed` записи webhook inbox.
- Количество safe cleanup/reprocess результатов `payload_unavailable`, `payload_expired`, `refetch_unavailable`, `provider_rate_limited` и возраст самой старой `pending`/`failed` записи, где `retain_until` уже истёк.
- Количество `sync_cursor` по priority `hot`, `warm`, `cold` и состояниям lease.
- Возраст самого старого cursor без успешной сверки.
- Количество `ProviderOperation` по типу операции и статусу.
- Частота `provider.operation.completed` и `provider.operation.failed`.
- Частота `provider.repository.bootstrap_merged` и `provider.repository.adoption_merged`, а также конфликтов merge signal по signal key.
- Количество внешних аккаунтов в состояниях `healthy`, `rate_limited`, `reauthorization_required`, `disabled`.
- Доля bootstrap/adoption PR операций с отказом по причине пустоты base branch для bootstrap, конфликтов refs, отсутствия прав или provider validation.

## Логи

Логи должны содержать только безопасные идентификаторы:

- `request_id`, `actor_type`, `actor_id`;
- `provider_slug`, `external_account_id`, `provider_target`;
- `work_item_id`, `comment_id`, `relationship_id`;
- `operation_id`, `command_id`, `sync_cursor_id`, `webhook_event_id`;
- короткий `error_code` и классификацию provider error.

В логи не попадают:

- provider token, Vault token, gRPC token, DSN;
- `secret_store_ref`, если он раскрывает путь к чувствительному хранилищу;
- raw webhook payload, raw provider response и тело файлов bootstrap/adoption PR;
- подписи webhook, merge signal source refs вместе с raw body или содержимое `services.yaml`;
- email, имена пользователей, приватные домены и адреса серверов из локального bootstrap-профиля.

## Проверки и рутинные health checks

- Liveness: процесс отвечает на `/health/livez`.
- Readiness: процесс видит БД `provider-hub` и, при включённой outbox-доставке, БД `platform-event-log`.
- gRPC integration check: `ProviderHubService/ListProviderOperations` должен давать application-level статус, а не сетевую ошибку.
- Webhook inbox: oldest pending не должен выходить за допустимое окно.
- Webhook inbox privacy: `payload_json` должен оставаться safe envelope only; legacy `retained_for_retry` payload очищается явной служебной операцией, read surface показывает только safe envelope и `payload_sha256`.
- Reconciliation: hot cursors не должны оставаться без попыток обработки.
- Provider operations: retryable errors должны снижаться после retry window, permanent errors не должны бесконечно повторяться.

## Алерты

- `provider-hub` readiness недоступен дольше установленного окна.
- `provider-hub` migration job завершился ошибкой.
- Outbox backlog растёт или самое старое событие старше допустимого порога.
- Webhook inbox backlog растёт или oldest pending/failed старше допустимого порога.
- Есть `pending`/`failed` записи с истёкшим `retain_until`, но cleanup не переводит их в safe envelope.
- Hot reconciliation cursors застряли без lease или без `last_success_at`.
- Частота `reauthorization_required` выросла по одному provider/account или группе аккаунтов.
- Rate limit budget ниже порога, а очередь hot cursors продолжает расти.
- Доля transient provider errors выше baseline.
- Bootstrap PR операции повторно падают с одинаковым error code.
- Conflict rate по safe merge signal растёт для одного provider target.
- `access-manager` недоступен для `ResolveExternalAccountUsage`.
- Secret resolver возвращает repeated not found/permission errors.

## Открытые вопросы

- Конкретные Prometheus recording rules и alert rules будут закреплены после появления штатного observability stack.
- GitLab-специфичные дашборды добавляются вместе с GitLab provider adapter.
- Метрики публичного webhook endpoint относятся к будущему `integration-gateway`, а не к `provider-hub`.

## Апрув

- request_id: `owner-2026-05-14-provider-hub-deploy`
- Решение: approved
- Комментарий: monitoring-документ входит в эксплуатационный контур PRV-9.
