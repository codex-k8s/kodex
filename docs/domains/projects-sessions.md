---
id: DOM-MC-003
title: Проекты, сессии и внешние диалоги
type: domain
status: approved
owner: architect
version: 1.1.0
updated: 2026-08-29
---

# Проекты, сессии и внешние диалоги

## Назначение

`Project` — единственный универсальный бизнес-контейнер. Он содержит members,
Agents, Workflows, Integration grants, Knowledge bindings, Schedules, Runs,
Artifacts, settings и audit. Workspace/Room не являются конкурирующими core
терминами и не используются как обязательные агрегаты.

## Проект

Проект имеет stable opaque ref, name, slug, description, lifecycle, version и
organization ownership. Он не обязан иметь repository, external chat, CRM,
storage или Kubernetes namespace. Archive запрещает новые launch/configuration
operations, но сохраняет history и audit.

## Сессия

`Session` принадлежит Project и Agent, хранит последовательную provider-neutral
историю и FIFO Turns. Она может быть создана direct launch, Workflow,
system-assistant, Schedule, Integration event, Agent delegation или optional
interaction adapter. Продолжение использует session ref из авторитетного
readback и не требует external thread ID.

Session является владельцем порядка Turns, active turn ref, provider session
binding и монотонной version. В одной Session исполняется не более одного Turn;
следующие Turns остаются в FIFO queue. Callback, WebSocket reconnect и повтор
inbound receipt не создают дополнительный Turn без нового idempotency receipt.

## Turn и ActiveSessionSnapshot

Перед постановкой каждого direct, delegated, Workflow или continuation Turn в
очередь Session owner создаёт `ActiveSessionSnapshot`. Это immutable снимок,
который pin-ит:

- organization, Project, Session, Agent и root actor/policy lineage;
- session history high-watermark и provider session generation;
- instruction, runtime, model/account policy и integration grant revisions;
- origin, parent/delegation edge, Workflow/node и route, если применимо;
- sealed `AttachmentSet`, `MaterializationManifest` id/digest и prompt
  materialization digest;
- Turn id, initial Attempt id и immutable input digest.

Request может выбрать доступную Session и передать пользовательский текст и
`AttachmentSet` ref, но не назначает lineage, authority, versions, manifest,
workspace paths или provider binding. Snapshot формируется сервером после
разрешения всех refs в Project boundary.

Snapshot активного Turn не меняется при редактировании Agent, окружения,
инструкций, интеграций, bindings или Artifact. Изменения учитываются только
новым Turn. Результат terminal Turn атомарно становится новым history
high-watermark для следующего snapshot.

## Continuation attachments

Каждое сообщение в Kodex, Run chat, Workflow chat или продолжение Session может
иметь собственный `AttachmentSet`. При submit сервер seal-ит set, создаёт
manifest и только затем ставит Turn в очередь. Ошибка хотя бы одного binding,
scan state, tombstone или materialization policy закрыто отклоняет весь submit;
файлы не отбрасываются молча.

Продолжение получает отдельный каталог `/workspace/input/<turn-ref>`. Перед
claim/materialization runtime публикует только paths из manifest текущего Turn.
Каталог прежнего Turn не монтируется как input, не сканируется повторно и не
используется как запасной источник. Provider history может хранить текстовое
упоминание прежнего файла, но не даёт новому Turn доступа к его байтам.

В prompt materialization доступна типизированная проекция файлов текущего Turn:
count, root и безопасный список metadata/path. Системный envelope продолжения
всегда указывает факт новых attachments; при большом наборе достаточно root и
count. Template не получает storage locator, credential или содержимое файла.

Если Artifact удалён после создания active snapshot, выполняющийся Turn
сохраняет неизменяемый input до terminal/cancel. Следующий Turn не может создать
manifest для tombstoned Artifact. После restore пользователь явно прикладывает
файл к новому continuation; прежний AttachmentSet автоматически не
переигрывается.

## Команды Session и Turn

- `CreateSession` — создаёт Session и, при наличии первого сообщения, первый
  Turn с server-owned lineage.
- `QueueTurn`, `QueueContinuationTurn`, `QueueDelegatedTurn` — создают точный
  origin, snapshot и FIFO entry; каждому origin соответствует типизированная
  команда.
- `ClaimTurn`, `RenewTurnLease` — выдают и продлевают claim одного поколения,
  связанный с Session, Turn, Attempt, snapshot и input digest.
- `CompleteTurn`, `FailTurn` — закрывают Attempt и Turn с точным outcome,
  результатом либо нормализованной причиной.
- `CancelTurn` — закрывает queued/running Turn и отзывает leases, grants и
  materialization capability.
- `RetryTurn` — создаёт новую Attempt того же Turn без изменения snapshot.

State-changing команды требуют `idempotency_key`. Scope включает organization,
Project, Session, command kind, origin actor и key. Повтор с тем же payload
возвращает прежний receipt, а reuse key с иным payload отклоняется. Команды,
которые меняют Session или Turn, требуют `expected_version`; authority и
принадлежность Project проверяются раньше version, чтобы не раскрывать foreign
state.

## Lifecycle

- `QUEUED -> RUNNING` возможен только для головы FIFO после проверки snapshot и
  readiness. Один claim generation принадлежит одной Attempt.
- `RUNNING -> SUCCEEDED|FAILED|CANCELLED|WAITING_OWNER|CHANGES_REQUESTED`
  фиксирует полный terminal либо управляемый pause envelope одной
  owner-транзакцией; частичное закрытие grants запрещено.
- `CompleteTurn` атомарно фиксирует assistant output, result bindings, session
  history high-watermark, audit/outbox и отзыв leases/grants. Late completion
  старого поколения отклоняется.
- `CancelTurn` для queued Turn удаляет его из исполняемой очереди, а для running
  Turn сначала отзывает claim/grants и инициирует cleanup. Snapshot следующего
  Turn создаётся только при его собственной queue command.
- `RetryTurn` создаёт монотонную Attempt, новый claim generation и event, но
  повторяет тот же immutable snapshot. Новые instructions, grants, runtime или
  attachments требуют нового Turn.
- Если retry не может materialize exact manifest из-за необратимого purge,
  Attempt завершается `FAILED` с machine-readable reason; fallback к другому
  version или residual workspace запрещён.
- После terminal Session доступна для continuation, если Project и Agent active.
  Archive Project запрещает новые Turns, не удаляя историю.

## ExternalConversationBinding

Binding связывает Session с необязательной поверхностью: Mattermost thread,
email conversation или иным adapter. Он хранит provider kind, masked display
metadata, revision и delivery policy. External IDs не выдаются как authority и
не входят в core lifecycle.

Удаление, outage или disable внешнего binding:

- не удаляет Project, Session, Run или Artifact;
- прекращает новые inbound/delivery операции данной capability;
- создаёт отдельный retryable delivery state и audit;
- не меняет успешный core Run на `FAILED`.

## Mattermost capabilities

Mattermost definition содержит независимые `INBOUND_MESSAGES`, `NOTIFICATIONS`,
`RESULT_MIRROR`, `HUMAN_GATE_DECISIONS`. Любая комбинация допустима. Team,
channel, post, thread и bot identity находятся только в adapter metadata.

## События

`project.created`, `project.updated`, `project.archived`, `session.created`,
`session.snapshot_created`, `session.turn_queued`, `session.turn_claimed`,
`session.turn_completed`, `session.turn_failed`, `session.turn_cancelled`,
`session.turn_retry_scheduled`, `external_binding.changed`,
`delivery.attempt_changed`.

Session owner атомарно записывает state, idempotency receipt, audit и outbox.
События Turn содержат origin, condition, cardinality, aggregate versions,
Attempt/generation и безопасные snapshot/manifest refs/digests. Они не содержат
prompt, сообщения, filenames, file bytes, secrets, external credentials или
provider session payload. Consumers используют version-pinned readback и не
считают event источником новых полномочий.

## Authority и инварианты

- Actor и root lineage происходят из проверенного transport context и
  server-owned delegation edge; `actor_id`, `parent_id` и `session_id` из
  payload не являются authority.
- Создание/продолжение требует права запуска exact Agent в exact Project и
  отдельного права чтения каждого attachment.
- Internal runner действует по узкому grant полного метода, Session, Turn,
  Attempt, snapshot digest и claim generation, а не от имени broad Project role.
- ActiveSessionSnapshot, sealed AttachmentSet и MaterializationManifest
  неизменяемы и сверяются до claim, materialization и complete.
- Один Session не имеет двух active Turns; параллельная работа одного Agent
  выполняется в разных Sessions.
- Новый Turn не наследует filesystem eligibility из предыдущего Turn. Residual
  bytes после terminal считаются дефектом cleanup, но не допустимым input.
- Удалённый файл недоступен новым Turns, даже если он упомянут в history,
  прежнем prompt, result или external conversation.

## Критерии приёмки

- web-only Project создаётся и исполняет Run при нуле external connections;
- в UI нет ручного ввода UUID или external IDs;
- optional inbound receipt идемпотентно создаёт не более одного Turn;
- continuation с несколькими attachments создаёт один sealed set, один
  immutable snapshot и один manifest без частичного принятия;
- delete между Turns запрещает повторную materialization, а delete во время
  active Turn не меняет его snapshot;
- cancel отзывает active claim/grants, retry сохраняет прежний input, а новые
  файлы всегда требуют нового Turn;
- удаление внешнего канала не запускает cleanup core Session;
- adapter outage не участвует в core Pod readiness.
