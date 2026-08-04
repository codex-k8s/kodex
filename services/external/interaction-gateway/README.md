---
id: SERVICE-DOC-INTERACTION-GATEWAY
title: Interaction gateway
type: service
status: approved
owner: manager
version: 1.1.0
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
  delivery-scoped bearer credential;
- `/livez`, `/readyz`, `/metrics` на отдельном техническом listener.

Перед созданием Session/Turn gateway перечитывает Post, Channel, Team и User
через Mattermost REST. `Team=Project`, `Channel=Chat`, actor, role, locale и bot
выбираются из environment-owned Vault manifest. Явный `@mention` разрешается
только через Mattermost User readback и точную assignment из manifest;
неизвестное, неоднозначное или неразрешённое назначение закрыто отклоняется.
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
| Owner result/progress | control-plane владеет Turn version/state/outcome и Artifact lineage | durable turn watch → version-pinned `GetResource(Turn/Artifact)` → update исходной run card либо artifact/status/incident delivery | watch связан с inbound/session/turn; delivery — с attempt/input/artifact digest | watch продвигается только после durable enqueue; create/update/upload получают отдельные receipts и exact Mattermost readback |
| Card delivery | Gateway-owned delivery с tenant/project/session/turn/input lineage | delivery claim → Mattermost upload/post → provider receipt → readback | PostgreSQL lease owner/token/fence; server-owned либо deterministic delivery ID | `PENDING → DELIVERING → PROVIDER_ACCEPTED → DELIVERED`; ambiguous HTTP сверяется по `PendingPostId`, client-owned props, root, bot и файлам |
| Owner decision | Только mapped recipient; post/channel/team и HMAC callback повторно сверяются | claim gate → безопасная публикация доступного S3 result → owner card → `RecordOwnerGateDelivery` → action/dialog/reaction → `ResolveOwnerGate` | server-owned delivery ID/payload digest, control-plane claim token/fence/version + event JWS | control-plane атомарно переводит Gate/Process/Turn; gateway inbound становится `COMPLETED` |
| Channel/Thread delete/restore | lifecycle actor берётся только из server-owned manifest, Channel/Post перечитываются через Mattermost | delete → exact Chat/Session transition → `WAITING_CLEANUP`; restore → cancel cleanup + `ACTIVE`; expiry → `FINALIZE` | provider event digest + interaction lifecycle operation profile + owner version | в `DELETION_PENDING` новые инструкции и callbacks запрещены; cleanup отложен на 24 часа и отменяем restore |
| Restart/reconnect | Нет нового actor | REST catch-up постов от durable create cursor и реакций незавершённых owner cards перед каждым WebSocket connect | стабильный provider event ID + immutable digest | duplicate без effect; конфликт digest закрыто отклоняется |

Для локальных terminal delivery отдельный domain event не объявлен: точный
авторитетный путь — защищённый delivery readback. OwnerGate terminal принадлежит
control-plane и читается через его version-pinned RPC. AsyncAPI не расширяется
несуществующим consumer: утверждённый control-plane contract пока не содержит
общего события статуса взаимодействия.

## Lifecycle matrix

| Объект | Create/claim | Renew/retry | Complete/terminal | Cancel/delete/restart |
|---|---|---|---|---|
| Inbound | unique provider event + digest; DB-time lease/token/fence/generation | expired `PROCESSING` reclaim, bounded backoff; scan poll не расходует terminal attempt budget | `COMPLETED`, `IGNORED` либо `FAILED` с durable semantic response/error/next action | restart продолжает `PENDING/WAITING_SCAN/WAITING_CLEANUP`; stale worker не сохраняет результат |
| Artifact input | S3 key из project/event/digest; metadata readback | повторный put допускается только при exact size/type/digest | только control-plane `CLEAN` разрешает Turn; reject terminal | gateway не удаляет object/Artifact; retention policy authoritative |
| Delivery | durable payload snapshot, tenant/session lineage и client-owned identity props | claim выдаёт lease owner/token/fence; bounded operation deadline строго меньше lease; `PROVIDER_ACCEPTED` удерживает ACK lease до expiry и имеет отдельный budget | каждый upload и create/update Post получает checkpoint/readback; затем `DELIVERED` или `DEAD_LETTER` | restart не повторяет подтверждённые uploads; ambiguous Post ищется по deterministic identity и exact payload/files |
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
до открытия barrier. Каждый statement с business DML выполняется внутри
HMAC-signed transaction context, связанного с immutable `session_user`,
credential generation и точными organization/project; `FORCE RLS` закрывает
inbound, cursor, delivery, upload receipt и turn watch. Worker discovery читает
только scope indexes без payload. Startup barrier внутри rollback-only
транзакции доказывает разрешённый scoped cursor DML и отказ cross-tenant
`WITH CHECK`. Migration job сверяет forward-only
high-watermark и context-key digest; rollback поколения и resurrection retired
identity запрещены. Конфигурация принимает только согласованную пару
`interaction_gateway_runtime_g<N>`/generation, поэтому следующее поколение
вводится тем же code-first render без возврата к `g1`. OwnerGate claim token шифруется AES-GCM
gateway-owned ключом и не возвращается readback API.
Result-файл публикуется только для exact project-prefixed объекта
на настроенном bucket после проверки size/type/SHA-256 metadata; другой
авторитетный result reference остаётся digest-only и не раскрывается в карточке.
Чистый result до лимита Mattermost публикуется как проверенный upload; больший
result получает tenant/project/time-scoped HTTPS presigned readback на 15 минут
с именем, размером и SHA-256, без ослабления scan boundary.

Mattermost event keyset хранится в control-plane как versioned
`CURRENT/NEXT/PREVIOUS/RETIRED`: durable high-watermark переживает restart,
разрешает bounded overlap и запрещает rollback или resurrection retired key.

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

1. Опубликовать environment-owned mapping в указанном staging/production Vault
   path и настроить названные Vault keys,
   workload certificates, CA ConfigMaps и immutable image digests.
2. Выполнить migration job; убедиться, что `/readyz` становится `204` только
   после готовности exact PostgreSQL/control-plane/Mattermost/S3 paths.
3. Отправить `/codex` и Post с файлом: до `CLEAN` ожидается artifact card, после
   него — ровно один Turn и одна run card.
4. Повторить тот же callback/event: новые Session, Turn и Post не создаются.
5. Оборвать HTTP-ответ Mattermost после принятия Post и перезапустить Pod:
   delivery восстанавливается по `PendingPostId` и receipt.
6. Проверить approve/reject/changes/cancel кнопкой, dialog и reaction от
   назначенного owner; чужой actor, post или channel получает закрытый отказ.
7. Остановить Mattermost/S3/control-plane: readiness становится false, а
   durable записи остаются для retry/dead-letter/readback.
8. Удалить Channel/Thread, убедиться в `DELETION_PENDING`, запрете новой
   инструкции и callback; восстановить до 24 часов и проверить отмену cleanup.
9. Проверить result больше 64 MiB: карточка содержит bounded protected HTTPS
   link с метаданными, а delivery не переходит в deterministic dead-letter.

Rollback описан в [runbook](../../../docs/runbooks/interaction-gateway.md).
