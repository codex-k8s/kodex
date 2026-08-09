---
id: SVC-MC-013
title: Control API gateway
type: service
status: approved
owner: backend
version: 1.6.0
updated: 2026-08-09
---

# Control API gateway

`control-api-gateway` — внешняя owner HTTP/WebSocket boundary Issues
[#191](https://github.com/codex-k8s/matter-codex/issues/191) и
[#237](https://github.com/codex-k8s/matter-codex/issues/237). Gateway проверяет
OIDC, выдаёт короткую защищённую browser session, применяет CORS/CSRF/rate
limits и преобразует запросы в сгенерированные gRPC clients `control-plane`,
`interaction-gateway` и `integration-gateway`.

Gateway не читает PostgreSQL, Redis, NATS или Vault API напрямую, не хранит
бизнес-состояние и не принимает `actor`, organization, project, owner или
tenant из payload. Эти поля разрешает `AuthorityProofResolverService` по
проверенному OIDC subject и авторитетному состоянию `control-plane`.

## Контракты и codegen

- source OpenAPI: `contracts/openapi/control-api-gateway/v1/openapi.yaml`;
- generated HTTP server/models:
  `internal/transport/http/generated/control_api_gateway.gen.go`;
- source AsyncAPI: `contracts/asyncapi/control-api-gateway/v1/asyncapi.yaml`;
- generated WebSocket models: `internal/transport/websocket/generated`;
- internal RPC: `contracts/proto/controlplane/v1/control_plane.proto`,
  `contracts/proto/interactiongateway/v1/mattermost_team.proto` и
  `contracts/proto/integrationgateway/v1/integration_management.proto`;
- authority policy: существующий профиль `control-api-gateway` в
  `deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json`.

AsyncAPI — единственный source of truth для имён WebSocket envelope, message
type, channel, resource kind, projection alias и list wrapper. Каждый такой
component имеет стабильные `title` и `$id`; projection DTO ссылаются на
канонические OpenAPI schemas. Команда
`make gen-control-api-gateway-asyncapi` сначала полностью удаляет только
generated directory, затем напрямую вызывает AsyncAPI CLI 6.0.2 с встроенной
Modelina 4.4.3 и выполняет fail-only structural check. Ручное редактирование
generated files и любой mutating postprocessor запрещены.

Generated package намеренно не содержит JSON tags/codecs и не участвует в
public runtime decode: Modelina Go enum decoder с `--goIncludeTags` не
гарантирует fail-closed unknown lookup. Тонкая граница
`internal/transport/websocket/protocol.go` принимает только named closed
enums, отклоняет unknown/empty/null и out-of-range marshal и перед выдачей
snapshot валидирует все OpenAPI projection enums. Internal Proto names и
oneof наружу не экспортируются.

OpenAPI требует `Idempotency-Key` для каждой state-changing команды и
`If-Match: "<positive-version>"` для update/transition/delete/detach/copy.
Missing либо malformed ETag централизованно даёт typed `400 INVALID_REQUEST`
до body decode и RPC; валидный ETag передаётся без ослабления. Gateway
передаёт их без ослабления. `control-plane` сначала разрешает ресурс внутри
trusted owner/tenant boundary, затем проверяет OCC и receipt. Create не
принимает project/owner/ownership: project и owner разрешает `control-plane`,
а UI transport server-side назначает `managed_by=UI`. Generic `PUT` не меняет
Git-owned объект: для `TEAM|ROLE|PROMPT_PROFILE` доступны только отдельные
`detach` и `copy`, а source/revision разрешаются сервером из locked resource.

## Сквозная карта сценариев

Во всех строках actor source — подписанный OIDC `sub` с `sid`, `jti` и
монотонной `session_revision`; authority source — resolver control-plane,
который server-side выводит actor/organization/project/ownership. Downstream
вызывается по exact mTLS SNI/CA и короткоживущему internal context из локального
issuer. Payload identity не используется.

| Требование и инициатор | Внешняя операция | Gateway mapping / internal RPC | Владелец, version/idempotency | Ответ, переход, событие или read path |
| --- | --- | --- | --- | --- |
| #191, owner создаёт сессию | `POST /api/v1/session`, OIDC bearer + idempotency | signature/issuer/audience/time/session claims → `control.owner-session.admit` → `AdmitOwnerSession`, затем AES-256-GCM cookie + CSRF | control-plane хранит `(organization, actor, sid)` current revision/digest; OIDC — credential | `204`; cookie выдаётся только после durable admit; rollback/stale digest закрыто отклоняются |
| #191, owner завершает сессию | `DELETE /api/v1/session`, cookie+Origin+CSRF+`If-Match`+idempotency | `control.owner-session.revoke` → `RevokeOwnerSession`, затем удаление cookies | control-plane атомарно фиксирует revocation/receipt; expected revision совпадает с verified session | `204`; повторный exact receipt допустим, любой следующий REST/WS proof закрыто отклоняется |
| #191, PRD-MC-001/005, owner создаёт проект | `POST /api/v1/projects` | `control.project.create` → `CreateProject`, exact `controlplane.project.create` | закрытая organization-scoped команда; control-plane назначает ID/owner; generic create не получает этот bypass; semantic receipt по `Idempotency-Key` | `201`+`ETag`; state/idempotency/audit и каждый обязательный outbox fact атомарны; client перечитывает REST/WS snapshot |
| #191, owner читает проекты | `GET /api/v1/projects` | `control.project.list` → `ListProjects` | control-plane eligibility и pagination token | `200`; только authoritative read path, события не требуются |
| #231, owner обновляет проект | `PUT /api/v1/projects/{projectId}` | `control.project.update` → generated `UpdateProject` | OIDC actor/organization разрешены resolver; `projectId` — locator; control-plane сначала доказывает exact owner project, затем блокирует row и проверяет `If-Match`/receipt; owner и `managed_by` сохраняет сервер | `200`+новый `ETag`; project, audit, receipt и domain event фиксируются одной transaction; скрытый project даёт `404`, stale version — `412` |
| #231, owner удаляет пустой проект | `DELETE /api/v1/projects/{projectId}` | `control.project.delete` → generated `DeleteProject` | tenant owner resolution предшествует OCC/receipt; control-plane под lock проверяет отсутствие non-`DELETED` children | одна transaction фиксирует `DELETION_PENDING`→`DELETED`, audit и событие каждого перехода; terminal tombstone — authoritative idempotent readback |
| #191, owner создаёт UI-конфигурацию | `POST /api/v1/resources` для `CHAT|CREDENTIAL_BINDING|REPOSITORY_WORKSPACE|INTEGRATION` | закрытый kind/spec caster → `control.resource.create` → `CreateResource` | control-plane назначает project/owner; `Idempotency-Key`; secret value запрещён | `201`+`ETag`; audit и каждый применимый outbox fact принадлежат owner transaction; WS получает свежий read snapshot |
| #191, owner читает ресурс/список | `GET /api/v1/resources[/{id}]` | `control.resource.get|list` → `GetResource|ListResources` | resource сначала разрешён control-plane внутри текущей области; verified actor predicate находится в named SQL до `ORDER BY/LIMIT` | `200`+`ETag` для single; cursor вычисляется по последней eligible строке, скрытый и отсутствующий одинаково `404`; authoritative read path |
| #191, owner ищет ресурс | `GET /api/v1/resources/search` | typed query → `control.resource.search` → `SearchResources` | control-plane применяет тот же authoritative organization/project/verified-actor predicate в named SQL до page limit для каждого закрытого kind | `200`; typed page/cursor прогрессирует по eligible строкам, скрытые ресурсы не попадают в search result и не создают ранний EOF |
| #191, owner обновляет UI-конфигурацию | `PUT /api/v1/resources/{id}` | closed kind/spec → `control.resource.update` → `UpdateResource` | owner resolution → `If-Match` → idempotency; Git-owned generic update запрещён | `200`+новый `ETag`; audit/read snapshot; parallel version даёт `412` |
| #191, owner меняет lifecycle | `POST /api/v1/resources/{id}/transition` | closed state/reason → `control.resource.transition` → `TransitionResource` | owner resolution → OCC/idempotency; полный lifecycle проверяет control-plane | `200`; недопустимый переход `409`; authoritative resource/audit read path |
| #191, owner удаляет конфигурацию | `DELETE /api/v1/resources/{id}` | `control.resource.delete` → `DeleteResource` | owner resolution → OCC/idempotency; tombstone/связи принадлежат control-plane | `200`; state/tombstone/audit фиксируются owner transaction; WS перечитывает snapshot |
| #191, owner управляет authority-bearing team/role/prompt | `POST /api/v1/access-resources` | closed kind/action/spec → `control.access.manage` → `ManageAccessResource` | специализированная команда; create без caller owner, остальные с resource ID+`If-Match` | `200`; self-grant/универсальный CRUD запрещены; audit/read snapshot |
| #191, owner отделяет Git-owned access resource | `POST /api/v1/access-resources/{id}/detach` | `control.access.detach` → `DetachAccessResource` | trusted owner/kind resolution → exact version/idempotency; ownership меняет control-plane | `200`; одна transaction обновляет ownership/version/audit/receipt |
| #191, owner копирует Git-owned access resource | `POST /api/v1/access-resources/{id}/copy` | `control.access.copy` → `CopyAccessResource` | locked source owner/kind/version; новый ID/owner и immutable UI lineage source/revision назначает сервер | `201`; copy создаётся `PAUSED`, source остаётся неизменным, copy/audit/receipt атомарны; activation — отдельный transition |
| #231, owner создаёт/обновляет/удаляет Schedule | `POST /api/v1/schedules`, `PUT|DELETE /api/v1/schedules/{scheduleId}` | closed HTTP caster → `control.schedule.manage` → generated `ManageSchedule(CREATE|UPDATE|DELETE)` | create не принимает ID/owner/state; update/delete сначала разрешают locked owner Schedule, затем OCC/idempotency; execution graph проверяет control-plane | create/update возвращают authoritative resource; delete одной transaction закрывает `ACTIVE|PAUSED`→`ARCHIVED`→`DELETION_PENDING`→`DELETED` с audit/event каждого фактического перехода; active occurrence/run закрыто даёт `409` |
| #231, owner решает OwnerGate | `POST /api/v1/owner-gates/{ownerGateId}/resolution` | closed decision → `control.owner-gate.resolve` → generated `ResolveOwnerGate` | gate ID — locator; control-plane блокирует полный process/session/turn/gate graph, сверяет delivered tuple, actor, version и receipt | `200` возвращает gate и process одной transaction; `APPROVE|REJECT|CHANGES_REQUESTED` закрывают прежние claim/lease/grant; continuation создаётся только сервером; audit/event/readback принадлежат control-plane |
| #231, owner читает backups | `GET /api/v1/backups[/{backupId}]` | `control.backup.list|get` → generated `ListBackups|GetBackup` | verified actor/project predicate применяется в PostgreSQL до cursor/limit; eligibility выводится из terminal runtime archive, verifier/cleanup state и одного server-owned retention clock; session-scoped public `version`/`updatedAt` одинаково растут при rank, retention deadline, restore generation и target lifecycle, immutable `sourceVersion` остаётся отдельным | authoritative `VERIFYING|RETENTION_PENDING|AVAILABLE|RESTORING|RESTORED|RESTORE_FAILED|RESTORE_CANCELLED|RESTORE_EXPIRED|EXPIRED|UNAVAILABLE`; наружу выходят только IDs, версии, safe digests, scope и timestamps; private данные отсутствуют |
| #231, owner запускает restore и читает результат | `POST /api/v1/backups/{backupId}/restore`, `GET|LIST /api/v1/restore-operations` | `control.backup.restore|restore-operation.get|restore-operation.list` → generated `RestoreBackup|GetRestoreOperation|ListRestoreOperations` | backup ID — locator; body несёт immutable `sourceVersion`, `If-Match` — public backup version; lifecycle receipt ищется до dynamic eligibility, затем control-plane проверяет exact source/fence/digests/latest retention; operation/target назначает сервер | потерянный `202` после terminal возвращается по тому же semantic request/key; owner transaction создаёт immutable operation и fresh attempt, discovery/readback не раскрывает private tuple |
| #191, owner наблюдает runs | `GET /api/v1/runs` | `control.resource.list` → `ListResources(PROCESS_RUN)` | control-plane владеет run lifecycle/version | `200`; авторитетный read path, gateway не выводит terminal state самостоятельно |
| #191, owner наблюдает incidents | `GET /api/v1/incidents` | `control.runtime-incident.list` → `ListRuntimeIncidents` | control-plane связывает incident с runtime execution и root `PROCESS_RUN.owner_actor_id`; organization/project/verified actor predicate применяется в named SQL до cursor/limit | `200`; другой actor того же project не видит hidden run/evidence в REST/WS; typed `incidentId/kind/evidenceSha256/executionFence/workloadId/occurredAt`, secret values отсутствуют |
| #191, owner читает configuration changes/audit | `GET /api/v1/configuration-changes|audit` | `control.audit.list` → paged `ListAuditEvents`; gateway сканирует authoritative cursor до полной requested page | control-plane владеет audit; gateway cap 500 сканированных событий | `200`; закрытый registry включает `detach_access_configuration` и `copy_access_configuration`; при превышении cap fail-closed |
| #191, owner читает diagnostics | `GET /api/v1/diagnostics` | `control.diagnostics.get` → `GetDiagnostics` | control-plane владеет schema/outbox/lease metadata | `200`; только ограниченный authoritative read path |
| #191, owner подписывается на realtime | `WSS /api/v1/realtime`, subprotocol `mattercodex.control.v1` + CSRF | та же session fence; все RPC pages читаются до конца, incidents идут через `ListRuntimeIncidents` | connection-local sequence не является domain version; typed items несут server version/evidence | один `complete=true` snapshot до 500 items; overflow/scan cap → PROBLEM+close без replace; reconnect = resubscribe/read current |

## Authority и lifecycle matrix

| Состояние/переход | Проверка | Закрытый отказ |
| --- | --- | --- |
| Session create | exact HTTPS OIDC issuer/audience, `sub/sid/jti` UUID, positive revision, остаток TTL ≥ 1 минута | cookie не выдаётся |
| Session use | AES-GCM current/previous key, expiry, повторная OIDC verify; перед каждым protected RPC resolver сверяет durable current sid/revision/bearer digest/revocation | `401`/protected RPC denial; stale/revoked bearer не получает fresh proof |
| Mutation | exact allowlisted `Origin`, session cookie, double-submit CSRF и digest внутри encrypted envelope | `403`, RPC не вызывается |
| Session rotation | новые cookies шифруются current key; current и previous принимаются на overlap | lower/unknown key не принимается; rollback key не материализуется gateway |
| Session expiry/logout | deadline закрывает HTTP/WS; logout сначала фиксирует durable revoke, затем удаляет cookies | rejoin только через более новую OIDC session revision и новый admit |
| Create | closed command registry; owner/project отсутствуют в request | unknown/protected kind → `400`, RPC не вызывается |
| Update/transition/delete/detach/copy | ETag парсится централизованно до RPC; resource ID передаётся как locator; control-plane делает owner resolution до OCC/receipt | missing/malformed `If-Match` → typed `400` без RPC; hidden/missing → одинаковый `404`; version → `412`; lifecycle → `409` |
| Project | только `CreateProject` с exact `controlplane.project.create` проходит PROJECT protected registry и назначает ID/owner/state; update сохраняет server-owned ownership; delete допускается только без live children и атомарно создаёт terminal tombstone | generic create/update/transition/delete остаются закрыты; caller project/owner не являются authority; self-ownership, stale version, непустой project и partial delete отклоняются |
| Schedule | только `ManageSchedule(CREATE|UPDATE|DELETE)`; create server-owned, update/delete под полным schedule execution graph lock; delete терминализирует весь lifecycle одной transaction | generic lifecycle не принимает Schedule; active occurrence/run, stale version и частичный terminal graph дают закрытый отказ |
| OwnerGate | только exact delivered gate и полный owner graph; decision registry закрыт; `CHANGES_REQUESTED` создаёт fresh continuation server-side | generic transition, payload actor/root lineage, stale delivery/version и повторное использование старых grant/claim запрещены |
| Backup eligibility | owner predicate, terminal archive, verifier+cleanup completion, latest session candidate и PostgreSQL retention deadline вычисляет control-plane | неизвестный/чужой/просроченный/неполный archive не становится restorable; private proof/storage metadata не проецируются |
| Restore create/readback | exact receipt проверяется до изменяемой eligibility; новый command сверяет public backup ETag отдельно от immutable source version/fence/digests/session; fresh attempt/revision/grant и immutable operation создаются owner transaction | exact terminal replay возвращает сохранённую operation; другой actor/key/digest закрыто отклоняется; caller не задаёт operation/turn/attempt/grant/locator |
| WS subscribe | exact Origin, session, CSRF subprotocol+cookie, ≤4 channel, ≤8 kind, bounded frame/connection | policy close без snapshot |
| WS retry/reconnect | новый authority proof/session fence на каждую страницу каждого poll; reconnect читает current state | gateway не replay-ит устаревший или частичный snapshot и не синтезирует delete |
| Rate/concurrency | малый pre-auth admission; раздельные global/per-organization+subject HTTP и WS quota, bounded inactive-key cleanup | один subject/долгий WS не занимает HTTP или чужой subject quota; close освобождает slot |
| Public TLS rotation | один `VaultStaticSecret` атомарно доставляет cert/key/CA и server-owned generation/digests; `Prepare` сохраняет PENDING, listener+loopback exact SNI/CA/DER readback предшествует `Confirm`, который переводит N+1 в APPLIED и N в bounded PREVIOUS | crash до Confirm оставляет N APPLIED; rollback/skipped/mixed material/expiry закрывают startup; старый N ready только до конца overlap, readiness выполняет read-only `Check` |

## Полная matrix dependency #231

| Aggregate / command | Допустимый source → target и cardinality | Authority, OCC/idempotency и readback | Terminal, cancel, retry, expiry и событие |
| --- | --- | --- | --- |
| Project `UPDATE` | nonterminal version N → N+1; одно изменение | tenant-level verified owner → trusted project resolution → row lock → `If-Match`/receipt; ID/owner/ownership сохраняет control-plane | один audit и один `ResourceChanged`; terminal/`DELETION_PENDING` update запрещён |
| Project `DELETE` | `ACTIVE|PAUSED|ARCHIVED` N → `DELETION_PENDING` N+1 → `DELETED` N+2; ровно два перехода | тот же owner resolver; под lock не должно быть child с state не `DELETED`; replay читает exact tombstone N+2 | два audit и два `ResourceChanged`; cancel/retry/restore проекта отсутствуют, terminal необратим |
| Schedule `CREATE` | server-owned `ACTIVE` version 1; один aggregate | project authority из signed context; ID/owner/state/watermark назначает control-plane; semantic receipt | один audit/event; caller не задаёт lineage или lifecycle |
| Schedule `UPDATE` | `ACTIVE|PAUSED` N → N+1 без смены lifecycle | locked owner Schedule и полный occurrence/run graph предшествуют OCC/receipt | один audit/event; open run/occurrence, archived/terminal state закрыто отклоняются |
| Schedule `DELETE` | `ACTIVE|PAUSED`: три перехода; `ARCHIVED`: два; `DELETION_PENDING`: один; итог `DELETED` | locked owner Schedule + отсутствие open occurrence/run; replay принимает только terminal version в пределах фактической цепочки | по одному audit/event на `ARCHIVED`, `DELETION_PENDING`, `DELETED`; delete нельзя отменить или продолжить частично другой командой |
| OwnerGate до delivery proof | `WAITING_OWNER`, `deliveryState=AWAITING_DELIVERY_PROOF`, `resolvable=false` | только server-owned delivery claim; Mattermost locator не принимается browser endpoint | `nextAction=WAIT_FOR_DELIVERY`; попытка решения закрыто отклоняется, graph не меняется |
| OwnerGate `APPROVED` | provider-readback `WAITING_OWNER` gate/turn/process → `SUCCEEDED`; один полный graph commit | provider receipt digest точно связан с delivery/payload/post/channel/root/fence; process/session/turn/attempt/input разрешаются из Gate и owner graph, а не из browser body | старые gate/claim/lease/grant закрываются; gate, turn, process и schedule occurrence получают audit/event; readback содержит gate+process |
| OwnerGate `REJECTED|CANCELLED` | gate/turn/process переходят соответственно в `FAILED` либо `CANCELLED` | та же exact owner graph authority; open dependent work запрещает terminal commit | одна owner transaction закрывает связанные authority; повтор возвращает тот же terminal readback |
| OwnerGate `CHANGES_REQUESTED` | старый gate terminal `SUCCEEDED`, старый turn terminal; fresh revision/input/turn/attempt создаются server-side | reason входит в immutable feedback binding, actor/root/process lineage не принимаются из payload | продолжение создаёт новый grant; прежние lease/claim закрыты; audit/event фиксируют старый terminal и fresh continuation |
| OwnerGate expiry | до или после delivery proof просроченный graph закрывается по PostgreSQL clock даже вместо позднего решения | gate блокируется после полного graph и deadline повторно проверяется | `deliveryState=EXPIRED`, `resolvable=false`, `nextAction=READ_TERMINAL`; поздний decision не воскресает graph |
| Backup projection | `VERIFYING` → `RETENTION_PENDING` → `AVAILABLE`; далее `RESTORING|RESTORED|RESTORE_FAILED|RESTORE_CANCELLED|RESTORE_EXPIRED`, либо `EXPIRED|UNAVAILABLE` | verified owner predicate + exact project/session + latest eligible archive; `sourceVersion` закрепляет archive identity, а session freshness timestamp и public `version` монотонно учитывают новый eligible rank, пересечённый retention deadline, operation generation, target execution и Turn | read-only, отдельного backup event нет; list/get возвращают одинаковую cache-safe projection и `restoreOperationId`, а `/restore-operations?backupId=` восстанавливает readback после потери Location; private locator/evidence/grant отсутствуют |
| Restore `CREATE` | eligible source `FAILED|EXPIRED` execution N/fence F → `RETRIED` N+1/F+1; fresh Turn/RuntimeRevision/attempt и operation version 1 | owner backup resolution → safe digest/scope/OCC → source+owner graph locks → latest archive/retention by DB clock → receipt; operation/target назначает сервер | source retry, fresh turn, operation, audit и Turn event одной transaction; restore-operation event отсутствует, authoritative `GET` обязателен |
| Restore claim/materialization | `QUEUED` → `ASSIGNED` → admission `ADMITTED` → `MATERIALIZING` → `READY` → `RUNNING` | verifier независимо пересчитывает `sourceAuthoritySHA256` из полного private source tuple; operation/generation/digest входят в EffectiveInput, WorkloadTicket и signed restore ticket; admission потребляет generation, затем controller и S3 exchanger раздельно атомарно расходуют current-generation effect slots непосредственно перед Kubernetes и STS | только exact `ADMITTED` ticket допустим; cancel/terminal/expiry повышает durable watermark, поэтому stale/revoked/reused ticket закрыто отклоняется даже при корректной подписи/TTL/локальном receipt; retry требует fresh generation; private tuple не выходит в browser/логи |
| Restore terminal/retry | cancel до runtime claim выводится из owner Turn; после claim target → `SUCCEEDED|FAILED|CANCELLED|EXPIRED`; `FAILED|EXPIRED` с attempt < 100 допускают `RETRYING` successor | retry загружает только exact original source из operation binding, даже если prior target был `CONSUMED`, и выдаёт fresh generation; terminal/cancel/expiry повышают durable revoke watermark | только `SUCCEEDED` даёт `RESTORED`; `RETRY_RUNTIME` показывается лишь для реально допустимых `FAILED|EXPIRED` и attempt < 100; `CANCELLED`, pre-claim terminal и cap 100 дают `NONE`; partial остаётся различимым |

## Материализация рабочего пути

## Сквозная карта Issue #237

Во всех строках actor, organization и workspace выводятся из проверенной
OIDC/session application grant. Browser передаёт только opaque selector/ref,
полученный предыдущим catalog/readback. Gateway выполняет transport-проверки и
безопасную проекцию; eligibility, lifecycle, owner resolution, OCC и receipt
принадлежат указанному RPC-владельцу.

| Owner-сценарий | External operation | Generated internal RPC | Version / результат / realtime |
| --- | --- | --- | --- |
| Team catalog/create/link/read/relink/unlink | `listMattermostTeams`, `createMattermostTeam`, `linkMattermostTeam`, `getMattermostTeamBinding`, `relinkMattermostTeam`, `unlinkMattermostTeam`, `getMattermostTeamMappingOperation`, `getMattermostTeamProviderReadback` | exact `interactiongateway.v1.MattermostTeamService/*` | mutation: CSRF, `Idempotency-Key`, для relink/unlink `If-Match` плюс authoritative generation; response содержит safe Team selector, mapping version/generation и operation state; канал `WORKSPACE_TEAMS` заменяется только полным snapshot |
| RoleDefinition | `listRoleDefinitions`, `getRoleDefinition`, `manageRoleDefinition`, `listRoleDefinitionHistory` | `ListRoleDefinitions`, `GetRoleDefinition`, `ManageRoleDefinition`, `ListRoleDefinitionHistory` | специализированные `CREATE|UPDATE|ARCHIVE|DELETE`, ID/owner server-owned, `If-Match` для existing aggregate; typed resource/history и ETag; `CONFIGURATION` snapshot |
| Agent | `listAgents`, `getAgent`, `manageAgent`, `listAgentHistory`, bot identity catalog/get/manage/operation/provider-readback | `ListAgents`, `GetAgent`, `ManageAgent`, `ListAgentHistory`, exact `AgentMattermostBotIdentityService/*` | browser передаёт только `runtimeSelectionKey` из versioned catalog, а runtime selection выводится control-plane из active RoleDefinition/RoleImageRecipe; bot bind/rebind/revoke принимает только catalog selector и возвращает safe binding/receipt без provider object/request digest |
| AgentAssignment | `listAgentAssignments`, `getAgentAssignment`, `manageAgentAssignment`, `listAgentAssignmentHistory` | `ListAgentAssignments`, `GetAgentAssignment`, `ManageAgentAssignment`, `ListAgentAssignmentHistory` | только `ASSIGN|UNASSIGN`; agent/role/room stable refs разрешаются owner-side до OCC/receipt; current/history |
| InstructionSet | `listInstructionSets`, `getInstructionSet`, `manageInstructionSet`, `listInstructionSetHistory`, `compareInstructionSetVersions` | exact одноимённые control-plane RPC | `CREATE|UPDATE|VALIDATE|PUBLISH|ROLLBACK|DETACH|COPY|ARCHIVE|DELETE`; Git-owned edit закрыт до detach/copy; immutable version/digest, validation problems и server compare |
| Provider catalog/authorization/connections | `listProviders`, `getProvider`, `startProviderAuthorization`, `getProviderAuthorization`, `restartProviderAuthorization`, `cancelProviderAuthorization`, `listProviderConnections`, `getProviderConnection`, `reauthorizeProviderConnection`, `revokeProviderConnection` | exact `integrationgateway.v1.IntegrationManagementService/*` | provider/connection refs только из catalog/readback; mutation имеет receipt, OCC и generation; выдаются URL, short user code и masked account, но не credential/token/raw payload; `PROVIDERS` snapshot |
| Provider pools | `listProviderPools`, `getProviderPool`, `manageProviderPool` | integration `List/Get/ManageProviderPool` | `CREATE|UPDATE|ARCHIVE|DELETE`; каждый member привязан к exact connection version/generation, weight bounded; effective eligibility и state только из response |
| Integration definitions/config/tests | `listIntegrationDefinitions`, `getIntegrationDefinition`, `listIntegrationConfigurations`, `getIntegrationConfiguration`, `configureIntegration`, `testIntegrationConnection`, `getIntegrationTestReceipt` | exact integration management RPC | catalog version/digest/capabilities и connection generation закрепляются в command; secret input отсутствует; test возвращает закрытую category и safe receipt digests |
| ApprovalRequest | `listIntegrationApprovals`, `getIntegrationApproval`, `decideIntegrationApproval` | `ListIntegrationApprovals`, `GetIntegrationApproval`, `DecideIntegrationApproval` | decision one-winner по request hash/version/receipt; preview повторно декодируется как bounded JSON и redacted; terminal response является readback |
| Schedule selectors/bind/current | `listScheduleSelectors`, `getOwnerConfigurationCatalog`, `listOwnerSchedules`, `getOwnerSchedule`, `createScheduleFromSelections`, `bindScheduleConfiguration` | `GetOwnerConfigurationCatalog`, `ListOwnerSchedules`, `GetOwnerSchedule`, `ManageOwnerSchedule(CREATE|UPDATE)` | basic form передаёт preset/timezone и inline prompt либо safe Artifact selector; versioned server defaults/effective values и advanced overrides возвращает control-plane без UUID joins |
| Run | `listRuns`, `getRunDetail`, `listRunTimeline`, `getRunLineage`, `listRunArtifacts`, `manageRun` | exact `ListOwnerRuns`, `GetRunDetail`, `ListRunTimeline`, `GetRunLineage`, `ListRunArtifacts`, `ManageRun` | typed display metadata, safe Session/Turn и закрытые `nextActions` копируются из owner projections; retry создаёт fresh attempt/RuntimeRevision/grant owner-side; `RUNS` complete snapshot |
| OwnerGate | существующий `resolveOwnerGate` | `ResolveOwnerGate` | exact delivered gate, owner graph и ETag; terminal/continuation создаёт control-plane; gateway не принимает lineage |
| Incident | `listIncidents`, `getIncident`, `listIncidentHistory`, `manageIncident` | `ListRuntimeIncidents`, `GetRuntimeIncident`, `ListRuntimeIncidentHistory`, `ManageRuntimeIncident` | severity/workspace/impact/safe correlation/runbook и закрытые `nextActions` приходят из owner projection; gateway не выводит affordances из state; `INCIDENTS` complete snapshot |
| Health | `getHealthSeries` | `GetDiagnostics`, integration `GetManagementDiagnostics`, interaction `CheckReadiness` | bounded current observations сохраняют authoritative `READY|DEGRADED|UNAVAILABLE|UNKNOWN`; valid degradation — `200`, а Problem выдаётся только при ошибке/неполном readback |
| Workspace backup/restore | `listWorkspaceBackups`, `getWorkspaceBackup`, `manageWorkspaceBackup`, `listWorkspaceRestores`, `getWorkspaceRestore`, `manageWorkspaceRestore` | exact `List/Get/ManageWorkspaceBackup`, `List/Get/ManageWorkspaceRestore` | scope/source server-owned; `CANCEL` требует reason, `RETRY` запрещает reason и создаёт fresh attempt; Restore state/`nextActions` возвращаются typed, без gateway lifecycle rules |
| Diagnostics/audit/configuration | `getDiagnostics`, `exportAudit`, `getConfigurationDiff`, `getConfigurationSourceDetail` | `GetDiagnostics`, integration `GetManagementDiagnostics`, bounded `ListAuditEvents`, `CompareInstructionSetVersions` | export больше 500 строк закрыто отклоняется до CSV headers; diff несёт bounded redacted typed changes/continuation owner-side; source detail содержит только safe display/source metadata |

## Lifecycle matrix Issue #237

| Aggregate | Create/read/update/archive/assign | Terminal/cancel/retry/expiry/stale | Авторитетный readback |
| --- | --- | --- | --- |
| Team mapping | create Team и bind используют semantic idempotency; read заново проверяет catalog selector/provider membership; relink требует version+generation | unlink закрывает mapping только после owner graph gate; ambiguous/provider-accepted/repair states остаются явными, gateway их не продвигает | binding + durable mapping operation; stale ETag не вызывает provider effect |
| RoleDefinition/Agent | create назначает ID/owner; update/archive выполняются специализированным registry | delete только по owner lifecycle; stale/hidden/Git-owned дают typed owner error; generic resource transition отсутствует | current `Resource` и immutable history |
| Assignment | assign разрешает оба aggregate и room внутри owner boundary | unassign закрывает exact assignment; повтор возвращает receipt, stale version конфликтует | assignment current/history |
| InstructionSet | validate не публикует; publish выбирает validated immutable version; rollback создаёт новую current version; detach/copy — единственные Git→UI пути | archive/delete только из разрешённого owner state; stale source/version/digest закрываются | current, history и exact two-version compare |
| Provider authorization | start создаёт attempt; status read-only; new-code/reauthorize создают fresh attempt/generation | cancel/revoke terminal; denied/expired/failed нельзя оживить старым code; retry всегда fresh attempt | authorization/connection state, masked display, expiry |
| Provider pool/integration | create/update закрепляют catalog, connection и capability versions | archive/delete/revoke invalidates eligibility owner-side; stale member/definition закрывается | effective state/capabilities/digests из integration gateway |
| Approval | pending exact request hash/version → approve/reject one-winner | expiry/terminal decision необратимы; stale decision не меняет invocation | approval terminal readback |
| Schedule | create из stable selections, versioned preset/defaults и inline/safe Artifact prompt; update под OCC | pause/resume/delete/recovery остаются control-plane; stale selector/version закрывается | owner Schedule projection с effective values, current selectors и ETag |
| Run | read-only detail/timeline/lineage/artifacts с display metadata | cancel атомарно закрывает leases/grants/claims; retry создаёт fresh attempt; terminal/expired/stale action отклоняется | typed state и exact owner `nextActions`; Session projection allowlisted |
| Incident | read/ack работают только на exact incident graph | retry/release/close специализированы; terminal/expiry/stale version закрываются | typed display/severity/history и exact owner `nextActions` |
| Backup/restore | create назначает immutable scope/source/operation | cancel требует terminal reason; retry запрещает reason, закрывает previous generation и создаёт fresh attempt | safe backup и typed restore state/`nextActions` без gateway-derived affordances |
| Realtime | subscribe проходит ту же Origin/session/CSRF owner boundary | lower version/sequence и `complete=false` никогда не replace-ят current UI state; overflow закрывает connection без partial snapshot | versioned `complete=true` snapshot, reconnect = fresh read/rejoin |

| Часть | Исполняемая материализация |
| --- | --- |
| Producer profile | существующий `control-plane.oidc`: OIDC bearer metadata, resolver exact mTLS SPIFFE, server-resolved actor/tenant/project/ownership и durable owner-session fence |
| Client operation profile | Закрытые registries `controlplaneclient.ControlAPIGatewayOperations` и `internal/clients/owner`: только materialized control-plane, interaction team и integration management methods, без broad lifecycle permissions |
| Generated adapters | OpenAPI std HTTP server/models, named structural AsyncAPI Go models, strict WebSocket JSON adapter и generated control-plane/interaction/integration gRPC clients |
| Error adapter | control-plane и Agent bot принимаются только с согласованным typed v1 detail; bare status legacy Team/integration нормализуется только для exact зарегистрированного full method в закрытую матрицу GUIDE-DOC-005; неизвестный method/code/detail даёт `500 INTERNAL`, а private downstream message не выходит наружу |
| Consumer effect | typed REST page либо connection-local atomic complete replace-snapshot; browser не подтверждает domain effect и не влияет на state owner |
| Readiness | OIDC/session/TLS state прочитаны; local served material разрешён read-only `CheckGatewayPublicTLS` как APPLIED, PENDING до confirm recovery либо неистёкший PREVIOUS; control-plane, interaction Team, interaction bot catalog и integration clients проходят тот же local issuer/application proof и exact generated working path; loopback TLS readback сверяет exact peer digest/expiry и не продвигает watermark |
| Deploy ownership | Dockerfile, Kustomize base/overlays, Service/Ingress TLS passthrough, issuer component, Vault Secrets Operator, exact NetworkPolicy, PDB, metrics/dashboard/alerts |
| Failure policy | startup fail-closed; readiness снимается при protected RPC/TLS mismatch; HTTP/WS unknown error detail → `INTERNAL`; admission останавливается, tracked WebSocket закрываются параллельно и force-close/join подчиняется shutdown budget до client/OIDC/telemetry shutdown |

## Security boundary

- public HTTP использует только TLS 1.3; cookies имеют `__Host-`, `Secure`,
  `SameSite=Strict`, session cookie дополнительно `HttpOnly`;
- session envelope зашифрован AES-256-GCM, ограничен по размеру и сроку;
  bearer никогда не логируется и не сохраняется вне encrypted cookie;
- OIDC HTTP client запрещает proxy/redirect, принимает только pinned HTTPS
  origin, exact SNI и CA;
- все три internal clients используют exact SNI/CA, одну workload client
  certificate, application proof, resolver proof, local issuer и internal
  authorization context;
- `mTLS` не заменяет OIDC/session, permission, owner resolution или replay
  protection;
- metric `route/status/channel/outcome` нормализованы закрытыми множествами;
  IDs, paths, actions и внешние значения не являются labels;
- configuration projection принимает только фактические control-plane audit
  actions `create|update|transition|delete|update_project|delete_project_*|detach_access_configuration|copy_access_configuration|create_schedule|manage_schedule_*`;
- backup/restore projections не содержат DSN, archive reference/object key,
  credential/PVC tuple, verifier/cleanup evidence, application bearer или
  worker grant; restore transport принимает только locator, OCC и safe digests;
- audit/diagnostics не содержат bearer, cookie, CSRF, key, DSN или secret value.

## Конфигурация и secret delivery

Все `CONTROL_API_GATEWAY_*_FILE` — абсолютные regular files без разрешений
`other`. Значения не входят в manifest, README, логи или ошибки. Vault Secrets
Operator одним versioned Secret атомарно материализует public TLS cert/key/CA
и forward-only TLS generation/digests,
current/previous session keys и readiness application grant в Kubernetes
Secrets с rollout restart. Уже
принятый `internal-rpc-authority-control-api-gateway-issuer` component
доставляет отдельную control-plane workload identity и trust material;
gateway монтирует их только для чтения и не создаёт параллельный auth
primitive. CA доставляются отдельными ConfigMap. Public TLS и client
certificate имеют bounded TTL; session key rotation forward-only использует
двухшаговый current+previous rollout overlap; deployed profile требует оба
key-файла.

NetworkPolicy разрешает только ingress controller, Prometheus, DNS,
control-plane, interaction-gateway, integration-gateway, identity SSO, Vault,
OTel, Sentry и точные issuer component destinations. Правил только по порту,
wildcard egress, plaintext fallback и `skipTLSVerify` нет.

## Ручная проверка Issue #237

На локальном или отдельно разрешённом staging-профиле с owner session и
заранее подготовленными authoritative данными:

1. Пройти Team catalog → create → provider readback → link → relink → unlink.
   Проверить selector из catalog, `ETag`, mapping generation и durable operation
   readback; внутренние provider ID вручную не вводить.
2. Создать RoleDefinition и Agent, выбрать runtime по server-authored catalog,
   затем создать/bind/rebind/revoke Mattermost bot identity и назначить/снять
   AgentAssignment. Для
   InstructionSet пройти edit → validate → publish → history → compare →
   rollback, а Git-owned вариант менять только через detach/copy. Повторить
   stale `If-Match` и убедиться в `412` без изменения owner state.
3. Пройти provider catalog → authorization → status/new-code → masked account →
   pool policy/weights → reauthorize/revoke. Проверить отсутствие token,
   credential, raw provider payload и secret input во всех JSON и логах.
4. Выбрать IntegrationDefinition, настроить connection/capabilities, выполнить
   bounded test и принять/отклонить ApprovalRequest. Preview должен содержать
   только summary и имена полей, но не их значения.
5. Создать Schedule по preset с inline prompt без advanced tuple, затем по safe
   Artifact selector и выполнить update current configuration. Проверить
   server defaults/effective values, list/get readback, отсутствие UUID/owner/project
   во вводе и новый `ETag`.
6. Для Run проверить list/detail/timeline/lineage/artifacts, затем отдельно
   cancel и retry. Для Incident проверить list/detail/history и допустимые
   acknowledge/retry/release/close. Состояния и `nextActions` берутся из RPC;
   gateway сохраняет display metadata без вычисления и UUID joins.
7. Для Workspace и All Workspaces backup/restore пройти list/get/create,
   cancel/retry/readback. Проверить рост version/generation, fresh attempt при
   retry и отсутствие DSN, object key, private evidence или grant.
8. Получить bounded health, diagnostics, source detail, configuration diff и
   audit CSV. CSV должен быть полным либо запрос должен закрыто завершиться до
   выдачи заголовков; ни один результат не содержит session/security material.
9. Подписаться двумя запросами на все десять WebSocket channels, не более
   восьми за один запрос. Каждый replace должен иметь `complete=true`,
   возрастающий sequence и typed version/digest; после reconnect выполняется
   fresh authoritative snapshot. Частичный/overflow snapshot должен дать
   bounded problem и закрыть connection.
10. Снять readiness interaction либо integration owner RPC и убедиться, что
    `/readyz` становится false тем же generated client/application authority
    path, который обслуживает рабочие запросы; после восстановления перечитать
    текущие authoritative projections.

Фактические provider effects, cancel/retry и backup/restore выполняются только
в отдельно разрешённом окружении; этот список не разрешает production action.

## Rollback

Приложение и source/generated contracts можно вернуть на предшествующий image
только если он совместим с уже опубликованными owner RPC и текущей authority
revision. Authority policy нельзя откатывать на меньшую ревизию. Созданные Team,
provider, integration, schedule, run/incident и backup/restore operations не
удаляются: их lifecycle остаётся у соответствующего owner RPC и закрывается
штатным terminal/cancel/expiry переходом.

## Локальная проверка

```bash
make gen-control-api-gateway-openapi-go
make lint-control-api-gateway-asyncapi
make gen-control-api-gateway-asyncapi
(cd services/external/control-api-gateway && go test -run '^$' ./...)
kubectl kustomize deploy/k8s/overlays/staging/control-api-gateway
kubectl kustomize deploy/k8s/overlays/production/control-api-gateway
```

Фактические OIDC/Vault/control-plane/Kubernetes и staging проверки требуют
отдельного разрешения. Поддерживаемый integration/E2E/deploy/lifecycle контур
отложен в [Issue #216](https://github.com/codex-k8s/matter-codex/issues/216).

## Проверенная актуальная документация

Context7 вызван для `coreos/go-oidc`, `coder/websocket` и `oapi-codegen`, но
вернул `Monthly quota exceeded`; документация через Context7 была недоступна.
Проверены официальные первичные источники:

- [go-oidc](https://github.com/coreos/go-oidc) — provider discovery,
  `IDTokenVerifier`, issuer/audience/signature/time verification;
- [coder/websocket](https://pkg.go.dev/github.com/coder/websocket) — exact
  origin patterns, subprotocol, read limit, ping и bounded context I/O;
- [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) — v2 config,
  models и `std-http-server` generation;
- [Redocly CLI](https://redocly.com/docs/cli/commands/lint/) — source OpenAPI
  validation с recommended rules;
- [AsyncAPI CLI 6.0.2](https://github.com/asyncapi/cli/tree/v6.0.2) и
  [CLI usage](https://www.asyncapi.com/docs/tools/cli/usage) — `validate`,
  `generate models golang`, `--goIncludeComments`/`--goIncludeTags`;
- [Modelina 4.4.3 Go](https://github.com/asyncapi/modelina/blob/v4.4.3/docs/languages/Go.md)
  и [name interpretation](https://github.com/asyncapi/modelina/blob/v4.4.3/src/interpreter/Utils.ts) —
  JSON tags/codecs и стабильное имя из `title`/`$id`;
- [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/),
  [Kustomize](https://kubectl.docs.kubernetes.io/references/kustomize/) и
  [Secrets Store CSI Driver](https://secrets-store-csi-driver.sigs.k8s.io/);
- [Vault Secrets Operator](https://developer.hashicorp.com/vault/docs/deploy/kubernetes/vso)
  и [Vault PKI issue API](https://developer.hashicorp.com/vault/api-docs/secret/pki#generate-certificate-and-key).

Эксплуатация описана в
[`docs/runbooks/control-api-gateway.md`](../../../docs/runbooks/control-api-gateway.md).
