---
id: ARCH-MC-008
title: Вложения и artifacts
type: architecture
status: approved
owner: architect
version: 2.1.0
updated: 2026-08-29
---

# Вложения и artifacts

## Владение и границы

Control-plane владеет `Artifact`, его immutable `ArtifactRevision`, scan и
lifecycle, `AttachmentSet`, bindings, retention, result relation и download
grants. PostgreSQL хранит tenant-scoped metadata и точный S3 receipt; тело
хранится только в обязательном S3-compatible bucket. Content не помещается в
prompt, audit, outbox, NATS, WebSocket, PostgreSQL `bytea`, логи, метрики или
tracing.

`Artifact` является стабильным логическим объектом. Каждая замена содержимого
создаёт новую `ArtifactRevision` с server-assigned номером версии, source,
provenance, media type, size, SHA-256 и scan state. Уже существующая revision не
изменяется и не начинает указывать на другое тело.

Server-assigned object key имеет форму
`organizations/<organizationRef>/projects/<projectRef>/artifacts/<artifactRef>/<revisionRef>/<sha256>`.
Ни один внешний actor не выбирает key, bucket, version, owner scope или путь в
workspace. Exact content receipt связывает `bucket`, `key`, S3 `version_id`,
ETag, checksum, размер и digest только внутри storage boundary; browser,
шаблонизатор и role Pod этих locator и credentials не получают.

Local hot-reload использует SeaweedFS 4.41 в `kodex-system`. Production
использует заранее подготовленный внешний S3 endpoint и bucket через Kubernetes
Secret. Это один внутренний storage port и не пользовательская integration.

## AttachmentSet и bindings

`AttachmentSet` является immutable envelope файлов одного пользовательского
действия. Он содержит:

- Organization, Project, actor, source и purpose;
- упорядоченные exact refs на `ArtifactRevision`;
- пользовательское display name для каждого item, не используемое как path;
- manifest digest и время finalization;
- только безопасные metadata, без file body и secret values.

После finalization состав, порядок и purpose набора не изменяются. Добавление или
удаление вложения до отправки создаёт новую draft-версию набора; отправленная
команда ссылается только на finalized `AttachmentSet`.

Один `AttachmentSet` может получить несколько server-owned bindings в рамках
одного Project, но каждый binding указывает на точный авторитетный target.
Закрытый registry binding kinds включает:

| Binding kind | Авторитетный target | Пользовательский сценарий |
| --- | --- | --- |
| `ASSISTANT_MESSAGE` | сообщение диалога Kodex | сообщение системному помощнику |
| `SESSION_TURN` | точный Turn и его immutable input | прямой запуск агента или продолжение Session |
| `RUN_INPUT` | root/child Run и attempt input snapshot | ручной или делегированный запуск |
| `WORKFLOW_INPUT` | опубликованная WorkflowVersion либо materialized invocation snapshot | запуск Process/Workflow |
| `OWNER_GATE_MESSAGE` | сообщение или решение конкретного Human Gate | комментарий к решению владельца |

Generic client-provided `target_type + target_id` не является authority.
Control-plane разрешает Organization, Project, actor и target, проверяет
permission конкретной команды и сам создаёт binding. Для каждого binding
действует database constraint ровно одного target; foreign Project, скрытый или
terminal target закрыто отклоняется. Input для message, Turn, Run, Workflow и
Gate связывается через `AttachmentSet`, а не набором mutable direct refs.

Generated result и иные выходы могут иметь прямой semantic
`ArtifactRevision` binding к exact Run/node/turn/attempt. Такой result binding
не превращается во вход следующего Turn без нового server-owned
`AttachmentSet` и повторной проверки eligibility.

## Upload без продуктового лимита числа файлов

Продукт не задаёт `max_files_per_message`, `max_files_per_turn` или другой
пользовательский предел количества вложений. Composer ведёт очередь любого
размера, а transport обрабатывает её ограниченными по числу элементов batches и
ограниченными по размеру chunks. Технические `max_batch_items`,
`max_chunk_bytes`, concurrency и timeout являются server policy и управляют
нагрузкой, но не меняют допустимое суммарное число файлов: клиент автоматически
продолжает следующими batch/chunk.

Installation policy может ограничивать размер одного файла, суммарные bytes и
занятое tenant storage. Это quota хранения и защиты ресурса, а не скрытый лимит
числа файлов. Превышение quota возвращает typed отказ с текущим usage и не
оставляет частично доступный Artifact.

Upload одного файла проходит следующий lifecycle:

1. Browser резервирует `ArtifactUpload` с безопасными metadata и получает
   server-assigned upload ref, chunk policy и idempotency scope.
2. Gateway проверяет session, Origin, CSRF, rate и chunk bounds, после чего
   передаёт bounded stream через generated client. Gateway и control-plane не
   буферизуют файл целиком.
3. Каждый chunk связан с upload ref, ordinal/offset, declared size и digest.
   Повтор того же chunk идемпотентен; иной payload для того же ordinal является
   conflict. Незавершённый upload не считается `ArtifactRevision`.
4. Storage adapter завершает multipart либо эквивалентную resumable upload,
   вычисляет полный SHA-256 и получает exact S3 receipt.
5. Control-plane выполняет bounded inspection, `HeadObject`/checksum readback и
   принимает только совпадающие digest и size.
6. Одна PostgreSQL transaction фиксирует `Artifact`, immutable
   `ArtifactRevision`, exact content receipt, audit, idempotency receipt и
   обязательное событие. При незавершённой transaction подготовленный object
   удаляется bounded cleanup.
7. Только revision со scan state `CLEAN` разрешено включать в finalized
   `AttachmentSet`, knowledge binding, preview или download. `QUARANTINED` и
   `FAILED` не материализуются и не выдаются как готовые.

S3 upload и PostgreSQL transaction не объявляются общей распределённой
transaction. Fail-closed readback, идемпотентный finalize и cleanup exact
prepared object являются явным компенсирующим контрактом.

## Generated result

Agent-runner завершает execution с bounded result manifest. Control-plane
проверяет claim/fence, digest, declared size/media type, подготавливает objects и
в одной terminal PostgreSQL transaction связывает immutable revisions с
точными Run/node/turn/attempt. Partial failure удаляет подготовленные objects;
broad S3 credentials в role Pod отсутствуют.

## Download и preview

1. Browser запрашивает `artifactRef + revisionRef` из авторитетного
   Project/Run readback.
2. Control-plane повторно проверяет Organization/Project eligibility, lifecycle,
   scan state и retention.
3. Exact S3 receipt разрешается только из PostgreSQL; version, digest и size
   повторно сверяются до выдачи.
4. Download выдаёт body bounded chunks; gateway не буферизует файл целиком и
   задаёт безопасные content headers.
5. Filename является недоверенным display metadata; active content не
   исполняется inline без allowlist preview.

Soft-deleted и purged revisions не получают новых preview/download grants.
Выданный grant короткоживущий, одноразовый и связан с actor, Project, revision и
purpose. Delete отзывает ещё не использованные grants.

## Runtime materialization и workspace

Перед каждым Turn/attempt control-plane создаёт immutable `RuntimeRevision`,
которая содержит точные finalized `AttachmentSet` refs и разрешённые
`ArtifactRevision` refs. Materializer получает их по execution-scoped bearer и
mTLS, повторно сверяет scan state, size и digest и не получает S3 endpoint или
credentials.

Каждый набор материализуется read-only в
`/workspace/input/<attachmentSetRef>/`. Безопасно нормализованные имена находятся
в подкаталоге `files/`; коллизии разрешаются детерминированным суффиксом.
Browser path, имя архива и исходное filename никогда не становятся filesystem
path напрямую. Symlink, device, socket, path traversal и выход за workspace
запрещены.

Материализатор создаёт:

- `/workspace/input/<attachmentSetRef>/manifest.json` с canonical descriptors;
- `/workspace/input/<attachmentSetRef>/README.md` с человекочитаемым списком;
- `/workspace/input/manifest.json` с перечнем всех sets текущей
  `RuntimeRevision` и их manifest digests.

Manifest содержит attachment set/ref, Artifact/ref/revision, исходное display
name, безопасное локальное имя и path, media type, size, SHA-256, source,
purpose и порядок. Он не содержит S3 locator, credentials, secret values,
cookies, provider payload или неразрешённые metadata. До запуска materializer
пересчитывает digest manifest и каждого body; mismatch закрыто отклоняет attempt.

Каталог input и manifests неизменяемы для процесса агента. Запись результатов
идёт в отдельный output boundary. Изменение исходного Artifact, binding или
Project после старта не меняет workspace уже работающего attempt.

## Типизированные prompt descriptors

Шаблонизатор получает только descriptors тех revisions, которые разрешены и
входят в текущую `RuntimeRevision`:

- `.input.files`, `.input.files_dir`, `.input.files_count`,
  `.input.manifest_path` — вложения текущего сообщения или Turn;
- `.session.files`, `.session.files_dir`, `.session.files_count`,
  `.session.manifest_path` — доступные immutable inputs текущей Session;
- `.run.files` — явно связанные inputs и результаты текущего Run, доступные
  actor;
- `.workflow.files` — materialized input snapshot текущего Workflow invocation;
- `.gate.files` — вложения текущего Human Gate message;
- `.project.files` — только явно выбранные Project files, а не полный каталог.

Каждый file descriptor имеет типизированные поля `artifact_ref`,
`revision_ref`, `name`, `media_type`, `size`, `sha256`, `path`, `source`,
`version` и `purpose`. Коллекции имеют стабильный порядок. Path всегда является
локальным read-only path текущего workspace и не пригоден вне attempt.

Для начального Turn descriptors рендерятся пользовательским шаблоном только при
явном обращении к соответствующей `*.files` переменной. File body автоматически
в prompt не вставляется. Если число элементов превышает server-controlled
`inline_descriptor_threshold`, шаблон и platform helpers используют count,
directory и manifest вместо разворачивания длинного списка. Этот threshold
ограничивает размер prompt, а не число вложений.

Secret descriptors и значения секретов в перечисленных scopes отсутствуют.
Наличие файла не расширяет permission агента на integrations, Project или
другие revisions.

## Platform continuation notice

Если пользователь, помощник или родительский агент продолжает существующую
Session с новым finalized `AttachmentSet`, platform-controlled system block
добавляется в Turn независимо от пользовательского шаблона. Он сообщает, что к
продолжению приложены новые файлы, и содержит:

- количество новых файлов;
- read-only directory набора;
- path к manifest;
- descriptors по одному файлу только до `inline_descriptor_threshold`.

Notice является отдельной typed частью materialized prompt, имеет ссылку на
Turn, AttachmentSet и manifest digest, не редактируется actor и сохраняется в
safe prompt provenance. Он не повторяет ранее доступные Session files и не
содержит body или secret values. Таким образом новая пачка входов не теряется,
даже если шаблон роли не использует `.input.files`.

## Soft delete, восстановление и purge

Удаление является отдельным lifecycle, а не удалением row или перезаписью
revision:

1. `Delete` после проверки exact Project owner и OCC переводит `Artifact` в
   `DELETED`, выставляет `deleted_at` и `purge_after = deleted_at + 30 days`,
   запрещает новые bindings и ordinary download. Exact finalized input уже
   созданного активного Run сохраняет право на runtime materialization до его
   terminal transition. Artifact появляется в корзине; исторические bindings
   остаются частью audit lineage.
2. `Restore` до `purge_after` возвращает `ACTIVE`, не создаёт новую revision и
   повторно разрешает только те bindings, для которых actor и target всё ещё
   eligible. Restore не оживляет terminal Run и не меняет старую
   `RuntimeRevision`.
3. Retention job после `purge_after` либо команда `Очистить корзину` с отдельным
   permission переводит объект в `PURGING` и удаляет каждую exact S3 version по
   сохранённым `bucket + key + version_id`.
4. Для каждой version storage adapter выполняет deletion readback. Только после
   подтверждённого отсутствия exact version transaction удаляет locator,
   переводит Artifact в `PURGED` и создаёт audit tombstone. Ошибка оставляет
   `PURGE_FAILED` с bounded retry; UI и API не заявляют успешную очистку.

В MVP exact content locator не разделяется разными Artifact. Если позднее будет
добавлена физическая дедупликация, purge обязан использовать авторитетный
reference count и не удалять body, пока существует хотя бы одна active или
retained revision.

Audit tombstone сохраняет только opaque Artifact ref, Organization/Project
scope, actor операции, времена delete/purge, reason category, число удалённых
revisions и digest deletion receipt. Filename, media type, content digest,
object key/version, body, prompt fragment и secret metadata после purge не
сохраняются.

## Активная Session и неизменяемый snapshot

Soft delete не изменяет exact finalized input уже созданного активного Run:
Run может получить `RuntimeRevision` после удаления и завершить attempt с тем же
snapshot. Иное поведение нарушило бы immutable input и оставило бы Run навсегда
в очереди. Перед подтверждением UI показывает активные Runs/Sessions,
использующие revision, и предлагает отменить их, если требуется немедленно
прекратить обработку.

После Delete:

- текущий active Run и его attempt могут материализовать и завершить работу со
  своим read-only snapshot;
- новый независимый Turn, continuation или child Run получает новую
  `RuntimeRevision` и не включает deleted revision; retry следует отдельному
  version-pinned контракту исходного Turn;
- новые ordinary download и materialization вне exact snapshot активного Run
  закрыто отклоняются;
- restore не мутирует текущий attempt, а влияет только на будущую
  материализацию после новой проверки eligibility.

Cancel активного Run отзывает lease/grants и завершает Pod по общему lifecycle,
но не переписывает исторический manifest. История хранит refs и manifest digest,
а после purge не предоставляет content.

## Readiness и эксплуатационная граница

Control-plane читает endpoint/region/bucket через `secretKeyRef`, а access key и
secret key только из read-only files. Startup выполняет authenticated
`HeadBucket`; отсутствие Secret, endpoint или bucket закрыто останавливает
готовность. Local bucket создаётся отдельной bootstrap Job без вывода
credentials. Production bucket создаётся оператором до запуска release.

Readiness дополнительно доказывает поддержку resumable upload, exact version
receipt и удаления exact version. Bucket versioning обязателен для production;
адаптер без эквивалентной immutable version identity не допускается.

PostgreSQL backup без соответствующих S3 objects не восстанавливает artifacts.
Полный backup/retention/restore drill реализует отдельный unit #1003.
Session JSONL не является Artifact body; его S3 archive/restore реализует
отдельный unit #1002.

## Ограничения первой версии

- Размер одного Artifact и tenant storage ограничиваются installation quota.
- Число файлов в message/Turn/Run/Workflow/Gate продуктом не ограничивается;
  transport всегда использует bounded batches/chunks и bounded concurrency.
- Range download и извлечение file body непосредственно в prompt остаются вне
  MVP.
- Knowledge/vector projection не расширяется этим контрактом и продолжает
  ссылаться на immutable `ArtifactRevision`.
