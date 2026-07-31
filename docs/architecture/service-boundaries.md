---
id: ARCH-MC-004
title: Границы сервисов и структура репозитория
type: architecture
status: approved
owner: architect
version: 1.2.1
updated: 2026-07-31
---

# Границы сервисов и структура репозитория

## Целевая структура

```text
contracts/
  proto/
  openapi/
  asyncapi/
  authorization/
  errors/
services/
  internal/
    internal-rpc-authority/
    control-plane/
    runtime-controller/
  external/
    control-api-gateway/
    interaction-gateway/
    integration-gateway/
  jobs/
    agent-runner/
    automation-scheduler/
    role-image-builder/
  staff/
    control-center/
libs/go/
config/catalog/
deploy/k8s/
infra/
tools/
docs/
```

Каждый unit реализуется целиком по `REPO-DOC-001` и `GUIDE-DOC-004`: contracts,
domain, storage, integrations, lifecycle, observability, deploy, README,
runbook и ручная проверка входят в один Issue и один PR.

## Реестр компонентов

| Компонент | Тип | Владеет | Не владеет |
| --- | --- | --- | --- |
| `internal-rpc-authority` | workload-local internal sidecar | короткоживущие authorization contexts, signing key lifecycle, JWKS manifest и verifier snapshot | пользователи, роли, проекты, permissions и transport identity caller |
| `control-plane` | internal service | проекты, чаты, роли, bindings, integrations metadata, runtime revisions, sessions, processes, schedules, memory, gates и artifact metadata | Mattermost transport, Kubernetes resources, MCP execution и AI process |
| `runtime-controller` | internal controller | reconciliation pod/PVC/Secret/ConfigMap, capacity, TTL, archive/restore и runtime health | бизнесовая конфигурация, Codex process и пользовательские сообщения |
| `control-api-gateway` | external gateway | HTTP/WebSocket transport state и owner session boundary | domain state и прямой доступ к PostgreSQL |
| `interaction-gateway` | external gateway | Mattermost transport, idempotency, cards, bot identities и file delivery | sessions, processes, schedules и Kubernetes state |
| `integration-gateway` | external gateway | MCP/API/CLI integration execution, grants, approvals и credential isolation | чужое domain state и agent orchestration |
| `agent-runner` | job/runtime process | один claimed turn, локальный process lifecycle, workspace и session materialization | authoritative session state и orchestration decisions |
| `automation-scheduler` | job | due occurrence selection, overlap/misfire policy и enqueue | AI execution, Mattermost transport и aggregate state других доменов |
| `role-image-builder` | job | build specification hash, BuildKit execution, SBOM, provenance, signature и registry artifact | runtime admission и role business state |
| `control-center` | staff PWA | UI state | business authority, secrets и прямой доступ к внутренним RPC |

Один aggregate имеет одного авторитетного владельца. Gateway, runner, cache,
search projection и UI не читают БД другого компонента и не изменяют его
состояние напрямую.

`control-plane` материализует эту границу в
`services/internal/control-plane`: транзакция PostgreSQL одновременно фиксирует
агрегат, квитанцию семантической идемпотентности, аудит и каждый обязательный
факт исходящего журнала. Redis остаётся только версионированным сквозным
кэшем. Шлюз получает подтверждение полномочий у доменного владельца, но не
передаёт идентификаторы actor/tenant/project как полномочия в прикладном
payload. Команды среды исполнения, планировщика и сканера открываются только
отдельными привязками политики с точными workload/SPIFFE, назначением
credential, audience, полным именем метода и permission. Сканер владеет
проверкой байтов, а `control-plane` — границей метаданных, состояния и
результата.

`role-image-builder` материализуется owner-triggered Job через
`tools/render-image-build-job.sh`: его вход — read-only source PVC и exact
digest `context.tar`, подготовленные владельцем workspace; Job не выбирает
tenant, рецепт или source revision. Он использует client-only mTLS BuildKit,
scoped staging push и promotion identity, а затем сохраняет digest readback.
Pull/admin credentials, runtime admission и изменение доменных агрегатов этому
Job не выдаются.

## Контракты

- внутренние синхронные вызовы: versioned Proto/gRPC;
- административный HTTP API: OpenAPI в `control-api-gateway`;
- realtime Control Center: AsyncAPI/WebSocket;
- доменные события: AsyncAPI, PostgreSQL transactional outbox,
  broker-neutral relay, NATS JetStream и durable inbox;
- Mattermost: typed adapter официального API/SDK;
- интеграции агентов: официальный MCP Go SDK и типизированные adapters.

Внутренний RPC использует mTLS/SPIFFE transport identity и authorization
context от workload-local `internal-rpc-authority`. Payload и caller-provided
identifier не являются источником полномочий.

## Работа с legacy

`services/external/bot-service`, текущий `services/jobs/agent-runner`,
`apps/control-center` и `specs/**` остаются только действующим legacy-контуром
до cutover. Они:

- не являются примером структуры нового кода;
- не получают новые продуктовые возможности;
- меняются только для критического сохранения работоспособности dogfooding;
- не требуют постоянного compatibility facade в новых unit.

После готовности новых компонентов выполняются backup, dry-run и one-shot
forward migration из #196, затем staging acceptance и переключение из #197.
Старый контур удаляется только после проверенного rollback window.
