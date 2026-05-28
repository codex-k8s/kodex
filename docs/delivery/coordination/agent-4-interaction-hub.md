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
- owner inbox read surface по собственным feedback, approval, Human gate и callback diagnostics;
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
| IH-4 | #806 | готово как request lifecycle | Feedback, approval и Human gate request lifecycle работает поверх PostgreSQL repository: create/get/list, response, cancel, expire, idempotency и безопасные `interaction.*` outbox events без внешних channel adapters и без владения decision state. |
| IH-5a | #821 | готово как notification/subscription lifecycle | `RequestNotification`, `UpsertSubscription`, `DisableSubscription`, `ListSubscriptions`, idempotency, optimistic concurrency и safe `interaction.*` outbox events работают без delivery attempts, callback routes и hardcoded external channel list. |
| IH-5b | #835 | готово как delivery attempt lifecycle | `PlanDelivery`, `RecordDeliveryResult`, `GetDeliveryStatus`, delivery attempt state machine, safe retry metadata и outbox events работают без channel adapters, callback routes и package runtime. |
| IH-6 | #843 | готово как channel contract integration | Delivery route/capability refs, safe delivery command refs, runtime/job refs и `RecordChannelCallback` работают без hardcoded каналов, внешнего callback route и запуска package runtime. |
| IH-6b | #855 | готово как callback request resolution | `RecordChannelCallback` идемпотентно связывает safe callback с delivery/request, создаёт `InteractionResponse` для terminal action и сохраняет diagnostic no-op для terminal/invalid callback без владения owner decision state. |
| IH-7 | #867 | готово как owner inbox read surface | `ListOwnerInboxItems` отдаёт pending/active feedback, approval, Human gate и callback diagnostics с safe refs/status/summary, фильтрами по scope/kind/status/assignee/actor/correlation refs и пагинацией; `staff-gateway`/`operations-hub` не входят. |
| IH-7b | #882 | готово как response boundary refs | `interaction.request.response_recorded` отдаёт safe request kind, scope, source owner, decision owner и context refs для owner resume без raw response text и без изменения чужого decision state. |
| IH-8 | #911 | готово как Human gate response surface | Response producer/read surface отдаёт safe request/response refs, source/decision owner refs, agent/provider/governance context refs, normalized outcome, digest/object refs, timestamps, correlation и idempotency digest для owner resume без raw response/callback payload; `agent-manager` consumer не входит. |
| IH-9a | #921 | готово как owner inbox UI-readiness | `GetOwnerInboxItem` даёт safe detail для будущего `staff-gateway` UI flow `list -> detail -> safe action`: request/source/decision/context refs, delivery/callback/response summaries, allowed actions, timestamps и version; ответ записывается существующим `RecordInteractionResponse`. |
| IH-9b | #928 | готово как таксономия действий владельца | `request_changes` закреплён как отдельный `InteractionResponseAction`: owner inbox list/detail и callback/response lifecycle отличают запрос доработки от `reject`, а завершённые request не возвращают `allowed_actions`. |
| SGW-1 | не назначено | готово как первый staff-gateway owner inbox contour | `services/staff/staff-gateway` отдаёт OpenAPI для списка входящих решений, карточки решения и безопасного ответа владельца; внутри вызывает `interaction-hub` gRPC и не хранит собственный decision state. |
| SGW-2 | не назначено | готово как owner inbox API hardening | `staff-gateway` усилил OpenAPI/HTTP-валидацию owner inbox, error mapping, filter/pagination/context/action tests и safe DTO без расширения доменных сценариев. |
| SGW-3 | не назначено | готово как runtime run summary | `staff-gateway` отдаёт `GET /v1/agent-runs/{run_id}/runtime-status` через `agent-manager.GetAgentRunRuntimeStatus`, возвращая safe Run/runtime job/Human gate waiting summary без чтения БД, Kubernetes, prompt body, workspace paths, provider payload, секретов и больших логов. |
| IH-11 | #894 | готово как ops deploy contour | Dockerfile, Kubernetes manifests, migration Job, services.yaml inventory, проверка готовности, runbook и monitoring docs подготовлены для первого backend deploy без новой бизнес-логики. |

## Текущий бэклог

| Срез | Статус | Почему не завершён |
|---|---|---|
| IH-9c+ | ожидает отдельные срезы | MCP, concrete channel packages, runtime worker, расширение `staff-gateway` за пределы owner inbox/runtime summary и междоменная inbox aggregation должны поставляться малыми PR. |

## Блокировки от других доменов

| Домен или сервис | Что блокирует | Решение |
|---|---|---|
| `agent-manager` | End-to-end Human gate, feedback и продолжение сессии после ответа или owner decision. | `interaction-hub` отдаёт request/response events с safe owner/source/context refs и не меняет `Run` сам. |
| `platform-mcp-server` | MCP tools `interaction.*` для slot-агентов и быстрого manager. | MCP остаётся policy/audit boundary и маршрутизирует к `interaction-hub`. |
| `codex-hook-ingress` | PermissionRequest и другие hook events, требующие вопроса человеку. | Hook ingress передаёт безопасный normalized event; `interaction-hub` создаёт request. |
| `provider-hub` | Owner decision refs для provider write pipeline. | `interaction-hub` хранит delivery/response lifecycle, а provider write и approval decision остаются у владельцев. |
| `package-hub` | Channel package capability, installation refs и manifest requirements. | Использовать чтения `package-hub`; не управлять установками пакетов. |
| `runtime-manager` и `fleet-manager` | Runtime-нагрузки channel package. | Runtime/fleet исполняют package-owned workloads; `interaction-hub` работает через channel contract. |
| `integration-gateway` | Публичный callback transport, подписи и OpenAPI. | HTTP route, signature edge и rate limit принадлежат gateway; callback lifecycle и request resolution принадлежат `interaction-hub`. |
| `operations-hub` | Dual-surface inbox, operator queue и агрегированные статусы. | `interaction-hub` отдаёт свои owner inbox items; `operations-hub` строит cross-domain read models по событиям и чтениям владельцев. |

## Рекомендуемый следующий шаг

Следующий рациональный срез — отдельная MCP, runtime или operations aggregation связка: `interaction-hub` уже отдаёт safe owner inbox list/detail и response lifecycle, `staff-gateway` прокидывает owner flow и runtime summary наружу, а cross-domain screen composition, concrete channel packages и runtime worker остаются вне домена.
