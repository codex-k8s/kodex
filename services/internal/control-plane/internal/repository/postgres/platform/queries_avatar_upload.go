package platform

import _ "embed"

var (
	//go:embed sql/avatar_upload_reserve.sql
	queryAvatarUploadReserve string
	//go:embed sql/avatar_upload_mark_materialized.sql
	queryAvatarUploadMarkMaterialized string
	//go:embed sql/avatar_upload_lock_reservation.sql
	queryAvatarUploadLockReservation string
	//go:embed sql/avatar_upload_mark_finalized.sql
	queryAvatarUploadMarkFinalized string
	//go:embed sql/avatar_upload_insert_audit.sql
	queryAvatarUploadInsertAudit string
	//go:embed sql/avatar_upload_insert_idempotency_receipt.sql
	queryAvatarUploadInsertIdempotencyReceipt string
	//go:embed sql/avatar_upload_claim_compensation.sql
	queryAvatarUploadClaimCompensation string
	//go:embed sql/avatar_upload_claim_expired.sql
	queryAvatarUploadClaimExpired string
	//go:embed sql/avatar_upload_record_compensation_descriptor.sql
	queryAvatarUploadRecordCompensationDescriptor string
	//go:embed sql/avatar_upload_complete_compensation.sql
	queryAvatarUploadCompleteCompensation string
)
