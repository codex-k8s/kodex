---
id: SERVICE-DOC-INTERACTION-GATEWAY
title: Interaction gateway
type: service
status: approved
owner: manager
version: 1.4.0
updated: 2026-08-06
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
  actor/Team/Channel membership;
- `/livez`, `/readyz`, `/metrics` на отдельном техническом listener.

## Contract-first матрица Workspace↔Mattermost Team

Защищённый внутренний интерфейс принадлежит `interaction-gateway` и состоит
только из специализированных RPC `ListMattermostTeams`,
`CreateMattermostTeam`, `LinkMattermostTeam`,
`GetMattermostTeamBinding`, `RelinkMattermostTeam`,
`UnlinkMattermostTeam` и `CheckMattermostTeamReadiness`. Универсальный
provider proxy, передача raw Mattermost Team ID и изменение mapping через
существующий inbound OpenAPI запрещены.

Actor, organization и project отсутствуют в request DTO. Их передаёт только
проверенный application authorization context от `internal-rpc-authority`:
точные issuer/audience, caller workload и SPIFFE ID, полный gRPC method,
permission, JTI/replay reservation и server-resolved actor/tenant/project.
Обычный Mattermost bearer, mTLS, opaque Team ref, workspace locator и любое
поле payload по отдельности полномочий не дают.

| Сценарий | Будущий owner endpoint #237 | Internal RPC и permission | Mattermost effect/readback | Control-plane #234, OCC и receipt | Durable результат, event/read path и readiness |
| --- | --- | --- | --- | --- | --- |
| Catalog | `GET /workspaces/{workspace}/mattermost/teams` | `ListMattermostTeams`, `interaction.team.catalog.read`; actor/org/project только из verified context | server-resolved Mattermost User; `GetTeamsForUser`, затем для каждой выдаваемой Team `GetTeam` и `GetTeamMember`; page не больше 200 | owner eligibility project проверяется точным защищённым query #234 до provider выдачи; mutation/receipt отсутствуют | bounded safe page: display name, slug, masked status, timestamps и opaque selector; cursor и selector принадлежат серверу, сырые Team ID отсутствуют; authoritative read path — fresh provider readback |
| Create | `POST /workspaces/{workspace}/mattermost-team` | `CreateMattermostTeam`, `interaction.team.create`; request содержит только display name, slug intent и idempotency key | до `CreateTeam` фиксируется intent; после ответа или возможного эффекта выполняются `GetTeamByName`, exact digest/readback и membership check | после exact provider proof вызывается специализированный bind #234 с server-resolved project, expected mapping version, тем же semantic scope и provider receipt digest | `PENDING -> PROVIDER_ACCEPTED -> BOUND|REPAIR_REQUIRED`; state/checkpoint/receipt сохраняются до/после каждого effect; event interaction-gateway не публикует, итог читается этим RPC и `GetMattermostTeamBinding`; readiness не создаёт Team |
| Link existing | `PUT /workspaces/{workspace}/mattermost-team` | `LinkMattermostTeam`, `interaction.team.link`; caller передаёт только opaque selector, expected mapping version и idempotency key | selector разрешается из durable actor/org/project scope; gateway заново читает `GetTeam` и `GetTeamMember` | специализированный bind #234 проверяет authoritative owner и весь dependent graph до OCC; gateway не копирует graph rules | `PENDING -> PROVIDER_ACCEPTED -> BOUND|REPAIR_REQUIRED`; receipt хранит безопасные digests и version, raw provider payload отсутствует; configuration event принадлежит #234 |
| Get/readback | `GET /workspaces/{workspace}/mattermost-team` | `GetMattermostTeamBinding`, `interaction.team.binding.read` | Team ID берётся только из current mapping #234, затем `GetTeam` и actor `GetTeamMember`; deleted/forbidden/lost membership fail closed | versioned authoritative mapping read #234; hidden/foreign Workspace неотличим от not found | joined snapshot связывает mapping generation/version/digest с fresh masked provider status; отдельного события нет; этот RPC является защищённым authoritative read path |
| Relink | `POST /workspaces/{workspace}/mattermost-team/relink` | `RelinkMattermostTeam`, `interaction.team.relink`; opaque selector, expected version, idempotency key | fresh Team и membership readback до owner transition | специализированный relink #234 блокирует полный Chat/Session/Turn/delivery graph, закрывает прежнюю generation и создаёт монотонную новую | gateway сохраняет provider proof/receipt; stale/revoked generation не обслуживается inbound/delivery; event и audit принадлежат owner transaction #234 |
| Unlink | `DELETE /workspaces/{workspace}/mattermost-team` | `UnlinkMattermostTeam`, `interaction.team.unlink`; expected version и idempotency key | provider mutation отсутствует; перед transition gateway подтверждает current Team status и membership | специализированный unlink #234 блокирует полный graph и закрывает current generation; открытый graph либо stale version отклоняются | provider checkpoint и безопасный authoritative result сохраняются; event и audit принадлежат #234, readback возвращает `UNBOUND`; readiness использует тот же protected owner/provider read path |

Точные имена, поля и generated client control-plane не объявляются локально:
до принятия #234 interaction-owned provider/domain/storage/transport path
фиксирует durable provider proof и возвращает `REPAIR_REQUIRED`, если owner
transition нельзя доказанно завершить. Перед ready SHA ветка обязана получить
принятый source Proto #234 из нового `origin/main`, подключить его generated
client и заменить этот dependency checkpoint точными method/field/permission
профилями без production mock или альтернативного owner contract.

### State/effect matrix

| Operation/outcome | Provider effect | Durable transition и recovery | Owner transition | Безопасный результат |
| --- | --- | --- | --- | --- |
| catalog success | только bounded read | opaque selectors/cursor имеют TTL, actor/org/project scope и provider digest | отсутствует | safe page без raw Team ID и private payload |
| catalog forbidden/deleted/unknown | только read | selector не создаётся; bounded error code | отсутствует | hidden/not found либо permission denied без provider diagnostics |
| create accepted | одна `CreateTeam` после durable `PENDING` | exact Team/slug/request digest фиксирует `PROVIDER_ACCEPTED`; raw response не хранится | bind #234, затем `BOUND` | masked Team snapshot, mapping version/generation |
| create conflict before effect | `CreateTeam` не принят | exact pre-existing `GetTeamByName` не присваивается операции; `REPAIR_REQUIRED` либо safe conflict | отсутствует | conflict без чужой Team identity |
| create timeout/connection loss | effect неизвестен | слепой второй POST запрещён; worker использует checkpoint и exact `GetTeamByName`/digest/time fence; доказанный собственный effect -> `PROVIDER_ACCEPTED`, доказанно отсутствующий effect -> bounded retry, иначе `REPAIR_REQUIRED` | только после доказанного readback | повтор того же key/hash возвращает сохранённое состояние; другой hash конфликтует |
| link/relink accepted | provider mutation отсутствует, только readback | `PENDING -> PROVIDER_ACCEPTED`; selector повторно связан с actor/org/project и fresh membership | bind/relink #234 с OCC и full graph gate | current version/generation/digest |
| lost membership/provider forbidden/deleted | только readback | operation terminal `REPAIR_REQUIRED`; receipt не подменяется synthetic provider state | owner transition не вызывается | hidden/not found или safe precondition error |
| unlink accepted | provider mutation отсутствует | current provider proof/checkpoint фиксируется до owner command | unlink #234 закрывает generation после graph gate | `UNBOUND` и новая mapping version |
| stale expected version/open graph | provider mutation отсутствует | provider proof может быть сохранён как завершённый read, но не как mapping success | #234 возвращает conflict/failed precondition | current safe readback без raw graph/provider data |
| worker restart/lease expiry | новый внешний effect до startup barrier запрещён | DB-time lease/fence, bounded retry; cancel/join до закрытия PostgreSQL и Mattermost transport | повтор owner command только с тем же semantic idempotency key | сохранённый receipt либо `REPAIR_REQUIRED` без дубля |

`interaction-gateway` не публикует domain event для этого lifecycle: provider
receipt является transport evidence, а авторитетный mapping, audit и
configuration event принадлежат одной PostgreSQL-транзакции `control-plane`
из #234. Отсутствие event компенсируют защищённые
`GetMattermostTeamBinding` и exact provider readback. Environment-owned
manifest остаётся immutable runtime admission/projection и не становится
редактируемым источником истины; active mapping generation всегда читается у
owner, а её отсутствие, отзыв или mismatch закрыто блокируют inbound и
delivery.

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
в репозитории указаны имена путей/ключей. Начальный environment render использует
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

Rollback описан в [runbook](../../../docs/runbooks/interaction-gateway.md).
