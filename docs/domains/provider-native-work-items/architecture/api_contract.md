---
doc_id: API-CK8S-PROVIDER-HUB-0001
type: api-contract
title: kodex — API-контракт provider-hub
status: active
owner_role: SA
created_at: 2026-05-06
updated_at: 2026-05-07
related_issues: [281, 282]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-06-provider-hub-boundaries"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-06
---

# API-контракт: provider-hub

## TL;DR

- Тип API: внутренний gRPC для команд и чтений, AsyncAPI для `provider.*` событий.
- Аутентификация: внутренний сервисный контур; команды дополнительно проверяют actor и право использования внешнего аккаунта через `access-manager`.
- Версионирование: стабильный `v1` зафиксирован в proto и AsyncAPI; этот документ объясняет карту операций.
- Основные операции: приём webhook, чтение проекций, операции провайдера, сверка, операционное состояние аккаунтов и снимки лимитов.

## Спецификации

| Контракт | Источник правды |
|---|---|
| gRPC proto | `proto/kodex/providers/v1/provider_hub.proto` |
| AsyncAPI | `specs/asyncapi/provider-hub.v1.yaml` |
| Go-контракты событий | `libs/go/platformevents/provider/events.gen.go` |

## Группы операций

### Webhook ingest

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `IngestWebhookEvent` | Принять проверенный webhook от пограничного слоя. | `integration-gateway` | По `provider_slug + delivery_id`. |
| `GetWebhookEvent` | Прочитать входящее событие для диагностики. | Операторский контур | Read-only. |
| `ListWebhookEvents` | Получить журнал входящих событий с фильтрами. | Операторский контур | Read-only. |
| `RetryWebhookEventProcessing` | Поставить событие на повторную нормализацию. | Операторский контур | По версии события. |

`provider-hub` не проверяет публичную подпись webhook. Он принимает только уже проверенный внутренний вызов.

### Проекции рабочих артефактов

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `GetWorkItemProjection` | Прочитать `Issue` или `PR/MR` по внутреннему id. | `agent-manager`, `operations-hub`, MCP | Read-only. |
| `FindWorkItemByProviderRef` | Найти проекцию по `owner/repo/number`, URL или provider id. | `agent-manager`, MCP | Read-only. |
| `ListWorkItemProjections` | Список по проекту, репозиторию, состоянию, типу, меткам, drift status. | `operations-hub`, `agent-manager`, MCP | Read-only. |
| `ListComments` | Комментарии, mentions и review-сигналы по артефакту. | `agent-manager`, `operations-hub` | Read-only. |
| `ListRelationships` | Связи артефакта с задачами, PR, follow-up и блокировками. | `agent-manager`, `operations-hub` | Read-only. |

### Reconciliation

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `RegisterProviderArtifactSignal` | Ускоряющий сигнал от slot-агента или agent-manager. | MCP, `agent-manager` | По signal id или provider ref + окно времени. |
| `EnqueueReconciliation` | Поставить область в очередь сверки. | Админский контур, сервисы-владельцы | По scope + idempotency key. |
| `RunReconciliationBatch` | Выполнить пачку сверки. | `worker` по поручению домена | Lease на `SyncCursor`. |
| `GetSyncCursor` | Прочитать состояние курсора. | Операторский контур | Read-only. |
| `ListSyncCursors` | Список курсоров и drift status. | Операторский контур | Read-only. |

Ручная пользовательская кнопка синхронизации не является нормальным UX. Допустима только админская постановка reconciliation job в очередь.

### Операции провайдера

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `CreateIssue` | Создать provider-native `Issue`. | `agent-manager`, MCP | `command_id`. |
| `UpdateIssue` | Обновить title/body/labels/type/assignees допустимыми полями. | `agent-manager`, MCP | `command_id + expected_provider_version`, если доступна. |
| `CreateComment` | Создать комментарий. | `agent-manager`, MCP | `command_id`. |
| `UpdateComment` | Обновить комментарий платформы, если policy разрешает. | `agent-manager`, MCP | `command_id + expected_provider_version`, если доступна. |
| `CreatePullRequest` | Создать `PR/MR`, если операция относится к платформенному сценарию. | `agent-manager`, MCP, package flow | `command_id`. |
| `CreateReviewSignal` | Оставить review/comment/approval там, где поддерживается провайдером. | Acceptance/gatekeeper контур | `command_id`. |
| `UpdateRelationship` | Зафиксировать или обновить provider-native связь, если провайдер поддерживает. | `agent-manager`, MCP | `command_id`. |

Каждая операция сначала запрашивает разрешение у `access-manager`. Если операция выполнена агентом напрямую через `gh` в слоте, она не попадает в этот набор как команда, но может передать ускоряющий сигнал и лимитный снимок.

В `UpdateIssue` списковые поля передаются через сообщения-патчи:
отсутствующее сообщение означает «не менять», присутствующее сообщение с пустым списком означает «очистить список», присутствующее сообщение со значениями означает «заменить список».

### Операционное состояние аккаунтов и лимиты

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `GetProviderAccountRuntimeState` | Получить операционное состояние аккаунта у провайдера. | Операторский контур, `agent-manager` | Read-only. |
| `ListProviderAccountRuntimeStates` | Список состояний по провайдеру, организации, проекту или статусу. | Операторский контур | Read-only. |
| `RecordProviderLimitSnapshot` | Записать снимок лимита после операции или от slot-агента. | `provider-hub`, MCP | По source + captured_at + account + class. |
| `ListProviderLimitSnapshots` | Диагностика лимитов. | Операторский контур | Read-only. |
| `ListProviderOperations` | Журнал операций провайдера. | Операторский контур, аудит | Read-only. |

## Модель ошибок

| Код | Смысл |
|---|---|
| `PROVIDER_PERMISSION_DENIED` | `access-manager` не разрешил использование аккаунта или действие. |
| `PROVIDER_AUTH_REQUIRED` | Аккаунт требует повторной авторизации. |
| `PROVIDER_RATE_LIMITED` | Лимит провайдера исчерпан или близок к исчерпанию. |
| `PROVIDER_NOT_FOUND` | Provider object не найден или недоступен. |
| `PROVIDER_CONFLICT` | Ожидаемая версия или состояние устарели. |
| `PROVIDER_RETRYABLE_ERROR` | Временная ошибка внешнего API. |
| `PROVIDER_PERMANENT_ERROR` | Ошибка не должна повторяться без изменения входных данных. |
| `PROVIDER_WEBHOOK_DUPLICATE` | Webhook уже принят. |
| `PROVIDER_DRIFT_DETECTED` | Проекция могла устареть и требует сверки. |

## События

| Событие | Когда публикуется |
|---|---|
| `provider.webhook.received` | Webhook принят во входящий журнал. |
| `provider.webhook.normalized` | Raw webhook разобран в нормализованное событие. |
| `provider.work_item.synced` | Проекция `Issue` или `PR/MR` обновлена. |
| `provider.work_item.drift_detected` | Обнаружена возможная рассинхронизация. |
| `provider.comment.synced` | Комментарий, mention или review-сигнал обновлён. |
| `provider.relationship.synced` | Связь обновлена или подтверждена. |
| `provider.sync_cursor.advanced` | Курсор сверки успешно продвинут. |
| `provider.account_runtime_state.changed` | Изменилось состояние аккаунта у провайдера. |
| `provider.limit_snapshot.recorded` | Зафиксирован снимок лимитов. |
| `provider.operation.completed` | Provider-операция завершилась успешно. |
| `provider.operation.failed` | Provider-операция завершилась ошибкой. |
| `provider.repository.bootstrap_required` | Provider-состояние показывает, что репозиторий пустой и требует решения о первичной инициализации. |
| `provider.repository.adoption_required` | Provider-состояние показывает, что существующий репозиторий требует агентного сканирования, отчёта и adoption через reviewable PR. |
| `provider.repository.bootstrap_completed` | Bootstrap пустого репозитория завершён. |
| `provider.repository.adoption_pr_created` | Создан reviewable PR для adoption. |

## Совместимость

- gRPC и AsyncAPI `v1` должны покрыть согласованный объём домена, даже если реализация поставляется по срезам.
- Если контракт опережает реализацию, delivery-документ содержит таблицу реализованных и отложенных операций.
- GitHub-specific поля остаются за adapter boundary или в provider-specific payload, если они не являются частью нормализованного контракта.

## Апрув

- request_id: `owner-2026-05-06-provider-hub-boundaries`
- Решение: approved
- Комментарий: API-карта `provider-hub` согласована как целевое состояние PRV-0.
