---
id: ARCH-MC-004
title: Границы сервисов и структура репозитория
type: architecture
status: approved
owner: architect
version: 1.2.4
updated: 2026-08-05
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

| Компонент                | Тип                             | Владеет                                                                                                                                    | Не владеет                                                              |
| ------------------------ | ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------- |
| `internal-rpc-authority` | workload-local internal sidecar | короткоживущие authorization contexts, signing key lifecycle, JWKS manifest и verifier snapshot                                            | пользователи, роли, проекты, permissions и transport identity caller    |
| `control-plane`          | internal service                | проекты, чаты, роли, bindings, integrations metadata, runtime revisions, sessions, processes, schedules, memory, gates и artifact metadata | Mattermost transport, Kubernetes resources, MCP execution и AI process  |
| `runtime-controller`     | internal controller             | reconciliation pod/PVC/Secret/ConfigMap, capacity, TTL, archive/restore и runtime health                                                   | бизнесовая конфигурация, Codex process и пользовательские сообщения     |
| `control-api-gateway`    | external gateway                | HTTP/WebSocket transport state и owner session boundary                                                                                    | domain state и прямой доступ к PostgreSQL                               |
| `interaction-gateway`    | external gateway                | Mattermost transport, idempotency, cards, bot identities и file delivery                                                                   | sessions, processes, schedules и Kubernetes state                       |
| `integration-gateway`    | external gateway                | MCP/API/CLI integration execution, grants, approvals и credential isolation                                                                | чужое domain state и agent orchestration                                |
| `agent-runner`           | job/runtime process             | один claimed turn, локальный process lifecycle, workspace и session materialization                                                        | authoritative session state и orchestration decisions                   |
| `automation-scheduler`   | job                             | bounded polling защищённых scheduler RPC и transient tracking выданных leases                                                              | cron/backoff/owner state, AI execution, Mattermost и Kubernetes          |
| `role-image-builder`     | job                             | trusted materialization, BuildKit execution, provenance и staging registry artifact                                                        | canonical build specification hash, SBOM/vulnerability/signature admission, promotion и role business state |
| `image-admission`        | bounded job                     | SBOM, vulnerability-policy verdict, signature verification, admission receipt и одноразовый promotion claim exact digest                   | build execution, node pull и role business state                        |
| `control-center`         | staff PWA                       | UI state                                                                                                                                   | business authority, secrets и прямой доступ к внутренним RPC            |

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

`role-image-builder` материализуется как отдельный deployable
`services/jobs/role-image-builder`. Он получает server-owned fenced attempt
через protected RPC, потоково читает exact OCI context/package/tool в private
`emptyDir`, использует pull-only input identity и client-only mTLS к вынесенному
rootless BuildKit. Base-pull и staging-push identities, credentials и egress
принадлежат только BuildKit. Tenant, owner, recipe, generation, policy и artifact eligibility
назначает `control-plane`; installation block доступен builder только в
immutable claim snapshot и не попадает в status/log/audit/provenance.
Отдельный `render-image-admission-job.sh` сначала получает server-owned artifact
claim, затем разделяет scanner, signer, admission owner и promotion по разным
Pod/ServiceAccount/Vault/mTLS границам. Admission фиксирует exact SBOM,
vulnerability, native BuildKit provenance, signature и receipt через protected
RPC; durable evidence OCI manifest проходит readback до verdict. Только server-selected
одноразовый owner promotion claim, включающий content/manifest receipt digests,
тот же exact evidence manifest digest и совместный registry readback делают
artifact пригодным для `RuntimeRevision`. Marker/PVC задают порядок только
внутри admission scan/sign/record; promotion восстанавливается из owner state и
выделенного read-only evidence path, а PVC не является источником lifecycle
state. Pull/admin/signing/promotion credentials
builder не выдаются.

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
