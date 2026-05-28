# Центр взаимодействий

## Назначение

Домен `interaction-hub` владеет platform-owned lifecycle взаимодействия с человеком:

- диалоговые ветки и сообщения;
- запросы обратной связи;
- Human gate;
- approval request;
- уведомления и напоминания;
- подписки;
- попытки доставки;
- callback внешних каналов;
- доменную read-поверхность owner inbox list/detail по собственным feedback, approval, Human gate и callback diagnostics.

Цель домена — дать UI, голосу, MCP и внешним каналам один lifecycle feedback, approval и inbox/outbox без дублирования правды между поверхностями.

## Ключевые решения

- Список каналов не фиксируется заранее.
- Внешние каналы подключаются через гибридную модель: package-owned runtime плюс стабильный channel delivery/callback contract.
- Плагин канала устанавливается и описывается через `package-hub`; runtime-нагрузку выполняют `runtime-manager` и `fleet-manager`.
- `interaction-hub` хранит request lifecycle, delivery attempts, callback records и ответы человека, но не владеет business decision state, package installation, UI или внешним HTTP gateway.
- `interaction-hub` отдаёт owner inbox items и detail только по собственным interaction-сущностям; междоменный inbox собирается позднее в gateway/operations-контуре, а ответ записывается через существующий request/response lifecycle.
- Действия владельца нормализованы: утвердить — `approve`, отклонить — `reject`, запросить доработку — `request_changes`, ответить — `answer`.
- OpenAPI-каркас внешнего callback-входа находится в `integration-gateway`; текущая доменная каноника фиксирует payload, callback lifecycle и применение safe callback к request response lifecycle.

## Границы

| Владеет домен | Не владеет домен |
|---|---|
| Диалоги, feedback request, approval request, Human gate, уведомления, подписки, delivery attempts, reminders, callback, owner inbox read surface по собственным interaction-сущностям, ответы человека, события `interaction.*`. | Flow, `Run`, session, acceptance, provider write pipeline, provider projections, runtime jobs, package catalog/installations, UI state, внешний HTTP gateway, биллинг, cross-domain operations inbox, business decision state соседних сервисов. |

## Документы домена

| Документ | Назначение |
|---|---|
| `product/requirements.md` | Требования `interaction-hub`, сценарии, границы и зависимости. |
| `architecture/design.md` | Детальный дизайн, выбранная channel model, потоки и междоменные связи. |
| `architecture/data_model.md` | Сущности, связи, индексы, retention и правила внешних ссылок. |
| `architecture/api_contract.md` | API-обзор будущего `InteractionHubService`, MCP-инструменты, channel contract и события. |
| `delivery/interaction_hub_delivery.md` | План поставки домена и порядок кодовых срезов. |
| `ops/runbook.md` | Runbook deploy, миграций, readiness и диагностики `interaction-hub`. |
| `ops/monitoring.md` | Метрики, health checks, alerts и safe logging для `interaction-hub`. |

## Карта Issue

- Доменная карта: `docs/delivery/issue-map/domains/interaction-hub.md`.
