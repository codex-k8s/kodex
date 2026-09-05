---
id: OPS-MAILBOX-OWNER-LIFECYCLE-1046
title: Полный owner lifecycle типизированной конфигурации mailbox
type: operations
status: approved
owner: developer
version: 1.1.0
updated: 2026-09-05
---

# Источники и владельцы

Issue #1046/#1018, MVP-UI-41, CFG, GUIDE-DOC-003/006. CP владеет
редактируемой конфигурацией, immutable revisions, привязками credentials,
authoritative accepted projection и публикацией. #1029 владеет общим typed
mail policy producer/render, #1037 — фактическим EMAIL consumer/reload.
Этот документ задаёт матрицу до реализации D5, не объявляет выполнение.

Browser OIDC/CSRF → HTTP #1045 → специализированные CP RPC → domain service →
PostgreSQL owner transaction → existing CP emailprojection reconciler →
Kubernetes policy/network/Secret/Deployment readback → EMAIL protected report →
CP READY. Actor и organization берутся из transport authority; connection
разрешается CP до проверки version/idempotency. Payload не назначает owner,
source, generation, secret values, wildcard destination или readiness.

# Матрица сценариев

`ListEmailMailboxConfigurations.next_actions` разрешает создание первого
черновика только после проверки `integration.manage` точного connection.
`View.next_actions` выводится из того же owner rule и включает закрытые
CREATE_DRAFT/SAVE/VALIDATE/PUBLISH/DISCARD/BIND/UNBIND/DETACH/COPY.
Для enabled-действия причина NONE; закрытый отказ объясняется STATE,
GIT_MANAGED, DELIVERY_PENDING, NO_BINDING либо CONNECTION_DISABLED.
Редактирование неполного UI draft не означает разрешения публикации.
Git-owned документ меняется через DETACH/COPY; PENDING delivery блокирует
новую BIND/UNBIND. Эта проекция не заменяет повторную проверку команды.

| Команда и состояние | Authority и OCC | Результат/ошибка и потребитель |
| --- | --- | --- |
| Create DRAFT | integration.manage exact existing EMAIL connection, idempotency | server configuration/mailbox refs и UI lineage; incomplete typed spec допустима; source/owner поля запрещены |
| Save DRAFT | тот же owner connection, UI-owned set, expected set version, revision принадлежит set | новая immutable DRAFT revision, predecessor DISCARDED; version++; неверный ref NotFound, Git-owned Conflict |
| Parse/preview YAML | exact readable connection; bounded closed schema | typed specification и canonical safe JSON/YAML; неизвестные/credential-value/protected поля InvalidArgument; без записи |
| Validate | owner connection, exact set/revision/version, fresh descriptor read | VALID только после формата, port/TLS/SNI, policy, size и exact credential owner/kind/generation; иначе INVALID diagnostics без значений |
| Publish | UI owner, VALID exact revision и повторная полная проверка | immutable PUBLISHED revision; не меняет runtime connection и не объявляет READY |
| Bind | PUBLISHED revision того же connection, set OCC и connection OCC, exact credential generation | server global publication revision/ref/digest, immutable desired snapshot, PENDING; текущая accepted projection сохраняется до readback |
| Rebind | тот же owner и fences, новый exact published revision | новая global publication; старые execution snapshots не переписываются; concurrent publication сериализуется watermark lock |
| Reconcile PENDING | внутренний CP worker за startup/cancel/join barrier, immutable desired snapshot | bounded DNS snapshots, exact base gateway policy digest; immutable ConfigMap, fixed NetworkPolicy и Deployment pins, Secret projection; ошибка оставляет PENDING с безопасной причиной |
| Kubernetes ACK | тот же publication ref/revision/digest, exact Secret UID/RV, policy digest, workload generation | durable applied receipt; сам по себе не означает READY |
| EMAIL report | exact email-bridge workload mTLS/bearer/proof, revision/digest разрешаются CP по durable publication | stale/unknown digest Conflict; подтверждает фактически принятый consumer snapshot; не доказывает provider health |
| READY | оба receipts и server-selected полный Deployment rollout: observed generation, updated/available replicas, exact template pins | одна owner transaction принимает configuration watermark, binding и safe READY; фактический SMTP/IMAP/POP health остаётся отдельным readiness scenario |
| Retry/restart | persisted desired snapshot и receipts, monotonic generation | повторяет тот же bounded apply/readback; более старый snapshot не откатывает рабочий contour |
| Discard DRAFT | UI owner, exact mutable revision и set version | terminal DISCARDED, опубликованные/bound revisions не удаляются |
| Git startup import | deployment-owned configured source, exact revision/digest, strict typed document | GIT lineage назначает CP; UI не меняет Git-owned revision; replay exact source идемпотентен |
| Detach Git | exact owner/set OCC | отдельный server UI lineage и DRAFT от прежней revision; последующий Git import не перезаписывает detached объект |
| Copy Git | readable source плюс manage target connection, source OCC | новая UI configuration с parent revision; descriptors повторно проверяются по target connection |
| Credential receipt read | exact connection owner и исходный actor/idempotency scope | только safe descriptor/kind/connection version либо NotFound; unknown не становится успехом; value/digest не возвращаются |
| Credential selection | тот же owner rule, bounded search/pagination | immutable descriptors только данной connection; старые bound keys не перезаписываются |
| Connection revoke/delete | существующая terminal owner transaction, точные bindings | новые commands/claims отвергаются, mailbox выключается новой publication; старый grant не даёт полномочий после terminal |

Create/Save/Validate/Publish/Discard используют существующие managed configuration
receipts/audit и authoritative configuration/history read; отдельный domain event
не добавляется. Bind/Rebind/READY изменяют connection projection атомарно с
существующим `INTEGRATION_CONNECTION_CHANGED` и platform event outbox. Reconcile
retry и EMAIL report не создают событие до READY; authoritative read — publication.
Git import, detach/copy и credential lifecycle имеют собственные receipts/audit;
read path не раскрывает значения credentials.

# Сеть и доставка

CP не импортирует internal package другого deployable. Shared mailpolicy получает
typed EMAIL configuration, deployment-owned gateway policy digest и bounded DNS
resolver; выдаёт один document для runtime/CNI/render. Все IP — exact /32 или
/128, только разрешённая SMTP/IMAP/POP port/TLS матрица, exact hostname/SNI.
ConfigMap create ограничивается CP-specific admission; fixed NetworkPolicy и
Deployment updates — narrow resourceNames RBAC. READY не появляется от одного
report одного pod при незавершённом rollout остальных replicas.

Проверки должны покрывать CRUD/round-trip, чужой connection, OCC/replay,
credential mismatch, Git copy/detach, interrupted apply/restart, stale reports,
partial replica rollout, terminal connection и immutable key retention.
До исполнения соответствующие checks имеют статус NOT RUN.

# Реализованный owner checkpoint

Специализированные typed RPC подключены к CP domain/repository owner; неполные
черновики сохраняют canonical JSON, YAML/JSON parser закрыто отклоняет неизвестные
и повторяющиеся поля, narrowing и integer overflow. Credential receipt read не
возвращает digest, locator, UID/resourceVersion или значение credential.

Миграции `625`–`627` сохраняют mapping configuration→connection, immutable
publication snapshot, fenced claim, source receipts Git и точные binding effects.
CP worker использует общий DNS resolver из `eaf23ce2` и mailpolicy producer из
`5d8b1770`. Disabled mailbox не создаёт DNS queries или разрешённых destinations.
CP проверяет фактический admission, базовый gateway digest, путь read-only mount,
Secret/policy pins и rollout обоих Deployment. Callback принимается после apply,
до READY; CP readiness не создаёт цикл ожидания с EMAIL callback.

Git import читает configured файл при startup и в каждом bounded reconcile.
Глобальный runtime revision назначает CP отдельно от revision/digest источника.
Повтор source идемпотентен, старый revision или новый content того же revision
отклоняется. Неизвестный/ошибочный новый source не отменяет recovery ранее
одобренной publication. Несколько connection фиксируются одной publication и
атомарным набором binding effects. Удаление mailbox из Git снимает binding после
полного readback. Detach/copy сохраняют parent revision; Git не перезаписывает
detached set или connection с UI-owned binding.

Истечение publication или изменение connection переводит прежнее намерение в
FAILED/SUPERSEDED и атомарно создаёт компенсацию с большим server revision.
Компенсация использует accepted snapshot и удаляет потерявшие полномочия
connections; прежний Secret revision не переиздаётся. Внутренний RECOVERY не
маскирует публичный FAILED/SUPERSEDED результат пользовательского намерения.
READY, recovery и Git import имеют audit; binding effects и platform events
фиксируются одной owner transaction.

Проверяемые сценарии: full owner CRUD/OCC/replay/credentials, Git import/replay/
delete/detach, callback до apply, READY до callback, competing claim, revoked и
expired publication; Kubernetes admission rejection до mutation, partial replicas,
неверный CIDR и rollback; app partial-effect ordering, Git reload и cancel/join.
Это локальный producer checkpoint. Сквозной HTTP/PWA→реальные EMAIL/egress,
развёртывание и provider health не объявляются проверенными этими fixtures.

Полный Issue #1046 остаётся открытым: runtime catalog/TOML contribution,
effective actor∩agent capabilities, VFS eligibility, Home gate query/total,
RoleImage/IntegrationDefinition CFG и остальные исходные producer gaps требуют
отдельного завершения в том же unit PR. READY доставки не означает SMTP/IMAP/POP
provider readiness и не заменяет общий baseline/triple review #1031.
