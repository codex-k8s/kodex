---
doc_id: DM-CK8S-PROVIDER-HUB-0001
type: data-model
title: kodex — модель данных provider-hub
status: active
owner_role: SA
created_at: 2026-05-06
updated_at: 2026-05-28
related_issues: [281, 282, 711, 719, 725, 729, 737, 748, 761, 770, 840, 864, 865, 908, 909]
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

- Ключевые сущности: операционное состояние аккаунта провайдера, входящее webhook-событие, нормализованное событие провайдера, проекция рабочего артефакта, проекция комментария, связь, безопасный merge signal bootstrap/adoption PR, безопасный adoption scan snapshot, запрос постановки сверки в очередь, курсор сверки, снимок лимита, журнал операций и локальный outbox доменных событий.
- Основные связи: все ссылки на проекты, репозитории, внешние аккаунты, run и job хранятся как внешние идентификаторы без SQL-связей с чужими БД.
- Риски миграций: нельзя хранить состояние провайдера как собственную истину платформы и нельзя копить сырые payload без политики хранения.

## Базовые правила

- БД `provider-hub` принадлежит только `provider-hub`.
- Таблицы не имеют `FOREIGN KEY` в БД других сервисов.
- Конкурентные команды используют версии агрегатов и идемпотентные ключи.
- Межсервисные доменные события сначала фиксируются в локальном outbox сервиса-владельца, а затем доставляются в общий `platform-event-log`.
- Canonical provider webhook payload хранится во внутреннем inbox только для `pending`/`failed` retry/reprocess до TTL; после терминального состояния или явной cleanup-операции запись хранит safe envelope и digest.
- Webhook inbox не является safe read surface: соседние сервисы получают только нормализованные safe refs/facts/digests/status через gRPC read surface и доменные события.
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

Назначение: внутренний входящий сигнал провайдера для дедупликации, нормализации и retry/reprocess.

Важные инварианты:

- дедупликация обязательна по delivery id или аналогу;
- digest canonical payload хранится в `payload_sha256` и используется для replay/conflict после редактирования payload;
- canonical provider webhook payload хранится во внутреннем поле `payload_json` только пока запись остаётся `pending` или `failed`, не истёк `retain_until`, и payload нужен для retry/reprocess;
- явная cleanup-операция `CleanupExpiredWebhookPayloads` для `pending`/`failed` записей с истёкшим `retain_until` заменяет `payload_json` safe envelope с `payload_storage=expired_after_retention`, `payload_cleanup_reason=payload_expired`, `payload_sha256`, delivery/source refs и временем очистки;
- после cleanup повторная обработка не пытается нормализовать payload и безопасно отказывает с причиной `payload_expired`; восстановление через re-fetch является отдельной будущей стратегией;
- после перехода в `processed` или `ignored` поле `payload_json` заменяется safe envelope с `payload_storage`, `payload_sha256`, delivery/source refs и retention metadata без raw provider body;
- migrated terminal rows с digest source `postgres_jsonb_text` принимают поздний duplicate delivery как replay по provider/delivery identity, потому что raw body уже удалён;
- нормализация может идти синхронно при приёме или через отдельный обработчик, но повторная обработка должна быть идемпотентной;
- если конкурентный повтор уже перевёл событие из `pending` или `failed` в терминальное состояние, команда повторной обработки перечитывает и возвращает это состояние вместо ложного `not found`;
- `pending` после повторного чтения не считается успешной обработкой и должен возвращаться как конфликт.
- read RPC и safe diagnostics никогда не возвращают raw/canonical webhook payload: `WebhookEvent.payload_json` в gRPC является safe envelope, а не копией storage payload.
- raw/canonical webhook payload не входит в `RepositoryMergeSignal`, provider-owned read responses, outbox/event-log payload или междоменные checked artifact inputs.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор события. |
| `provider_slug` | text | no | indexed | Поставщик. |
| `delivery_id` | text | no | unique by provider | Идентификатор доставки webhook. |
| `event_name` | text | no | indexed | Имя события провайдера. |
| `repository_provider_id` | text | no | default '' | Внешний id репозитория, если есть. |
| `received_at` | timestamptz | no | indexed | Время приёма. |
| `processing_status` | text | no | indexed | `pending`, `processed`, `failed`, `ignored`. |
| `payload_json` | jsonb | no | object | Canonical provider webhook payload для `pending`/`failed` retry до TTL или safe envelope после `processed`/`ignored`/cleanup; внутреннее поле, не safe read surface. |
| `payload_sha256` | text | no | sha256 hex | Digest canonical provider payload для replay/conflict и safe diagnostics. |
| `last_error` | text | no | default '' | Короткая ошибка обработки. |
| `retain_until` | timestamptz | no | indexed | Срок хранения полного payload для retryable статусов; запись inbox остаётся как safe diagnostic envelope после очистки. |

### `ProviderEvent`

Назначение: нормализованное событие провайдера после разбора raw webhook или сверки.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор нормализованного события. |
| `source_webhook_event_id` | UUID | yes | indexed | Ссылка внутри БД `provider-hub`. |
| `event_type` | text | no | indexed | Нормализованный тип. |
| `aggregate_type` | text | no | indexed | `work_item`, `comment`, `relationship`, `account_runtime_state`, `limit`, `repository_adoption_scan`. |
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
- управляемые provider write-команды, включая bootstrap пустого репозитория и adoption существующего репозитория, могут сразу проставить `project_id` и `repository_id`, если вызывающий контур уже передал проверенную проектную привязку;
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

Bootstrap/adoption-команды могут создать служебную связь `project_repository_binding`: источник — созданная bootstrap или adoption `PR/MR` проекция, `target_provider_ref` — безопасная ссылка вида `project-catalog:project:<project_id>:repository:<repository_id>`. Эта связь не означает владение проектной политикой со стороны `provider-hub`; она нужна, чтобы UI/MCP и сверка видели provider-native артефакт в контексте проверенного project/repository binding.

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

### `RepositoryMergeSignal`

Назначение: безопасный факт, что bootstrap/adoption `PR/MR` был принят владельцем через merge у провайдера.

Важные инварианты:

- сигнал создаётся из provider-native webhook/reconciliation факта, связанного с bootstrap/adoption PR через безопасную PR-проекцию, watermark, provider relationship или operation ref;
- `provider-hub` хранит только safe refs, digest, timestamps и статус, но не хранит raw provider payload, body PR, содержимое файлов, provider response, токены или секреты;
- естественный ключ строится из provider slug, kind и provider work item id; повтор того же сигнала идемпотентен, а повтор с тем же ключом и другим commit/source ref считается конфликтом;
- gRPC read surface отдаёт сигнал по `id`/`signal_key` или списком по project/repository/provider context, включая version и safe `etag`, вычисленный из provider-owned refs/digests/version;
- merge signal не запускает импорт `services.yaml` сам: `project-catalog` остаётся владельцем проверки policy payload и активации repository binding, а project-side reconciliation принимает только safe signal refs и checked artifact metadata.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор merge signal. |
| `signal_key` | text | no | unique | Естественный ключ идемпотентности. |
| `kind` | text | no | enum-like | `bootstrap` или `adoption`. |
| `provider_slug` | text | no | indexed | Поставщик; в текущем срезе активен GitHub. |
| `project_id` | UUID | no | indexed | Внешняя ссылка на проект. |
| `repository_id` | UUID | no | indexed | Внешняя ссылка на repository binding. |
| `repository_full_name` | text | no | indexed | `owner/name` или аналог. |
| `provider_repository_id` | text | no |  | Внешний id репозитория. |
| `work_item_projection_id` | UUID | no | FK внутри БД `provider-hub` | PR/MR projection, из которой получен сигнал. |
| `provider_work_item_id` | text | no | indexed | Стабильный id PR/MR у провайдера. |
| `pull_request_number` | bigint | no | > 0 | Номер PR/MR у провайдера. |
| `pull_request_provider_id` | text | no |  | Внутренний id PR/MR у провайдера, если доступен. |
| `pull_request_url` | text | no |  | Безопасная web-ссылка PR/MR. |
| `base_branch` | text | no |  | Целевая ветка. |
| `head_branch` | text | no |  | Ветка PR/MR. |
| `merge_commit_sha` | text | no |  | Commit, которым принят PR/MR. |
| `source_ref` | text | no |  | Watermark `source_ref` или safe fallback на head branch. |
| `related_provider_operation_ref` | text | no |  | Безопасная ссылка на provider operation/ref, если известна. |
| `watermark_digest` | text | no |  | Digest разобранного watermark, без тела PR/MR. |
| `observed_at` | timestamptz | no | indexed | Когда сигнал принят или замечен. |
| `merged_at` | timestamptz | no | indexed | Когда provider сообщил merge. |
| `status` | text | no | enum-like | `merged`. |
| `version` | bigint | no | monotonic | Версия записи. |

### `RepositoryAdoptionScanSnapshot`

Назначение: безопасный lightweight снимок существующего репозитория для planning в adoption-сценарии.

Важные инварианты:

- snapshot создаётся только из provider metadata/ref/tree API и не читает blob/file contents, diff, archive или raw provider response;
- `provider-hub` хранит только safe refs, counts, object digests, bounded warnings, timestamps и digest snapshot;
- project/adoption decision, импорт `services.yaml`, deep workspace scan/report и запуск агента остаются у соседних доменов;
- gRPC read surface отдаёт snapshot по `id`/`snapshot_key`/`provider_operation_id` или списком по provider/repository context, включая status, version и safe `etag`, вычисленный из provider-owned refs/digests/version;
- естественный ключ строится из provider slug, repository refs, scanned ref, head sha и operation ref; повтор команды по `command_id` возвращает уже сохранённый snapshot.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор snapshot. |
| `snapshot_key` | text | no | unique | Естественный ключ идемпотентности snapshot. |
| `provider_operation_id` | UUID | no | FK внутри БД `provider-hub` | Операция `scan_repository_for_adoption`. |
| `external_account_id` | UUID | no | indexed | Внешний аккаунт, подтверждённый через `access-manager`. |
| `provider_slug` | text | no | indexed | Поставщик; активен `github`. |
| `repository_full_name` | text | no | indexed | `owner/name` или аналог. |
| `provider_repository_id` | text | no |  | Внешний id репозитория, если доступен. |
| `repository_url` | text | no |  | Безопасная web-ссылка репозитория. |
| `default_branch` | text | no |  | Default branch провайдера. |
| `requested_ref` | text | no |  | Запрошенный ref; пусто, если использован default branch. |
| `scanned_ref` | text | no |  | Фактически просканированный branch/ref. |
| `head_sha` | text | no |  | Commit sha фактического ref. |
| `status` | text | no | enum-like | `completed`, `limited`, `needs_review`. |
| `markers_json` | jsonb | no | default [] | Marker path refs, kind, object digest и size; без содержимого файлов. |
| `file_count` | bigint | no | >= 0 | Общее число blob entries, видимых по tree API. |
| `visible_file_count` | bigint | no | >= 0 | Число blob entries в пределах bounded scan. |
| `tree_truncated` | boolean | no | default false | Provider tree или локальные scan limits обрезали результат. |
| `warnings_json` | jsonb | no | default [] | Bounded warning codes без provider payload. |
| `snapshot_digest` | text | no |  | Digest безопасного snapshot payload. |
| `observed_at` | timestamptz | no | indexed | Когда snapshot зафиксирован. |
| `version` | bigint | no | monotonic | Версия записи. |

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
| `provider_object_id` | text | no | default '' | Стабильный id созданного или изменённого объекта у провайдера, если доступен. |
| `repository_full_name` | text | no | default '' | Фактическое имя репозитория в формате `owner/name`, если результат операции относится к репозиторию. |
| `error_code` | text | no | default '' | Классификация ошибки. |
| `error_message` | text | no | default '' | Короткое сообщение без секрета. |
| `rate_limit_snapshot_id` | UUID | yes | indexed | Снимок лимитов после операции. |
| `operation_policy_context_json` | jsonb | no | default {} | Безопасный снимок контекста политики: роль, проект, стадия, операция, цель, изменяемые поля, риск, версия политики. |
| `approval_gate_ref_json` | jsonb | no | default {} | Ссылка на уже принятое approval/gate решение, если оно требовалось policy. |
| `provider_version` | text | no | default '' | Версия или update marker результата у провайдера, если доступна. |
| `base_branch` | text | no | default '' | Начальная ветка, подготовленная provider-side созданием репозитория, если применимо. |
| `started_at` | timestamptz | no | indexed | Начало. |
| `finished_at` | timestamptz | yes | indexed | Завершение. |
| `version` | bigint | no | monotonic | Версия записи операции. |

`operation_policy_context_json` и `approval_gate_ref_json` не содержат секретов, token refs, сырых provider payload, email, имён или тел комментариев. Это только проверяемый след того, по какой роли, проекту, стадии, target, набору полей и версии политики команда была разрешена. `approval_gate_ref_json` хранит ссылку на внешний approval/gate, но не переносит в `provider-hub` владение самим решением.

Идемпотентный повтор provider-операции по `operation_type + command_id` сначала читает уже записанную операцию по ключу команды. Replay разрешён только при совпадении области команды: `actor_id`, `external_account_id`, `provider_slug`, `operation_type`, `target_ref`, `operation_policy_context_json` и `approval_gate_ref_json`. Сравнение контекста выполняется по каноническому JSON, чтобы round-trip через PostgreSQL не превращал пустые списки и отсутствующие поля в ложный конфликт. Если тот же `command_id` приходит с другой областью, операция конфликтует.
Перед внешним write-вызовом `provider-hub` создаёт durable-запись `ProviderOperation` в состоянии `in_progress`. Если процесс упал после provider side effect, но до завершения локальной транзакции, повтор той же команды увидит `in_progress`, вернёт конфликт и не выполнит второй внешний write. Recovery такого состояния выполняется отдельным эксплуатационным контуром через сверку и разбор незавершённых операций.
`ProviderOperation` является идемпотентным журналом внешней записи или bounded provider-side чтения: после успешного сохранения повтор той же команды возвращает записанный результат и не выполняет provider call повторно. Реальный адаптер записи после успешного ответа провайдера завершает `in_progress`-операцию и обновляет локальные проекции и связи в той же транзакции, где фиксируются outbox-события. Для `CreateRepository` отдельная таблица репозитория у провайдера не создаётся: команда сохраняет безопасный `target_ref`, result URL, provider repository id, provider version и публикует `provider.repository.created`, а авторитетная проектная привязка остаётся в `project-catalog`. Для `ScanRepositoryForAdoption` отдельная snapshot-таблица сохраняет только safe refs, markers, counts, warnings и digest по operation ref. Для `CreateBootstrapPullRequest` и `CreateAdoptionPullRequest` подготовленные файлы являются только входом внешнего вызова: их содержимое не сохраняется в `ProviderOperation`, outbox, событиях, логах и ошибках. Ошибки провайдера сохраняются только как безопасная классификация без сырого payload и без секретов.

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
| Найти adoption scan snapshot | unique `(provider_operation_id)`, unique `(snapshot_key)`, `(provider_slug, repository_full_name, observed_at DESC)` |

## Политика хранения

| Данные | Политика |
|---|---|
| Raw webhook payload | Хранить только для `pending`/`failed` до `retain_until`; после явной cleanup-операции оставлять safe envelope, `payload_sha256`, статус и причину `payload_expired`. |
| Нормализованные provider events | Хранить по политике аудита и диагностики домена. |
| Проекции рабочих артефактов | Хранить пока артефакт связан с активным проектом, архивом или аудитом. |
| Комментарии | Хранить digest, краткую выдержку и provider ref; полные тела не копить без подтверждённого сценария. |
| Снимки лимитов | Хранить агрегированно и с ограничением по сроку. |
| Adoption scan snapshots | Хранить как безопасный provider-side summary без raw file contents; срок хранения задаётся audit/diagnostic policy. |
| Operation log | Хранить как аудит provider-операций без секретов и полных payload. |
| Локальный outbox | Хранить до успешной доставки или до ручного разбора окончательных отказов. |

## Миграции

Миграции живут в `services/internal/provider-hub/cmd/cli/migrations/*.sql`. Первый срез создаёт схему `provider_hub_*` для целевой доменной модели и локального outbox. Создание БД в Kubernetes, migration job и эксплуатационные манифесты фиксируются отдельным эксплуатационным срезом.

## Апрув

- request_id: `owner-2026-05-06-provider-hub-boundaries`
- Решение: approved
- Комментарий: модель данных `provider-hub` согласована как целевое состояние.
