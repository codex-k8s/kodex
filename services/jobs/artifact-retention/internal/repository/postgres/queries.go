package postgres

import _ "embed"

var (
	// queryClaimDue leases a bounded batch of due artifact versions.
	// RU: queryClaimDue арендует ограниченную пачку подлежащих очистке версий артефактов.
	//go:embed sql/claim_due.sql
	queryClaimDue string
	// queryLockClaim locks and verifies the exact owner and generation before finalization.
	// RU: queryLockClaim блокирует и проверяет точного владельца и поколение перед финализацией.
	//go:embed sql/lock_claim.sql
	queryLockClaim string
	// queryDeleteBindings embeds a binding-deletion statement that current finalization does not call.
	// RU: queryDeleteBindings встраивает SQL удаления привязок, который текущая финализация не вызывает.
	//go:embed sql/delete_bindings.sql
	queryDeleteBindings string
	// queryDeleteDownloadGrants embeds a grant-deletion statement not called by current finalization.
	// RU: queryDeleteDownloadGrants встраивает SQL удаления разрешений, не вызываемый текущей финализацией.
	//go:embed sql/delete_download_grants.sql
	queryDeleteDownloadGrants string
	// queryDeleteContent removes the exact stored object receipt from the ledger.
	// RU: queryDeleteContent удаляет из ledger receipt точного сохранённого объекта.
	//go:embed sql/delete_content.sql
	queryDeleteContent string
	// queryDeleteRevisions removes revision metadata after the last content version is purged.
	// RU: queryDeleteRevisions удаляет метаданные версий после очистки последнего содержимого.
	//go:embed sql/delete_revisions.sql
	queryDeleteRevisions string
	// queryHasContent checks whether other revisions still retain content.
	// RU: queryHasContent проверяет, осталось ли содержимое в других версиях.
	//go:embed sql/has_content.sql
	queryHasContent string
	// queryReleaseClaim releases a fenced claim when further revisions remain.
	// RU: queryReleaseClaim освобождает защищённый claim, если остались другие версии.
	//go:embed sql/release_claim.sql
	queryReleaseClaim string
	// queryUpsertServiceSubject resolves the organization-scoped audit actor.
	// RU: queryUpsertServiceSubject находит служебного actor аудита в области организации.
	//go:embed sql/upsert_service_subject.sql
	queryUpsertServiceSubject string
	// queryFinalizeTombstone marks the artifact purged under its exact claim.
	// RU: queryFinalizeTombstone отмечает артефакт очищенным под его точным claim.
	//go:embed sql/finalize_tombstone.sql
	queryFinalizeTombstone string
	// queryInsertAuditEvent records the final purge with project and correlation provenance.
	// RU: queryInsertAuditEvent записывает окончательную очистку с проектом и корреляцией.
	//go:embed sql/insert_audit_event.sql
	queryInsertAuditEvent string
)
