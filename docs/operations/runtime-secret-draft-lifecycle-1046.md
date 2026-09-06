---
id: OPS-RUNTIME-SECRET-DRAFT-1046
title: Контракт жизненного цикла зашифрованного черновика Secret
type: operations
status: approved
owner: developer
version: 1.1.0
updated: 2026-09-06
---

# Владение и граница checkpoint

Источники: #1046/#1018, корректирующий broker unit #1068, CFG-03,
GUIDE-DOC-003/006, GO-DOC-001/002/005. Документ описывает реализованный D6 owner
lifecycle: специализированные команды, encrypted draft metadata, fenced
completion, публикацию и impact/recovery. Ранний source-only checkpoint был
историческим этапом; текущая реализация включает PostgreSQL owner и проверки.

Browser → HTTP gateway с OIDC/CSRF → специализированный CP prepare →
одноразовый server grant → Secret Broker с plaintext только в его запросе →
CP consume/complete. CP владеет draft metadata, авторизацией, OCC, generation,
attempt/fence, idempotency, activation и аудитом. Broker владеет ciphertext,
keyring и Kubernetes apply/readback. Ключи, plaintext, ciphertext и value digest
не выдаются в публичной metadata. `operation_grant` не покидает gateway.

Существующий Secret revision owner остаётся CP. Только успешная fenced
PUBLISH completion может добавить immutable revision и активировать её.
Существующие impact/rebind проверяют эту опубликованную revision; они не
изменяют сохранённые execution snapshots.

| Переход | Полномочия/ограждение | Атомарный результат и восстановление |
| --- | --- | --- |
| SAVE prepare | OIDC actor/tenant, secret.create для нового либо secret.rotate для существующего; existing owner до OCC; fresh auth; idempotency exact intent | server secret/draft refs, новая generation, PREPARING, bounded grant, receipt/audit; значения отсутствуют |
| SAVE consume | exact secret-broker workload, одноразовый grant, actor/tenant/source актуальны | одна CLAIMED attempt с claimant/generation/lease; ciphertext locator назначен CP |
| SAVE complete | та же lease/fence, exact draft, immutable encrypted descriptor, key ID/generation >0 | DRAFT и descriptor, version++, receipt/audit; public GetRuntimeSecretDraft — authoritative read |
| VALIDATE prepare/consume | fresh actor/owner, exact draft version, не terminal/expired, новый grant/claim | прежний encrypted snapshot закреплён; никакой повторной передачи значения |
| VALIDATE complete | broker decrypt+format/size validation и exact UID/RV/digest readback; тот же fence | VALID, version++, receipt/audit; ошибка не создаёт VALID |
| PUBLISH prepare/consume | fresh actor/owner, VALID, draft OCC и отдельная expected_secret_version, новый grant/claim | PUBLISHING, уникальная server target_revision; encrypted descriptor неизменяем |
| Impact prepare | fresh exact draft owner, VALID draft OCC, current Secret version | immutable actor-bound plan с source revision, target draft/version, exact eligible bindings, permission snapshot и expiry; idempotency replay возвращает тот же plan |
| Impact read | тот же actor/owner, permission проверяется повторно, query/cursor относятся к plan | bounded safe items и итоговые receipts; скрытые bindings не раскрываются после отзыва прав |
| Publish с plan | exact неизрасходованный plan, draft/secret pins и expiry, выбранные item refs принадлежат plan | при activation каждый выбранный consumer повторно проверяет authority/OCC; отдельный receipt APPLIED/CONFLICT/FORBIDDEN, невыбранные остаются прежними; plan терминален |
| Impact cancel/expiry | owner failure/discard/lease expiry закрывают связанный plan вместе с operation | CANCELLED не публикует и не заменяет; неиспользованный plan после expires_at читается EXPIRED; retry создаёт новый plan |
| PUBLISH complete | exact fence, secret OCC, ciphertext/source binding и immutable materialization readback | одна transaction: revision/current pointer, draft PUBLISHED, receipt/audit; execution snapshots прежние |
| DISCARD prepare | fresh owner и draft OCC; publish/terminal conflict закрыто отклоняется | draft DISCARDED и отзыв всех прежних grants/claims до cleanup intent |
| DISCARD consume/complete | только exact cleanup grant для уже terminal draft | broker удаляет ciphertext с UID/RV preconditions; completion фиксирует cleanup, не открывает grant заново |
| Ошибка операции | exact claimant/generation, closed failure code | FAILED operation, grant закрыт; draft не публикуется, сохранённый ciphertext не теряется молча |
| Lease expiry | PostgreSQL time, row lock, весь draft/operation graph | прежняя claim и grant закрыты; внешняя запись без owner completion не активируется |
| Recovery external-before-complete | отдельный broker worker authority, immutable exact descriptor | CP возвращает отдельные KEEP/DELETE для encrypted и published materialization; активная revision всегда KEEP |
| Draft expiry | PostgreSQL time, нет активного publish либо согласованный terminal всей operation | EXPIRED, закрытые grants, durable cleanup; consumer Get показывает terminal |
| Cleanup ACK | exact operation/fence и те же descriptors после UID/RV delete либо authoritative NotFound | CompleteRuntimeSecretDraftCleanup фиксирует cleanup; повтор не удаляет новый Secret с тем же именем |
| Retry/unknown outcome | тот же owner, operation/idempotency key и семантический intent | сохранённый результат либо новый fenced grant только без повторения завершённого эффекта |
| Cancel/deadline | request context ограничивает RPC/SQL | незавершённая transaction откатывается; зафиксированный intent виден recovery; успех не синтезируется |

Событие для каждой строки отсутствует: нет независимого event consumer;
authoritative public read — GetRuntimeSecretDraft/GetRuntimeSecret, worker
rejoin — ListRuntimeSecretDraftRecoveryWork/RecoverRuntimeSecretDraftMaterialization.
Claims не создают Session/Turn/Run/делегирование: эти узлы здесь неприменимы.

AEAD AAD связывает exact draft ref/generation/project/secret/value type/content
digest из CP work response. SAVE создаёт новую immutable generation, VALIDATE
не меняет encrypted bytes, PUBLISH не принимает новое значение, DISCARD не
может воскресить черновик. Broker keyring имеет отдельные current write key и
retained read keys; его доставка/rotation/restart/readback принадлежит #1068.

Public save Mutation.expected_version относится к существующему Secret; для
нового Secret отсутствует. Остальные draft mutations используют expected_version
черновика. PUBLISH дополнительно принимает expected_secret_version. Поля
project/name/description для existing Secret выводятся CP из owner state;
переданный locator не назначает authority.

Материализация: Proto → controlplaneapi → controlplaneclient operation profiles →
authority policy → CP domain/repository/transport → broker adapter/worker/deploy
#1068 → HTTP #1045 → PWA #1022. Runtime Secret draft worker не использует
EMAIL_EFFECT_RECONCILIATION. Public ingress/listener и новый deployable CP не
добавляются. Готовность CP проверяет собственную schema/state; broker проверяет
собственные ключи/Kubernetes, полный protected path проверяется отдельным smoke.

CP хранит назначенный сервером staging namespace `kodex-secret-drafts` отдельно
от runtime namespace `kodex-runtime`; encrypted data key — `ciphertext`,
published data key — `value`. Readiness проверяет таблицы, sequence и уникальный
индекс активной операции. Policy revision 58 включает отдельный OIDC producer
для вызовов Secret Broker с точным target workload. Все D6 work tuples связаны
через `UNARY_PROTO_SHA256`; их resource/version/attempt metadata запрещены.

Legacy scan опубликованных Kubernetes Secrets разрешает D6 operation через
тот же CP owner. Exact descriptor сохраняется, пока revision нужна активному
Secret/environment/binding/Run. После revoke либо освобождения последнего
потребителя owner разрешает DELETE и retire. Неизвестная operation не считается
разрешением удалить ресурс. До завершения claim materialization остаётся KEEP;
после истечения claim без completion она удаляется, а не активируется.

SAVE, VALIDATE, PUBLISH и DISCARD не создают domain event: каждый переход имеет
атомарные state/receipt/audit и authoritative `GetRuntimeSecretDraft`.
Опубликованный Secret дополнительно доступен через существующий Secret read.
FAIL и expiry не создают event; read path — draft и bounded recovery work.
Cleanup не создаёт event; его exact UID/RV intention и ACK находятся в owner
operation, повторный read выполняет broker recovery.
Impact prepare/read не создают event; immutable plan и per-item receipts читаются
по отдельному protected plan read. Успешная замена Environment использует его
существующий platform event/outbox; конфликт/отзыв прав не создаёт событие замены.

Локальный CP checkpoint проверяет save/reissue/consume/validate/publish,
неверный encrypted descriptor, stale completion, потерю cleanup ACK,
монотонный target revision после orphan, discard, expiry всех active grants,
fresh authentication и общий legacy/D6 recovery после revoke.
Prepublish impact ограничен 1000 items, каждый принадлежит server plan.
Plan.total — immutable исходное число; paged response.total учитывает текущую
eligibility и поиск. Cursor связывает actor, plan digest/state и query; terminal
переход требует новой первой страницы. APPLIED item содержит новую Environment
revision, а для Agent также тот же binding ref с большей version. Остальные
outcomes не содержат result refs. Публикация без выбранных items сохраняет
bindings и отмечает их NOT_SELECTED. Legacy и D6 используют общий монотонный
диапазон target revisions, включая неубранные orphan attempts.
Broker apply/readback, HTTP/PWA и live подтверждаются только соответствующими
unit checkpoints. Локальные owner fixtures не заменяют общий baseline #1031,
product/security/architecture review и разрешение владельца на merge/deploy.
