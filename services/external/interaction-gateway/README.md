---
id: SERVICE-DOC-INTERACTION-GATEWAY
title: Interaction gateway
type: service
status: approved
owner: manager
version: 1.8.0
updated: 2026-08-09
---

# Interaction gateway

`interaction-gateway` — внешний deployable unit MatterCodex для Mattermost.
Он владеет только transport state, inbound dedup/cursor, подготовкой объектов и
durable delivery. Владельцем Project, Chat, Session, Turn, Artifact, Process и
OwnerGate остаётся `control-plane`.

Legacy `services/external/bot-service` не используется и не имеет dual write.
Доступ к PostgreSQL других сервисов отсутствует. Синхронные доменные операции
идут через generated `controlplaneclient`; authority формируется из
проверенного Mattermost readback и environment-owned versioned Vault mapping,
а не из payload ID.

## Поверхности

- `POST /mattermost/v1/commands/codex` — slash command с mTLS и exact token;
- `POST /mattermost/v1/actions` — кнопки карточек и запуск dialog;
- `POST /mattermost/v1/dialogs` — решение с bounded reason;
- WebSocket `posted`, `post_edited`, `reaction_added`;
- `GET /internal/v1/deliveries/{deliveryId}` — полный безопасный readback
  payload и provider receipt для разрешённого SPIFFE peer с обязательным
  delivery-scoped bearer credential; после JWS gateway online сверяет у
  control-plane exact durable issue row, credential digest и отсутствие
  revocation тем же generated client profile;
- `GET /internal/v1/runtime-materializations/{executionId}/{artifactId}` —
  exact agent-runner mTLS proxy: bearer передаётся owner RPC control-plane,
  затем gateway читает только возвращённый project-scoped private object и
  повторно сверяет version, size и SHA-256;
- `GET /mattermost/v1/artifacts/{grantId}/content` — одноразовая выдача
  private S3 object после аутентификации Mattermost User и повторного readback
  actor/Team/Channel membership, а также current `BOUND` mapping у
  control-plane;
- `/livez`, `/readyz`, `/metrics` на отдельном техническом listener.

## Contract-first матрица Workspace↔Mattermost Team

Реализованный interaction-owned source contract находится в
`contracts/proto/interactiongateway/v1/interaction_gateway.proto` и состоит
из специализированных RPC `ListMattermostTeams`, `CreateMattermostTeam`,
`LinkMattermostTeam`, `GetMattermostTeamBinding`, `RelinkMattermostTeam`,
`UnlinkMattermostTeam`, `GetMattermostTeamMappingOperation`,
`GetMattermostTeamProviderReadback` и `CheckReadiness`.
Универсальный provider proxy, передача raw Mattermost Team ID и изменение
mapping через существующий inbound OpenAPI запрещены. Owner-state вызывается
только через generated `controlplane.v1` client; локального mock, placeholder
consumer, копии owner contract и dual write нет.

Actor, organization и project отсутствуют в request DTO. Их передаёт только
проверенный application authorization context от `internal-rpc-authority`:
точные issuer/audience, caller workload и SPIFFE ID, полный gRPC method,
permission, JTI/replay reservation и server-resolved actor/tenant/project.
Обычный Mattermost bearer, mTLS, opaque Team ref, workspace locator и любое
поле payload по отдельности полномочий не дают.

| Сценарий | Проектируемый owner endpoint #237 | Internal RPC и permission | Mattermost effect/readback | Control-plane #234, OCC и receipt | Durable результат, event/read path и readiness |
| --- | --- | --- | --- | --- | --- |
| Catalog | `GET /workspaces/{workspace}/mattermost/teams` | `ListMattermostTeams`, `interaction.team.catalog.read`; actor/org/project только из verified context | server-resolved Mattermost User; `GetTeamsForUser`, затем для каждой выдаваемой Team `GetTeam` и `GetTeamMember`; page не больше 100 | signed `ListWorkspaceMattermostMappings` подтверждает exact project owner boundary; пустой список оставляет доступным первый bind; mutation отсутствует | bounded safe page: display name, slug, masked status, timestamps и opaque selector; cursor и selector принадлежат серверу, сырые Team ID отсутствуют; authoritative read path — fresh provider readback |
| Create | `POST /workspaces/{workspace}/mattermost-team` | `CreateMattermostTeam`, `interaction.team.create`; request содержит только display name, slug intent и idempotency key | до `CreateTeam` атомарно фиксируются project fence, случайная correlation и operation-bound provider slug; recovery принимает только exact correlation marker+slug и лишь затем добавляет owner membership | после exact provider proof вызывается `ManageWorkspaceMattermostMapping(bind)` с server-resolved Workspace/Project и отсутствующим predecessor | `PENDING -> PROVIDER_ACCEPTED -> BOUND|REPAIR_REQUIRED`; provider checkpoint и mapping operation сохраняются отдельно; domain event отсутствует, итог читается этим RPC, operation readback и `GetMattermostTeamBinding` |
| Link existing | `PUT /workspaces/{workspace}/mattermost-team` | `LinkMattermostTeam`, `interaction.team.link`; caller передаёт только opaque selector и idempotency key | selector разрешается из durable actor/org/project scope; gateway заново читает `GetTeam` и `GetTeamMember` | `ManageWorkspaceMattermostMapping(bind)` проверяет отсутствие mapping и server-resolved owner; gateway не копирует owner rules | `PENDING -> BOUND|REPAIR_REQUIRED`; signed receipt содержит внутренний provider ref/digests, но raw provider payload наружу не выдаётся; audit/history и authoritative get принадлежат control-plane |
| Get/readback | `GET /workspaces/{workspace}/mattermost-team` | `GetMattermostTeamBinding`, `interaction.team.binding.get` | Team ID берётся только из current mapping control-plane, затем `GetTeam` и actor `GetTeamMember`; deleted/forbidden/lost membership fail closed | signed typed `ListWorkspaceMattermostMappings` + `GetWorkspaceMattermostMapping`; hidden/foreign Workspace неотличим от not found | joined snapshot связывает mapping generation/version с fresh masked provider status; отдельного события нет; этот RPC является защищённым authoritative read path |
| Relink | `POST /workspaces/{workspace}/mattermost-team/relink` | `RelinkMattermostTeam`, `interaction.team.relink`; opaque selector, expected version/generation, idempotency key | fresh exact Team и membership readback перед каждым новым one-use receipt и каждой owner attempt | `ManageWorkspaceMattermostMapping(relink)` проверяет exact OCC и блокирует полный Chat/Session/Turn/delivery graph | gateway атомарно заменяет durable joined runtime route и high-watermark; stale generation не обслуживается inbound/delivery/artifact/catch-up; control-plane атомарно пишет mapping, protected history и audit |
| Unlink | `DELETE /workspaces/{workspace}/mattermost-team` | `UnlinkMattermostTeam`, `interaction.team.unlink`; expected version/generation и idempotency key | provider mutation отсутствует; перед transition gateway подтверждает current Team status и membership | `ManageWorkspaceMattermostMapping(unlink)` проверяет exact OCC и полный graph; открытый graph либо stale OCC отклоняются | provider checkpoint и безопасный authoritative result сохраняются; domain event отсутствует, `GetWorkspaceMattermostMapping` возвращает `UNLINKED`; management readiness остаётся доступным для readback |
| Operation readback | проектируемый owner endpoint #237 | `GetMattermostTeamMappingOperation`, `interaction.team.mapping-operation.get`; action+semantic key, actor/org/project только из verified context | provider вызов отсутствует | owner-scoped local operation не меняет control-plane state | безопасно различает `PENDING|AMBIGUOUS|BOUND|UNLINKED|REPAIR_REQUIRED`, возвращает только safe failure code/timestamps и mapping result без raw Team ID, provider payload или raw error |

Принятый owner contract вызывается точными операциями
`control.interaction.workspace-mapping.manage|get|list` и методами
`ManageWorkspaceMattermostMapping`, `GetWorkspaceMattermostMapping`,
`ListWorkspaceMattermostMappings`. Manage использует permission
`controlplane.workspace_mapping.manage`, get/list — `controlplane.resource.read`;
producer `control-plane.interaction-provider-readback` связывает exact workload
`interaction-gateway`, SPIFFE ID, `PROVIDER_READBACK`, signed one-use receipt и
server-resolved Workspace/Project. `REPAIR_REQUIRED` означает, что owner outcome
нельзя безопасно доказать либо predecessor изменился, и запрещает новый effect.

Create фиксирует normalized semantic intent, project-scoped single-winner
fence, случайную provider correlation, operation-bound provider slug и
DB-time recovery deadline в PostgreSQL до единственного `CreateTeam`. Между
durable `EFFECT_PENDING` и provider POST есть явная crash-граница: после
timeout или restart worker выполняет только `GetTeamByName` и сверяет exact
operation slug, correlation marker, display/type и provider causality digest.
Membership создаётся лишь после этого доказательства; чужая/raced Team не
изменяется и не принимается. Слепой второй POST запрещён. Transient readback
остаётся `AMBIGUOUS` до PostgreSQL-authoritative deadline. Подтверждённый
provider snapshot атомарно создаёт opaque selector, receipt digest и
монотонный project-scoped provider generation. Повтор того же idempotency key
и semantic digest возвращает сохранённую operation; другой digest конфликтует
до обращения к Mattermost.

### State/effect matrix

| Operation/outcome | Provider effect | Durable transition и recovery | Owner transition | Безопасный результат |
| --- | --- | --- | --- | --- |
| catalog success | только bounded read | opaque selectors/cursor имеют TTL, actor/org/project scope и provider digest | отсутствует | safe page без raw Team ID и private payload |
| catalog forbidden/deleted/unknown | только read | selector не создаётся; bounded error code | отсутствует | hidden/not found либо permission denied без provider diagnostics |
| create accepted | одна `CreateTeam` после durable `PENDING` и project fence | operation-bound slug/correlation/causality фиксируют `PROVIDER_ACCEPTED`; raw response не хранится | bind #234, затем `BOUND` | masked Team snapshot, mapping version/generation |
| create conflict before effect | `CreateTeam` не принят | exact pre-existing `GetTeamByName` не присваивается операции; `REPAIR_REQUIRED` либо safe conflict | отсутствует | conflict без чужой Team identity |
| create timeout/connection loss | effect неизвестен | слепой второй POST запрещён; worker сверяет operation-bound provider identity; transient readback остаётся `AMBIGUOUS`, а `RECOVERY_TIMEOUT` назначает только DB clock после durable deadline | только после доказанного readback | повтор того же key/hash возвращает сохранённое состояние; другой hash конфликтует |
| link/relink accepted | provider mutation отсутствует, только readback | `PENDING -> BOUND`; перед каждым новым JTI gateway заново читает exact Team+owner membership | bind/relink #234 с OCC и full graph gate | current version/generation, provider effect generation и атомарно заменённый runtime route |
| lost membership/provider forbidden/deleted | только readback | operation terminal `REPAIR_REQUIRED`; receipt не подменяется synthetic provider state | owner transition не вызывается | hidden/not found или safe precondition error |
| unlink accepted | provider mutation отсутствует | current provider proof/checkpoint фиксируется до owner command | unlink #234 закрывает generation после graph gate | `UNLINKED` и новая mapping version/generation |
| stale expected version/open graph | provider mutation отсутствует | provider proof может быть сохранён как завершённый read, но не как mapping success | #234 возвращает conflict/failed precondition | current safe readback без raw graph/provider data |
| worker restart/lease expiry | новый внешний effect до startup barrier запрещён | DB-time lease/fence, bounded retry; cancel/join до закрытия PostgreSQL и Mattermost transport | повтор owner command только с тем же semantic idempotency key | сохранённый receipt либо `REPAIR_REQUIRED` без дубля |

`interaction-gateway` и `control-plane` не публикуют domain event для этого
lifecycle: provider receipt является transport evidence, а авторитетный
mapping, protected history и audit фиксируются одной PostgreSQL-транзакцией
`control-plane`. Отсутствие event компенсируют защищённые
`GetMattermostTeamBinding`, `GetMattermostTeamMappingOperation` и exact provider
readback. Environment-owned manifest задаёт только route-policy templates и не
содержит current Team authority. `BOUND` атомарно материализует в PostgreSQL
единственную joined projection exact owner mapping generation и fresh
Mattermost Team/channel; отдельный durable high-watermark переживает `UNLINKED`
и не допускает rollback старой generation. Отсутствие, отзыв или mismatch
закрыто блокируют inbound, direct/owner delivery, artifact и catch-up. Та же
joined проверка выполняется после reclaim queued inbound, прямо перед publish
и в readiness, поэтому relink/unlink не оставляет достижимого старого provider
path и не даёт false-green.

Перед созданием Session/Turn gateway перечитывает Post, Channel, Team и User
через Mattermost REST. `Team=Project`, `Channel=Chat`, actor, role, locale и bot
выбираются из environment-owned Vault manifest. Селектором Agent/Bot считается
только `@mention`, чья username и UserID точно входят в server-owned assignment
и подтверждены Mattermost User readback. Обычные human mentions сохраняются в
prompt и не меняют default Agent; несколько Agent selectors, unknown либо
несовпавший assignment закрыто отклоняются.
Bot messages и `#notrigger` завершаются до доменной команды. Базовый render не
содержит sample IDs: staging/production получают собственные versioned manifest
и bot-token Secret из Vault; отсутствие или несовпадение server-owned readback
блокирует startup.
Новая Session получает стабильный idempotency key от exact root Post/Thread
(для slash — от provider event), поэтому edit/reconnect не создаёт вторую Session.
Для каждой пары project/actor разрешён ровно один channel с
`owner_delivery=true`; неоднозначная delivery route закрыто отклоняется.

## Contract-first матрица Agent↔Mattermost Bot identity

Authoritative source contract состоит из закрытых RPC
`ListAgentMattermostBotIdentities`,
`CreateAndBindAgentMattermostBotIdentity`,
`BindAgentMattermostBotIdentity`, `GetAgentMattermostBotIdentity`,
`RebindAgentMattermostBotIdentity`, `RevokeAgentMattermostBotIdentity`,
`GetAgentMattermostBotIdentityOperation`,
`GetAgentMattermostBotIdentityProviderReadback` и
`CheckAgentMattermostBotIdentityReadiness`. Универсальный provider proxy,
передача raw Mattermost Bot/User/Team ID, возврат token, cookie, provider
payload либо raw error и изменение произвольного Agent lifecycle запрещены.

Verified application context является единственным источником actor,
organization, project, caller workload, полного метода и permission. `agent_ref`
и `identity_selector` — только локаторы: gateway разрешает Agent через
авторитетный control-plane read, а selector — через actor/tenant/project-scoped
PostgreSQL row. Username нормализуется сервером по правилам Mattermost и
operation-bound суффиксу; provider UserID, BotID, TeamID и credential binding
назначаются сервером. Typed owner command `ManageAgentMattermostBotIdentity`
вычисляет тот же canonical intent, что producer receipt; request-поле не может
подменить authority, target, action, predecessor или generation.

| Сценарий | Предшественник и authority | Provider effect/readback | Durable checkpoint, winner и допустимый retry | Owner command и terminal/read path |
| --- | --- | --- | --- | --- |
| Catalog success/empty | verified actor/org/project; server-resolved eligible Agents и Team mapping | bounded `GET /bots`, затем exact bot/user/team readback; пустой список допустим | server-owned selectors с TTL и provider digest; mutation/winner отсутствуют | owner command отсутствует; bounded safe catalog либо empty page |
| Catalog forbidden/deleted/unknown | та же authority, payload IDs не используются | только read; raw status/body не сохраняются | selector не создаётся; retry только как новый read | safe `PERMISSION_DENIED` либо hidden `NOT_FOUND` |
| Create accepted | Agent существует, принадлежит project, не имеет current identity; exact intent и idempotency key | ровно один `POST /bots`, затем создание credential и membership только после exact bot readback | `PENDING_EFFECT` с normalized username/correlation/intent hash фиксируется до POST; один PostgreSQL winner; после accept — `PROVIDER_ACCEPTED`; blind retry запрещён | signed one-use receipt → `ManageAgentMattermostBotIdentity(BIND)`; terminal `BOUND`; защищённые get/operation/provider-readback |
| Create conflict/exact pre-existing match | тот же predecessor; чужой username не доказывает ownership | preflight/readback сравнивает exact server marker, username, owner и causality | exact operation match восстанавливается; foreign/raced bot → `REPAIR_REQUIRED`; второй POST запрещён | owner вызывается только для доказанного exact match |
| Create timeout/5xx/connection loss | durable `PENDING_EFFECT` уже существует | effect считается ambiguous; worker выполняет только exact readback по operation username/correlation | `AMBIGUOUS`; DB-time deadline/lease/fence; тот же key+hash читает operation, другой hash конфликтует | после доказательства продолжается прежний BIND; по deadline terminal `RECOVERY_TIMEOUT`; blind retry отсутствует |
| Membership accepted/already present | exact bot доказан и Team разрешён current mapping | одна assign/membership mutation с последующим exact membership readback; already-present принимается только при exact bot/team | checkpoint `MEMBERSHIP_PENDING` до effect, затем `PROVIDER_ACCEPTED`; ambiguous восстанавливается readback, не повтором | BIND только после exact membership proof |
| Membership forbidden/ambiguous | тот же operation winner | raw provider diagnostics скрыты; только exact bot/team membership readback | `AMBIGUOUS` до deadline либо `REPAIR_REQUIRED`; retry только readback | owner command отсутствует до доказательства |
| Bind existing accepted | Agent без current identity; opaque selector scoped actor/org/project; fresh bot/user/team readback | provider mutation отсутствует | один winner по Agent+exact predecessor для всех конкурирующих action и actor; fresh receipt/JTI только после нового readback | `BIND`; terminal `BOUND` с owner version и provider generation |
| Rebind accepted | current owner version/generation и identity predecessor совпадают; свежий `Bot.OwnerId` равен server-resolved actor Mattermost User | fresh target readback; прежний exact token отзывается и новый token создаётся только между owner pre/post-readback fences | старый local admission atomically закрывается до первого credential effect; raced/foreign owner даёт ambiguous/repair без выдачи token | `REBIND` с OCC; успех выдаётся только после fresh provider owner и owner readback новой generation |
| Rebind stale/conflict/open graph | stale expected version/generation либо owner gate запрещает переход | provider effect отсутствует | operation terminal `CONFLICT`/`FAILED_PRECONDITION`; закрытая старая admission не воскресает | typed owner error и authoritative get; новый effect отсутствует |
| Revoke accepted | exact current owner version/generation | disable/revoke effect checkpoint фиксируется до provider call; затем include-deleted/disabled readback | `REVOKE_PENDING -> PROVIDER_ACCEPTED`; ambiguous effect восстанавливается readback; generation закрыта до provider call | signed receipt → `REVOKE`; terminal `REVOKED`; owner get/operation/provider-readback |
| Bot disabled/deleted до команды | identity разрешена из current owner state | include-deleted readback доказывает disabled/deleted | current generation немедленно inadmissible; repair/revoke operation без synthetic success | readiness false; revoke допускается только с exact evidence и owner OCC |
| Same key/same intent | существующая operation в том же actor/org/project/Agent/action scope | новый provider effect запрещён | возвращается durable result/checkpoint; recovery продолжает ту же lease generation | тот же owner outcome/receipt либо безопасный pending |
| Same key/different intent | semantic digest, target, action, predecessor либо generation отличаются | provider не вызывается | durable idempotency conflict | typed `CONFLICT`; состояние не меняется |
| Crash до provider effect | winner и `PENDING_EFFECT` сохранены | effect неизвестен, поэтому POST не повторяется | recovery начинает exact readback; DB lease/fence блокирует второго worker | pending/ambiguous read path |
| Crash после provider accept | `PENDING_EFFECT` либо `MEMBERSHIP_PENDING` | только exact bot/membership readback | подтверждение сохраняется как `PROVIDER_ACCEPTED` | прежний owner command с тем же semantic key |
| Crash после readback/receipt | provider checkpoint и receipt digest сохранены | новый provider вызов запрещён | новая receipt выдаётся только после fresh readback и новым one-use JTI; старый JTI не переиспользуется | тот же typed owner intent; replay receipt path version-pinned |
| Crash после owner accept/до ответа | owner receipt мог быть принят | provider effect и новый owner transition запрещены | worker выполняет owner authoritative get и связывает exact receipt/intent/version/generation | durable `BOUND`/`REVOKED` возвращается повтору |
| Worker lease expiry | DB-time lease/fence истёк | effect запрещён до startup barrier; новый worker делает readback | monotonic lease generation; stale worker не записывает checkpoint/outcome | продолжение той же operation, не новая команда |
| Receipt replay | existing operation и exact provider checkpoint | fresh exact readback обязателен | новый ES256 JWS: exact `aud`, `purpose`, JTI, target/action/effect, intent hash, authority tuple, provider version/generation и digests | consumer recomputes canonical intent from typed command; replay mismatch/used JTI fail closed |
| Readiness | current signed control-plane owner identity, provider generation, credential binding и runtime route должны совпасть | exact Bot owner/Team membership/active token и authenticated `GetMe`; тот же proof выполняется runtime admission | read-only; signer/trust, Vault, owner, token или generation mismatch не ремонтируется автоматически | bounded per-component reason; per-identity и общий `/readyz` закрыты для обязательных current routes |
| Stale inbound/delivery/artifact | route identity generation меньше current high-watermark либо revoked | provider effect запрещён | joined admission row и high-watermark проверяются после reclaim и прямо перед effect | closed typed failure; stale identity не создаёт Session/Turn, delivery или grant |

Процесс и management gRPC запускаются после dependency barrier даже до первого
назначения Agent identity, но `/readyz` остаётся закрытым. Отдельный внутренний
Service `interaction-gateway-management` публикует только mTLS gRPC port для
этого bootstrap/readback path и не выводит наружу runtime HTTP. Это позволяет
материализовать исходную identity без static credential fallback; runtime
workers на каждом пути всё равно требуют admitted current generation. После
назначения exact joined route readiness открывается автоматически.

`ProviderEffectReadbackReceipt` — единственное переносимое доказательство effect:
gateway подписывает versioned canonical claims и сохраняет terminal JTI/digest
операции, а control-plane в owner-транзакции выполняет one-use consume и хранит
replay/revocation state. Для Agent bot receipt переносит только opaque proof
exact mapping version/generation и gateway-owned bot ref: raw Team ID, credential binding ID, Vault
version и token digest остаются исключительно в durable state gateway. Ни одна
сторона не сохраняет raw credential или private provider payload во внешнем readback.
Receipt связывает exact workload, audience, full owner method,
actor/org/project, Agent stable key, action, predecessor version/generation,
provider effect version/generation и semantic intent hash. После возможного
provider effect разрешены только readback/recovery; любое повторение mutation
требует новой operation и нового доказанного predecessor. Отдельный domain event
не публикуется: защищённые operation, owner binding и provider readback являются
version-pinned authoritative read/rejoin paths.

## Сквозная карта сценариев

| Сценарий | Actor и authority | Boundary → command | Владелец и idempotency | Результат и состояние |
|---|---|---|---|---|
| Post/slash | Mattermost User после REST readback + mTLS connector; signed event JWS связывает actor/org/project/event digest | HTTP/WS → `ManageSession(CREATE)` → `RegisterArtifact` → `EnqueueTurn` | control-plane назначает owner; ключи UUIDv5 от immutable provider event | inbound `PROCESSING → WAITING_SCAN → COMPLETED`; run/status/artifact/incident delivery |
| Upload | Channel membership следует из прочитанного Post/FileInfo и mapping | Mattermost download → S3 content-addressed put/readback → `RegisterArtifact` | gateway хранит size/type/SHA-256/provenance; control-plane владеет Artifact и scan state | Turn запрещён до `CLEAN`; `QUARANTINED/FAILED` terminal |
| Owner result/progress | runtime owner пишет result handoff с Session/Turn/attempt/input/runtime revision/artifact ID+version+SHA; control-plane в той же terminal/status/incident transaction создаёт `interaction_delivery_work` | agent-runner → runtime-controller generated client → `CompleteRuntimeExecution`; либо owner command `ClaimTurn`/`RecordRuntimeIncident` → durable claim gateway | deterministic delivery ID, owner DB time, lease token/fence; gateway не копирует business state | `PROGRESS/STATUS/INCIDENT/RUN_CARD/FINAL_MARKDOWN/PUBLISH_ARTIFACT`; provider receipt возвращается `RecordInteractionDelivery` и читается защищённым readback |
| Card delivery | Gateway-owned transport snapshot с tenant/project/session/turn/input lineage | owner delivery claim → deterministic Mattermost create/update Post → provider receipt → control-plane acknowledgement | PostgreSQL lease/token/fence допускает reclaim только после expiry; новый owner fence атомарно rebind-ит ту же локальную delivery | `PENDING → DELIVERING → PROVIDER_ACCEPTED → DELIVERED`; неоднозначный create без exact `PendingPostId` readback становится terminal repair outcome и не повторяет effect |
| Owner decision | Только mapped recipient; post/channel/team и HMAC callback повторно сверяются | claim gate → безопасная публикация доступного S3 result → owner card → `RecordOwnerGateDelivery` → action/dialog/reaction → `ResolveOwnerGate` | server-owned delivery ID/payload digest, control-plane claim token/fence/version + event JWS | control-plane атомарно переводит Gate/Process/Turn; gateway inbound становится `COMPLETED` |
| Channel/Thread delete/restore | lifecycle actor берётся только из server-owned manifest, Channel/Post перечитываются через Mattermost | delete → exact Chat/Session transition → `WAITING_CLEANUP`; restore → cancel cleanup + `ACTIVE`; expiry → `FINALIZE` | provider event digest + interaction lifecycle operation profile + owner version | в `DELETION_PENDING` новые инструкции и callbacks запрещены; cleanup отложен на 24 часа и отменяем restore |
| Restart/reconnect | Нет нового actor | сначала authoritative include-deleted reconciliation всех configured Channels и известных root Threads, затем REST cursor/reaction catch-up и WebSocket | стабильный provider event ID + immutable digest | пропущенные delete/restore снова вызывают specialized owner command; duplicate без effect, конфликт digest закрыто отклоняется |

Terminal виды не объединены скрытым событием: progress/status/run-card создаются
в owner transaction соответствующей команды, incident — в transaction incident,
а final Markdown и publish-artifact — в terminal runtime transaction. Для
каждого вида authoritative read path — точная строка
`control_plane.interaction_delivery_work` и защищённый gateway delivery
readback; отдельный недостоверный AsyncAPI envelope не создаётся.

## Lifecycle matrix

| Объект | Create/claim | Renew/retry | Complete/terminal | Cancel/delete/restart |
|---|---|---|---|---|
| Inbound | unique provider event + digest; DB-time lease/token/fence/generation | expired `PROCESSING` reclaim, bounded backoff; scan poll не расходует terminal attempt budget | `COMPLETED`, `IGNORED` либо `FAILED` с durable semantic response/error/next action | restart продолжает `PENDING/WAITING_SCAN/WAITING_CLEANUP`; stale worker не сохраняет результат |
| Artifact input | S3 key из project/event/digest; metadata readback | повторный put допускается только при exact size/type/digest | только control-plane `CLEAN` разрешает Turn; reject terminal | gateway не удаляет object/Artifact; retention policy authoritative |
| Delivery | durable payload snapshot, tenant/session lineage и client-owned identity props | claim выдаёт lease owner/token/fence; reclaim только после DB-time expiry; `PROVIDER_ACCEPTED` удерживает ACK lease и новый fence переиспользует provider receipt | create/update Post получает checkpoint/readback; затем `DELIVERED` или `DEAD_LETTER` | `UploadFile` не выполняется: у Mattermost нет документированного восстановления потерянного `file_id`; artifacts публикуются одноразовой gateway-mediated ссылкой, а UNKNOWN create не повторяется автоматически |
| OwnerGate | control-plane назначает delivery ID/payload digest и выдаёт token/fence/expiry | новая claim generation атомарно rebind-ит незавершённую transport delivery | delivery receipt, затем exact decision; использованный claim token удаляется из gateway state | stale callback/recipient/post fail closed; gateway не удаляет Gate |
| Chat/Session delete | verified delete переводит owner state в `DELETION_PENDING` и создаёт `WAITING_CLEANUP` | 24 часа допускается verified restore; retries fenced | `FINALIZE` переводит Chat/Session в `DELETED`; Thread close закрывает активный граф и pending gates | restart reclaim-ит cleanup; в pending новые Turn и callbacks получают durable terminal conflict |
| Worker | запускается после PostgreSQL/control-plane/Mattermost/S3 barrier | каждый цикл bounded context; WebSocket reconnect с backoff | cancel/join до закрытия PostgreSQL и gRPC | tracing и Sentry получают независимые shutdown contexts |

## Защита и секреты

Public listener требует TLS 1.3 и проверенный клиентский сертификат. Mattermost,
S3, PostgreSQL и control-plane используют exact SNI/CA; plaintext fallback,
`skipTLSVerify` и wildcard egress отсутствуют. В Vault хранятся только значения;
в репозитории указаны имена путей/ключей. Agent bot token материализуется в
отдельный CAS=0 KV v2 path по server-owned binding ID. Gateway получает
короткоживущий Vault token через Kubernetes auth exact ServiceAccount/namespace/
audience, читает только version-pinned credential с SHA-256 и при revoke пишет
новую `REVOKED` version без token; generic secret CRUD, destroy и второй writer
отсутствуют. Code-first policy/role задаёт
`tools/interaction-gateway/configure-agent-bot-credential-vault.sh` и может
применяться только после review/merge и отдельного owner OK.
Начальный environment render использует
generation-scoped principal `interaction_gateway_runtime_g1`, входящий в non-login роль
`interaction_gateway_runtime`; startup проверяет exact `session_user` и членство
до открытия barrier. Pod не подписывает и не выбирает DB scope: отдельная
migrator/controller identity атомарно связывает каждый LOGIN generation ровно
с одной organization и закрытым списком projects. `SECURITY DEFINER` выводит
authority из неизменяемого `session_user`, а `FORCE RLS` закрывает
inbound, cursor, delivery, upload receipt и turn watch. Worker discovery читает
только scope indexes без payload. Startup barrier внутри rollback-only
транзакции доказывает разрешённый scoped cursor DML и отказ cross-tenant
`WITH CHECK`. Migration job выполняет только явные stage → NEXT → CURRENT и
проверяет principal→organization/projects readback. Явно заданный PREVIOUS
выводится forward-only через `NOLOGIN`, revoke membership, terminate/readback;
rollback поколения и resurrection retired identity запрещены. Конфигурация принимает только согласованную пару
`interaction_gateway_runtime_g<N>`/generation, поэтому следующее поколение
вводится тем же code-first render без возврата к `g1`. Старый caller-signed
HMAC API лишён runtime прав. OwnerGate claim token шифруется AES-GCM
gateway-owned ключом и не возвращается readback API.
Result-файл публикуется только для exact project-prefixed объекта
на настроенном bucket после проверки size/type/SHA-256 metadata; другой
авторитетный result reference остаётся digest-only и не раскрывается в карточке.
Любой чистый result остаётся в private S3. Карточка содержит не bearer URL, а
непрозрачный grant ID: gateway требует пользовательский Mattermost bearer,
повторно проверяет exact UserID, membership и actor/org/project/Team/Channel/
Session/Turn/Artifact/version/SHA binding, затем единожды проксирует объект.
Delete lifecycle немедленно отзывает незавершённые grants; readback сохраняет
consumed/revoked/authenticated audit без токенов.

Mattermost event и delivery-readback keysets хранят verifier-owned историю
`generation → kid + RFC7638 thumbprint + status` и durable high-watermark.
Migration job выполняет явный single-winner genesis из immutable public keyset
и принимает повтор только после exact audit/readback; отсутствие fence/history
закрывает startup. `CURRENT/NEXT/PREVIOUS/RETIRED`, overlap, promotion и retired
union запрещают rollback, переиздание key identity и resurrection после restart.

`control-plane.interaction-gateway` — отдельный закрытый producer profile для
`ManageSession`, `RegisterArtifact`, `GetResource`, `EnqueueTurn`,
`ResolveOwnerGate` и exact conversation lifecycle operation. Профиль owner-gate delivery остаётся отдельным и не меняет
семантику существующих producers.
Для Team UI операций `control-plane.oidc` выпускает exact contexts с audience
`urn:mattercodex:internal-rpc:interaction-gateway`, caller workload
`control-api-gateway`, его SPIFFE ID, полным method и одной из permissions
`interaction.team.catalog.read|create|link|binding.get|relink|unlink|mapping-operation.get|provider.readback|readiness`.
Gateway verifier проверяет этот context поверх TLS 1.3/mTLS; request DTO не
содержат actor, organization, project, Workspace или raw provider ID.

## Сборка и миграции

```bash
cd services/external/interaction-gateway
go test -run '^$' ./...
go build ./cmd/interaction-gateway ./cmd/cli
interaction-gateway-cli migrate up
```

OpenAPI генерируется корневой целью `make gen-openapi-go`. Миграции
forward-only создают только gateway-owned transport state, signed runtime
context, credential fence и RLS policy.

Proto `interactiongateway.v1` генерируется корневой командой `buf generate` в
`libs/go/interactiongatewayapi`. Team RPC слушает `:9443`, требует TLS 1.3 с
exact control-api SPIFFE и verified application authorization context от
локального authority verifier. Provider receipts содержат exact `aud`; общий
control-plane verifier сравнивает его с policy audience и для Mattermost, и
для AI provider path. Startup/readiness проверяют полный named-SQL corpus,
RLS-scoped DML и тот же joined owner mapping+Mattermost Team/channel route;
recovery worker
запускается только после barrier и завершается до PostgreSQL.
Server-owned primary bot credential должен иметь только необходимые для этого
пути Mattermost права на Team catalog/create и membership readback/add; значение
credential не попадает в конфигурацию, ответы, логи или provider receipt.

## Ручная проверка

1. Опубликовать environment-owned mapping в immutable staging/production Vault
   revision path через `tools/interaction-gateway/mapping-revision.sh` с CAS=0
   и настроить названные Vault keys,
   workload certificates, CA ConfigMaps и immutable image digests.
2. Выполнить migration job; убедиться, что `/readyz` становится `204` только
   после готовности exact PostgreSQL/control-plane/Mattermost/S3 paths.
3. Отправить `/codex` и Post с файлом: до `CLEAN` ожидается artifact card, после
   него — ровно один Turn и одна run card.
4. Повторить тот же callback/event: новые Session, Turn и Post не создаются.
5. Оборвать HTTP-ответ Mattermost после принятия Post и перезапустить Pod:
   exact `PendingPostId`/payload readback восстанавливает receipt; отсутствие
   доказуемого readback даёт terminal repair outcome без повторного effect.
6. Проверить approve/reject/changes/cancel кнопкой, dialog и reaction от
   назначенного owner; чужой actor, post или channel получает закрытый отказ.
7. Остановить Mattermost/S3/control-plane: readiness становится false, а
   durable записи остаются для retry/dead-letter/readback.
8. Удалить Channel/Thread, убедиться в `DELETION_PENDING`, запрете новой
   инструкции и callback; восстановить до 24 часов и проверить отмену cleanup.
9. Проверить result больше 64 MiB: карточка содержит gateway-mediated grant,
   чужой/повторный User получает закрытый отказ, а точный actor единожды получает
   object с совпавшим SHA-256 без `UploadFile` и direct S3 URL.
10. Вызвать Team catalog с verified control-api context: убедиться, что DTO
    содержит только opaque selector и безопасный snapshot без Mattermost Team
    ID. Повторить create с тем же UUID key и эквивалентным normalized intent —
    второй Team не создаётся; тот же key с другим intent получает conflict.
    Параллельный иной create того же Project останавливается durable fence до
    provider POST.
11. Оборвать ответ `CreateTeam` после возможного принятия и перезапустить Pod:
    operation остаётся `AMBIGUOUS`, worker использует только operation-bound
    slug/correlation readback и переводит её в `PROVIDER_ACCEPTED` либо после
    DB-time deadline в `REPAIR_REQUIRED` без второго provider effect и без
    изменения membership чужой Team.
12. Выполнить link/get/relink/unlink с exact OCC: открытый dependent graph и
    stale version/generation получают закрытый conflict, успешный transition
    возвращает `BOUND` либо `UNLINKED` без raw Team ID. Оборвать ответ owner RPC:
    mapping worker обязан завершить operation по signed list/get readback без
    повторной команды, а недоказуемый predecessor перевести в `REPAIR_REQUIRED`.
    Прочитать terminal outcome через `GetMattermostTeamMappingOperation` и
    убедиться, что DTO не содержит raw Team ID/provider payload/raw error.
13. После relink проверить, что новая Team принимается inbound/delivery, а
    прежняя generation закрыто отклоняется. После unlink inbound, delivery,
    artifact и `/readyz` должны закрыто отклоняться. Удаление/запрет Team при
    активном `BOUND` также переводит `/readyz` в неготовность на том же joined
    Mattermost+control-plane path.

Rollback описан в [runbook](../../../docs/runbooks/interaction-gateway.md).
