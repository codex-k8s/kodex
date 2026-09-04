---
id: SVC-MC-013
title: control-api-gateway
type: service
status: approved
owner: developer
version: 2.1.0
updated: 2026-09-04
---

# control-api-gateway

Owner-facing HTTP/WebSocket boundary для production Control Center.

## Ответственность

- проверяет OIDC/browser session, exact Origin, CSRF и rate limits;
- требует semantic `Idempotency-Key` и `If-Match` для применимых mutations;
- преобразует OpenAPI requests в generated control-plane gRPC clients;
- потоково передаёт bounded multipart audio в `stt-tts-service` через
  generated `sttapi`, не буферизуя запись целиком;
- нормализует typed domain errors в стабильный HTTP/error contract;
- авторизует каждую Run subscription через тот же control-plane owner rule,
  что HTTP;
- одним owner session socket доставляет platform invalidations, authoritative
  graph snapshots и ordered resumable Run deltas.

Gateway не читает PostgreSQL, не вычисляет permissions, lifecycle, terminal
state или `nextActions`, не владеет event store и не обращается к Mattermost.
Actor/organization/project/lineage не принимаются из browser payload.

## HTTP lifecycle

- `GET /api/v1/search` передаёт server-side `projectRef`, `limit` и opaque
  `pageToken`; 500 ms debounce принадлежит только PWA;
- `PUT /api/v1/projects/{projectRef}/agents/{agentRef}/avatar` вызывает
  атомарный streaming `UploadAgentAvatar`, который фиксирует Artifact, binding
  и Agent version в одной owner transaction;
- environment agents/readiness используют только
  `/api/v1/runtime-environments/{environmentRef}/agents` и `/readiness`;
- provider device flow разделён на start, verification и reauthorization, а
  API-key account удаляется отдельным `DELETE /api/v1/provider-accounts/{ref}`;
- `POST /api/v1/projects/{projectRef}/speech/transcriptions` принимает одну
  multipart `audio` part до 25 MiB и возвращает текст с безопасным receipt без
  actor, tenant, credential и provider account metadata.

STT transport проверяет multipart/media type/declared size до открытия
защищённого RPC, отправляет chunks не более 64 KiB последовательно и наследует
request cancellation/deadline. Permission, server-owned policy, credential,
media signature и фактический размер проверяет `stt-tts-service` до provider
effect. Audio, transcript, secret и provider content не записываются в логи.
Direct STT operation зарегистрирована в отдельном `ProofOperations` профиле с
обязательным project scope; дочерние policy/credential RPC получают только
canonical continuation от проверенного parent `platform.stt.transcribe`.

## Realtime

`WSS /api/v1/session/stream` является единственным browser realtime transport.
Client передаёт platform cursor и bounded список Run cursors, а затем динамически
подписывает и отписывает Runs в том же socket. Каждая Run получает собственные
snapshot/catch-up/deltas и восстанавливает gap независимо от остальных потоков.
Duplicate игнорируется, slow client закрывается по bounded backpressure и
возобновляется с сохранённых cursors. Raw stdout, stderr, Codex JSONL, provider
payload, secret и file body запрещены.

PWA продлевает session через межвкладочный lease и передаёт только монотонную
session revision через `BroadcastChannel`. Server renew выполняется только по
explicit `PUT /api/v1/session` после exact Origin и double-submit CSRF и не
превышает expiry исходного bearer. Durable logout revocation выигрывает у
запоздавшего renew, поскольку browser session ID при renew не меняется.

## Локализация ошибок

Backend возвращает stable code/message key и безопасные parameters. Go gateway
выбирает текст из embedded RU/EN YAML по доверенной locale пользователя, а PWA
локализует собственный UI из согласованного RU/EN-каталога. Gateway не
возвращает raw downstream message или stack trace.

## Health/readiness

`/healthz` проверяет process. `/readyz` читает локальный snapshot browser/
transport configuration и прямых sidecars; control-plane, NATS producer,
runtime и optional adapters не вызываются на probe. Их рабочий outage даёт
typed `Unavailable`/HTTP `502/503/504`.

JWKS LKG ограничен двумя минутами без продления на повторной ошибке. Signature,
rollback, conflicting revision и expiry немедленно fail closed.

## HTTP lifecycle управляемых конфигураций (#1045)

Источник требований: #1018, #1019, #1022 и корректирующие #1045/#1046.
Проверенная browser session задаёт actor и organization; query/body refs
служат только локаторами. Generated controlplaneclient выбирает exact RPC
и зарегистрированный authority operation. Owner проверяет resource eligibility
до OCC/idempotency; gateway не принимает managedBy/source/actor от клиента.

| HTTP базовый путь /api/v1/ | POST /drafts | POST /{configurationRef}/revisions/{revisionRef}/validation | POST .../publication | POST .../consumer-bindings |
| --- | --- | --- | --- | --- |
| `prompt-template-configurations` | `CreatePromptTemplateDraft` | `ValidatePromptTemplateDraft` | `PublishPromptTemplateDraft` | `RebindPromptTemplateConsumers` |
| `role-image-configurations` | `CreateRoleImageRevisionDraft` | `ValidateRoleImageRevisionDraft` | `PublishRoleImageRevisionDraft` | `RebindRoleImageConsumers` |
| `integration-definition-configurations` | `CreateIntegrationDefinitionDraft` | `ValidateIntegrationDefinitionDraft` | `PublishIntegrationDefinitionDraft` | `RebindIntegrationDefinitionConsumers` |
| `system-stt-configurations` | `CreateSystemSTTConfigurationDraft` | `ValidateSystemSTTConfigurationDraft` | `PublishSystemSTTConfigurationDraft` | `RebindSystemSTTConsumers` |

В каждой строке draft создаёт серверную ревизию DRAFT; validation возвращает
VALID/INVALID с диагностикой; publish фиксирует PUBLISHED; rebind меняет
только явно выбранные bindings после проверки impact digest и версий
потребителей. Existing configuration требует If-Match, все mutations требуют
Idempotency-Key и CSRF. Configuration/ref/version назначает control-plane.
Состояние и idempotency receipt сохраняет owner transaction, gateway не
публикует события; потребитель результата здесь PWA, runtime читает точную
закреплённую ревизию своим защищённым read path.

| Дополнительный HTTP путь | RPC | Результат |
| --- | --- | --- |
| GET /managed-configurations/{ref}/revisions | ListManagedConfigurationHistory | bounded items, configuration, total и nextPageToken |
| GET /managed-configurations/{ref}/revisions/{revisionRef}/impact | GetManagedConfigurationImpact | exact consumer bindings, target revision и digest |
| POST /managed-configurations/{ref}/detachment | DetachGitManagedConfiguration | UI source ownership без выдуманной новой ревизии |
| POST /managed-configurations/{ref}/copies | CopyGitManagedConfiguration | отдельный server-owned ref и DRAFT |
| GET /system-stt-configuration | GetSystemSTTConfiguration | безопасная конфигурация и provider blockers; НЕ разрешение пользователя на STT |

Отказы owner сохраняют Problem mapping: InvalidArgument → 400,
PermissionDenied → 403, NotFound → 404, Aborted/OCC → 412,
FailedPrecondition → 409, Unavailable → 503. Неизвестный/повреждённый
producer response закрыто отклоняется. Source content и name не проходят
общую строковую enum/i18n нормализацию, иначе редактор потеряет исходный текст.

Доказательство mapping: TestManagedConfigurationRoutesCallExactTypedRPC
проверяет все перечисленные маршруты, точные RPC, idempotency/OCC и pagination.
Дополнительные тесты проверяют сохранение source, отсутствие caller authority,
невалидные revisions, owner denial и duplicate consumers.

Глобальные каталоги, environment/secret revision lifecycle и per-user STT
eligibility зависят от завершения #1046. Этот раздел не заявляет готовность
всего #1045 до подключения этих producer contracts.

## Общие каталоги и виртуальные файлы (#1045)

| HTTP GET /api/v1/ | RPC | Результат для PWA |
| --- | --- | --- |
| agents | ListAgents | ИИ-сотрудники доступных проектов |
| workflows | ListWorkflows | Процессы доступных проектов |
| schedules | ListSchedules | Автоматизации доступных проектов |
| runtime-environments | ListRuntimeEnvironmentSets | Безопасные описания окружений |
| runtime-secrets | ListRuntimeSecrets | Метаданные без значений секретов |
| vfs/nodes | ListVFSNodes | Типизированные элементы виртуальной папки |
| vfs/search | SearchVFS | Серверный поиск разрешённых виртуальных ресурсов |

Источник actor/organization тот же проверенный browser context. Optional
projectRef лишь сужает owner query; отсутствие фильтра не даёт доступа к чужим
проектам. Одна HTTP-операция вызывает один RPC, без обхода проектов через fanout.
Control-plane разрешает eligibility, lifecycle и scan state; путь VFS не
используется как authority или ключ физического хранилища. Чтения не создают
события или mutable state, ответ и cursor являются авторитетным read path.

Query, pageSize и pageToken ограничены до RPC, включая защиту от переполнения
при преобразовании int в int32. Names/paths VFS сохраняются дословно, enum
переводится только из закрытого реестра kind. Пустые списки представлены [];
secret metadata всегда no-store. Unknown kind и повреждённый producer response
отклоняются с 502. Глобальная выборка и bounded SQL pagination владельца
дорабатываются в #1046; HTTP mapping не является доказательством этой части.

Проверки: TestOrganizationCatalogsForwardFiltersAndCursorWithoutProjectFanout,
TestOrganizationCatalogRejectsInvalidBoundsBeforeRPC,
TestOrganizationCatalogPropagatesAuthoritativeDenial,
TestVFSRoutesPreserveTypedSourceAndPagination,
TestVFSRejectsMalformedPathAndUnknownProducerKind.

## Контракты и проверка

- OpenAPI: `contracts/openapi/control-api-gateway/v1/openapi.yaml`;
- WebSocket: `contracts/asyncapi/control-api-gateway/v1/asyncapi.yaml`;
- deploy: `deploy/k8s/base/control-api-gateway`.

Проверенная документация внешних библиотек: gRPC-Go client streaming/flow
control/context cancellation и oidc-client-ts session/events. Через Context7
`/oapi-codegen/oapi-codegen` проверена stdhttp generation: generated handler
не заменяет валидацию ограничений query/request на границе.

```bash
cd services/external/control-api-gateway
GOWORK=off go test ./...
```
