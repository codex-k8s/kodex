---
doc_id: DLV-CK8S-PROVIDER-HUB
type: delivery-plan
title: kodex — поставка provider-hub
status: active
owner_role: EM
created_at: 2026-05-06
updated_at: 2026-05-28
related_issues: [281, 282, 711, 719, 725, 729, 737, 754, 761, 770, 781, 840, 864, 865, 895, 908, 909, 939]
related_prs: []
related_docsets:
  - docs/domains/provider-native-work-items/product/requirements.md
  - docs/domains/provider-native-work-items/architecture/design.md
  - docs/domains/provider-native-work-items/architecture/data_model.md
  - docs/domains/provider-native-work-items/architecture/api_contract.md
  - docs/domains/provider-native-work-items/ops/provider_hub_runbook.md
  - docs/domains/provider-native-work-items/ops/provider_hub_monitoring.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-06-provider-hub-boundaries"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-06
---

# Поставка provider-hub

## TL;DR

`provider-hub` поставляется малыми PR-срезами: сначала доменная документация, затем контракты, каркас сервиса, GitHub-адаптер и лимиты, журнал webhook, проекции рабочих артефактов, сверка, операции провайдера, часть сценариев bootstrap/adoption и эксплуатационный контур.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования домена | `docs/domains/provider-native-work-items/product/requirements.md` |
| Дизайн домена | `docs/domains/provider-native-work-items/architecture/design.md` |
| Модель данных | `docs/domains/provider-native-work-items/architecture/data_model.md` |
| API-обзор | `docs/domains/provider-native-work-items/architecture/api_contract.md` |
| Сквозная модель интеграции с провайдерами | `docs/platform/architecture/provider_integration_model.md` |
| Runbook | `docs/domains/provider-native-work-items/ops/provider_hub_runbook.md` |
| Наблюдаемость | `docs/domains/provider-native-work-items/ops/provider_hub_monitoring.md` |

## Срезы поставки

| Срез | Результат |
|---|---|
| PRV-0 | Доменная документация, границы, требования, модель данных, API-карта и план поставки готовы. |
| PRV-1 | gRPC/AsyncAPI контракты `provider-hub`, сгенерированный код и таблица реализации операций готовы. |
| PRV-2 | Сервисный каркас, схема БД, миграции, слой репозитория, конфигурация, health/readiness и базовые тесты готовы. |
| PRV-3 | Операционное состояние внешних аккаунтов у провайдера, интерфейс клиента провайдера, GitHub-адаптер, лимиты и журнал операций готовы. |
| PRV-4 | Журнал webhook, дедупликация, нормализация GitHub-событий и публикация базовых `provider.*` событий готовы. |
| PRV-5 | Проекции `Issue`, `PR/MR`, комментариев, review-сигналов, watermark и provider relationships готовы. |
| PRV-6.1 | Очередь сверки, `sync_cursor`, приоритеты `hot/warm/cold`, чтение курсоров и короткая аренда курсора для worker готовы. |
| PRV-6.2 | Инкрементальная пакетная сверка GitHub по курсорам, выбранному внешнему аккаунту, окнам перекрытия, лимитному бюджету и drift status готова в режиме только чтения. |
| PRV-6.3 | Ускоряющие сигналы от agent-manager/MCP и slot-агентов ставят hot cursor без обращения к provider API и секретам. |
| PRV-6.4 | Общий контракт безопасного разрешения секретов готов: `provider-hub` использует resolver-клиент в пакетной сверке, не сохраняя токен и не раскрывая его через публичные ответы; доступны реализации хранилища `kubernetes_mounted_secret`, `env` и `vault`. |
| PRV-7a | Контрактный каталог платформенных операций записи для agent-manager/MCP: типизированные инструменты, общий конвейер команд, контекст политики и ссылка на approval/gate. |
| PRV-7b | Реализация общего конвейера команд операций записи без специфичного для провайдера расползания по сервисному слою. |
| PRV-7c | Реализация GitHub-адаптера записи для операций из каталога с журналом операций, лимитами, проекциями и событиями. |
| PRV-7d | Усиление GitHub write-адаптера и жизненного цикла `ProviderOperation`: повтор по `idempotency_key`, конфликт той же key с другой целью, классификация `rate_limited`/secondary limit, временных ошибок, permission denied, not found, conflict/validation и безопасных сводок ошибок проверены без реального GitHub. |
| PRV-8a | Provider-часть bootstrap для уже созданного пустого репозитория: запись подготовленных файлов в bootstrap branch, создание или обновление bootstrap PR, provider relationships и локальные проекции без adoption scan. |
| PRV-8b | Provider-side создание GitHub-репозитория с начальным default branch через `auto_init` готово. |
| PRV-8c | Provider-часть adoption существующего репозитория; содержательное сканирование и отчёт остаются агентной работой через workspace. |
| PRV-8d | Provider-side merge signal для bootstrap/adoption PR: GitHub `pull_request closed + merged` фиксируется безопасно и публикуется как `provider.repository.*_merged`. |
| PRV-8e | Lightweight provider-side adoption scan snapshot: GitHub metadata/ref/tree scan фиксирует только safe refs, markers, counts, warnings и digest без raw file contents. |
| PRV-8f | Provider-native read surface: gRPC отдаёт safe merge signals и adoption scan snapshots по stable ids/context без checked artifact registry, raw file contents и validated `services.yaml` payload. |
| PRV-8g | Smoke/diagnostic producer path: safe GitHub `pull_request closed + merged` fixture проверяет цепочку edge route -> `IngestWebhookEvent` -> safe merge signal -> producer outbox/event-log readiness без consumer framework. |
| PRV-8h | Adoption counterpart и producer-side hardening: bootstrap/adoption fixtures проверяют route/ingest/read/outbox, replay не создаёт дубль события, conflict даёт безопасную диагностику, raw/canonical webhook payload не попадает в safe signal/read surface/outbox. |
| PRV-8i | GitHub live-smoke: управляемый скрипт с режимами dry-run/apply создаёт или переиспользует тестовый репозиторий в `codex-k8s`, ветку, PR и merge, формирует настоящий `pull_request closed + merged` payload и опционально проверяет запущенные endpoints `integration-gateway`/`provider-hub`. |
| PRV-9 | Kubernetes-манифесты, БД, migration job, smoke-путь, runbook и документы наблюдаемости готовы. |

## Таблица реализации

Контракты `provider-hub` зафиксированы в `proto/kodex/providers/v1/provider_hub.proto` и `specs/asyncapi/provider-hub.v1.yaml`. Этот раздел показывает разницу между контрактной готовностью и фактической реализацией сервиса.

| Группа | Контракт | Реализация |
|---|---|---|
| Приём webhook | Готово: `IngestWebhookEvent`, чтение, список, повторная обработка и явная cleanup-операция истёкших retryable payload. | Реализовано в PRV-4: входящий журнал, дедупликация по `provider_slug + delivery_id`, базовая нормализация GitHub-событий, статусы обработки и outbox-события `provider.webhook.received` / `provider.webhook.normalized`. Privacy-hardening добавляет `payload_sha256`, safe envelope для терминальных и истёкших payload, `CleanupExpiredWebhookPayloads` и безопасный отказ retry с `payload_expired`. Публичный HTTP webhook endpoint остаётся ответственностью `integration-gateway`. |
| Проекции артефактов провайдера | Готово: чтение рабочих артефактов, комментариев и связей. | Реализовано в PRV-5: запись проекций `Issue`, `PR/MR`, комментариев и review-сигналов при нормализации webhook, разбор watermark, связи из watermark, чтение по provider ref и списочные gRPC-операции. |
| Сверка | Готово: сигналы, очередь сверки, пакетная обработка и курсоры. | Реализовано в режиме только чтения: PRV-6.1 добавил доменную модель `sync_cursor`, постановку области в очередь, чтение, список и короткую аренду курсора; PRV-6.3 добавил ускоряющий сигнал, который ставит `hot` cursor по provider target и выбранному внешнему аккаунту; PRV-6.2b подключил `ResolveExternalAccountUsage` и `libs/go/secretresolver` к обработчику, читает GitHub API по курсорам, обновляет проекции провайдера, лимитный бюджет, операционное состояние и безопасно продвигает курсор. |
| Операции провайдера | Готово: типизированные команды записи, общий command pipeline, журнал операций и outbox-события результата. | PRV-7a зафиксировал контракт, PRV-7b реализовал gRPC handlers, casters, domain pipeline, optimistic concurrency, `ProviderOperation` с policy/gate trace и безопасный `ProviderOperationResponse`. PRV-7c подключает GitHub write-адаптер для создания и обновления задач, комментариев, `PR`, review-сигналов и provider-native связей без хранения токенов. PRV-7d усиливает классификацию отказов GitHub write и проверки жизненного цикла операций: `rate_limited`/secondary limit остаются retryable, временные ошибки провайдера/сети остаются retryable, permission denied, not found, conflict и validation становятся безопасными терминальными отказами, а operation log/outbox не включают raw provider response, token, private URL или payload. GitLab-адаптер остаётся следующим расширением той же границы. |
| Операционное состояние аккаунта и лимиты | Готово: состояние аккаунта у провайдера, снимки лимитов и журнал операций. | Реализовано в PRV-3: доменная логика, PostgreSQL-репозиторий, gRPC-чтение/запись снимков лимитов, базовый GitHub-адаптер для проверки лимитов. Фильтры по проекту и организации в списке операционных состояний остаются контрактным заделом до подключения разрешения внешних аккаунтов через `access-manager`. |
| Первичная инициализация пустого репозитория | Готово: `CreateRepository` создаёт GitHub-репозиторий и provider default branch, `CreateBootstrapPullRequest` принимает подготовленные файлы и refs, создаёт или обновляет bootstrap branch/PR. | PRV-8b реализует создание репозитория на стороне провайдера через общий pipeline и GitHub-адаптер с `auto_init=true`, фиксирует `base_branch` и событие `provider.repository.created`. PRV-8a реализует provider-side запись bootstrap branch/PR, обновляет проекцию `PR`, provider relationship к project/repository binding и событие `provider.repository.bootstrap_completed`. End-to-end вызов из проектного или агентного контура остаётся отдельным срезом. |
| Подключение существующего репозитория | Готово: `ScanRepositoryForAdoption` снимает lightweight provider-side snapshot, `GetRepositoryAdoptionScanSnapshot`/`ListRepositoryAdoptionScanSnapshots` отдают сохранённые snapshots, а `CreateAdoptionPullRequest` принимает подготовленные файлы и refs, создаёт или обновляет adoption branch/PR, фиксирует проекцию, связь и событие `provider.repository.adoption_pr_created`. | PRV-8e реализует GitHub metadata/ref/tree scan без чтения содержимого файлов и сохраняет только safe refs, marker path refs/digests/counts, bounded warnings и snapshot digest. PRV-8f добавляет read surface по stable ids/context со status/version/etag. PRV-8c реализует provider-side запись adoption branch/PR без генерации `services.yaml`, выбора шаблонов и project policy decision. End-to-end вызов, глубокий workspace scan/report и импорт политики остаются отдельными срезами соседних доменов. |
| Merge bootstrap/adoption PR | Готово: безопасный provider-side merge signal, gRPC-чтение этих сигналов и AsyncAPI события `provider.repository.bootstrap_merged` / `provider.repository.adoption_merged`. | Реализовано в PRV-8d: GitHub `pull_request closed + merged` связывается с уже известной bootstrap/adoption PR-проекцией, сохраняет только safe refs/digest/timestamps, дедуплицируется по signal key и конфликтует при другом commit/source ref. PRV-8f добавляет `GetRepositoryMergeSignal`/`ListRepositoryMergeSignals` со status/version/etag. PRV-8g добавляет staged smoke fixture и live HTTP diagnostic mode для producer-side проверки route -> ingest -> merge signal -> outbox/event-log. PRV-8h добавляет adoption fixture, replay/conflict smoke и leak assertions для safe outputs. PRV-8i добавляет управляемый GitHub live-smoke на настоящем тестовом репозитории, ветке и PR без печати raw payload и с опциональными проверками запущенного gateway/provider-hub. `provider-hub` не вызывает `project-catalog` напрямую; project-side reconciliation вызывает `ReconcileBootstrapMergeSignal` или `ReconcileAdoptionMergeSignal` с safe signal и checked artifact metadata, которые остаются в `project-catalog`. |
| Эксплуатационный контур | Готово: Dockerfile, Kubernetes-манифесты, bootstrap БД, migration job, smoke-путь, runbook и monitoring docs. | PRV-9 добавил `deploy/base/provider-hub/**`, подключение `kodex_provider_hub` к PostgreSQL bootstrap/runtime secrets, build/smoke scripts и эксплуатационные документы. Реальная проверка на кластере выполняется отдельным операторским запуском smoke-скрипта с нормализованным bootstrap env. |

## Текущее состояние реализации

Сервисный процесс `provider-hub` создан в `services/internal/provider-hub/**`.

Готово:

- запуск процесса с HTTP health/readiness, `/metrics` и общим gRPC-рантаймом через `libs/go/grpcserver`;
- конфигурация gRPC и PostgreSQL через env с ограничениями на соединения и retry-подключение;
- собственная схема БД `provider_hub_*` и goose-миграция для таблиц доменной модели;
- слой репозитория и доменный сервис для проверки готовности хранилища;
- регистрация полного gRPC-контракта `ProviderHubService`;
- операции чтения операционных состояний внешних аккаунтов, записи и чтения снимков лимитов, чтения журнала операций;
- атомарное обновление операционного состояния аккаунта при записи снимка лимита;
- базовый GitHub-адаптер на `go-github`, который получает `/rate_limit`, классифицирует состояние аккаунта и возвращает нормализованные снимки лимитов без сохранения секрета;
- проверка `CommandMeta` для команд записи снимков лимитов: команда должна иметь `command_id` или `idempotency_key`;
- идемпотентная запись снимков лимитов по естественному ключу без перезаписи исторического наблюдения;
- разделение частичного runtime update от снимка лимита и авторитетного runtime upsert;
- защита runtime state от устаревших частичных snapshot-наблюдений;
- проверка области и результата при идемпотентном повторе provider operation;
- отдельное replay-чтение для конкурентных повторов snapshot и provider operation;
- входящий журнал webhook с идемпотентной записью по `provider_slug + delivery_id`;
- синхронный первый проход нормализации для базовых GitHub-событий `issues`, `pull_request`, `issue_comment`, `pull_request_review` и `pull_request_review_comment`;
- повторная обработка webhook для записей в статусах `pending` и `failed`;
- явная очистка истёкших retryable webhook payload через `CleanupExpiredWebhookPayloads`: `pending`/`failed` после `retain_until` сохраняют safe envelope, `payload_sha256`, статус `failed` и причину `payload_expired`; повторная обработка таких записей безопасно отказывает без попытки нормализации;
- запись нормализованных provider events и локальных outbox-событий `provider.webhook.received` / `provider.webhook.normalized`;
- запись нормализованных проекций `Issue` и `PR/MR` из GitHub webhook payload;
- запись проекций комментариев и review-сигналов, привязанных к рабочему артефакту; review-сигналы сохраняют `review_state`;
- защита проекций от задержанных webhook: более старый `provider_updated_at` не перезаписывает актуальные поля и не порождает событие проекции как свежее;
- разбор watermark из тела рабочего артефакта, фиксация статуса `missing`, `valid` или `invalid` и перенос безопасных полей в `watermark_json`; `valid` требует `kind`, `managed_by`, `work_type`, совпадения `kind` с артефактом и `source_ref` для `PR/MR`;
- построение provider relationships из watermark-полей `source_ref`, `parent_ref` и `next_ref` с пересборкой текущего watermark-набора при свежем обновлении;
- gRPC-чтение проекций через `GetWorkItemProjection`, `FindWorkItemByProviderRef`, `ListWorkItemProjections`, `ListComments` и `ListRelationships`;
- публикация локальных outbox-событий `provider.work_item.synced`, `provider.comment.synced` и `provider.relationship.synced`;
- очередь сверки через `EnqueueReconciliation`, `GetSyncCursor`, `ListSyncCursors` и базовый lease-путь `RunReconciliationBatch` без обращения к внешнему provider API;
- явное сохранение `external_account_id` в запросе постановки и курсоре сверки, чтобы worker не выбирал внешний аккаунт неявно;
- идемпотентная постановка сверки по `provider_slug + scope_type + scope_ref + idempotency_key`: повтор той же команды не меняет курсоры, а повтор с другим внешним аккаунтом или составом запроса возвращает конфликт;
- PostgreSQL-хранение курсоров сверки с естественным ключом `provider_slug + scope_type + scope_ref + artifact_kind`, пакетной атомарной постановкой нескольких `artifact_kind`, сохранением более высокого приоритета при новой постановке и защитой lease через `FOR UPDATE SKIP LOCKED`;
- `RegisterProviderArtifactSignal` для внутренних сигналов от `agent-manager`, platform MCP и slot-агентов: вызывающий контур передаёт `external_account_id`, source, время наблюдения и provider target, а `provider-hub` сохраняет signal-level идемпотентность и ставит `hot` cursor без чтения секрета и без обращения в GitHub/GitLab API; след сигнала и курсоры фиксируются одной транзакцией, чтобы принятый сигнал не оставался без работы сверки;
- для сигналов по `Issue`/`PR/MR` ставятся курсоры основного артефакта, комментариев и связей; если тип рабочего артефакта неизвестен, ставятся hot cursors для `issue`, `pull_request`, `merge_request`, комментариев и связей; для repository target ставится курсор репозитория;
- штатный outbox dispatcher `provider-hub` в `platform-event-log`;
- общий `libs/go/secretresolver`, через который пакетная сверка получает значение секрета после `ResolveExternalAccountUsage` без хранения токена в `provider-hub`;
- пакетная GitHub-сверка в режиме только чтения по арендованному курсору: обработчик подтверждает выбранный внешний аккаунт через `access-manager`, получает `SecretValue` только на время API-вызова, читает `Issue`, `PR`, комментарии, review и состояние репозитория в согласованном объёме, сохраняет нормализованные проекции, публикует события синхронизации и обновляет `cursor_value`, `overlap_since`, `last_success_at`, `last_error`, `rate_budget_state_json` и операционное состояние;
- безопасная классификация ошибок сверки: rate limit оставляет lease до retry-времени, auth failure переводит runtime state в `reauthorization_required`, not found/permanent/transient ошибки фиксируются коротким кодом без токена;
- контрактный каталог операций записи для `agent-manager` и platform MCP: `CreateIssue`, `UpdateIssue`, `CreateComment`, `UpdateComment`, `CreatePullRequest`, `UpdatePullRequest`, `CreateReviewSignal`, `UpdateRelationship`;
- общий pipeline этих команд: gRPC handlers, transport casters, единый domain service, idempotent `ProviderOperation`, проверка `expected_version`, policy context, `approval_gate_ref` и outbox-события `provider.operation.completed/failed`;
- безопасный `ProviderOperationResponse` без секретов, token refs и сырых provider payload;
- GitHub write-адаптер поверх общего command pipeline: создание задач, обновление задач, создание и обновление комментариев, создание и обновление `PR`, review-сигналы и обновление provider-native связей;
- перед внешним write-вызовом `provider-hub` подтверждает выбранный внешний аккаунт через `access-manager`, получает `SecretValue` через общий resolver только в памяти процесса и очищает его после вызова;
- перед внешним GitHub write-вызовом `provider-hub` резервирует `ProviderOperation` в состоянии `in_progress`; повтор такой команды получает конфликт и не создаёт второй внешний side effect;
- повтор уже записанной команды по `command_id` или `idempotency_key` возвращает сохранённый `ProviderOperation` до проверки локальной версии и не выполняет внешний GitHub write повторно; повтор той же key с другой целью считается конфликтом;
- успешные GitHub write-вызовы сразу обновляют локальные проекции рабочих артефактов, комментариев и связей, чтобы UI/MCP не ждали полной сверки;
- ошибки GitHub классифицируются безопасно: rate limit и secondary limit фиксируются как retryable, transient provider/network failures — как retryable, auth/permission, not found, conflict и validation — как terminal failure; в журнал операции и события попадает только короткий код без provider payload, raw response, private URL и без секрета.
- чтобы не оставлять частичный side effect, GitHub `CreatePullRequest` в текущем срезе не принимает `labels` и `linked_issue_ref`; эти изменения должны идти отдельными командами после создания `PR`.
- чтобы не оставлять частичное изменение без транзакционной гарантии, GitHub `UpdatePullRequest` отклоняет смешанные команды, где одновременно есть метаданные PR на issue-стороне (`labels`/`assignee_provider_logins`/`milestone`) и собственные поля PR (`base_branch`/`maintainer_can_modify`); вызывающий контур должен разбивать такие изменения на отдельные идемпотентные команды и связывать их общим `correlation_id`, если это один пользовательский сценарий.
- `CreateRepository` создаёт GitHub-репозиторий с `auto_init=true`, чтобы провайдер создал начальный default branch и минимальный начальный commit; команда возвращает `base_branch`, provider repository id и URL, фиксирует `ProviderOperation` и событие `provider.repository.created`, но не генерирует `services.yaml`, не выбирает шаблоны, не сканирует репозиторий и не меняет branch protection.
- `CreateBootstrapPullRequest` создаёт или обновляет bootstrap branch и bootstrap PR для уже созданного GitHub-репозитория: вызывающий контур передаёт уже подготовленные файлы, base branch, bootstrap branch, title/body и watermark, а `provider-hub` не генерирует `services.yaml` и не сканирует репозиторий. Команда требует существующий base branch, допускает пустое дерево или безопасный `README.md`, созданный GitHub при `auto_init`, запрещает совпадение base/bootstrap branch и не наследует старые файлы из ранее созданной bootstrap branch.
- успешный bootstrap PR сразу создаёт локальную PR-проекцию с `project_id` и `repository_id`, provider relationship `project_repository_binding` и событие `provider.repository.bootstrap_completed`; `ProviderOperation` и outbox не содержат файловый payload, секрет или raw provider response.
- `CreateAdoptionPullRequest` создаёт или обновляет adoption branch и reviewable adoption PR для существующего GitHub-репозитория: вызывающий контур передаёт уже подготовленные файлы, base branch, adoption branch, title/body и watermark, а `provider-hub` не сканирует репозиторий, не генерирует `services.yaml`, не выбирает шаблон и не принимает проектное решение. Команда требует существующий base branch, допускает непустое дерево base branch, запрещает совпадение base/adoption branch и не хранит файловый payload.
- успешный adoption PR сразу создаёт локальную PR-проекцию с `project_id` и `repository_id`, provider relationship `project_repository_binding` и событие `provider.repository.adoption_pr_created`; `ProviderOperation` и outbox не содержат файловый payload, секрет или raw provider response.
- `ScanRepositoryForAdoption` снимает lightweight provider-side snapshot существующего GitHub-репозитория: читает repository metadata, default/scanned ref, head sha и bounded tree entries, детектирует marker path refs (`services.yaml`, `.gitmodules`, `README`, `AGENTS.md`, `docs`, workflow/deploy/module/package markers), сохраняет object digests/counts/warnings/status и публикует `provider.repository.adoption_scan_completed`; raw file contents, diff/archive, provider response, секреты и project decision не сохраняются.
- merge bootstrap/adoption PR фиксируется provider-side как `RepositoryMergeSignal`: GitHub `pull_request closed + merged` использует уже известную PR-проекцию, project/repository refs, base/head branch, merge commit sha, source ref, related operation ref и watermark digest; raw/canonical webhook payload, body PR, содержимое файлов, provider response и секреты в signal/read surface/outbox/event-log payload не попадают.
- `GetRepositoryMergeSignal`, `ListRepositoryMergeSignals`, `GetRepositoryAdoptionScanSnapshot` и `ListRepositoryAdoptionScanSnapshots` отдают только provider-owned safe refs/status/timestamps/version/etag; checked artifact, checked payload и нормализованный `services.yaml` остаются в `project-catalog`.
- повтор merge signal с тем же signal key не создаёт второй доменный outbox event, а конфликтующий сигнал с другим commit/source ref безопасно отклоняется; импорт проверенной `services.yaml` и активация binding остаются в `project-catalog`.
- `scripts/smoke-provider-merge-signal.sh` проверяет producer-side путь safe bootstrap/adoption fixtures -> `integration-gateway` route wiring -> `provider-hub IngestWebhookEvent` -> `RepositoryMergeSignal` read surface -> локальный outbox event; live HTTP режим дополнительно может проверить попадание события в `platform-event-log`, если включён dispatch и доступен безопасный diagnostic DSN.
- `scripts/smoke-provider-github-live.sh` добавляет управляемую live-проверку GitHub provider path: dry-run не меняет GitHub, `--apply` создаёт или переиспользует тестовый репозиторий в `codex-k8s`, ветку, PR и merge, формирует настоящий `pull_request closed + merged` payload во временном файле и опционально проверяет развёрнутые `integration-gateway`/`provider-hub`.
- эксплуатационный контур `provider-hub`: Dockerfile, `deploy/base/provider-hub/**`, создание БД `kodex_provider_hub` в PostgreSQL bootstrap, runtime Secret refs без значений секретов, migration job, Service/Deployment с HTTP/gRPC ports, readiness/liveness/metrics, requests/limits, build/smoke scripts и runbook/monitoring документы.

Миграция `external_account_id` для очереди сверки явно очищает строки `provider_hub_sync_cursors` и `provider_hub_reconciliation_requests`, созданные предыдущим срезом без знания внешнего аккаунта. Эти строки являются эфемерным состоянием планировщика и пересоздаются повторной постановкой сверки; так тестовые кластеры с уже развёрнутым PRV-6.1 не упираются в `ADD COLUMN ... NOT NULL`.

Ограничение текущей сверки: пакетная GitHub-сверка работает только на чтение и обрабатывает один provider target за завершение аренды курсора, после чего обработчик повторно входит через продвинутый курсор. GitHub write-адаптер подключён к общему исполнителю команд записи, включая создание GitHub-репозитория, bootstrap PR, adoption PR и lightweight adoption scan snapshot, но MCP-поверхность, интеграция с agent-manager, UI/gateway и GitLab write adapter остаются отдельными срезами. Эксплуатационный контур готов на уровне манифестов, smoke-пути и runbook; реальный запуск на кластере выполняется оператором через нормализованный bootstrap env.

Архитектурное исключение среза: вспомогательные функции gRPC caster остаются локальными в `provider-hub`, потому что вынос общего transport-пакета требует согласованного изменения `access-manager`, `project-catalog` и текущего сервиса. Это не должно копироваться в новые сервисы; отдельный малый срез перед следующим доменом должен вынести общую часть в `libs/go/**` и перевести существующие сервисы.

## Зависимости и синхронизация

| С кем синхронизироваться | Когда | Что согласовать |
|---|---|---|
| `project-catalog` | Перед end-to-end bootstrap/adoption | `project_id`, `repository_id`, provider ref, состояние подключения репозитория, `services.yaml` bootstrap/adoption. PRV-8e отдаёт только provider-side snapshot для planning; PRV-8a и PRV-8c принимают project/repository ссылки как готовый вход и не ходят в `project-catalog`. |
| `access-manager` | Перед PRV-6.2/PRV-7 и при включении фильтров области операционных состояний | Системные действия провайдера, контракт `ResolveExternalAccountUsage`, подтверждение выбранного внешнего аккаунта, `provider_slug` и ссылка на секрет без значения секрета. Значение после разрешения доступа получает общий `libs/go/secretresolver`; `provider-hub` не хранит токен. |
| `package-hub` | Перед PRV-8 | Как пакеты ссылаются на provider-репозитории и PR в пакетных репозиториях. |
| `integration-gateway` | Перед публичным приёмом webhook | IGW-0 закрепил границу и OpenAPI-каркас внешнего HTTP-входа. Формат внутреннего вызова `IngestWebhookEvent` уже закреплён в `provider-hub`; `integration-gateway` отвечает за внешний HTTP, проверку подписи, лимиты, backpressure и передачу проверенного сигнала. |
| `agent-manager` и `platform-mcp-server` | После PRV-7c | GitHub write-адаптер готов на внутренней gRPC-границе. MCP-0 фиксирует внешнюю MCP-поверхность: provider tools маршрутизируются в `provider-hub`, а источник решения политики по риску и передача `approval_gate_ref` остаются частью последующих интеграционных срезов. |
| `operations-hub` | Перед расширением операторских экранов | Какие дополнительные поля проекций нужны операторским экранам, сверке и диагностике. |

## Связь с задачами подключения репозиториев

Задачи #281 и #282 остаются открытыми до полного end-to-end bootstrap/adoption. PRV-8a, PRV-8b, PRV-8c, PRV-8d, PRV-8e, PRV-8f, PRV-8g, PRV-8h и PRV-8i закрывают только provider-side часть создания репозитория, bootstrap/adoption PR, безопасного merge signal, lightweight adoption scan snapshot, чтения этих provider-owned фактов и producer-side smoke/diagnostic проверки.

Решение:

- `project-catalog` владеет проектной привязкой, политикой и `services.yaml`;
- `provider-hub` владеет фактом provider-состояния, provider-операциями, зеркалом, provider relationships и созданием или обновлением provider-native артефактов;
- `provider-hub` владеет только lightweight provider-side snapshot существующего репозитория: safe refs, marker path refs, object digests, counts, bounded warnings и digest; содержательное сканирование, отчёт, выбор шаблона и проектное решение выполняет `project-catalog`/`agent-manager` через соседние контуры;
- empty repository допускает controlled direct bootstrap только как исключение, при этом `provider-hub` выполняет provider write после решения о составе bootstrap-артефактов;
- в PRV-8b `provider-hub` создаёт GitHub-репозиторий с provider-side default branch, но не генерирует `services.yaml`, не выбирает шаблон и не выполняет adoption scan;
- в PRV-8a `provider-hub` принимает подготовленный набор файлов и refs, создаёт или обновляет bootstrap branch/PR и фиксирует связи, но не генерирует `services.yaml` и не выполняет adoption scan;
- в PRV-8c `provider-hub` принимает подготовленный набор файлов и refs, создаёт или обновляет adoption branch/PR и фиксирует связи, но не выполняет scan, не генерирует `services.yaml` и не выбирает шаблон;
- в PRV-8d/PRV-8h `provider-hub` фиксирует факт merge bootstrap/adoption PR и публикует безопасный сигнал, но не проверяет содержимое `services.yaml`, не импортирует project policy и не активирует repository binding;
- в PRV-8e `provider-hub` снимает lightweight provider-side scan snapshot без raw file contents, но не строит adoption report, не импортирует `services.yaml`, не запускает агента и не принимает project/adoption decision;
- `project-catalog` принимает safe bootstrap/adoption merge signal только через checked artifact metadata и вызывает свой import use-case; provider-native факт merge остаётся в `provider-hub`, а raw webhook доступен только внутреннему retryable inbox до terminal обработки;
- existing repository adoption end-to-end остаётся проектно-агентным сценарием: глубокий workspace scan, отчёт, выбор шаблона, approval и импорт политики выполняют соседние домены.

## Граница webhook payload и safe surface

`provider-hub` webhook inbox хранит canonical provider webhook payload в `provider_hub_webhook_events.payload_json` только для `pending` и `failed` записей до `retain_until`, чтобы сохранить PRV-4 retry/reprocess semantics в коротком диагностическом окне. После `processed` или `ignored` полный provider payload заменяется safe envelope с digest/source refs и storage status. После явной cleanup-операции истёкшие `pending`/`failed` записи тоже получают safe envelope с `payload_storage=expired_after_retention`, `payload_cleanup_reason=payload_expired` и `payload_sha256`. Это внутреннее хранилище `provider-hub`, а не safe read surface для соседних сервисов.

Наружу через `GetWebhookEvent`/`ListWebhookEvents`, `GetRepositoryMergeSignal`/`ListRepositoryMergeSignals`, `ProviderEventPayload`, outbox и `platform-event-log` уходят только safe refs/facts/digests/status/timestamps/version: provider slug, repository refs, PR number/id/url, base/head branch, merge commit sha, source ref, related provider operation ref, watermark digest и статус. В webhook read RPC поле `payload_json` является safe envelope, а `payload_sha256` — digest canonical body. Raw/canonical webhook body, body PR, provider response, diff, checked artifact payload и checked `services.yaml` не входят в read surface, merge signal и доменные события.

Оставшиеся шаги privacy-hardening для webhook inbox: encryption-at-rest/KMS policy и re-fetch/reprocess strategy для сценариев, где полный payload нельзя удерживать даже в коротком `failed` окне.

## Definition of Done для каждого PR

- Обновлены документы домена и карта Issue, если меняется состав срезов.
- Если меняются контракты, выполнена генерация и обновлена таблица реализации.
- Если меняется Go-код, выполнены профильные Go-проверки.
- Если меняются события, обновлены AsyncAPI и сгенерированные Go-контракты событий.
- PR закрывает или ссылается на соответствующую GitHub Issue через тело PR.

## Апрув

- request_id: `owner-2026-05-06-provider-hub-boundaries`
- Решение: approved
- Комментарий: план поставки `provider-hub` согласован как целевое состояние PRV-0.
