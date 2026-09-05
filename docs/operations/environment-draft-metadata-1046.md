---
id: OPS-DOC-1046-ENVIRONMENT-DRAFT-METADATA
title: Сохранение черновика RuntimeEnvironment и исходной ревизии
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Owner metadata черновика

Источник: #1046, Epic #1018, MVP-UI-46 в исходном документе
`12-automations-environments-and-secrets.md`, строки 67–75.
Existing Create/Save/Validate/Publish/Discard/GetRuntimeEnvironmentDraft
сохраняют прежние RPC, HTTP mapping и authority. Новые поля только output:
`base_version_ref`, `base_revision`, `saved_at`.

| Сценарий | Authority/OCC | Переход и authoritative read |
| --- | --- | --- |
| Create для существующего environment | Existing project.manage, exact environment owner и set version | Owner фиксирует actual immutable current version ref/number и DB save time; новый draft, active binding не меняется |
| Create нового environment | Existing project.manage; environment ref пустой, expected version 0 | Base tuple пустой/0, DB save time назначен; draft ref принадлежит серверу |
| Save | Existing owner boundary до idempotency/OCC | Specification и draft version меняются; savedAt назначается DB, base tuple неизменен |
| Validate | Exact draft version/owner | VALID/INVALID с прежним savedAt и base; validation не выдаётся за server save |
| Publish | Exact validated draft и повторная environment authority/OCC | Новая immutable environment revision/существующий publication effect; draft metadata сохраняется |
| Discard | Exact owner/draft OCC | DISCARDED, base/savedAt сохраняются; active environment не меняется |
| Replay | Existing actor/owner boundary и exact intent receipt | Возвращается прежний receipt, не новое время сохранения |
| Re-auth/reload | Existing protected Get | Читаются exact saved draft/base metadata, локальный dirty state сервер не выдумывает |

Для draft lifecycle, кроме уже существующего publish effect, нового event нет:
durable command receipt/audit и защищённый Get остаются authoritative read.
UI выполняет refetch после command, без вымышленного realtime push.

Migration638 восстанавливает savedAt существующего draft из последнего
успешного create/save audit; при отсутствии save audit используется createdAt.
Base исторического draft восстанавливается только при совпадении сохранённой
expected set version с текущей set version. Для уже stale legacy draft, где
историческая base не доказуема, ref/number остаются пустыми/0. Это неизвестная
base, а не ссылка на текущую версию. Существующий publication OCC по-прежнему
отклоняет stale draft; пользователь может создать новый draft с известной base.

New fields допускают legacy unknown base, но запрещают частичную пару ref/number.
Input не принимает эти owner поля. Namespace/credential/secret locator не добавляются.
Исторический immutable idempotency receipt, созданный до migration638, может
не содержать savedAt. Caster оставляет поле отсутствующим; recovery читает
защищённый Get, а не назначает историческому receipt вымышленное время.

Owner migration638, SQL read/write, domain entity и caster подключены.
На дереве поверх `8675ad7d02ba98f73e96736ca72afc551efb0144` полный
`TestBootstrapComponent` — PASS (18.654 s): новый draft без base, точная base
существующего environment, save time, Validate/Publish/replay и неизменность
base после конкурентной публикации/Discard. Repository/transport race,
полный control-plane vet/build и SQL boundary — PASS. Изолированный draft
subtest без preceding image fixture первоначально FAIL; полный Bootstrap
предоставляет обязательный setup и проходит. Browser/live — NOT RUN.
