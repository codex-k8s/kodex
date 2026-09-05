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
owner SQL/domain. Skill lifecycle, list/get/history и agent bindings также
подключены. Реальный scanner deploy и
runtime materialization нельзя считать PASS по codegen или PostgreSQL fixture.

## Передача runtime consumer

Внутренний `RuntimeRevisionSnapshot` получает `skill_bundles=69` и
`memory_records=70`. CP claim наполняет поля из exact owner bindings.
Shared ABI `23774ee12` принят без ручного изменения; runner и controller
materialization принадлежат Beauvoir, CP SQL/pins остаются в #1046.

`RuntimeSkillBundleSnapshot`: binding_ref/version, bundle_ref, revision_ref,
revision, digest, scan_engine/digest/scanned_at, files, provenance, name,
description. File использует существующий `SkillBundleFile`: path,
artifact_ref/revision, digest с `sha256:` и size_bytes. В runtime допускается
только exact PUBLISHED/CLEAN revision с серверным binding, не текущая draft.

`RuntimeMemoryRecordSnapshot`: binding_ref/version, record_ref, revision_ref,
revision, digest, title, summary, retention_until и provenance. Tenant,
project, agent, root/run/attempt назначаются из owner execution lineage.
Оба вида входят в immutable RuntimeRevision digest, не environment_tools,
knowledgeArtifactRefs или пользовательский filesystem path.

File bytes выдаёт существующий fenced ReadExecutionArtifact после
проверки exact Skill pin и текущего binding version/enabled, lifecycle и прав
root actor. После unbind/rebind старый snapshot не получает файл снова.
Materializer назначает read-only mount paths и
проверяет content digest. Дополнительный произвольный download URL или
object-store authority контрактом не вводится.

Миграция 00616 согласует budget safe_snapshot с shared ABI: 2 MiB; отдельная
Memory summary допускает 64 KiB. Не более 32 Skills/64 Memories в execution;
SQL получает максимум limit+1 после eligibility и отклоняет переполнение.
File 32 MiB, bundle 64 MiB, materialized context 512 MiB. Fenced file response
ограничен 32 MiB, protected client receive envelope 33 MiB; request и proof
resolver limits не увеличены. Превышение нельзя скрывать truncation или
молчаливым пропуском доступного binding.

В safe_snapshot сохраняется typed `contextSnapshot` со schema
`kodex.runtime-context.v1`; `skills: []` и `memories: []` явные даже для global
assistant. Его digest входит в RuntimeRevision и shared warm compatibility.
Сортировка bindings по ref стабильна, immutable summary/provenance и file pins
передаются без подмены latest. Active execution pins удерживают artifact даже
после удаления bundle из каталога; owner artifact impact использует тот же
reference count. Этот checkpoint не закрывает немедленный cancel уже запущенного
процесса при отзыве context, физический Memory GC или scanner deploy.

## Общие правила

- Создание требует проверенный project context. Остальные команды разрешают
  tenant/project по существующему opaque ref; браузер не назначает provenance.
- `MutationContext.expected_version` относится к aggregate version; для agent
  binding это agent version, `expected_binding_version=0` означает отсутствие
  включённого binding. Повтор с тем же idempotency key не создаёт дополнительную revision.
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
Bind/unbind/rebind и readback проверены в обоих targeted PostgreSQL scenarios.
CP claim/pins и fenced Skill download проверены в disposable PG; полный
controller/runner materialization требует интегрированного дерева.
VFS tree/search уже читает реальные SkillBundle и MemoryRecord, не knowledge
artifacts или selected tool names. Узлы содержат owner resource ref и digest
текущей revision (для Skill открытая draft имеет приоритет, как в каталоге).
Memory retention, archive/purge, source-run visibility и Skill artifact
visibility проверяются в SQL до LIMIT. Списки Memory по agent_ref дополнительно
требуют видимость самого агента, иначе не раскрывают наличие binding.
Targeted PostgreSQL `memory_records|skill_bundle` проверяет tree/global search,
archive/restore/purge и отрицательные project-only reader сценарии.
Внутри `/projects/{project}/entities/agents/{agent}` разделы `skills` и
`memories` создаются только при наличии видимых активных bindings. Строка
`context-binding:{binding_ref}` содержит resource ref и exact bound revision
digest, а не digest более новой revision из каталога. Unbind убирает пустой
раздел; скрытый агент не раскрывает раздел даже через global search.
Физический retention GC и VFS отдельных Skill file узлов остаются
незавершёнными; VFS проекция не материализует runtime content.
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

Skill lifecycle Proto shape не изменён относительно `d97753154`. Доступны все
девять команд Skill lifecycle, три query RPC и bind/unbind.

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
- Exact artifact revision/digest удерживаются всеми revisions не-PURGED
  bundle, кроме DISCARDED. Artifact DELETE/PURGE impact возвращает blocker
  `ARTIFACT_USED_BY_SKILL`; общий DB trigger также закрывает обход через avatar
  replacement или прямой lifecycle update. Архив сохраняет файлы для restore;
  purge bundle снимает это удержание. Migration 00613 и targeted PostgreSQL
  Skill lifecycle / role-image artifact promotion: PASS локально.
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

## Agent bindings

`BindAgentSkillBundle`, `UnbindAgentSkillBundle`, `BindAgentMemoryRecord`,
`UnbindAgentMemoryRecord` используют ранее переданные request/response без
изменений. В `AgentRuntimeConfigurationView` добавлены только
`skill_bindings = 8` и `memory_bindings = 9`, оба repeated AgentContextBinding.
GetAgentRuntimeConfiguration возвращает их вместе с точным agent_version в
одной repeatable-read транзакции. Чтение использует canonical agent.view,
не legacy membership; вложенные ссылки фильтруются по owner eligibility.

- Не более 128 включённых Skill+Memory bindings на Agent.
- Bind требует agent.manage и независимый доступ к exact context revision,
  source run/artifacts. Tenant/project совпадают; agent-scoped Memory нельзя
  передать другому Agent. Global system assistant этим API не связывается.
- Skill revision должна быть PUBLISHED/CLEAN, её файлы повторно сверяются по
  exact object receipts и digest. Memory record ACTIVE, current и выбранная
  revision не должны быть expired. Current pointer не заменяет выбранную revision.
- Agent version и enabled binding version проверяются в owner-транзакции;
  unbind требует exact bound revision. Новый bind после unbind использует
  expected_binding_version=0, а сервер продолжает монотонную version старой строки.
- Mutation атомарно сохраняет binding, увеличивает Agent version, audit,
  receipt и AGENT_CHANGED. UI перечитывает AgentRuntimeConfigurationView для
  следующего agent_version; response.binding.version относится к binding.
- Readback содержит разрешённые включённые bindings. Expired Memory остаётся
  metadata-ссылкой для unbind, но не разрешает выдачу summary/runtime snapshot.
- Archive/purge context отключает bindings без автоматического восстановления.
  CP RuntimeRevision pins реализованы; полный materialization, немедленное
  закрытие активного процесса при отзыве и автоматический retention GC остаются
  незавершёнными частями того же #1046, не скрываются за binding PASS.

Проверка:
`bash scripts/tests/control-plane-postgres-test.sh '^TestBootstrapComponent$/(memory_records|skill_bundle)'`.
Оба сценария выполнены; проверены bind/readback, stale Agent/binding OCC,
unbind и повторный bind с монотонной версией. Proto lint/codegen и Go
transport/domain: локальный PASS. Существующий полный PostgreSQL suite на этом
промежуточном checkpoint не повторялся.

Дополнительные targeted проверки CP runtime: Go/race transport/repository/domain
и controlplaneclient PASS; PG `memory_records|skill_bundle|email_receipt` PASS
с реальным claim, typed snapshot и fenced Skill file read, отказом после
unbind/rebind. PG `direct_run|integration_read` PASS. Integration fixture явно
закрывает rejected phase после проверки живого root graph, прежде чем занимать
provider capacity следующим run; production capacity invariant не изменён.
