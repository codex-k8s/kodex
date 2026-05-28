---
doc_id: API-CK8S-PROVIDER-HUB-0001
type: api-contract
title: kodex — API-контракт provider-hub
status: active
owner_role: SA
created_at: 2026-05-06
updated_at: 2026-05-28
related_issues: [281, 282, 711, 719, 725, 729, 737, 761, 770, 781, 794, 818, 840, 864, 865, 908, 909]
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
| `RetryWebhookEventProcessing` | Повторить обработку без raw body: поддержанный GitHub `pull_request` перечитывается через provider API по safe refs, неподдержанные/expired записи получают safe diagnostic. | Операторский контур | По версии события. |
| `CleanupExpiredWebhookPayloads` | Явно очистить legacy `retained_for_retry` payload из inbox. | Операторский/служебный контур | Ограниченная пачка по `retain_until`; повтор после очистки no-op. |

`provider-hub` не проверяет публичную подпись webhook. Он принимает только уже проверенный внутренний вызов от `integration-gateway`. Edge-проверки источника, подписи, размера payload, лимитов и backpressure находятся в `integration-gateway`; входящий журнал, дедупликация и нормализация остаются в `provider-hub`.

`payload_json` во входящем журнале является safe envelope, а `payload_sha256` хранит fingerprint canonical body для replay/conflict. `CleanupExpiredWebhookPayloads` не является фоновым бесконтрольным циклом: вызывающий контур явно задаёт audit `CommandMeta` и может ограничить размер пачки. Сервис берёт TTL из `KODEX_PROVIDER_HUB_WEBHOOK_PAYLOAD_RETENTION`, а размер пачки по умолчанию из `KODEX_PROVIDER_HUB_WEBHOOK_PAYLOAD_CLEANUP_LIMIT`. Cleanup нужен для legacy `retained_for_retry` строк: после очистки остаются `payload_sha256`, delivery/source refs, статус `failed`, safe reason `payload_expired` и safe envelope. `RetryWebhookEventProcessing` не нормализует raw/canonical payload из БД. Для поддержанного GitHub `pull_request` safe envelope он может перечитать `PR` через provider API, если по stable refs найден уже разрешённый onboarding `ProviderOperation` и внешний аккаунт подтверждён через `access-manager`; иначе запись терминализируется безопасной причиной `payload_unavailable`, `payload_expired` или `refetch_unavailable` без вывода raw/canonical payload наружу.

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
| `CreateRepository` | Создать provider-native репозиторий и начальный provider default branch. | Проектный контур, `agent-manager`, MCP | `command_id`. |
| `CreateBootstrapPullRequest` | Создать или обновить bootstrap branch и bootstrap `PR/MR` для уже созданного репозитория. | Проектный контур, `agent-manager`, MCP | `command_id`. |
| `CreateAdoptionPullRequest` | Создать или обновить adoption branch и reviewable `PR/MR` для существующего репозитория по уже подготовленным файлам. | Проектный контур, `agent-manager`, MCP | `command_id`. |
| `ScanRepositoryForAdoption` | Снять lightweight provider-side snapshot существующего репозитория для adoption planning без чтения содержимого файлов. | Проектный контур, `agent-manager`, MCP | `command_id`. |
| `UpdatePullRequest` | Обновить разрешённые поля `PR/MR`. | `agent-manager`, MCP, package flow | `command_id + expected_provider_version`, если доступна. |
| `CreateReviewSignal` | Оставить review/comment/approval там, где поддерживается провайдером. | Acceptance/gatekeeper контур | `command_id`. |
| `UpdateRelationship` | Зафиксировать или обновить provider-native связь, если провайдер поддерживает. | `agent-manager`, MCP | `command_id`. |

Каждая операция сначала запрашивает разрешение у `access-manager`. После положительного решения `provider-hub` получает секрет через `libs/go/secretresolver` только для операций, которым нужен внешний вызов, и удерживает значение в памяти процесса на время вызова адаптера. Для GitHub адаптер записи уже выполняет внешний write поверх общего pipeline; операции локального зеркала, например `UpdateRelationship`, не читают секрет и не обращаются к GitHub API. Если операция выполнена агентом напрямую через `gh` в слоте, она не попадает в этот набор как команда, но может передать ускоряющий сигнал и лимитный снимок.

В `UpdateIssue` и `UpdatePullRequest` списковые поля передаются через сообщения-патчи:
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
| `CreateRepository` | `provider.repository.write` | Требуется, потому что операция создаёт новый репозиторий у провайдера и начальный provider default branch. | `meta.command_id` обязателен; provider version не нужен. | `project_id`, `repository_id`, `provider_slug`, `owner_kind`, `provider_owner`, `repository_name`, `visibility`, `description`, `external_account_id`. | Генерация `services.yaml`, выбор шаблона, adoption scan, branch protection, секреты, raw provider payload, запись файлов bootstrap. | `ProviderOperationResponse` с операцией, result target, provider repository id, URL, provider version и `base_branch`. | `ProviderOperation` типа `CREATE_REPOSITORY`; `provider.operation.completed/failed`, `provider.repository.created`. |
| `CreatePullRequest` / `CreateMergeRequest` | `provider.pull_request.write` | Обычно требуется для защищённой ветки, release/adoption/bootstrap, package source, prod-impact и high-risk изменений. | `meta.command_id` обязателен; provider version не нужен. | `title`, `body`, `head_branch`, `base_branch`, `draft`, `labels`, `linked_issue_ref`, `watermark_json`. | Прямой merge, force push, изменение branch policy, обход обязательных проверок, секреты, владение проектом у провайдера. | `ProviderOperationResponse` с операцией, проекцией `PR/MR`, provider id, URL и provider version, если доступна. | `ProviderOperation` типа `CREATE_PULL_REQUEST`; `provider.operation.completed/failed`, `provider.work_item.synced` или горячая сверка. |
| `CreateBootstrapPullRequest` | `provider.repository.write` | Обычно требуется, потому что операция пишет ветку репозитория и создаёт provider-native `PR/MR` для первичной инициализации. | `meta.command_id` обязателен; provider version не нужен. | `repository_target`, `base_branch`, `bootstrap_branch`, `commit_message`, `title`, `body`, `draft`, подготовленные текстовые `files`, `watermark_json`. | Создание репозитория, генерация `services.yaml`, adoption scan, изменение branch policy, merge, force push, секреты и raw provider payload. | `ProviderOperationResponse` с операцией, проекцией bootstrap `PR/MR`, provider id, URL и provider version. | `ProviderOperation` типа `CREATE_BOOTSTRAP_PULL_REQUEST`; `provider.operation.completed/failed`, `provider.work_item.synced`, `provider.repository.bootstrap_completed`; локальная связь `project_repository_binding`. |
| `CreateAdoptionPullRequest` | `provider.repository.write` | Обычно требуется, потому что операция пишет adoption branch и создаёт provider-native `PR/MR` для подключения существующего репозитория. | `meta.command_id` обязателен; provider version не нужен. | `repository_target`, `base_branch`, `adoption_branch`, `commit_message`, `title`, `body`, `draft`, подготовленные текстовые `files`, `watermark_json`. | Сканирование репозитория, отчёт adoption, генерация `services.yaml`, выбор шаблона, project policy decision, изменение branch policy, merge, force push, секреты и raw provider payload. | `ProviderOperationResponse` с операцией, проекцией adoption `PR/MR`, provider id, URL и provider version. | `ProviderOperation` типа `CREATE_ADOPTION_PULL_REQUEST`; `provider.operation.completed/failed`, `provider.work_item.synced`, `provider.repository.adoption_pr_created`; локальная связь `project_repository_binding`. |
| `ScanRepositoryForAdoption` | `provider.reconciliation.run` | Штатно не требуется, потому что операция читает только provider metadata/tree refs; если политика вызывающего контура требует gate, ссылка передаётся как `approval_gate_ref`. | `meta.command_id` обязателен; provider version не нужен. | `repository_target`, `requested_ref`, `allowed_ref_prefixes`, `max_tree_entries`, `max_marker_paths`, bounded `marker_path_hints`. | Чтение blob/file contents, diff/archive, бизнес-классификация, импорт `services.yaml`, project/adoption decision, запуск агента, создание PR, секреты и raw provider payload. | `ProviderOperationResponse` с `RepositoryAdoptionScanSnapshot`: provider refs, default/scanned refs, head sha, marker refs/digests/counts, bounded warnings, snapshot digest и status. | `ProviderOperation` типа `SCAN_REPOSITORY_FOR_ADOPTION`; `provider.operation.completed/failed`, `provider.repository.adoption_scan_completed`; отдельная безопасная snapshot-запись по operation ref. |
| `GetRepositoryMergeSignal` / `ListRepositoryMergeSignals` | `provider.work_item.read` | Не требуется. | Read-only. | Фильтры `signal_id`, `signal_key`, project/repository refs, provider slug, repository refs, kind/status, PR/MR number, `merged_since`. | Raw webhook body, provider response, body PR/MR, checked artifact metadata, `services.yaml` payload и импорт политики. | `RepositoryMergeSignal` со safe refs: provider slug, project/repository ids, repository refs, PR/MR ids/url/number, base/head branch, merge commit sha, source ref, related provider operation ref, watermark digest, timestamps, status, version и safe `etag`. `Get*` возвращает явный `read_status`, если сигнала ещё нет. | Read-only: соседний сервис может повторять чтение по `signal_key` и получать стабильный факт без прямого GitHub/GitLab доступа. |
| `GetRepositoryAdoptionScanSnapshot` / `ListRepositoryAdoptionScanSnapshots` | `provider.work_item.read` | Не требуется. | Read-only. | Фильтры `snapshot_id`, `snapshot_key`, `provider_operation_id`, project/repository context из policy trace операции, provider slug, external account, repository refs, status, `observed_since`. | Raw file contents, diff/archive, checked artifact registry, `services.yaml` payload, project/adoption decision и deep workspace scan/report. | `RepositoryAdoptionScanSnapshot` со safe refs: provider refs, default/requested/scanned ref, head sha, marker refs/digests/counts, bounded warnings, snapshot digest, status, timestamps, version и safe `etag`. `Get*` возвращает явный `read_status`, если snapshot ещё не готов. | Read-only: snapshot остаётся provider-owned фактом; `project-catalog` и `agent-manager` используют его как вход для своего решения, не ходя напрямую в provider API. |
| `UpdatePullRequest` / `UpdateMergeRequest` | `provider.pull_request.write` | Обычно требуется для защищённой ветки, release/adoption/bootstrap, package source, prod-impact и high-risk изменений. | `meta.expected_version` для локальной проекции; `expected_provider_version`, если вызывающий контур работал с маркером провайдера. | `title`, `body`, `state`, `labels`, `assignee_provider_logins`, `milestone`, `base_branch`, `maintainer_can_modify`, `watermark_json`. | `head_branch`, прямой merge, force push, изменение branch policy, обход обязательных проверок, секреты, поля вне типизированного патча. | `ProviderOperationResponse` с операцией, обновлённой проекцией `PR/MR`, provider id, URL и provider version, если доступна. | `ProviderOperation` типа `UPDATE_PULL_REQUEST`; `provider.operation.completed/failed`, `provider.work_item.synced` или горячая сверка. |
| `CreateReviewSignal` | `provider.review_signal.write` | Требуется для approval или changes requested, если политика не допускает автоматическое решение конкретной роли. | `meta.command_id` обязателен; provider version не нужен. | `kind`, `body`, `inline_comments` с полями `path`, `body`, `line`, `start_line`, `side`, `start_side`, `in_reply_to_provider_comment_id`. | Merge, изменение PR body/branch, изменение labels/state, скрытые проверки, review от имени неподтверждённого актёра, свободный JSON payload. | `ProviderOperationResponse` с операцией, проекцией комментария или review, result target и provider review id. | `ProviderOperation` типа `CREATE_REVIEW_SIGNAL`; `provider.operation.completed/failed`, `provider.comment.synced` или горячая сверка. |
| `UpdateRelationship` | `provider.relationship.write` | Требуется, если связь влияет на release, blocker, follow-up или cross-project dependency. | `meta.expected_version` для локальной связи; provider version не нужен, если связь хранится только в зеркале. | `source`, `target`, `target_provider_ref`, `relationship_type`, `source_kind`, `confidence`. | Изменение provider object без отдельной команды записи, удаление чужой связи без политики, скрытые runtime-ссылки и секреты. | `ProviderOperationResponse` с операцией и связью. | `ProviderOperation` типа `UPDATE_RELATIONSHIP`; `provider.operation.completed/failed`, `provider.relationship.synced`. |

Контекст политики должен перечислять `changed_fields` в терминах типизированного запроса. `provider-hub` не принимает свободный JSON patch для операций записи: inline comments review-сигнала передаются через `ReviewInlineComment`, а не через строковый JSON.
Для `UpdateRelationship` вызывающий контур берёт `meta.expected_version` из `ProviderRelationship.version`, который возвращается в `ListRelationships` и в `ProviderOperationResponse.relationship`.
Для GitHub `expected_provider_version` передаётся как `If-Match`, если вызывающий контур получил provider version из предыдущего ответа или проекции. Если команда уже успешно записана по `command_id`, повтор возвращает сохранённый `ProviderOperation` до проверки локальной версии и не выполняет внешний write повторно. Если команда уже зарезервирована как `in_progress`, повтор получает конфликт и не выполняет второй внешний write.
В текущем GitHub-адаптере `CreatePullRequest` отклоняет непустые `labels` и `linked_issue_ref`, потому что GitHub требует для этих полей дополнительные write-вызовы или отдельную модель связи. До появления recovery/compensation контура такие изменения должны выполняться отдельными командами после создания `PR`.
Для `UpdatePullRequest` GitHub-адаптер не смешивает в одной команде метаданные PR, которые GitHub хранит на issue-стороне (`labels`, `assignee_provider_logins`, `milestone`), и собственные поля PR (`base_branch`, `maintainer_can_modify`). В UI GitHub эти поля видны как поля PR, но API меняет их через разные HTTP-ручки. Такая смешанная команда отклоняется до внешнего write, чтобы не оставить частичное изменение без транзакционной гарантии. Вызывающий контур должен разбить действие на две идемпотентные команды: `UpdateIssue` для метаданных PR на issue-стороне и `UpdatePullRequest` для собственных полей PR. Если нужен один пользовательский сценарий, внешний контур оркестрации должен связать эти команды общим `correlation_id`, а не требовать атомарности от GitHub.
`CreateRepository` является provider-side командой модели C: проектный или агентный контур заранее выбирает владельца, имя, видимость и внешний аккаунт, а `provider-hub` только выполняет нативное создание репозитория у провайдера. Для GitHub команда использует инициализацию на стороне провайдера `auto_init=true`, чтобы провайдер создал начальный default branch и минимальный начальный commit. Ответ возвращает `base_branch` из default branch провайдера. Для организации `provider_owner` обязателен; для authenticated user `provider_owner` не передаётся, чтобы не смешивать пользовательский и организационный режимы. Команда не генерирует `services.yaml`, не выбирает шаблон, не сканирует репозиторий и не меняет branch protection. Когда создание запускается через `project-catalog`, `project-catalog` заранее выбирает `repository_id`, создаёт pending project/repository binding и передаёт этот id в `CreateRepository`; после ответа он сохраняет только безопасные provider refs и `base_branch` в binding.
`CreateBootstrapPullRequest` является provider-side командой модели C: проектный или агентный контур готовит payload и refs заранее, а `provider-hub` только пишет их в уже созданный репозиторий через provider API, создаёт или обновляет bootstrap branch/PR, обновляет проекцию и фиксирует безопасный журнал операции. `base_branch` должен существовать, отличаться от `bootstrap_branch` и иметь пустое дерево или только безопасный `README.md`, созданный инициализацией на стороне провайдера. Если bootstrap branch уже существует, новая команда строит commit от текущей головы bootstrap branch, но дерево commit собирается из пустого дерева или дерева только с `README.md` и подготовленного набора файлов, чтобы не наследовать старые файлы. Содержимое файлов не хранится в `ProviderOperation`, outbox, событиях, audit payload и логах.

Когда bootstrap запускается через `project-catalog`, именно `project-catalog` владеет project/repository binding, проектной `base_branch` policy, связью prepared files с проверенной проекцией `services.yaml`, watermark и безопасным `operation_policy_context`. `provider-hub` остаётся владельцем provider-native записи, журнала `ProviderOperation`, локальной PR/MR-проекции и событий `provider.*`; он не принимает решение о проектной политике и не превращается в генератор шаблонов репозитория.

После merge bootstrap/adoption PR/MR `provider-hub` и webhook/reconciliation контур остаются владельцами provider-native факта merge и provider projection. Для GitHub `pull_request closed + merged` фиксируется отдельный safe merge signal с kind `bootstrap` или `adoption`, provider slug, project/repository refs, PR target ref/number/id/url, base/head branch, merge commit sha, source ref, related provider operation ref, watermark digest, timestamps, status и version. Повтор того же сигнала идемпотентен, а повтор с тем же signal key и другим commit/source ref считается конфликтом. Внутренний webhook inbox `provider-hub` хранит только safe envelope и `payload_sha256` для replay/conflict diagnostics; это не safe read surface и не междоменный контракт. `project-catalog` не читает GitHub/GitLab напрямую: внутренний контур передаёт в `project-catalog ReconcileBootstrapMergeSignal` или `ReconcileAdoptionMergeSignal` только safe provider signal и checked artifact metadata с безопасным provider target, source ref, commit, artifact ref/digest/version, `content_hash`, watermark и нормализованным `services.yaml`. Provider-native raw/canonical webhook payload, body PR/MR и полный provider response не переходят в `project-catalog`, outbox/event-log payload или `RepositoryMergeSignal` read surface.

`ScanRepositoryForAdoption` является provider-side read-командой модели C для существующего репозитория. Она использует тот же command pipeline и подтверждение внешнего аккаунта, что сверка, читает только provider metadata/ref/tree по GitHub API, применяет bounded scan options и branch/ref policy, фиксирует безопасный `RepositoryAdoptionScanSnapshot` и публикует `provider.repository.adoption_scan_completed`. Snapshot содержит provider refs, default/scanned refs, head sha, marker path refs, object digests, counts, bounded warnings, timestamps, operation ref и snapshot digest. Содержимое файлов, diff/archive, provider response, токены, секреты, PII и raw payload не сохраняются и не попадают в события. Повтор по тому же `command_id` возвращает сохранённый snapshot и не выполняет второй provider API scan.

Provider-owned read surface для bootstrap/adoption состоит только из `GetRepositoryMergeSignal`, `ListRepositoryMergeSignals`, `GetRepositoryAdoptionScanSnapshot` и `ListRepositoryAdoptionScanSnapshots`. Эти методы возвращают уже проверенные provider-side факты и safe refs/digests/status/version/etag, но не возвращают checked artifact metadata, checked payload или нормализованный `services.yaml`. Если точный signal/snapshot ещё не создан, `Get*` отвечает явным `read_status=NOT_FOUND`; если данные требуют будущей freshness-политики, контракт уже имеет статусы `NOT_VERIFIED` и `STALE`, но владелец проверки остаётся outside provider-hub. `project-catalog` связывает provider signal со своим checked artifact input и импортом политики, не читая GitHub/GitLab напрямую.

`CreateAdoptionPullRequest` является provider-side командой модели C для существующего репозитория: проектный или агентный контур заранее использует lightweight snapshot, глубокий workspace scan или другой согласованный вход, готовит отчёт, принимает проектное решение и передаёт в `provider-hub` только готовые файлы, refs, title/body и watermark. `provider-hub` проверяет существование `base_branch`, создаёт или обновляет adoption branch и reviewable `PR/MR`, но эта команда не сканирует репозиторий, не генерирует `services.yaml`, не выбирает шаблон и не хранит содержимое файлов в БД, событиях или логах.

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
| `provider.repository.created` | Репозиторий создан у провайдера, начальный default branch известен. |
| `provider.repository.bootstrap_required` | Provider-состояние показывает, что репозиторий пустой и требует решения о первичной инициализации. |
| `provider.repository.adoption_required` | Provider-состояние показывает, что существующий репозиторий требует агентного сканирования, отчёта и adoption через reviewable PR. |
| `provider.repository.bootstrap_completed` | Provider-side bootstrap branch/PR создан или обновлён. |
| `provider.repository.adoption_pr_created` | Создан reviewable PR для adoption. |
| `provider.repository.adoption_scan_completed` | Lightweight provider-side adoption snapshot готов; payload содержит только safe refs, counts, warnings и digest. |
| `provider.repository.bootstrap_merged` | GitHub bootstrap PR принят владельцем через merge; payload содержит только безопасные refs, digest и timestamps. |
| `provider.repository.adoption_merged` | GitHub adoption PR принят владельцем через merge; payload содержит только безопасные refs, digest и timestamps. |

## Совместимость

- gRPC и AsyncAPI `v1` должны покрыть согласованный объём домена, даже если реализация поставляется по срезам.
- Если контракт опережает реализацию, delivery-документ содержит таблицу реализованных и отложенных операций.
- GitHub-specific поля остаются за adapter boundary или в provider-specific payload, если они не являются частью нормализованного контракта.

## Апрув

- request_id: `owner-2026-05-06-provider-hub-boundaries`
- Решение: approved
- Комментарий: API-карта `provider-hub` согласована как целевое состояние.
