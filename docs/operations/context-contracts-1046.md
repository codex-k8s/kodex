---
id: OPS-CONTEXT-1046
title: Контрактная передача SkillBundle и MemoryRecord
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Промежуточный контракт для HTTP/PWA

Источник: MVP-UI-37, Issue #1046. Это согласованная передача Proto/generated/policy
внутри одного полного CP unit, не готовая реализация всего owner lifecycle.
Memory create/revise/archive/restore/purge и list/get/history уже подключены к
owner SQL/domain. Skill lifecycle и list/get/history также подключены;
agent bindings пока наследуют gRPC Unimplemented. Реальный scanner deploy и
runtime materialization нельзя считать PASS по codegen или PostgreSQL fixture.

## Общие правила

- Создание требует проверенный project context. Остальные команды разрешают
  tenant/project по существующему opaque ref; браузер не назначает provenance.
- `MutationContext.expected_version` относится к aggregate version; для agent
  binding это agent version, `expected_binding_version=0` означает отсутствие
  binding. Повтор с тем же idempotency key не создаёт дополнительную revision.
- Path файла внутри Skill является относительным manifest path, не filesystem
  authority. Ровно один `SKILL.md`; supporting files проходят owner validation,
  scan и policy review. Artifact ref/revision повторно проверяются владельцем.
- Списки имеют optional project_ref/agent_ref/query, state и page; UNSPECIFIED
  state означает ACTIVE. List/Get/history используют одну eligibility boundary.
- Archive допускает restore; purge необратим. Истечение Memory retention
  закрывает content read, возвращает EXPIRED/redacted metadata и исключает
  выдачу нового runtime snapshot. Purge оставляет audit/tombstone без summary.
- Command response не означает runtime materialization. Только exact published
  Skill revision или разрешённая Memory revision может войти в новый runtime
  snapshot; текущий attempt не меняется при edit/archive/rebind.

## SkillBundle

`ListSkillBundles`, `GetSkillBundle`, `ListSkillBundleRevisions` принадлежат
`PlatformQueryService`. List response: `bundles,total,page`; Get: `bundle`;
history: `revisions,total,page`.

`PlatformCommandService`:

- `CreateSkillBundleDraft`: mutation, project_ref, optional bundle_ref,
  specification. Пустой bundle_ref создаёт aggregate; непустой создаёт следующую
  draft в существующем aggregate с OCC. Project должен совпадать с owner state.
- `SaveSkillBundleDraft`: mutation, bundle_ref, revision_ref, specification.
- `ValidateSkillBundleDraft`: mutation, bundle_ref, revision_ref, expected_digest.
- `ReviewSkillBundleDraft`: те же поля, decision APPROVE/REJECT и comment.
- `PublishSkillBundleDraft`, `DiscardSkillBundleDraft`: mutation, bundle_ref,
  revision_ref, expected_digest.
- `ArchiveSkillBundle`, `RestoreSkillBundle`, `PurgeSkillBundle`: mutation и
  bundle_ref. Все перечисленные ответы содержат `bundle`.
- `BindAgentSkillBundle`, `UnbindAgentSkillBundle`: mutation, agent_ref,
  bundle_ref, revision_ref, expected_binding_version; ответ `binding`.

Specification: name, description, files[]; file input содержит path,
artifact_ref, artifact_revision. Сервер возвращает дополнительно digest/size.
SkillBundle содержит ref/version/project_ref/state, optional current_revision
и draft_revision, timestamps. Revision содержит ref/number, name/description,
files, digest, parent_revision_ref, provenance, scan_state/engine/digest/time,
review actor/time и diagnostics. Scan/review поля не принимаются от браузера.

## KodexMemoryRecord

`ListMemoryRecords`, `GetMemoryRecord`, `ListMemoryRecordRevisions` принадлежат
`PlatformQueryService`. List response: `records,total,page`; Get: `record`;
history: `revisions,total,page`.

`PlatformCommandService`:

- `CreateMemoryRecord`: mutation, project_ref, optional agent_ref, specification.
- `ReviseMemoryRecord`: mutation, record_ref, specification.
- `ArchiveMemoryRecord`, `RestoreMemoryRecord`, `PurgeMemoryRecord`: mutation,
  record_ref. Ответы содержат `record`.
- `BindAgentMemoryRecord`, `UnbindAgentMemoryRecord`: mutation, agent_ref,
  record_ref, revision_ref, expected_binding_version; ответ `binding`.

Specification: title, summary, optional source_run_ref, обязательный retention_until.
Source run повторно разрешается по owner boundary. Отсутствующий source run
означает явно созданную пользователем summary, не автоматическую память Codex.
KodexMemoryRecord содержит ref/version/project_ref/optional agent_ref/state,
current_revision и timestamps. Revision содержит title/summary, ref/number/digest,
parent_revision_ref, server-owned provenance, retention_until, redacted.

## Policy и проверки

Operation IDs находятся в `ControlAPIGatewayOperations`: prefixes
`platform.query.skill-bundles`, `skill-bundle-revisions`, `memory-records`,
`memory-record-revisions`; command prefixes `skill-bundle-drafts`, `skill-bundles`,
`agent-skill-bundles`, `memory-records`, `agent-memory-records`.
Policy revision 51 сохраняет scheduler и interaction operations.

Proto lint/codegen, policy codegen и Go compatibility: PASS локально.
Memory CRUD/history: локальный targeted PostgreSQL PASS, точка входа
`bash scripts/tests/control-plane-postgres-test.sh '^TestBootstrapComponent$/memory_records'`.
Проверены immutable history, version conflict, page cursor, archive/restore,
terminal purge и отсутствие summary в replay после purge. Migration 00611
применена штатным runner в disposable PostgreSQL; live-проверки не выполнялись.
Skill lifecycle/list/history: локальный targeted PostgreSQL PASS с явно тестовым
scanner port; production Unix-socket client проверен Go/race protocol fixtures.
Agent bindings и runtime materialization ещё не реализованы.
STT parameters уже реализованы checkpoint `a88caf7f2`;
upgrade существующих системных ролей находится в `9911ddb38`.

## Матрица Memory owner

| Сценарий | Authority и переход | Receipt, аудит и чтение |
| --- | --- | --- |
| Create | Проверенный tenant; project.manage либо agent.manage; agent принадлежит exact project; source run требует run.view | Сервер назначает memr/memv и provenance; audit + receipt в одной транзакции |
| Revise | Owner ref разрешается до OCC и receipt; создаётся новая immutable revision с parent | Старый summary не меняется; Get/history показывают exact revision |
| Archive | ACTIVE → ARCHIVED, OCC | Bindings отключаются в той же транзакции; Get возвращает tombstone state |
| Restore | ARCHIVED → ACTIVE, OCC, retention ещё действителен | Старые bindings не включаются автоматически |
| Purge | Только ARCHIVED → PURGED, OCC; DB запрещает обратный переход | Summary всех revisions очищается атомарно; receipt не хранит summary |
| Replay | Повторная owner/read/source-run проверка; digest исходного intent неизменен | Summary exact revision повторно читается с проверкой retention/purge |
| List/history | SQL eligibility до LIMIT; cursor связан с tenant/actor/filter | Total считается в SQL; содержимое материализуется только для ограниченной страницы |
| Retention | EXPIRED/redacted вычисляется авторитетной SQL projection | Автоматический физический GC и runtime pins ещё не реализованы |

Для каждой перечисленной команды domain event пока отсутствует: авторитетный
read path — GetMemoryRecord/ListMemoryRecords/ListMemoryRecordRevisions. Команды
не запускают фонового consumer; дальнейшая runtime materialization должна
проверять активное состояние и retention заново перед каждым attempt.

## Skill limits и передача HTTP

Proto shape не изменён относительно `d97753154`. Доступны все девять команд
Skill lifecycle и три query RPC; bind/unbind ещё не подключены.

- Не более 128 файлов; каждый до 32 MiB, суммарно до 64 MiB.
- `SKILL.md` до 256 KiB, UTF-8, обязательные YAML name/description и непустые
  инструкции; frontmatter должен совпадать с specification.
- Name: 1–160 символов, description: до 2000; manifest description непустой.
- Path: до 240 UTF-8 bytes, относительный canonical path; запрещены traversal,
  dotfiles, backslash, colon, NUL, CR/LF, whitespace по краям сегментов и
  регистронезависимые дубли. Ровно один root `SKILL.md` с точным регистром.
- Supporting files: `.md`, `.txt`, `.json`, `.csv`, `.png`, `.jpg`, `.jpeg`, `.webp`.
  Executable scripts, HTML, архивы и другие расширения не разрешены.
- Save сбрасывает scan/review. Validate требует exact digest; проверяет object
  receipt и фактический SHA-256, structural manifest и malware scanner.
- Review: только VALIDATED с CLEAN scan digest не старше суток. Publish:
  только APPROVED с тем же scan digest, exact file revisions и действующим
  artifact access. Публикация без review закрыто отклоняется.
- Archive отключает bindings и завершает открытую draft как DISCARDED;
  restore не включает прежние bindings; purge необратим и удаляет file refs.
- Команды атомарно сохраняют audit/receipt. Domain event отсутствует;
  авторитетный read path — GetSkillBundle/ListSkillBundles/ListSkillBundleRevisions.

Реальный адаптер использует только Unix socket из
`CONTROL_PLANE_SKILL_SCANNER_SOCKET` (default
`/run/kodex-skill-scanner/clamd.sock`) и bounded timeout
`CONTROL_PLANE_SKILL_SCANNER_TIMEOUT` (default 15s). TCP fallback отсутствует.
INSTREAM framing и VERSION provenance реализованы по
[официальному протоколу ClamAV](https://docs.clamav.net/manual/Usage/ClamdProtocol.html).
Смена engine/database revision во время scan, ошибка, неизвестный ответ или база
старше семи суток закрыто отклоняются. Verdict не выдаётся за runtime readiness.

CP-owned scanner container, signature database delivery и его readiness ещё
не подключены. Без них Validate возвращает INVALID/ERROR с
`SKILL_MALWARE_SCANNER_UNAVAILABLE`, а не фиктивный CLEAN. Реальный ClamAV: NOT RUN.
Структурный artifact scan не заменяет malware scanner.
