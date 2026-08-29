package postgres

import _ "embed"

var (
	//go:embed sql/claim_due.sql
	queryClaimDue string
	//go:embed sql/lock_claim.sql
	queryLockClaim string
	//go:embed sql/delete_bindings.sql
	queryDeleteBindings string
	//go:embed sql/delete_download_grants.sql
	queryDeleteDownloadGrants string
	//go:embed sql/delete_content.sql
	queryDeleteContent string
	//go:embed sql/upsert_service_subject.sql
	queryUpsertServiceSubject string
	//go:embed sql/finalize_tombstone.sql
	queryFinalizeTombstone string
	//go:embed sql/insert_audit_event.sql
	queryInsertAuditEvent string
)
