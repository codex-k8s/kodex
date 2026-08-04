---
id: SERVICE-DOC-INTERACTION-GATEWAY
title: Interaction gateway
type: service
status: approved
owner: manager
version: 1.2.0
updated: 2026-08-04
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
- `GET /mattermost/v1/artifacts/{grantId}/content` — одноразовая выдача
  private S3 object после аутентификации Mattermost User и повторного readback
  actor/Team/Channel membership;
- `/livez`, `/readyz`, `/metrics` на отдельном техническом listener.

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
