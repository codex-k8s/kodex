---
id: DOM-MC-008
title: Файлы, результаты и знания
type: domain
status: approved
owner: architect
version: 1.2.0
updated: 2026-08-29
---

# Файлы, результаты и знания

## Агрегаты

### Artifact

`Artifact` принадлежит Organization и Project и является владельцем жизненного
цикла логического файла. Каждая `ArtifactVersion` неизменяема и хранит source,
provenance, media type, size, digest, scan status и retention. Filename является
недоверенным display metadata, не используется как storage key и не влияет на
authority.

Источники: Control Center upload, Agent result, Integration result, Knowledge
source и interaction attachment. Ни один источник не требует Mattermost
post/thread. Content-addressed object locator остаётся внутренним и не попадает
в browser, prompt, event или runtime manifest.

### FileBinding

`FileBinding` — отдельный versioned aggregate, который связывает точную
`ArtifactVersion` с одним target и purpose. Закрытый реестр target kind включает
как минимум Project library, Knowledge source, Agent knowledge, Run input, Run
result, Session continuation и assistant conversation.

Binding не является источником полномочий. Чтение разрешается только при
одновременном доступе actor к Artifact и target. Binding всегда pin-ит version и
digest: переход на новую ArtifactVersion создаёт новую ревизию binding. Отзыв
binding не удаляет Artifact, а tombstone Artifact делает его bindings
непригодными для новых чтений и materialization.

### AttachmentSet

`AttachmentSet` группирует упорядоченные `FileBinding` одного пользовательского
или агентского сообщения. Он принадлежит Organization и Project, хранит origin,
target kind/ref, version и состояние `DRAFT` либо `SEALED`.

- В `DRAFT` bindings добавляются и удаляются с optimistic concurrency.
- Перед постановкой сообщения или Turn в очередь сервер переводит set в
  `SEALED`; после этого состав, порядок и pinned versions неизменяемы.
- Изменение уже отправленного набора создаёт новый `AttachmentSet`, а не меняет
  историю.
- Фиксированного продуктового лимита количества файлов нет. Применяются tenant
  quotas по суммарному размеру, количеству объектов и бюджету materialization;
  list/upload API поддерживают pagination и resumable обработку.

### MaterializationManifest

`MaterializationManifest` — назначаемый сервером неизменяемый снимок файлов,
которые разрешено выдать одному точному Session/Turn/Attempt. Manifest содержит
refs точных `ArtifactVersion`, digests, размеры, media types, безопасные
относительные workspace paths, единый input root и digest всего manifest. Он не
содержит storage locators и credentials.

Manifest создаётся только из `SEALED` AttachmentSet после повторной проверки
Project boundary, bindings, состояния `CLEAN` и отсутствия tombstone. Пути
назначаются сервером внутри отдельного каталога Turn, нормализуются и не могут
выйти за root. Одинаковые и Unicode filenames получают безопасные
детерминированные имена без перезаписи.

Materializer принимает только внутренний grant, связанный с organization,
Project, Session, Turn, Attempt, manifest id/digest и claim generation. Он
сверяет digest каждого объекта до публикации файла в workspace. Runtime не
может запросить Artifact, отсутствующий в manifest.

Проекция для prompt формируется из того же manifest и содержит `count`, `root`
и для каждого файла безопасные `name`, `path`, `mediaType`, `size` и `version`.
Если файлов много, системный envelope обязан как минимум сообщить root и count;
сырые байты, credentials и storage locators в prompt не включаются.

## Команды

- `BeginArtifactUpload`, `CompleteArtifactUpload` — создают Artifact и
  immutable version после проверки фактических size/digest и scan result.
- `CreateFileBinding`, `ReplaceFileBindingVersion`, `RevokeFileBinding` —
  управляют одной типизированной связью с exact target.
- `CreateAttachmentSet`, `AddAttachment`, `RemoveAttachment`,
  `SealAttachmentSet` — собирают и фиксируют набор одного сообщения.
- `CreateMaterializationManifest` — создаёт manifest для точного
  Session/Turn/Attempt; caller не назначает paths, digests или eligibility.
- `DeleteArtifact` — ставит tombstone и переносит Artifact в корзину на 30 дней.
- `RestoreArtifact` — снимает tombstone до purge, если content сохранён и actor
  по-прежнему имеет полномочие.
- `PurgeArtifact` — необратимо удаляет content из object storage и переводит
  metadata в terminal state `PURGED`.
- `EmptyProjectTrash` — bounded batch-команда, применяющая `PurgeArtifact` к
  доступным объектам и возвращающая per-item outcome.

Все state-changing команды требуют `idempotency_key`; update, delete, restore и
purge дополнительно требуют `expected_version`. Idempotency scope включает
organization, Project, actor, command kind и key. Повтор с тем же payload
возвращает прежний receipt, а повтор с другим payload закрыто отклоняется.
Version проверяется только после разрешения Artifact внутри авторитетной tenant
boundary.

## Жизненный цикл и корзина

Fresh baseline выполняет bounded inspection синхронно до фиксации upload и
сохраняет один из итоговых scan states: `CLEAN`, `QUARANTINED`, `FAILED`.
Transport-состояние ongoing upload отображает клиент, но не является
долговечным доменным переходом. `PENDING` и `SCANNING` зарезервированы для
будущего отдельного scanner adapter и не выдаются как успешная готовность без
фактического consumer. Runtime получает только `CLEAN` version. Upload повторно
сверяет declared size/digest с фактически прочитанным content.

Download использует короткоживущий one-time grant, связанный с User,
organization, Project, ArtifactVersion и purpose. Browser не получает storage
credential или внутренний locator. Preview поддерживает только allowlisted safe
media и никогда не исполняет активный контент.

`DeleteArtifact` атомарно ставит tombstone, фиксирует `deleted_at`,
`purge_after = deleted_at + 30 days`, actor и reason. После commit новые
bindings, AttachmentSet и manifests для Artifact запрещены. Draft set с
удалённым файлом нельзя seal-ить. Удаление не переписывает уже активный manifest
и не обещает отозвать байты, материализованные выполняющемуся Turn.

`RestoreArtifact` допустим до `purge_after` и создаёт новую aggregate version.
Он не возобновляет отозванные bindings автоматически. Действующие bindings,
которые не отзывались отдельно, снова проходят обычную eligibility-проверку.

`PurgeArtifact` и автоматический purge после 30 дней запрещены, пока exact
version присутствует в manifest не terminal Turn. Purge помечается ожидающим и
повторяется после terminal transition. После purge content удаляется из bucket,
а metadata сохраняет минимальный terminal tombstone для audit и idempotency;
restore и повторная materialization невозможны. Очистка корзины не скрывает
per-item отказ и не выдаёт частично выполненный batch за полный успех.

## Knowledge

Knowledge source связывает immutable ArtifactVersions либо typed external
source с Agent/Project. Индекс и embeddings являются перестраиваемой проекцией и
не расширяют eligibility исходного документа. Проекция хранит source version,
content/model provenance и tenant/project scope. Ошибка projection не блокирует
авторитетное чтение доступного файла.

## Realtime и delivery

`artifact.available` содержит только safe metadata и ref; файл читается
отдельным HTTP API, не через WebSocket. Optional result mirror создаёт отдельный
DeliveryAttempt. Его outage не меняет Artifact availability и core Run outcome.

## Authority и события

Authority берётся из проверенного user/service context. Organization, Project,
owner, target и purpose не становятся доказанными из request. Upload, bind и
attach требуют полномочий на запись в Project и exact target; read, download и
materialize требуют отдельного полномочия чтения; delete, restore, purge и
очистка корзины являются разными permissions. Materializer не получает broad
Project read и действует только по exact manifest grant.

Каждый переход фиксирует business state, idempotency receipt, audit и outbox
event одной owner-транзакцией. Канонические события:

- `artifact.available` — одна CLEAN version стала доступна;
- `artifact.deleted`, `artifact.restored`, `artifact.purge_requested`,
  `artifact.purged` — переходы retention lifecycle;
- `file_binding.created`, `file_binding.version_replaced`,
  `file_binding.revoked` — переходы exact target binding;
- `attachment_set.sealed` — один set получил окончательный состав;
- `materialization_manifest.created` — создан immutable manifest ref/digest.

Events содержат aggregate ref/version, origin и condition, но не raw bytes,
prompt text, filenames, credentials или внутренние locators. Consumers обязаны
делать version-pinned readback; event не расширяет доступ.

## Инварианты terminal, retry и cancel

- Terminal Turn закрывает manifest grants и инициирует bounded cleanup
  materialized workspace; исторический manifest сохраняется как metadata.
- Cancel атомарно отзывает claim, lease и grant. Поздняя materialization или
  публикация результата со старым поколением отклоняется.
- Retry того же Turn создаёт новую Attempt с тем же immutable input snapshot и
  manifest digest. Если content уже `PURGED`, retry закрыто завершается
  предсказуемой ошибкой; подмена другим файлом запрещена.
- Новые или изменённые файлы после terminal/failure оформляются новым
  continuation Turn и новым AttachmentSet, а не retry прежнего input.
- Новый Turn не наследует доступ к Artifact из provider history, прежнего
  workspace или старого manifest.

## Критерии приёмки

- пользователь загружает input и скачивает generated result в web-only режиме;
- Unicode/повторяющиеся имена не перезаписывают content;
- foreign Project и expired/replayed grant закрыто отклоняются;
- Artifact со state, отличным от `CLEAN`, не materialize-ится в role Pod;
- удалённый Artifact не попадает в новый Turn, а старые workspace paths не
  используются как источник доступа;
- restore работает в течение 30 дней, purge необратим и очистка корзины удаляет
  object bytes без удаления минимального audit tombstone;
- sealed AttachmentSet и MaterializationManifest невозможно изменить;
- raw file bytes, provider payload и secret не попадают в events, logs или audit.
