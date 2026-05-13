---
doc_id: DM-CK8S-PROVIDER-HUB-0001
type: data-model
title: kodex — модель данных provider-hub
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

# Модель данных: provider-hub

## TL;DR

- Ключевые сущности: операционное состояние аккаунта провайдера, входящее webhook-событие, нормализованное событие провайдера, проекция рабочего артефакта, проекция комментария, связь, запрос постановки сверки в очередь, курсор сверки, снимок лимита, журнал операций и локальный outbox доменных событий.
- Основные связи: все ссылки на проекты, репозитории, внешние аккаунты, run и job хранятся как внешние идентификаторы без SQL-связей с чужими БД.
- Риски миграций: нельзя хранить состояние провайдера как собственную истину платформы и нельзя копить сырые payload без политики хранения.

## Базовые правила

- БД `provider-hub` принадлежит только `provider-hub`.
- Таблицы не имеют `FOREIGN KEY` в БД других сервисов.
- Конкурентные команды используют версии агрегатов и идемпотентные ключи.
- Межсервисные доменные события сначала фиксируются в локальном outbox сервиса-владельца, а затем доставляются в общий `platform-event-log`.
- Сырые webhook payload имеют срок хранения.
- Нормализованные проекции хранят только поля, нужные платформе для UI, поиска, приёмки, синхронизации и аудита.
- Полные diff, review truth, ветки и теги остаются у провайдера.

## Сущности

### `ProviderAccountRuntimeState`

Назначение: операционное состояние внешнего аккаунта у провайдера.

Важные инварианты:

- политика аккаунта и область применения находятся в `access-manager`;
- `provider-hub` хранит только операционное состояние использования аккаунта у провайдера;
- сырые секреты не хранятся в БД.
- частичный снимок одного класса лимита может перевести аккаунт в `limited`, но не может вернуть его в `active`; снятие `limited` выполняется только полным пересчётом лимитов или отдельным согласованным переходом состояния.
- авторитетное обновление операционного состояния отделено от частичного обновления по снимку лимита и может снять `limited`, если полный пересчёт показал восстановление лимита.
- частичное обновление по снимку лимита не применяет наблюдение, которое старше текущего `last_checked_at`, и не откатывает `status`, `last_checked_at`, `last_success_at`, `version` и поля последней ошибки.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор записи операционного состояния. |
| `external_account_id` | UUID | no | indexed | Ссылка на внешний аккаунт из `access-manager`. |
| `provider_slug` | text | no | indexed | `github`, позднее `gitlab`. |
| `status` | text | no | enum-like | `active`, `reauthorization_required`, `limited`, `disabled`, `error`. |
| `last_checked_at` | timestamptz | yes |  | Последняя проверка. |
| `last_success_at` | timestamptz | yes |  | Последняя успешная операция. |
| `last_error_code` | text | no | default '' | Классификация ошибки. |
| `last_error_message` | text | no | default '' | Короткое описание без секрета. |
| `version` | bigint | no | monotonic | Оптимистичная конкуренция. |

### `WebhookEvent`

Назначение: сырой входящий сигнал провайдера.

Важные инварианты:

- дедупликация обязательна по delivery id или аналогу;
- payload хранится ограниченный срок;
- нормализация может идти синхронно при приёме или через отдельный обработчик, но повторная обработка должна быть идемпотентной;
- если конкурентный повтор уже перевёл событие из `pending` или `failed` в терминальное состояние, команда повторной обработки перечитывает и возвращает это состояние вместо ложного `not found`;
- `pending` после повторного чтения не считается успешной обработкой и должен возвращаться как конфликт.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор события. |
| `provider_slug` | text | no | indexed | Поставщик. |
| `delivery_id` | text | no | unique by provider | Идентификатор доставки webhook. |
| `event_name` | text | no | indexed | Имя события провайдера. |
| `repository_provider_id` | text | no | default '' | Внешний id репозитория, если есть. |
| `received_at` | timestamptz | no | indexed | Время приёма. |
| `processing_status` | text | no | indexed | `pending`, `processed`, `failed`, `ignored`. |
| `payload_json` | jsonb | no |  | Сырой payload с ограниченным сроком хранения. |
| `last_error` | text | no | default '' | Короткая ошибка обработки. |
| `retain_until` | timestamptz | no | indexed | Срок хранения payload. |

### `ProviderEvent`

Назначение: нормализованное событие провайдера после разбора raw webhook или сверки.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор нормализованного события. |
| `source_webhook_event_id` | UUID | yes | indexed | Ссылка внутри БД `provider-hub`. |
| `event_type` | text | no | indexed | Нормализованный тип. |
| `aggregate_type` | text | no | indexed | `work_item`, `comment`, `relationship`, `account_runtime_state`, `limit`. |
| `aggregate_id` | text | no | indexed | Внешний или внутренний id агрегата. |
| `payload_json` | jsonb | no |  | Типизированный payload в реализации. |
| `occurred_at` | timestamptz | no | indexed | Время изменения у провайдера. |

### `ProviderWorkItemProjection`

Назначение: нормализованное зеркало `Issue` или `PR/MR`.

Важные инварианты:

- источник истины остаётся у провайдера;
- задержанный webhook не должен откатывать более свежую проекцию; если текущий `provider_updated_at` заполнен, входящий снимок с пустым или более старым `provider_updated_at` не обновляет поля проекции и не порождает событие проекции как свежее;
- одна задача может иметь несколько связанных `PR/MR`;
- поле `project_id` и `repository_id` являются внешними идентификаторами из `project-catalog`.
- первичная запись строится из provider webhook или сверки, а `project_id` и `repository_id` могут оставаться пустыми до связывания с `project-catalog`;
- watermark хранится только как разобранные безопасные поля; полный текст тела остаётся у провайдера, а в проекции хранится digest.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Внутренний id проекции. |
| `provider_slug` | text | no | indexed | Поставщик. |
| `provider_work_item_id` | text | no | unique by provider | Стабильная идентичность у провайдера. Для GitHub используется ссылка вида `github:<owner>/<repo>:<kind>:<number>`, чтобы `issue_comment` для PR и `pull_request` webhook сходились в одну PR-проекцию. |
| `project_id` | UUID | yes | indexed | Внешняя ссылка на проект. |
| `repository_id` | UUID | yes | indexed | Внешняя ссылка на repository binding. |
| `repository_full_name` | text | no | indexed | `owner/name` или аналог. |
| `kind` | text | no | indexed | `issue`, `pull_request`, `merge_request`. |
| `number` | bigint | no | indexed | Номер у провайдера. |
| `url` | text | no |  | Ссылка на провайдера. |
| `title` | text | no |  | Текущее название. |
| `state` | text | no | indexed | `open`, `closed`, `merged`, provider-normalized. |
| `work_item_type` | text | no | default '' | `initiative`, `dev`, `qa` и другие типы. |
| `labels_json` | jsonb | no | default [] | Нормализованные метки. |
| `assignees_json` | jsonb | no | default [] | Назначенные участники. |
| `milestone` | text | no | default '' | Нормализованная веха, если есть. |
| `project_fields_json` | jsonb | no | default {} | Нужные project fields. |
| `watermark_status` | text | no | indexed | `missing`, `valid`, `invalid`, `stale`. |
| `watermark_json` | jsonb | no | default {} | Разобранный watermark. |
| `body_digest` | text | no | default '' | Digest тела. |
| `provider_updated_at` | timestamptz | yes | indexed | Время обновления у провайдера. |
| `synced_at` | timestamptz | no | indexed | Время последней успешной синхронизации. |
| `drift_status` | text | no | indexed | `fresh`, `suspected`, `stale`, `failed`. |
| `version` | bigint | no | monotonic | Версия проекции. |

### `ProviderCommentProjection`

Назначение: нормализованный комментарий, mention или review-сигнал.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Внутренний id. |
| `work_item_projection_id` | UUID | no | indexed | Ссылка внутри БД `provider-hub`. |
| `provider_comment_id` | text | no | unique by provider | Внешний id. |
| `kind` | text | no | indexed | `comment`, `review`, `mention`, `system`. |
| `review_state` | text | no | default '' | `approved`, `changes_requested`, `commented`, `dismissed`, `pending`; пусто для обычных комментариев. |
| `author_provider_login` | text | no | indexed | Логин автора у провайдера. |
| `body_digest` | text | no | default '' | Digest тела. |
| `summary` | text | no | default '' | Короткая выдержка для UI. |
| `provider_created_at` | timestamptz | yes |  | Время создания у провайдера. |
| `provider_updated_at` | timestamptz | yes |  | Время обновления у провайдера. |
| `version` | bigint | no | monotonic | Версия проекции. |

### `ProviderRelationship`

Назначение: нормализованная связь provider-native объектов.

Примеры связей: исходная задача, связанный `PR/MR`, follow-up, blocks, blocked-by, release link, package source link.

Связь может ссылаться на уже известную внутреннюю проекцию через `target_work_item_id` или на внешнюю ссылку провайдера через `target_provider_ref`, если целевая проекция ещё не создана. Связи, извлечённые из watermark, помечаются source `watermark` и confidence `confirmed`. При свежем обновлении рабочего артефакта набор watermark-связей пересобирается целиком по полям `source_ref`, `parent_ref` и `next_ref`: отсутствующая в текущем watermark связь удаляется из подтверждённой проекции, чтобы `ListRelationships` не возвращал устаревшую ссылку. Локальная версия связи меняется только при изменении управляемых полей связи и используется для оптимистичной конкурентной защиты в `UpdateRelationship`.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор связи. |
| `source_work_item_id` | UUID | no | indexed | Внутренняя ссылка на проекцию. |
| `target_work_item_id` | UUID | yes | indexed | Внутренняя ссылка, если цель уже известна. |
| `target_provider_ref` | text | no | default '' | URL или provider ref, если проекции ещё нет. |
| `relationship_type` | text | no | indexed | Нормализованный тип связи. |
| `source` | text | no | indexed | `provider`, `watermark`, `comment`, `manual`, `reconciliation`. |
| `confidence` | text | no | default 'confirmed' | `confirmed`, `inferred`, `suspected`. |
| `created_at` | timestamptz | no |  | Время создания связи. |
| `version` | bigint | no | monotonic | Версия локальной связи для `meta.expected_version` в `UpdateRelationship`. |

### `SyncCursor`

Назначение: состояние инкрементальной сверки по области синхронизации и выбранному внешнему аккаунту.

Постановка курсоров выполняется через `ReconciliationRequest`. Вызывающий сценарий выбирает внешний аккаунт заранее и передаёт его в очередь сверки; `provider-hub` не выбирает аккаунт неявно во время запуска обработчика. Перед обращением к API провайдера курсор подтверждается через `access-manager`, который возвращает только ссылку на секрет без значения токена. Значение получается через общий `libs/go/secretresolver` только в памяти процесса и не сохраняется в таблицы курсоров, журнал операций, outbox или ошибки. Один запрос с одним `idempotency_key` может создать или повысить приоритет сразу нескольких курсоров в одной транзакции. Повтор того же запроса возвращает текущие курсоры без изменения `updated_at` и `version`; повтор с тем же `provider_slug + scope_type + scope_ref + idempotency_key`, но другим внешним аккаунтом, набором артефактов или приоритетом, считается конфликтом.

Завершение пакетной сверки атомарно сохраняет обновлённые projections, provider events, outbox events, снимки лимитов, runtime state и новое состояние cursor. Успешная сверка очищает lease, обновляет `cursor_value`, ставит окно перекрытия и переводит `hot` cursor обратно в `warm`. Rate limit сохраняет безопасный код `provider_rate_limited` и удерживает lease до retry-времени, чтобы другой worker не начал немедленный повтор. Ошибки secret resolution, auth failure, not found, transient и permanent фиксируются только коротким кодом без значения секрета, provider payload и `secret_store_ref`.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор курсора. |
| `provider_slug` | text | no | indexed | Поставщик. |
| `external_account_id` | UUID | no | indexed | Внешний аккаунт из `access-manager`, выбранный политикой вызывающего сценария. |
| `scope_type` | text | no | indexed | `repository`, `organization`, `work_item`, `package_source`. |
| `scope_ref` | text | no | indexed | Внешняя область. |
| `artifact_kind` | text | no | indexed | `issue`, `pull_request`, `merge_request`, `comment`, `relationship`, `repository`. |
| `cursor_value` | text | no | default '' | Provider cursor или timestamp marker. |
| `overlap_since` | timestamptz | yes |  | Начало окна перекрытия. |
| `priority` | text | no | indexed | `hot`, `warm`, `cold`. |
| `last_success_at` | timestamptz | yes | indexed | Последняя успешная сверка. |
| `last_checked_at` | timestamptz | yes | indexed | Последняя попытка. |
| `last_error` | text | no | default '' | Короткая ошибка. |
| `rate_budget_state_json` | jsonb | no | default {} | Снимок бюджета лимитов. |
| `lease_owner` | text | no | default '' | Владелец короткой аренды. |
| `lease_until` | timestamptz | yes | indexed | Конец аренды. |
| `version` | bigint | no | monotonic | Версия курсора. |

### `ProviderArtifactSignal`

Назначение: идемпотентный след ускоряющего сигнала и атомарной постановки курсоров сверки.

Сигнал сохраняется отдельно от `ReconciliationRequest`, потому что явный `signal_id`, ключ идемпотентности команды и `command_id` должны быть signal-level ключами, а не частью естественного ключа курсора. След сигнала, `ReconciliationRequest` и `SyncCursor` фиксируются одной транзакцией; если транзакция не завершилась, не остаётся принятого сигнала без курсоров. Повтор с тем же ключом и тем же содержимым возвращает уже принятую запись и повторно проходит тот же атомарный путь постановки курсоров. Повтор с тем же ключом, но другим `external_account_id`, `target`, `source`, payload, временем наблюдения, scope или набором артефактов считается конфликтом.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор принятого сигнала. |
| `identity_key` | text | no | unique | Signal-level ключ идемпотентности. |
| `provider_slug` | text | no | indexed | Поставщик. |
| `external_account_id` | UUID | no | indexed | Внешний аккаунт из `access-manager`, выбранный вызывающим контуром. |
| `source` | text | no | indexed | Источник сигнала: `agent_manager`, `platform_mcp`, `slot_agent_after` и подобные. |
| `scope_type` | text | no | indexed | `work_item` или `repository` для текущего среза. |
| `scope_ref` | text | no | indexed | Нормализованная область сверки. |
| `artifact_kinds_json` | jsonb | no | array | Набор курсоров, которые нужно ускорить. |
| `target_json` | jsonb | no | object | Канонический снимок нормализованного `ProviderTarget`. |
| `payload_json` | jsonb | no | object | Безопасный дополнительный payload сигнала. |
| `observed_at` | timestamptz | no | indexed | Когда источник увидел изменение у провайдера. |
| `created_at` | timestamptz | no |  | Когда сигнал принят `provider-hub`. |

### `ReconciliationRequest`

Назначение: идемпотентный след команды постановки области в очередь сверки.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор запроса. |
| `provider_slug` | text | no | indexed | Поставщик. |
| `external_account_id` | UUID | no | indexed | Внешний аккаунт из `access-manager`, выбранный для создаваемых курсоров. |
| `scope_type` | text | no | unique part | Область сверки. |
| `scope_ref` | text | no | unique part | Внешняя область. |
| `idempotency_key` | text | no | unique part | Ключ повтора команды внутри области. |
| `artifact_kinds_json` | jsonb | no | array | Канонический набор артефактов запроса. |
| `priority` | text | no | enum-like | Приоритет постановки курсоров. |
| `created_at` | timestamptz | no |  | Время первой постановки. |
| `updated_at` | timestamptz | no |  | Время записи запроса. |

### `ProviderLimitSnapshot`

Назначение: известный снимок лимитов провайдера.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор снимка. |
| `external_account_id` | UUID | no | indexed | Внешний аккаунт из `access-manager`. |
| `provider_slug` | text | no | indexed | Поставщик. |
| `limit_class` | text | no | indexed | `core`, `graphql`, `search`, provider-specific class. |
| `remaining` | bigint | yes |  | Остаток, если известен. |
| `limit_value` | bigint | yes |  | Общий лимит, если известен. |
| `reset_at` | timestamptz | yes | indexed | Время сброса. |
| `captured_at` | timestamptz | no | indexed | Время снимка. |
| `source` | text | no | indexed | `provider_hub`, `slot_agent_before`, `slot_agent_after`, `slot_agent_signal`. |

Идемпотентность записи снимка обеспечивается естественным ключом
`external_account_id + provider_slug + limit_class + captured_at + source`.
Повтор с тем же ключом и тем же содержимым возвращает уже записанный снимок без изменения исторического факта. Повтор с тем же ключом, но другим `remaining`, `limit_value` или `reset_at`, считается конфликтом доставки.
Повтор уже записанного снимка не обновляет соседнее операционное состояние аккаунта: runtime state меняется только для нового наблюдения.
Для конкурентных повторов запись снимка выполняется как `INSERT ... ON CONFLICT DO NOTHING RETURNING`, а replay-чтение выполняется отдельным SQL-вызовом внутри той же транзакции, чтобы второй запрос видел строку, которую только что зафиксировала конкурирующая транзакция.

### `ProviderOperation`

Назначение: аудит и диагностика операции платформы во внешнем провайдере.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор операции. |
| `command_id` | text | no | unique by operation | Идемпотентный ключ. |
| `actor_id` | UUID | yes | indexed | Субъект платформы, если есть. |
| `external_account_id` | UUID | no | indexed | Использованный внешний аккаунт. |
| `provider_slug` | text | no | indexed | Поставщик. |
| `operation_type` | text | no | indexed | Нормализованный тип операции. |
| `target_ref` | text | no | indexed | Provider target. |
| `status` | text | no | indexed | `succeeded`, `failed`, `retryable_failed`, `denied`. |
| `result_ref` | text | no | default '' | URL/id результата. |
| `error_code` | text | no | default '' | Классификация ошибки. |
| `error_message` | text | no | default '' | Короткое сообщение без секрета. |
| `rate_limit_snapshot_id` | UUID | yes | indexed | Снимок лимитов после операции. |
| `operation_policy_context_json` | jsonb | no | default {} | Безопасный снимок контекста политики: роль, проект, стадия, операция, цель, изменяемые поля, риск, версия политики. |
| `approval_gate_ref_json` | jsonb | no | default {} | Ссылка на уже принятое approval/gate решение, если оно требовалось policy. |
| `provider_version` | text | no | default '' | Версия или update marker результата у провайдера, если доступна. |
| `started_at` | timestamptz | no | indexed | Начало. |
| `finished_at` | timestamptz | yes | indexed | Завершение. |
| `version` | bigint | no | monotonic | Версия записи операции. |

`operation_policy_context_json` и `approval_gate_ref_json` не содержат секретов, token refs, сырых provider payload, email, имён или тел комментариев. Это только проверяемый след того, по какой роли, проекту, стадии, target, набору полей и версии политики команда была разрешена. `approval_gate_ref_json` хранит ссылку на внешний approval/gate, но не переносит в `provider-hub` владение самим решением.

Идемпотентный повтор provider-операции по `operation_type + command_id` возвращает уже записанную операцию только при совпадении области и результата: `actor_id`, `external_account_id`, `provider_slug`, `target_ref`, `status`, `result_ref`, `error_code`, `error_message`, `rate_limit_snapshot_id`, `operation_policy_context_json`, `approval_gate_ref_json` и `provider_version`. Если тот же `command_id` приходит с другой областью или другим содержимым результата, операция конфликтует.
Replay-чтение операции выполняется отдельным SQL-вызовом после `INSERT ... ON CONFLICT DO NOTHING RETURNING`, чтобы одинаковые конкурентные повторы не превращались в ложный конфликт.
`ProviderOperation` является идемпотентным журналом внешней записи: после успешного сохранения повтор той же команды возвращает записанный результат и не выполняет provider write повторно. Реальный адаптер записи после успешного ответа провайдера обновляет локальные проекции и связи в той же транзакции, где фиксируется операция и outbox-события. Ошибки провайдера сохраняются только как безопасная классификация без сырого payload и без секретов.

### `ProviderHubOutboxEvent`

Назначение: локальная очередь доменных событий, которые `provider-hub` уже зафиксировал в своей транзакции и должен доставить в общий `platform-event-log`.

Важные инварианты:

- таблица находится в БД `provider-hub`, а не в БД `platform-event-log`;
- запись outbox создаётся в той же транзакции, что и изменение доменной модели;
- повторная доставка управляется lease, счётчиком попыток и временем следующей попытки;
- режим `diagnostic-log-lossy` не является штатной доставкой для контуров, где события должны получить другие сервисы.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор события outbox. |
| `event_type` | text | no | indexed | Тип доменного события. |
| `schema_version` | integer | no | > 0 | Версия схемы события. |
| `aggregate_type` | text | no | indexed | Тип агрегата. |
| `aggregate_id` | UUID | no | indexed | Идентификатор агрегата. |
| `payload` | jsonb | no | object | Payload события. |
| `occurred_at` | timestamptz | no | indexed | Время возникновения события. |
| `published_at` | timestamptz | yes | indexed | Время успешной доставки. |
| `attempt_count` | integer | no | >= 0 | Количество попыток публикации. |
| `next_attempt_at` | timestamptz | no | indexed | Когда можно делать следующую попытку. |
| `locked_until` | timestamptz | yes | indexed | Lease публикации. |
| `failed_permanently_at` | timestamptz | yes |  | Время окончательного отказа. |
| `failure_kind` | text | no | enum-like | `''`, `transient`, `permanent`. |
| `last_error` | text | no | default '' | Короткая ошибка без секрета. |

## Индексы и критичные запросы

| Запрос | Индексы |
|---|---|
| Найти рабочий артефакт по provider ref | `(provider_slug, repository_full_name, kind, number)` и `(provider_slug, provider_work_item_id)` |
| Получить активные `Issue/PR` проекта | `(project_id, kind, state, provider_updated_at)` |
| Найти рассинхронизированные артефакты | `(drift_status, synced_at)` |
| Дедуплицировать webhook | unique `(provider_slug, delivery_id)` |
| Выбрать курсоры сверки | `(priority, last_checked_at)`, `(lease_until)`, `(external_account_id, priority, last_checked_at)` |
| Посмотреть лимиты аккаунта | `(external_account_id, limit_class, captured_at)` |
| Найти operation по идемпотентному ключу | unique `(operation_type, command_id)` |

## Политика хранения

| Данные | Политика |
|---|---|
| Raw webhook payload | Хранить ограниченный срок, достаточный для диагностики и повторной обработки. |
| Нормализованные provider events | Хранить по политике аудита и диагностики домена. |
| Проекции рабочих артефактов | Хранить пока артефакт связан с активным проектом, архивом или аудитом. |
| Комментарии | Хранить digest, краткую выдержку и provider ref; полные тела не копить без подтверждённого сценария. |
| Снимки лимитов | Хранить агрегированно и с ограничением по сроку. |
| Operation log | Хранить как аудит provider-операций без секретов и полных payload. |
| Локальный outbox | Хранить до успешной доставки или до ручного разбора окончательных отказов. |

## Миграции

Миграции живут в `services/internal/provider-hub/cmd/cli/migrations/*.sql`. Первый срез создаёт схему `provider_hub_*` для целевой доменной модели и локального outbox. Создание БД в Kubernetes, migration job и эксплуатационные манифесты фиксируются отдельным эксплуатационным срезом.

## Апрув

- request_id: `owner-2026-05-06-provider-hub-boundaries`
- Решение: approved
- Комментарий: модель данных `provider-hub` согласована как целевое состояние PRV-0.
