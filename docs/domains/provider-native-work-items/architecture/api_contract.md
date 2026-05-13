---
doc_id: API-CK8S-PROVIDER-HUB-0001
type: api-contract
title: kodex — API-контракт provider-hub
status: active
owner_role: SA
created_at: 2026-05-06
updated_at: 2026-05-13
related_issues: [281, 282, 711, 719, 725, 729, 737]
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
| `ListComments` | Комментарии, mentions и review-сигналы по артефакту; для review-сигналов возвращает нормализованный `review_state`. | `agent-manager`, `operations-hub` | Read-only. |
| `ListRelationships` | Связи артефакта с задачами, PR, follow-up и блокировками. | `agent-manager`, `operations-hub` | Read-only. |

### Reconciliation

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `RegisterProviderArtifactSignal` | Ускоряющий сигнал от slot-агента или agent-manager. | MCP, `agent-manager` | По signal id, ключу идемпотентности команды или provider ref + окно времени. |
| `EnqueueReconciliation` | Поставить область в очередь сверки. | Админский контур, сервисы-владельцы | По `provider_slug + scope_type + scope_ref + idempotency_key`; `idempotency_key` и `external_account_id` обязательны. |
| `RunReconciliationBatch` | Выполнить пачку сверки. | `worker` по поручению домена | Lease на `SyncCursor`; `max_items` должен быть положительным и не выше сервисного лимита; внешний аккаунт берётся из курсора и подтверждается через `access-manager` перед API провайдера. |
| `GetSyncCursor` | Прочитать состояние курсора. | Операторский контур | Read-only. |
| `ListSyncCursors` | Список курсоров и drift status. | Операторский контур | Read-only. |

Ручная пользовательская кнопка синхронизации не является нормальным UX. Допустима только админская постановка reconciliation job в очередь.

Перед внешним API-вызовом пакетная сверка подтверждает выбранный внешний аккаунт через `ResolveExternalAccountUsage`. Значение секрета не входит в gRPC-контракт: после положительного решения доступа сервис использует общий `libs/go/secretresolver`, получает `SecretValue` только в памяти процесса и не записывает его в журнал операций, события, аудит, трассировку, логи или ошибки.

В текущем объёме только чтения `RunReconciliationBatch` поддерживает GitHub repository/work item cursors для `issue`, `pull_request`, `comment`, `relationship` и `repository`. Обработчик читает provider API, сохраняет только нормализованные projections и безопасные статусы, а cursor продвигает через локальную транзакцию. Операции записи в провайдера, создание `Issue`/`PR`/comment/review и MCP-инструменты остаются отдельными операциями.

`RegisterProviderArtifactSignal` принимает `external_account_id`, выбранный политикой вызывающего сценария. `provider-hub` не выбирает аккаунт неявно, не получает значение секрета и не ходит во внешний API провайдера. Сигнал только создаёт или поднимает до `hot` курсоры сверки для переданного `ProviderTarget`.

Поддерживаемые формы `ProviderTarget`:

- `repository_full_name + work_item_kind + number` — точный `Issue`, `PR` или `MR`;
- `repository_full_name + number` без `work_item_kind` — рабочий артефакт неизвестного типа, который нужно быстро досверить;
- `provider_object_id` без `work_item_kind` — рабочий артефакт по стабильному id провайдера;
- `web_url` без `work_item_kind` — рабочий артефакт по безопасной ссылке провайдера;
- `repository_full_name` или `provider_repository_id` без полей рабочего артефакта — repository scope.

Если тип рабочего артефакта известен, ставятся курсоры основного артефакта, комментариев и связей. Если тип неизвестен, ставятся hot cursors для `issue`, `pull_request`, `merge_request`, комментариев и связей; обработчик сверки сам определяет фактический тип. Repository target создаёт только курсор репозитория.

Идемпотентность сигнала хранится отдельно от очереди сверки. Явный `signal_id`, `meta.idempotency_key` и `meta.command_id` являются signal-level ключами: повтор с тем же ключом и тем же `target`, `external_account_id`, `source`, payload и временем наблюдения возвращает уже принятую запись, а повтор с другой областью считается конфликтом. Сохранение signal-level следа и постановка курсоров выполняются одной транзакцией, поэтому принятый сигнал не может остаться без соответствующего `ReconciliationRequest` и `SyncCursor`. Резервный ключ по provider ref и минутному окну времени остаётся target-scoped и нужен только когда вызывающий контур не передал явный ключ.

### Операции провайдера

Инструменты записи провайдера — это типизированные внешние инструменты для `agent-manager`, `platform-mcp-server` и внутренних контуров приёмки. Снаружи каждый инструмент имеет отдельный gRPC-метод и типизированный запрос, а внутри `provider-hub` все команды проходят общий конвейер команд:

1. Проверить `CommandMeta`, `command_id` или `idempotency_key`, актёра и безопасный `RequestContext`.
2. Проверить выбранный `external_account_id` через `access-manager` с нужным действием доступа.
3. Проверить наличие `operation_policy_context`; если `approval_required=true`, проверить наличие `approval_gate_ref`.
4. Если команда требует внешнего write-вызова, взять значение секрета через `libs/go/secretresolver` только на время вызова адаптера.
5. Выполнить общий command pipeline: до внешнего write-вызова зарезервировать `ProviderOperation` в состоянии `in_progress`, проверить optimistic concurrency только для новой команды, зафиксировать `operation_policy_context` и `approval_gate_ref`, выполнить provider write через подключённый адаптер и сразу завершить операцию вместе с обновлением локальных проекций или связи.
6. Вернуть `ProviderOperationResponse` с безопасным результатом без токенов, сырых provider payload и внутренних ссылок на секреты.

`provider-hub` не становится владельцем approval-сервиса. Он принимает ссылку на уже принятое решение как `approval_gate_ref`, фиксирует её в журнале операции и отклоняет команду, если политика вызывающего контура указала обязательность gate, но ссылка не передана.

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `CreateIssue` | Создать provider-native `Issue`. | `agent-manager`, MCP | `command_id`. |
| `UpdateIssue` | Обновить title/body/labels/type/assignees допустимыми полями. | `agent-manager`, MCP | `command_id + expected_provider_version`, если доступна. |
| `CreateComment` | Создать комментарий. | `agent-manager`, MCP | `command_id`. |
| `UpdateComment` | Обновить комментарий платформы, если policy разрешает. | `agent-manager`, MCP | `command_id + expected_provider_version`, если доступна. |
| `CreatePullRequest` | Создать `PR/MR`, если операция относится к платформенному сценарию. | `agent-manager`, MCP, package flow | `command_id`. |
| `CreateReviewSignal` | Оставить review/comment/approval там, где поддерживается провайдером. | Acceptance/gatekeeper контур | `command_id`. |
| `UpdateRelationship` | Зафиксировать или обновить provider-native связь, если провайдер поддерживает. | `agent-manager`, MCP | `command_id`. |

Каждая операция сначала запрашивает разрешение у `access-manager`. После положительного решения `provider-hub` получает секрет через `libs/go/secretresolver` только для операций, которым нужен внешний вызов, и удерживает значение в памяти процесса на время вызова адаптера. Для GitHub адаптер записи уже выполняет внешний write поверх общего pipeline; операции локального зеркала, например `UpdateRelationship`, не читают секрет и не обращаются к GitHub API. Если операция выполнена агентом напрямую через `gh` в слоте, она не попадает в этот набор как команда, но может передать ускоряющий сигнал и лимитный снимок.

В `UpdateIssue` списковые поля передаются через сообщения-патчи:
отсутствующее сообщение означает «не менять», присутствующее сообщение с пустым списком означает «очистить список», присутствующее сообщение со значениями означает «заменить список».

### Каталог инструментов записи провайдера

Общие входные поля для всех команд записи:

- `external_account_id` — выбранный вызывающим контуром внешний аккаунт;
- `repository_target` — provider-native репозиторий для команд создания `Issue` и `PR/MR`; `project_id` и `repository_id` остаются ссылками на проектную модель и не используются адаптером как `owner/repo`;
- `meta.command_id` или `meta.idempotency_key` — защита повтора;
- `meta.operation_policy_context` — вход и результат политики по риску: роль, проект, стадия, операция, цель, изменяемые поля, риск, версия политики;
- `meta.approval_gate_ref` — ссылка на approval/gate, если политика требует подтверждение;
- `meta.expected_version` — версия локального агрегата `provider-hub`, если команда меняет уже известную проекцию;
- `expected_provider_version` — версия или update marker провайдера, если провайдер поддерживает конкурентную защиту конкретного объекта.

| Инструмент | Действие доступа | Ссылка на approval/gate | Ожидаемая версия | Можно менять | Запрещено менять | Результат | События и журнал |
|---|---|---|---|---|---|---|---|
| `CreateIssue` | `provider.issue.write` | Требуется, если политика по роли, проекту, стадии, target или полям повышает риск. | `meta.command_id` обязателен; provider version не нужен. | `title`, `body`, `labels`, `assignee_provider_logins`, `milestone`, `work_item_type`, `watermark_json`. | Владение проектом у провайдера, скрытые поля доступа, секреты, runtime-статусы, поля вне типизированного запроса. | `ProviderOperationResponse` с операцией, созданной проекцией рабочего артефакта, result target, provider id, URL и provider version, если доступна. | `ProviderOperation` типа `CREATE_ISSUE`; после успешной записи `provider.operation.completed`, `provider.work_item.synced` или горячая сверка; при ошибке `provider.operation.failed`. |
| `UpdateIssue` | `provider.issue.write` | Требуется для опасных полей, release/ops стадий, цели, чувствительной для владельца, или высокой роли риска. | `meta.expected_version` для локальной проекции; `expected_provider_version`, если вызывающий контур работал с маркером провайдера. | `title`, `body`, `labels`, `assignee_provider_logins`, `milestone`, `state`, `work_item_type`, `watermark_json`. | Авторство, provider id, ссылка на репозиторий, привязка проекта, runtime-диагностика, ссылки на секреты, поля вне типизированного патча. | `ProviderOperationResponse` с операцией, обновлённой проекцией или поставленной сверкой, provider version. | `ProviderOperation` типа `UPDATE_ISSUE`; `provider.operation.completed/failed`, `provider.work_item.synced` или горячая сверка. |
| `CreateComment` | `provider.comment.write` | Требуется, если комментарий создаёт внешнее обязательство, решение владельца или публичное обновление статуса в рискованной стадии. | `meta.command_id` обязателен; provider version не нужен. | `body` нового комментария к `ProviderTarget`. | Изменение чужих комментариев, review-решение, labels, state, секреты и вложения вне согласованного payload. | `ProviderOperationResponse` с операцией, проекцией комментария, result target, provider comment id и URL. | `ProviderOperation` типа `CREATE_COMMENT`; `provider.operation.completed/failed`, `provider.comment.synced` или горячая сверка. |
| `UpdateComment` | `provider.comment.write` | Требуется при обновлении комментариев, видимых владельцу, release notes, подтверждения approval или публичных статусов. | `expected_provider_version`, если маркер провайдера известен; `meta.expected_version` для локальной проекции. | `body` платформенного комментария, который политика разрешает обновлять. | Чужие комментарии без политики владения, review-решение, целевой объект, provider id, секреты. | `ProviderOperationResponse` с операцией, проекцией комментария и provider version. | `ProviderOperation` типа `UPDATE_COMMENT`; `provider.operation.completed/failed`, `provider.comment.synced` или горячая сверка. |
| `CreatePullRequest` / `CreateMergeRequest` | `provider.pull_request.write` | Обычно требуется для защищённой ветки, release/adoption/bootstrap, package source, prod-impact и high-risk изменений. | `meta.command_id` обязателен; provider version не нужен. | `title`, `body`, `head_branch`, `base_branch`, `draft`, `labels`, `linked_issue_ref`, `watermark_json`. | Прямой merge, force push, изменение branch policy, обход обязательных проверок, секреты, владение проектом у провайдера. | `ProviderOperationResponse` с операцией, проекцией `PR/MR`, provider id, URL и provider version, если доступна. | `ProviderOperation` типа `CREATE_PULL_REQUEST`; `provider.operation.completed/failed`, `provider.work_item.synced` или горячая сверка. |
| `CreateReviewSignal` | `provider.review_signal.write` | Требуется для approval или changes requested, если политика не допускает автоматическое решение конкретной роли. | `meta.command_id` обязателен; provider version не нужен. | `kind`, `body`, `inline_comments` с полями `path`, `body`, `line`, `start_line`, `side`, `start_side`, `in_reply_to_provider_comment_id`. | Merge, изменение PR body/branch, изменение labels/state, скрытые проверки, review от имени неподтверждённого актёра, свободный JSON payload. | `ProviderOperationResponse` с операцией, проекцией комментария или review, result target и provider review id. | `ProviderOperation` типа `CREATE_REVIEW_SIGNAL`; `provider.operation.completed/failed`, `provider.comment.synced` или горячая сверка. |
| `UpdateRelationship` | `provider.relationship.write` | Требуется, если связь влияет на release, blocker, follow-up или cross-project dependency. | `meta.expected_version` для локальной связи; provider version не нужен, если связь хранится только в зеркале. | `source`, `target`, `target_provider_ref`, `relationship_type`, `source_kind`, `confidence`. | Изменение provider object без отдельной команды записи, удаление чужой связи без политики, скрытые runtime-ссылки и секреты. | `ProviderOperationResponse` с операцией и связью. | `ProviderOperation` типа `UPDATE_RELATIONSHIP`; `provider.operation.completed/failed`, `provider.relationship.synced`. |

Контекст политики должен перечислять `changed_fields` в терминах типизированного запроса. `provider-hub` не принимает свободный JSON patch для операций записи: inline comments review-сигнала передаются через `ReviewInlineComment`, а не через строковый JSON.
Для `UpdateRelationship` вызывающий контур берёт `meta.expected_version` из `ProviderRelationship.version`, который возвращается в `ListRelationships` и в `ProviderOperationResponse.relationship`.
Для GitHub `expected_provider_version` передаётся как `If-Match`, если вызывающий контур получил provider version из предыдущего ответа или проекции. Если команда уже успешно записана по `command_id`, повтор возвращает сохранённый `ProviderOperation` до проверки локальной версии и не выполняет внешний write повторно. Если команда уже зарезервирована как `in_progress`, повтор получает конфликт и не выполняет второй внешний write.
В текущем GitHub-адаптере `CreatePullRequest` отклоняет непустые `labels` и `linked_issue_ref`, потому что GitHub требует для этих полей дополнительные write-вызовы или отдельную модель связи. До появления recovery/compensation контура такие изменения должны выполняться отдельными командами после создания `PR`.

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
