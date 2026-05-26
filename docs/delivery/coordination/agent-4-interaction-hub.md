# Агент #4 — центр взаимодействий

## Зона ответственности

Агент #4 ведёт домен `interaction-hub`.

Ответственность:

- диалоговые ветки и сообщения;
- запросы обратной связи владельца и пользователя;
- Human gate;
- approval request;
- уведомления и напоминания;
- подписки;
- попытки доставки и retry/reminder metadata;
- callback внешних каналов;
- стабильный channel delivery/callback contract поверх package-owned runtime;
- доменная документация `docs/domains/interaction-hub/**`.

`interaction-hub` не владеет flow, `Run`, session, acceptance, provider write pipeline, runtime jobs, package catalog/installations, UI, внешним HTTP gateway, биллингом и операционными read models.

## Что уже сделано

| Срез | Issue | Статус | Результат |
|---|---:|---|---|
| IH-0 | #582 | готово как docs-first срез | Доменная документация, границы, требования, дизайн, модель данных, API-обзор, delivery-план, карта Issue и координация агента #4 подготовлены без кода, proto, AsyncAPI и OpenAPI. |
| IH-1 | #768 | готово как контрактный срез | gRPC/AsyncAPI контракты `interaction-hub`, события `interaction.*`, Go-сгенерированные transport/event contracts, действия доступа и stable channel delivery/callback DTO подготовлены без сервисной реализации, БД, миграций, gateway OpenAPI и конкретных каналов. |
| IH-2 | #783 | готово как сервисный каркас | `services/internal/interaction-hub` содержит runnable process scaffold, env config, health/readiness/metrics, gRPC transport registration, domain service skeleton и repository stub; бизнес-операции возвращают `Unimplemented`. |
| IH-3 | #800 | готово как persistence foundation | PostgreSQL-модель, real repository для thread/message MVP lifecycle, command result idempotency и service-local outbox `interaction.*`; request/delivery/callback lifecycle остаётся для следующих срезов. |

## Текущий бэклог

| Срез | Статус | Почему не завершён |
|---|---|---|
| IH-4+ | ожидает контрактные срезы | Lifecycle feedback, approval, Human gate, notifications, delivery, callback, MCP и ops-связки должны поставляться малыми PR. |

## Блокировки от других доменов

| Домен или сервис | Что блокирует | Решение |
|---|---|---|
| `agent-manager` | End-to-end Human gate, feedback и продолжение сессии после ответа или owner decision. | `interaction-hub` отдаёт request/response events и не меняет `Run` сам. |
| `platform-mcp-server` | MCP tools `interaction.*` для slot-агентов и быстрого manager. | MCP остаётся policy/audit boundary и маршрутизирует к `interaction-hub`. |
| `codex-hook-ingress` | PermissionRequest и другие hook events, требующие вопроса человеку. | Hook ingress передаёт безопасный normalized event; `interaction-hub` создаёт request. |
| `provider-hub` | Owner decision refs для provider write pipeline. | `interaction-hub` хранит delivery/response lifecycle, а provider write и approval decision остаются у владельцев. |
| `package-hub` | Channel package capability, installation refs и manifest requirements. | Использовать чтения `package-hub`; не управлять установками пакетов. |
| `runtime-manager` и `fleet-manager` | Runtime-нагрузки channel package. | Runtime/fleet исполняют package-owned workloads; `interaction-hub` работает через channel contract. |
| `integration-gateway` | Публичный callback transport, подписи и OpenAPI. | IGW-0 закрепил HTTP-каркас gateway; активация callback route требует стабилизации channel contract `interaction-hub`. |
| `operations-hub` | Dual-surface inbox, operator queue и агрегированные статусы. | `operations-hub` строит read models по событиям и чтениям `interaction-hub`. |

## Рекомендуемый следующий шаг

Следующий рациональный срез — IH-4: lifecycle feedback, approval и Human gate requests поверх уже подготовленной persistency-основы. Конкретные внешние каналы, callback routes `integration-gateway` и runtime-нагрузки пакетов не смешивать с IH-4.
