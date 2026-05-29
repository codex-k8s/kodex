---
doc_id: MAP-CK8S-DOMAIN-CONSOLE-AND-OPERATIONS-UX
type: issue-map
title: kodex — карта Issue домена консоли и операционных интерфейсов
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-05-28
---

# Карта Issue — консоль и операционные интерфейсы

## TL;DR

Долгоживущая карта домена `console-and-operations-ux`.

## Матрица

| Issue/PR | Документы | Волна | Статус | Примечание |
|---|---|---|---|---|
| не назначено | `services/staff/web-console/**`<br>`specs/openapi/staff-gateway.v1.yaml`<br>`docs/domains/console-and-operations-ux/README.md`<br>`docs/platform/architecture/service_boundaries.md`<br>`docs/platform/architecture/c4_container.md`<br>`docs/design-guidelines/common/external_dependencies_catalog.md` | WCON-1 | web-console-mvp-shell | Первый `web-console` MVP создан поверх `staff-gateway`: каркас приложения, командный центр, owner inbox list/detail/respond, runtime summary и activity timeline одного `AgentRun` через сгенерированный TypeScript-клиент. Агрегат командного центра, список `Run`, чат и команды создания/запуска остаются отключёнными до появления OpenAPI-ручек. |
| не назначено | `specs/openapi/staff-gateway.v1.yaml`<br>`services/staff/staff-gateway/**`<br>`docs/platform/architecture/service_boundaries.md`<br>`docs/platform/architecture/c4_container.md`<br>`docs/domains/console-and-operations-ux/README.md` | SGW-4 | agent-run-activity-timeline | `staff-gateway` отдаёт `GET /v1/agent-runs/{run_id}/activities`: тонкий HTTP -> gRPC adapter к `agent-manager.ListAgentActivities` с typed фильтрами `activity_kind`/`status`, cursor pagination и safe DTO без raw tool input/output, stdout/stderr, prompt body, transcript, provider payload, workspace paths, секретов и больших логов. |
| не назначено | `specs/openapi/staff-gateway.v1.yaml`<br>`services/staff/staff-gateway/**`<br>`docs/platform/architecture/service_boundaries.md`<br>`docs/platform/architecture/c4_container.md`<br>`docs/domains/console-and-operations-ux/README.md` | SGW-3 | runtime-run-summary | `staff-gateway` отдаёт `GET /v1/agent-runs/{run_id}/runtime-status`: тонкий HTTP -> gRPC adapter к `agent-manager.GetAgentRunRuntimeStatus` с safe DTO для Run/runtime job/Human gate waiting, без чтения БД, Kubernetes, workspace paths, prompt body, provider payload, секретов и больших логов. |
| не назначено | `specs/openapi/staff-gateway.v1.yaml`<br>`services/staff/staff-gateway/**`<br>`docs/platform/architecture/service_boundaries.md`<br>`docs/domains/console-and-operations-ux/README.md` | SGW-2 | готовность-owner-inbox-api | Owner inbox API усилен для использования из `web-console`: уточнена OpenAPI-валидация path/body/status responses, проверяются парные assignee refs, idempotency/expected version остаются обязательными для ответа, покрыты filter/pagination/context/action/error edge cases без собственной бизнес-логики gateway. |
| не назначено | `specs/openapi/staff-gateway.v1.yaml`<br>`services/staff/staff-gateway/**`<br>`docs/platform/architecture/service_boundaries.md`<br>`docs/platform/architecture/c4_container.md`<br>`docs/domains/interaction-hub/architecture/api_contract.md`<br>`docs/domains/interaction-hub/architecture/design.md`<br>`docs/domains/console-and-operations-ux/README.md` | SGW-1 | owner-inbox-gateway | Первый `staff-gateway` для консоли сотрудников: OpenAPI list/detail/respond по входящим решениям владельца, gRPC-вызовы `interaction-hub`, safe DTO без собственной бизнес-истины. |
| не назначено | `docs/domains/console-and-operations-ux/` | wave 14 | planned | Операторская консоль и рабочие пространства. |
