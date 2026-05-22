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

## Текущий бэклог

| Срез | Статус | Почему не завершён |
|---|---|---|
| IH-1 | ожидает отдельный Issue | Нужно создать gRPC/AsyncAPI контракты `interaction-hub`, события `interaction.*`, действия доступа и channel contract DTO. |
| IH-2 | ожидает IH-1 | Сервисный каркас должен опираться на утверждённые transport и event contracts. |
| IH-3 | ожидает IH-1/IH-2 | PostgreSQL-модель должна следовать утверждённому lifecycle и не опережать контракты. |
| IH-4+ | ожидает контрактные срезы | Lifecycle feedback, approval, Human gate, notifications, delivery, callback, MCP и ops-связки должны поставляться малыми PR. |

## Блокировки от других доменов

| Домен или сервис | Что блокирует | Решение |
|---|---|---|
| `agent-manager` | End-to-end Human gate, feedback и продолжение сессии после решения. | `interaction-hub` отдаёт request/decision events и не меняет `Run` сам. |
| `platform-mcp-server` | MCP tools `interaction.*` для slot-агентов и быстрого manager. | MCP остаётся policy/audit boundary и маршрутизирует к `interaction-hub`. |
| `codex-hook-ingress` | PermissionRequest и другие hook events, требующие вопроса человеку. | Hook ingress передаёт безопасный normalized event; `interaction-hub` создаёт request. |
| `provider-hub` | Approval gate refs для provider write pipeline. | `interaction-hub` хранит approval lifecycle, а provider write остаётся у `provider-hub`. |
| `package-hub` | Channel package capability, installation refs и manifest requirements. | Использовать чтения `package-hub`; не управлять установками пакетов. |
| `runtime-manager` и `fleet-manager` | Runtime-нагрузки channel package. | Runtime/fleet исполняют package-owned workloads; `interaction-hub` работает через channel contract. |
| Future gateway | Публичный callback transport, подписи и OpenAPI. | Gateway проектируется отдельным срезом после стабилизации channel contract. |
| `operations-hub` | Dual-surface inbox, operator queue и агрегированные статусы. | `operations-hub` строит read models по событиям и чтениям `interaction-hub`. |

## Рекомендуемый следующий шаг

Следующий рациональный срез — IH-1: контракты `interaction-hub` без сервисной реализации. В него должны войти proto, AsyncAPI, события `interaction.*`, действия доступа и DTO channel contract. Конкретные внешние каналы, gateway OpenAPI и runtime-нагрузки пакетов не смешивать с IH-1.
