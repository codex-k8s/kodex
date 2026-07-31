package controlplane

import _ "embed"

// Каждая декларация связывает конкретный именованный запрос с бинарём на
// этапе компиляции. Удалённый или переименованный SQL-файл поэтому завершает
// сборку ошибкой, а не вызывает панику уже запущенного сервиса.

//go:embed sql/audit__append.sql
var sqlAuditAppend string

//go:embed sql/audit__list.sql
var sqlAuditList string

//go:embed sql/cache_epoch__bump.sql
var sqlCacheEpochBump string

//go:embed sql/cache_epoch__get.sql
var sqlCacheEpochGet string

//go:embed sql/diagnostics__get.sql
var sqlDiagnosticsGet string

//go:embed sql/memory__search.sql
var sqlMemorySearch string

//go:embed sql/memory_projection__upsert.sql
var sqlMemoryProjectionUpsert string

//go:embed sql/outbox__append.sql
var sqlOutboxAppend string

//go:embed sql/outbox__check.sql
var sqlOutboxCheck string

//go:embed sql/outbox__claim.sql
var sqlOutboxClaim string

//go:embed sql/outbox__mark_failed.sql
var sqlOutboxMarkFailed string

//go:embed sql/outbox__mark_published.sql
var sqlOutboxMarkPublished string

//go:embed sql/owner_gate__next_delivery.sql
var sqlOwnerGateNextDelivery string

//go:embed sql/permission_index__actor_list.sql
var sqlPermissionIndexActorList string

//go:embed sql/permission_index__actor_roles.sql
var sqlPermissionIndexActorRoles string

//go:embed sql/permission_index__rebuild.sql
var sqlPermissionIndexRebuild string

//go:embed sql/process__has_active_children.sql
var sqlProcessHasActiveChildren string

//go:embed sql/project__authorize.sql
var sqlProjectAuthorize string

//go:embed sql/project__list_eligible.sql
var sqlProjectListEligible string

//go:embed sql/proof_revision__next.sql
var sqlProofRevisionNext string

//go:embed sql/readiness__check.sql
var sqlReadinessCheck string

//go:embed sql/receipt__get.sql
var sqlReceiptGet string

//go:embed sql/receipt__save.sql
var sqlReceiptSave string

//go:embed sql/resource__get.sql
var sqlResourceGet string

//go:embed sql/resource__get_for_update.sql
var sqlResourceGetForUpdate string

//go:embed sql/resource__insert.sql
var sqlResourceInsert string

//go:embed sql/resource__list.sql
var sqlResourceList string

//go:embed sql/resource__list_tombstones.sql
var sqlResourceListTombstones string

//go:embed sql/resource__search.sql
var sqlResourceSearch string

//go:embed sql/resource__update.sql
var sqlResourceUpdate string

//go:embed sql/runtime_revision__components.sql
var sqlRuntimeRevisionComponents string

//go:embed sql/runtime_revision__latest.sql
var sqlRuntimeRevisionLatest string

//go:embed sql/schedule__due.sql
var sqlScheduleDue string

//go:embed sql/schedule_occurrence__get_for_update.sql
var sqlScheduleOccurrenceGetForUpdate string

//go:embed sql/schedule_occurrence__has_open.sql
var sqlScheduleOccurrenceHasOpen string

//go:embed sql/schedule_occurrence__list.sql
var sqlScheduleOccurrenceList string

//go:embed sql/schedule_occurrence__next.sql
var sqlScheduleOccurrenceNext string

//go:embed sql/schedule_occurrence__recover_expired.sql
var sqlScheduleOccurrenceRecoverExpired string

//go:embed sql/schedule_occurrence__save.sql
var sqlScheduleOccurrenceSave string

//go:embed sql/schedule_occurrence__skip_overlap.sql
var sqlScheduleOccurrenceSkipOverlap string

//go:embed sql/schedule_occurrence__update.sql
var sqlScheduleOccurrenceUpdate string

//go:embed sql/transaction__set_scope.sql
var sqlTransactionSetScope string

//go:embed sql/turn__expired_claimed.sql
var sqlTurnExpiredClaimed string

//go:embed sql/turn__next_queued.sql
var sqlTurnNextQueued string

//go:embed sql/turn_attempt__finish.sql
var sqlTurnAttemptFinish string

//go:embed sql/turn_attempt__save.sql
var sqlTurnAttemptSave string

//go:embed sql/turn_lease__delete.sql
var sqlTurnLeaseDelete string

//go:embed sql/turn_lease__get_for_update.sql
var sqlTurnLeaseGetForUpdate string

//go:embed sql/turn_lease__renew.sql
var sqlTurnLeaseRenew string

//go:embed sql/turn_lease__save.sql
var sqlTurnLeaseSave string

//go:embed sql/turn_lease__validate.sql
var sqlTurnLeaseValidate string
