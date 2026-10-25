package platform

import _ "embed"

var (
	// querySessionArchiveRunWake embeds the latest server-owned root anchor for a session ID/ref to
	// wake its organization projection. It has no caller/tenant filter and does not restrict
	// subscription scope; trusted worker admission must precede use.
	// querySessionArchiveRunWake встраивает latest server-owned root anchor session ID/ref для
	// пробуждения organization projection. Caller/tenant filter отсутствует; не ограничивает
	// subscription scope, требует предварительного trusted worker admission.
	//go:embed sql/session_archive_run_wake.sql
	querySessionArchiveRunWake string
	// querySessionArchiveMaterializeTasks embeds a worker sweep that recovers expired claims with
	// bounded backoff/dead-letter, cancels unsafe PVC deletion when execution resumes, and schedules
	// idle snapshots, needed restores and retained object GC. PURGE_PENDING projects are excluded from
	// new work. It updates storage/task state but performs no external I/O; system admission and
	// transaction ownership remain outside SQL.
	// querySessionArchiveMaterializeTasks встраивает worker sweep с recovery expired claims через
	// bounded backoff/dead-letter, отменой unsafe PVC deletion при возобновлении execution и
	// назначением idle snapshots, необходимых restores и retained object GC. PURGE_PENDING projects
	// исключены из новой work. Меняет storage/task state без external I/O; system admission и
	// transaction ownership остаются вне SQL.
	//go:embed sql/session_archive_materialize_tasks.sql
	querySessionArchiveMaterializeTasks string
	// querySessionArchiveSelectClaimableTasks embeds tenant-scoped READY task selection with
	// available-at/purge guards, stable kind priority and SKIP LOCKED task locks. It returns exact
	// source/archive/runtime descriptor metadata, not a claim or permission to delete storage.
	// querySessionArchiveSelectClaimableTasks встраивает tenant-scoped READY task selection с
	// available-at/purge guards, стабильным kind priority и SKIP LOCKED task locks. Возвращает exact
	// source/archive/runtime descriptor metadata, а не claim либо permission удалить storage.
	//go:embed sql/session_archive_select_claimable_tasks.sql
	querySessionArchiveSelectClaimableTasks string
	// querySessionArchiveClaimTask embeds READY-to-CLAIMED assignment with incremented
	// attempt/generation and supplied workload/lease/fence/expiry, plus storage-generation transition
	// when required. Missing final row can follow a task update, so the caller must validate and roll
	// back instead of committing a partial claim; authority checks occur before this SQL.
	// querySessionArchiveClaimTask встраивает READY-to-CLAIMED assignment с увеличением
	// attempt/generation и supplied workload/lease/fence/expiry, плюс storage-generation transition
	// при необходимости. Missing final row возможен после task update, поэтому caller обязан проверить
	// и выполнить rollback вместо commit partial claim; authority проверяется до SQL.
	//go:embed sql/session_archive_claim_task.sql
	querySessionArchiveClaimTask string
	// querySessionArchiveRenewTask embeds renewal only for exact tenant/ref CLAIMED state with
	// matching lease/fence/generation and an unexpired old lease. It returns the stored new expiry;
	// caller workload/grant binding and commit remain separate.
	// querySessionArchiveRenewTask встраивает renewal только для exact tenant/ref CLAIMED state с
	// matching lease/fence/generation и unexpired old lease. Возвращает stored new expiry; caller
	// workload/grant binding и commit остаются отдельными.
	//go:embed sql/session_archive_renew_task.sql
	querySessionArchiveRenewTask string
	// querySessionArchiveLockTask embeds tenant/ref task plus storage locks and
	// source/archive/generation/current-execution metadata. It returns lease/fence/state for
	// subsequent owner checks rather than validating those supplied bindings itself.
	// querySessionArchiveLockTask встраивает tenant/ref task и storage locks с
	// source/archive/generation/current-execution metadata. Возвращает lease/fence/state для
	// последующих owner checks, самостоятельно не проверяя supplied bindings.
	//go:embed sql/session_archive_lock_task.sql
	querySessionArchiveLockTask string
	// querySessionArchiveCompleteSnapshot embeds exact storage-generation snapshot adoption, task
	// success/lease clearing, storage transition and optional PVC-delete scheduling in one statement.
	// Active execution supersedes the archive and keeps storage LIVE. Caller must have validated the
	// locked task/fence and require the returned archive before committing; SQL itself does not read
	// external object bytes.
	// querySessionArchiveCompleteSnapshot встраивает exact storage-generation snapshot adoption, task
	// success/lease clearing, storage transition и optional PVC-delete scheduling одним statement.
	// Active execution делает archive SUPERSEDED и сохраняет storage LIVE. Caller заранее проверяет
	// locked task/fence и требует returned archive до commit; SQL сам не читает external object bytes.
	//go:embed sql/session_archive_complete_snapshot.sql
	querySessionArchiveCompleteSnapshot string
	// querySessionArchiveCompleteRestore embeds task success and exact archive
	// object/version/etag/digest/size/format/source matching before superseding the archive and
	// returning RESTORING storage to LIVE. Empty final result requires caller rollback because task
	// mutation can already have occurred; lease/tenant admission belongs to the locked owner path.
	// querySessionArchiveCompleteRestore встраивает task success и exact archive
	// object/version/etag/digest/size/format/source comparison до superseding archive и возврата
	// RESTORING storage в LIVE. Empty final result требует caller rollback, поскольку task mutation
	// уже могла произойти; lease/tenant admission принадлежит locked owner path.
	//go:embed sql/session_archive_complete_restore.sql
	querySessionArchiveCompleteRestore string
	// querySessionArchiveCompletePVCDeletion embeds completion of an already validated task, clears
	// its lease and moves matching DELETE_PVC_READY storage to ARCHIVED or RESTORE_READY. An active
	// turn schedules a restore; empty final result must not be committed as successful deletion, and
	// external PVC absence must be proven by the caller.
	// querySessionArchiveCompletePVCDeletion встраивает завершение заранее проверенной task, очищает
	// lease и переводит matching DELETE_PVC_READY storage в ARCHIVED либо RESTORE_READY. Active turn
	// назначает restore; empty final result нельзя фиксировать как successful deletion, external PVC
	// absence доказывает caller.
	//go:embed sql/session_archive_complete_pvc_deletion.sql
	querySessionArchiveCompletePVCDeletion string
	// querySessionArchiveCompleteObjectDeletion embeds completion for exact task/object key/version,
	// marks a matching archive DELETED and purges its current storage only for a CLOSED session. It
	// can complete orphan-object cleanup without an archive row; external absence and locked
	// task/fence admission remain caller responsibilities.
	// querySessionArchiveCompleteObjectDeletion встраивает completion exact task/object key/version,
	// отмечает matching archive DELETED и очищает current storage только для CLOSED session. Может
	// завершать orphan-object cleanup без archive row; external absence и locked task/fence admission
	// остаются ответственностью caller.
	//go:embed sql/session_archive_complete_object_deletion.sql
	querySessionArchiveCompleteObjectDeletion string
	// querySessionArchiveFailTask embeds READY retry or DEAD_LETTER from the current attempt budget
	// with bounded exponential delay and lease clearing, schedules possible failed-snapshot object
	// cleanup and updates storage readiness/error. The owner must validate task/fence and safe error
	// input first; the statement performs no external retry itself.
	// querySessionArchiveFailTask встраивает READY retry либо DEAD_LETTER по current attempt budget с
	// bounded exponential delay и lease clearing, назначает possible failed-snapshot object cleanup и
	// обновляет storage readiness/error. Owner заранее проверяет task/fence и safe error input;
	// statement сам не повторяет external operation.
	//go:embed sql/session_archive_fail_task.sql
	querySessionArchiveFailTask string
)
