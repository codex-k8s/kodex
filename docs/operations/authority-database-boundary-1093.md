---
id: OPS-AUTHORITY-DATABASE-1093
title: PostgreSQL boundary issuer и verifier
type: operations
status: approved
owner: backend
version: 1.0.0
updated: 2026-09-06
---

# Причина и граница

Refs #1093, #1059, #1031, #1018. Общая capability DB-роль не доказывает
владение строкой. Неизменяемый `session_user` разрешается в owner-owned реестре
точного workload, capability, generation и lifecycle. `SET ROLE`, произвольный
GUC и совпадение имени payload не создают identity. Application mTLS/JWS,
exact actor/permission/replay проверки сохраняются перед PostgreSQL adapter.

| Сценарий | Владелец и команда | Авторитетный результат / отказ |
| --- | --- | --- |
| Fresh / upgrade | Migrator применяет прежний baseline и новую forward-only migration | EMAIL LOGIN и 16 runtime identities доступны в обоих случаях; опубликованные байты baseline сохранены. |
| Issuer reserve | Verified proof → repository Reserve → exact caller-workload RLS | Proof watermark продвигается монотонно; reservation уникален по workload/JTI. Прямой SQL не откатывает watermark. |
| Verifier accept | Verified context/snapshot → AcceptVerification → snapshot и replay одна transaction | Snapshot требует свежий независимый receipt и совпадение всех revisions с owner history; другой workload закрыто отклонён. |
| Snapshot activation / restart | Same workload → свежий receipt того же snapshot | Direct INSERT/UPDATE проходит те же attestation и monotonic guards. Смена replica receipt не требует удаления watermark. |
| Continuation | Accepted parent replay → issuer того же workload → child reservation | Issuer читает только собственный target replay; чужой parent не создаёт child. |
| Cleanup / replay | Exact identity → DeleteExpired | Reservation неизменяем; удаление возможно только после expiry и обязательного десятиминутного retention. Caller cutoff не сокращает этот минимум. |
| Readiness / restore fence | Рабочий LOGIN → own snapshot и write probe | Те же RLS/trigger guards, недоступный реестр или закрытый restore fence не превращаются в готовность. |
| Retirement | Только owner forward migration | CURRENT → RETIRED необратим; прежний LOGIN теряет RLS/write access и в уже открытой сессии. Миграция также выполняет exact NOLOGIN, REVOKE membership, termination и независимый readback перед завершением retirement. Runtime не получает lifecycle DDL. |

Новых public endpoints или business events нет. Авторитетное состояние —
PostgreSQL receipt/watermark и owner registry. Runtime не получает SELECT/DML
реестра, SECURITY DEFINER использует фиксированный search_path; ACL функций
закрывает PUBLIC. Табличные права остаются ограничены RLS и обязательными
триггерами, поэтому прямой SQL не обходит identity/attestation/monotonic/retention.
`TRUNCATE`, изменение trigger/RLS и создание SECURITY DEFINER runtime запрещены.

Проверка: `make test-internal-rpc-authority-postgres`, service race/vet/build,
authority render и общий baseline нового exact SHA. Реальные LOGIN проверяют
own/cross-workload writes, reads, revision rollback, replay deletion,
caller GUC, retirement существующей сессии и миграцию baseline → upgrade → up.
Evidence выполненных проверок привязывается к точному SHA в PR #1060;
исторический baseline e1e8 не подтверждает исправление. Live acceptance —
**NOT RUN**, она выполняется отдельно после общего review и owner gate.

Context7: PostgreSQL18 `/websites/postgresql_18`, `CREATE POLICY`, FORCE RLS,
`session_user`/`current_user` и безопасный `SECURITY DEFINER search_path`.
Rollback не удаляет durable state и не оживляет retired identity: только
совместимая forward migration. Секреты и приватные данные не публикуются.
