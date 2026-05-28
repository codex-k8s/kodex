---
doc_id: PRD-CK8S-INTERACTION-HUB-0001
type: prd
title: kodex — требования домена центра взаимодействий
status: active
owner_role: PM
created_at: 2026-05-22
updated_at: 2026-05-28
related_issues: [582, 768, 781, 800, 867, 921, 928]
related_prs: []
related_docsets:
  - docs/platform/architecture/domain_map.md
  - docs/platform/architecture/service_boundaries.md
  - docs/platform/architecture/mcp_and_interaction_model.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-22-interaction-hub-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-22
---

# PRD: центр взаимодействий

## TL;DR

- Что строим: домен `interaction-hub` для диалогов, запросов обратной связи, Human gate, approval request, уведомлений, подписок, попыток доставки и callback внешних каналов.
- Для кого: пользователи платформы, Owner, операторы, быстрый `agent-manager`, ролевые агенты в слотах, соседние сервисы и будущие channel packages.
- Почему: owner feedback, approvals и inbox/outbox должны иметь один platform-owned lifecycle, а UI и внешние каналы не должны становиться отдельными источниками правды.
- Минимум первой версии: контракт запросов к человеку, delivery lifecycle, callback envelope, события `interaction.*` и интеграционные точки для MCP, `agent-manager`, `codex-hook-ingress`, `provider-hub` и `operations-hub`.
- Критерии успеха: `agent-manager` и slot-агенты могут запросить feedback или approval через `platform-mcp-server`, человек отвечает через UI или внешний канал, а платформа видит единое состояние запроса, доставки, ответа и аудита.

## Проблема и цель

Проблема:

- платформа должна спрашивать человека в разных местах: интерактивный диалог, owner feedback, Human gate, approval перед risk action, permission request от Codex hook, операционное уведомление;
- UI, голосовой ввод и внешние каналы не должны вводить разные статусы и разные правила жизненного цикла одного ответа человеку;
- внешние каналы должны подключаться без vendor-specific списка в ядре;
- `agent-manager`, `provider-hub`, `runtime-manager` и `operations-hub` не должны хранить доставку, callback и диалоговую переписку как свою доменную истину.

Цель:

- выделить `interaction-hub` как сервис-владелец platform-owned interaction lifecycle;
- описать расширяемый channel contract без реализации конкретного канала;
- подготовить первый кодовый PR с контрактами и сервисным каркасом, не создавая proto, AsyncAPI или gateway в этом документационном срезе.

## Пользователи и роли

| Роль | Главный сценарий |
|---|---|
| Пользователь платформы | Общается с системой через web-console, голос или будущий внешний канал и видит ответы, вопросы и ожидающие решения. |
| Owner | Получает запросы approval, Human gate и обратной связи, принимает или отклоняет решение с объяснением. |
| Оператор платформы | Видит застрявшие запросы, ошибки доставки, повторные напоминания, callback с ошибкой и очередь внимания. |
| Быстрый `agent-manager` | Создаёт запросы к человеку и продолжает агентную сессию после решения. |
| Ролевой агент в слоте | Через MCP запрашивает feedback или approval, не выбирая канал доставки сам. |
| Channel package | Получает delivery command по стабильному контракту и возвращает callback, не владея lifecycle запроса. |
| `operations-hub` | Строит проекции чтения и операторские очереди из событий и авторитетных чтений `interaction-hub`. |

## Функциональные требования

| ID | Требование | Приоритет |
|---|---|---|
| INT-FR-1 | Домен должен хранить диалоговые ветки и сообщения, которые относятся к платформенному взаимодействию с пользователем или владельцем. | Обязательно |
| INT-FR-2 | Домен должен поддерживать запросы обратной связи владельца с единым lifecycle: created, routed, waiting, answered, expired, cancelled, failed. | Обязательно |
| INT-FR-3 | Домен должен поддерживать approval request для рискованных действий, provider write pipeline и release/governance сценариев, не выполняя само рискованное действие. | Обязательно |
| INT-FR-4 | Домен должен поддерживать Human gate как запрос решения человека, связанный с agent/run/release/runtime/provider контекстом, но не владеть состоянием этих соседних агрегатов. | Обязательно |
| INT-FR-5 | Домен должен создавать уведомления и напоминания по доменным событиям, правилам подписки и явным командам соседних сервисов. | Обязательно |
| INT-FR-6 | Домен должен хранить подписки на типы событий, области, каналы и политики доставки без владения UI-настройками и package installation. | Обязательно |
| INT-FR-7 | Домен должен хранить попытки доставки, статус, retry/reminder metadata, безопасные ошибки и связь с исходным запросом. | Обязательно |
| INT-FR-8 | Домен должен принимать callback внешних каналов через проверенный пограничный контур, связывать callback с исходным request/delivery attempt и создавать итоговый `InteractionResponse`, если callback выбирает допустимый terminal action. | Обязательно |
| INT-FR-9 | Внешние каналы должны подключаться через гибридную модель: package-owned runtime плюс стабильный channel delivery/callback contract. | Обязательно |
| INT-FR-10 | Список внешних каналов не должен фиксироваться в ядре; manifest plugin package объявляет capability канала, требуемые API, права, секреты и runtime-требования. | Обязательно |
| INT-FR-11 | Домен должен отдавать типизированные операции запроса feedback, approval, чтения статуса доставки и owner inbox list/detail по собственным interaction-сущностям. | Обязательно |
| INT-FR-12 | Домен должен принимать от `agent-manager` запросы feedback, Human gate и notification intent, но не хранить flow, `Run`, session или acceptance как свою истину. | Обязательно |
| INT-FR-13 | Домен должен принимать нормализованные события `codex-hook-ingress`, если hook требует вопроса, разрешения или уведомления человеку. | Обязательно |
| INT-FR-14 | Домен должен связывать запросы с provider-native артефактами через safe refs и события, но не выполнять provider write pipeline. | Обязательно |
| INT-FR-15 | Домен должен публиковать `interaction.*` события по созданию запроса, доставке, callback, ответу человека, истечению срока и ошибке. | Обязательно |
| INT-FR-16 | Домен должен поддерживать идемпотентные команды, ожидаемую версию для конкурентных ответов и audit-safe повтор callback. | Обязательно |
| INT-FR-17 | Домен не должен владеть UI, внешним HTTP gateway, runtime job, package installation, provider write operation, flow/run/session или operations read model. | Обязательно |

## Критерии приёмки

| ID | Критерий |
|---|---|
| INT-AC-1 | Если slot-агент запрашивает feedback через MCP, `interaction-hub` создаёт request, выбирает допустимый delivery route и возвращает request ref без знания конкретного внешнего канала агентом. |
| INT-AC-2 | Если `agent-manager` или `governance-manager` запрашивает Human gate, `interaction-hub` хранит delivery request, attempts и ответ человека, а состояние `Run` или gate decision меняется только через сервис-владелец. |
| INT-AC-3 | Если provider write operation требует approval, `provider-hub` получает ссылку на решение от владельца decision state; `interaction-hub` передаёт только delivery/callback результат и не становится владельцем provider approval. |
| INT-AC-4 | Если пользователь отвечает через UI или внешний канал, callback приводит к одному и тому же lifecycle переходу запроса. |
| INT-AC-5 | Если внешний канал недоступен, `interaction-hub` фиксирует ошибку доставки, refs retry/reminder policy и событие для операторской видимости. |
| INT-AC-6 | Если установлен channel package, `interaction-hub` использует его capability и package installation ref, но не меняет установку и не запускает runtime-нагрузку сам. |
| INT-AC-7 | Если канал возвращает callback повторно, команда идемпотентна и не создаёт второй ответ. |
| INT-AC-8 | Если UI или operator surface запрашивает входящие решения, `interaction-hub` возвращает только pending/active request и callback diagnostics по своим сущностям; cross-domain aggregation остаётся вне домена. |
| INT-AC-9 | Если UI или gateway открывает одно входящее решение, `interaction-hub` возвращает safe detail с allowed actions, refs, delivery/callback/response summaries и version; ответ записывается через существующий response lifecycle с idempotency и expected version. Действие `request_changes` означает “запросить доработку” и не смешивается с отказом `reject`. |
| INT-AC-10 | Если запрос истёк, домен публикует событие истечения и соседний сервис-владелец решает, как менять своё состояние. |

## Что не входит

- Не хранить flow, stage, role, `Run`, session, acceptance или automation rules.
- Не владеть provider write pipeline, provider operations, provider projections и нативными комментариями как источником истины.
- Не запускать runtime jobs, Kubernetes workloads или workloads плагинов.
- Не владеть package catalog, package installation, manifest verification и secret binding.
- Не проектировать UI и не хранить состояние клиентского интерфейса.
- Не проектировать внешний HTTP gateway как часть домена.
- Не фиксировать список конкретных внешних каналов.
- Не создавать proto, AsyncAPI или OpenAPI в этом документационном срезе.

## Нефункциональные требования

| ID | Категория | Требование |
|---|---|---|
| INT-NFR-1 | Надёжность | Запрос, ответ, callback и delivery attempt фиксируются транзакционно вместе с outbox-событием. |
| INT-NFR-2 | Безопасность | Callback принимает только проверенный пограничный контур; сырые секреты, токены channel package и непроверенные payload не хранятся в БД. |
| INT-NFR-3 | Наблюдаемость | Каждый request, delivery attempt и callback имеет correlation id, source refs, безопасный error code и метрики задержки. |
| INT-NFR-4 | Расширяемость | Новый внешний канал добавляется как plugin package capability и channel contract, а не через изменение enum конкретных каналов. |
| INT-NFR-5 | Совместимость | Lifecycle запроса не зависит от поверхности: UI, голос, MCP и внешний канал используют одни статусы и одну модель ответа. |
| INT-NFR-6 | Локализация | Сообщения, действия ответа, причины отказа и ошибки доставки должны иметь локализуемые шаблоны или message id. |
| INT-NFR-7 | Хранение данных | Временные медиа и голосовые вложения хранятся ссылками с retention policy, а не binary payload в PostgreSQL. |

## Зависимости

| Зависимость | Зачем нужна |
|---|---|
| `agent-manager` | Источник запросов feedback, Human gate, approval и продолжения агентной сессии после решения. |
| `platform-mcp-server` | Контролируемая MCP-поверхность для slot-агентов и быстрого `agent-manager`. |
| `codex-hook-ingress` | Нормализованные Codex hook events, включая permission request и пользовательский ввод, который требует реакции человека. |
| `provider-hub` | Provider refs, provider operation refs, owner decision refs для рискованных provider write commands и события provider-состояния. |
| `package-hub` | Установленные plugin packages, manifest capability канала, required APIs, права и секреты; установка пакета остаётся у `package-hub`. |
| `runtime-manager` и `fleet-manager` | Технический запуск runtime-нагрузки channel package и размещение; `interaction-hub` только вызывает согласованный delivery contract. |
| `access-manager` | Проверка прав на создание запросов, отправку ответа, использование channel package и чтение статусов. |
| `operations-hub` | Операторские очереди, ленты событий и агрегированные статусы по запросам и ошибкам доставки. |
| `integration-gateway` | Аутентификация внешнего HTTP, проверка подписи callback и маршрутизация в доменный сервис без владения lifecycle. |

## Апрув

- request_id: `owner-2026-05-22-interaction-hub-kickoff`
- Решение: approved
- Комментарий: требования `interaction-hub` согласованы как стартовое целевое состояние; выбран гибрид package-owned runtime плюс стабильный channel contract.
