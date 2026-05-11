---
doc_id: DLV-CK8S-PROVIDER-HUB
type: delivery-plan
title: kodex — поставка provider-hub
status: active
owner_role: EM
created_at: 2026-05-06
updated_at: 2026-05-08
related_issues: [281, 282]
related_prs: []
related_docsets:
  - docs/domains/provider-native-work-items/product/requirements.md
  - docs/domains/provider-native-work-items/architecture/design.md
  - docs/domains/provider-native-work-items/architecture/data_model.md
  - docs/domains/provider-native-work-items/architecture/api_contract.md
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
| PRV-6.2 | Инкрементальная batch-сверка GitHub по курсорам, выбранному внешнему аккаунту, окнам перекрытия, лимитному бюджету и drift status готова. |
| PRV-6.3 | Ускоряющие сигналы от agent-manager/MCP и slot-агентов ставят hot cursor без обращения к provider API и секретам. |
| PRV-7 | Платформенные provider-операции для agent-manager/MCP готовы с аудитом и идемпотентностью. |
| PRV-8 | Provider-часть empty repository bootstrap и existing repository adoption готова; содержательное сканирование и отчёт по существующему репозиторию остаются агентной работой через workspace. |
| PRV-9 | Kubernetes-манифесты, БД, migration job, metrics, alerts, runbook и smoke-путь готовы. |

## Таблица реализации

Контракты `provider-hub` зафиксированы в `proto/kodex/providers/v1/provider_hub.proto` и `specs/asyncapi/provider-hub.v1.yaml`. Этот раздел показывает разницу между контрактной готовностью и фактической реализацией сервиса.

| Группа | Контракт | Реализация |
|---|---|---|
| Приём webhook | Готово: `IngestWebhookEvent`, чтение, список и повторная обработка. | Реализовано в PRV-4: входящий журнал, дедупликация по `provider_slug + delivery_id`, базовая нормализация GitHub-событий, статусы обработки и outbox-события `provider.webhook.received` / `provider.webhook.normalized`. Публичный HTTP webhook endpoint остаётся ответственностью будущего `integration-gateway`. |
| Проекции артефактов провайдера | Готово: чтение рабочих артефактов, комментариев и связей. | Реализовано в PRV-5: запись проекций `Issue`, `PR/MR`, комментариев и review-сигналов при нормализации webhook, разбор watermark, связи из watermark, чтение по provider ref и списочные gRPC-операции. |
| Сверка | Готово: сигналы, очередь сверки, batch-обработка и курсоры. | Частично реализовано: PRV-6.1 добавил доменную модель `sync_cursor`, постановку области в очередь, чтение, список и короткую аренду курсора; PRV-6.3 добавил ускоряющий сигнал, который ставит `hot` cursor по provider target и выбранному внешнему аккаунту. Реальная batch-сверка провайдера и drift status остаются в PRV-6.2. |
| Операции провайдера | Готово: создание и обновление `Issue`, комментариев, `PR/MR`, review-сигналов и связей. | Не начата; будет в PRV-7 после согласования `agent-manager` и MCP-инструментов. |
| Операционное состояние аккаунта и лимиты | Готово: состояние аккаунта у провайдера, снимки лимитов и журнал операций. | Реализовано в PRV-3: доменная логика, PostgreSQL-репозиторий, gRPC-чтение/запись снимков лимитов, базовый GitHub-адаптер для проверки лимитов. Фильтры по проекту и организации в списке операционных состояний остаются контрактным заделом до подключения разрешения внешних аккаунтов через `access-manager`. |
| Первичная инициализация пустого репозитория | Готово на уровне событий bootstrap required/completed и операций провайдера. | Запись в провайдера и зеркало оставлены до PRV-8; решение о составе первичных артефактов приходит из проектного и агентного контура. |
| Подключение существующего репозитория | Готово на уровне событий adoption required/adoption PR created и операций провайдера. | `PR` у провайдера, зеркало и связи оставлены до PRV-8; сканирование и отчёт выполняет агентная роль через workspace. |

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
- `RegisterProviderArtifactSignal` для внутренних сигналов от `agent-manager`, platform MCP и slot-агентов: вызывающий контур передаёт `external_account_id`, source, время наблюдения и provider target, а `provider-hub` ставит `hot` cursor без чтения секрета и без обращения в GitHub/GitLab API;
- для сигналов по `Issue`/`PR/MR` ставятся курсоры основного артефакта, комментариев и связей; для repository target ставится курсор репозитория;
- штатный outbox dispatcher `provider-hub` в `platform-event-log`.

Миграция `external_account_id` для очереди сверки явно очищает строки `provider_hub_sync_cursors` и `provider_hub_reconciliation_requests`, созданные предыдущим срезом без знания внешнего аккаунта. Эти строки являются эфемерным состоянием планировщика и пересоздаются повторной постановкой сверки; так тестовые кластеры с уже развёрнутым PRV-6.1 не упираются в `ADD COLUMN ... NOT NULL`.

Ограничение текущей сверки: сервис ставит курсоры в очередь, отдаёт их на чтение, выдаёт короткую аренду worker и принимает ускоряющие сигналы. Он ещё не читает GitHub API, не продвигает `cursor_value`, не публикует `provider.sync_cursor.advanced` и не выставляет итоговый drift status. Реальная batch-сверка провайдера будет добавлена в PRV-6.2. Команды записи в провайдера, bootstrap/adoption и эксплуатационный контур пока остаются `Unimplemented`. Kubernetes-манифесты, создание БД в deploy-контуре, migration job, alerts и runbook остаются в PRV-9.

Архитектурное исключение среза: вспомогательные функции gRPC caster остаются локальными в `provider-hub`, потому что вынос общего transport-пакета требует согласованного изменения `access-manager`, `project-catalog` и текущего сервиса. Это не должно копироваться в новые сервисы; отдельный малый срез перед следующим доменом должен вынести общую часть в `libs/go/**` и перевести существующие сервисы.

## Зависимости и синхронизация

| С кем синхронизироваться | Когда | Что согласовать |
|---|---|---|
| `project-catalog` | До PRV-1 и перед PRV-8 | `project_id`, `repository_id`, provider ref, состояние подключения репозитория, `services.yaml` bootstrap/adoption. |
| `access-manager` | Перед PRV-6.2/PRV-7 и при включении фильтров области операционных состояний | Системные действия провайдера, контракт `ResolveExternalAccountUsage`, подтверждение выбранного внешнего аккаунта, `provider_slug` и ссылка на секрет без значения секрета. |
| `package-hub` | Перед PRV-7 и PRV-8 | Как пакеты ссылаются на provider-репозитории и PR в пакетных репозиториях. |
| `integration-gateway` | Перед публичным приёмом webhook | Формат внутреннего вызова `IngestWebhookEvent` уже закреплён в `provider-hub`; `integration-gateway` отвечает за внешний HTTP, проверку подписи и передачу проверенного сигнала. |
| `agent-manager` и `platform-mcp-server` | До PRV-7 | Каталог provider-инструментов, идемпотентность и ожидаемый результат операций. |
| `operations-hub` | Перед PRV-6 и PRV-9 | Какие дополнительные поля проекций нужны операторским экранам, сверке и диагностике. |

## Связь с задачами подключения репозиториев

Задачи #281 и #282 остаются открытыми до PRV-8.

Решение:

- `project-catalog` владеет проектной привязкой, политикой и `services.yaml`;
- `provider-hub` владеет фактом provider-состояния, provider-операциями, зеркалом, provider relationships и созданием или обновлением provider-native артефактов;
- `provider-hub` не владеет содержательным сканированием и отчётом по существующему репозиторию: его выполняет `agent-manager` через агентную роль и workspace с нужными инструкциями;
- empty repository допускает controlled direct bootstrap только как исключение, при этом `provider-hub` выполняет provider write после решения о составе bootstrap-артефактов;
- existing repository adoption идёт через reviewable PR, который `provider-hub` создаёт или обновляет по результату агентного отчёта и проектного решения.

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
