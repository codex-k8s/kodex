---
id: OPS-MAILBOX-OWNER-LIFECYCLE-1046
title: Полный owner lifecycle типизированной конфигурации mailbox
type: operations
status: approved
owner: developer
version: 1.0.0
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
