---
id: ARCH-MC-004
title: Границы сервисов и структура репозитория
type: architecture
status: approved
owner: architect
version: 1.4.1
updated: 2026-09-05
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
    stt-tts-service/
  external/
    control-api-gateway/
    egress-gateway/
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
| `control-plane`          | internal service                | организации, Проекты, агенты, role image lifecycle, integrations metadata, runtime revisions, sessions, Run graph/events, schedules, memory, gates и artifact metadata | channel transport, Kubernetes resources, MCP execution и AI process  |
| `runtime-controller`     | internal controller             | reconciliation pod/PVC/Secret/ConfigMap, capacity, TTL и runtime health; archive/restore добавит отдельный unit #1002                     | бизнесовая конфигурация, Codex process, session archive job и пользовательские сообщения |
| `stt-tts-service`        | internal service                | stateless STT decode/validation, server-pinned OpenAI request, bounded transcript и protected availability                                | TTS, browser transport, STT policy, permission, provider account и credential lifecycle |
| `control-api-gateway`    | external gateway                | HTTP/WebSocket transport state и owner session boundary                                                                                    | domain state и прямой доступ к PostgreSQL                               |
| `egress-gateway`         | platform external gateway       | immutable FQDN/443 policy, CONNECT+ClientHello SNI validation, server-owned DNS snapshot и literal dial                                    | TLS termination, application credentials, provider lifecycle и business state |
| `interaction-gateway`    | optional external adapter       | independent inbound/notification/result-mirror/gate-decision deliveries                                                                     | core readiness, sessions, gates, artifacts и terminal Run state          |
| `integration-gateway`    | external gateway                | MCP/API/CLI integration execution, credential isolation и provider effect receipts                                                          | integration metadata/grants, Human Gates, чужое domain state и agent orchestration |
| `agent-runner`           | job/runtime process             | один claimed turn, локальный process lifecycle, workspace и session materialization                                                        | authoritative session state и orchestration decisions                   |
| `automation-scheduler`   | job                             | bounded polling защищённых scheduler RPC и transient tracking выданных leases                                                              | cron/backoff/owner state, AI execution, Mattermost и Kubernetes          |
| `role-image-builder`     | job                             | trusted materialization, BuildKit execution, provenance и staging registry artifact                                                        | canonical build specification hash, SBOM/vulnerability/signature admission, promotion и role business state |
| `image-admission`        | bounded job                     | SBOM, vulnerability-policy verdict, signature verification, admission receipt и одноразовый promotion claim exact digest                   | build execution, node pull и role business state                        |
| `control-center`         | staff PWA                       | UI state                                                                                                                                   | business authority, secrets и прямой доступ к внутренним RPC            |

Один aggregate имеет одного авторитетного владельца. Gateway, runner, cache,
search projection и UI не читают БД другого компонента и не изменяют его
состояние напрямую.

### Будущая package/application boundary

Модель `ARCH-MC-012` относится к `POST-MVP` и не добавляет package-компонент в
текущий реестр или профиль развертывания. Будущий package catalog владеет
источниками, пакетами, версиями, manifest и desired state установок, но не
исполняет workloads и не хранит значения секретов.

`control-plane` остаётся владельцем actor/scope resolution, permissions, Human
Gate, grants и общего audit/outbox. `runtime-controller` материализует только
одобренный immutable runtime plan. `integration-gateway` исполняет
типизированные внешние effects и изолирует credentials. Store adapter только
синхронизирует внешний индекс в локальный каталог. Такое разделение действует
независимо от того, будет package catalog отдельным сервисом или модулем
`control-plane`.

`egress-gateway` материализует переиспользуемую сетевую границу в
`services/external/egress-gateway` и `deploy/k8s/base/egress-gateway`. Он
принимает только exact bodyless `CONNECT` к policy FQDN на `443`, до внешнего
dial проверяет фактический ClientHello SNI и полный A/AAAA DNS snapshot, а
`net.Dialer` получает только проверенный literal `AddrPort`. TLS peer,
сертификат и application authentication остаются у consumer. Gateway не
владеет management lifecycle `integration-gateway` и не получает его secrets.
На том же Service `:8080` доступен только совместимый bodyless `GET /readyz`
без query, связанный с тем же effective readiness; произвольная HTTP
поверхность не открывается. Environment overlays принадлежат gateway, а policy
rollout выбирает отдельный content-addressed immutable ConfigMap.

`control-plane` материализует эту границу в
`services/internal/control-plane`: транзакция PostgreSQL одновременно фиксирует
агрегат, квитанцию семантической идемпотентности, аудит и каждый обязательный
факт исходящего журнала. Основной профиль использует только локальный bounded
cache; Redis не является зависимостью control-plane и может появиться позднее
только как отдельный version-bound adapter. Шлюз получает подтверждение полномочий у доменного владельца, но не
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
BuildKit с process sandbox в обязательном Pod user namespace. Base-pull и
staging-push identities, credentials и egress
принадлежат только BuildKit. Tenant, owner, recipe, generation, policy и artifact eligibility
назначает `control-plane`; installation block доступен builder только в
immutable claim snapshot и не попадает в status/log/audit/provenance.
`image-admission-controller` автоматически материализует только одну
последовательную цепочку `claim → scan → sign → admit` и отдельную
`promote`-задачу. Он читает immutable policy и состояние собственных Job/PVC
через Kubernetes API, но не получает registry, signing, secret publisher либо
control-plane credentials. Fail-closed `ValidatingAdmissionPolicy` связывает
его identity с точными образами, командами, ServiceAccount, env и volume
sources каждой фазы; одного RBAC `create jobs` недостаточно. Скрипт
`render-image-admission-job.sh` является встроенным deterministic renderer и
read-only диагностикой, а не ручным production trigger. Созданные фазы сначала
получают server-owned artifact claim, затем разделяют scanner, signer,
admission owner и promotion по разным Pod/ServiceAccount/Kubernetes Secret/mTLS границам.
Admission фиксирует exact SBOM,
vulnerability, native BuildKit provenance, signature и receipt через protected
RPC; durable evidence OCI manifest проходит readback до verdict. Только server-selected
одноразовый owner promotion claim, включающий content/manifest receipt digests,
тот же exact evidence manifest digest и совместный registry readback делают
artifact пригодным для `RuntimeRevision`. Marker/PVC задают порядок только
внутри admission scan/sign/record; promotion восстанавливается из owner state и
выделенного read-only evidence path, а PVC не является источником lifecycle
state. Pull/admin/signing/promotion credentials
builder не выдаются.

`stt-tts-service` входит в `web-only`, `web-with-mattermost` и release image set
и обслуживает короткий синхронный STT path. Actor,
organization и optional project приходят из проверенного authorization context; immutable
model/limits/provider generation принадлежат projection `control-plane`, а
краткоживущий API key — projection `secret-broker`. Аудио проверяется по
bounded size, actual container decode и длительности decoded samples до provider
effect. OpenAI доступен только через exact `egress-gateway`; сервис не хранит
аудио/transcript/credential и не публикует событие. Producer contracts
находятся в `stt.v1`, но их реализации принадлежат #1019 и #1024, а единый
server-owned continuation proof — #1023. Без proof оба adapter закрыто
отказывают до сетевого RPC; payload locators не являются authority.

Возможности STT adapter принадлежат этому же сервису и читаются отдельным
`GetModelCatalog` до первой конфигурации или credential. CP разрешает право
организации на управление системой, gateway переносит exact unary authority,
STT возвращает version/observedAt и typed model/parameter profiles. Это чтение
не вызывает policy/credential/provider и не подтверждает пользовательскую
доступность распознавания. PWA и gateway не ведут собственный каталог моделей.

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

## Fresh reset

`apps/control-center`, legacy bot-service, migration/cutover jobs, compatibility
contracts и dual-write удаляются. `services/jobs/agent-runner` не является
legacy: это защищённый runtime ABI внутри каждого promoted role image.
`role-image-builder` и image supply chain также остаются активными обязательными
unit. Mattermost выносится в optional overlay без core authority.
