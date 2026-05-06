---
doc_id: DLV-CK8S-PROVIDER-HUB
type: delivery-plan
title: kodex — поставка provider-hub
status: active
owner_role: EM
created_at: 2026-05-06
updated_at: 2026-05-06
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

`provider-hub` поставляется малыми PR-срезами: сначала доменная документация, затем контракты, каркас сервиса, GitHub adapter и лимиты, webhook inbox, проекции рабочих артефактов, сверка, provider-операции, provider-часть сценариев bootstrap/adoption и эксплуатационный контур.

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
| PRV-2 | Сервисный каркас, БД, миграции, repository layer, config, health/readiness и базовые тесты готовы. |
| PRV-3 | Runtime-состояние внешних аккаунтов у провайдера, provider client interface, GitHub adapter, лимиты и operation log готовы. |
| PRV-4 | Webhook inbox, дедупликация, нормализация GitHub-событий и публикация базовых `provider.*` событий готовы. |
| PRV-5 | Проекции `Issue`, `PR/MR`, комментариев, review-сигналов, watermark и provider relationships готовы. |
| PRV-6 | Incremental reconciliation, `sync_cursor`, hot/warm/cold приоритеты, drift status и ускоряющие сигналы готовы. |
| PRV-7 | Платформенные provider-операции для agent-manager/MCP готовы с аудитом и идемпотентностью. |
| PRV-8 | Provider-часть empty repository bootstrap и existing repository adoption готова; содержательное сканирование и отчёт по существующему репозиторию остаются агентной работой через workspace. |
| PRV-9 | Kubernetes-манифесты, БД, migration job, metrics, alerts, runbook и smoke-путь готовы. |

## Таблица реализации

Контракты `provider-hub` зафиксированы в `proto/kodex/providers/v1/provider_hub.proto` и `specs/asyncapi/provider-hub.v1.yaml`. Этот раздел показывает разницу между контрактной готовностью и фактической реализацией сервиса.

| Группа | Контракт | Реализация |
|---|---|---|
| Приём webhook | Готово: `IngestWebhookEvent`, чтение, список и повторная обработка. | Не начата; будет в PRV-4 после контракта `integration-gateway`. |
| Проекции артефактов провайдера | Готово: чтение рабочих артефактов, комментариев и связей. | Не начата; будет в PRV-5. |
| Сверка | Готово: сигналы, очередь сверки, batch-обработка и курсоры. | Не начата; будет в PRV-6. |
| Операции провайдера | Готово: создание и обновление `Issue`, комментариев, `PR/MR`, review-сигналов и связей. | Не начата; будет в PRV-7 после согласования `agent-manager` и MCP-инструментов. |
| Операционное состояние аккаунта и лимиты | Готово: состояние аккаунта у провайдера, снимки лимитов и журнал операций. | Не начата; будет в PRV-3 после синхронизации с `access-manager`. |
| Первичная инициализация пустого репозитория | Готово на уровне событий bootstrap required/completed и операций провайдера. | Запись в провайдера и зеркало оставлены до PRV-8; решение о составе первичных артефактов приходит из проектного и агентного контура. |
| Подключение существующего репозитория | Готово на уровне событий adoption required/adoption PR created и операций провайдера. | `PR` у провайдера, зеркало и связи оставлены до PRV-8; сканирование и отчёт выполняет агентная роль через workspace. |

## Зависимости и синхронизация

| С кем синхронизироваться | Когда | Что согласовать |
|---|---|---|
| `project-catalog` | До PRV-1 и перед PRV-8 | `project_id`, `repository_id`, provider ref, состояние подключения репозитория, `services.yaml` bootstrap/adoption. |
| `access-manager` | До PRV-3 | Набор действий доступа для provider-операций и контракт разрешения внешнего аккаунта. |
| `package-hub` | До PRV-5 и PRV-7 | Как пакеты ссылаются на provider-репозитории и PR в пакетных репозиториях. |
| `integration-gateway` | До PRV-4 | Формат внутреннего вызова `IngestWebhookEvent` и ответственность за проверку подписи. |
| `agent-manager` и `platform-mcp-server` | До PRV-7 | Каталог provider-инструментов, идемпотентность и ожидаемый результат операций. |
| `operations-hub` | До PRV-5 и PRV-9 | Какие поля проекций нужны операторским экранам и диагностике. |

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
