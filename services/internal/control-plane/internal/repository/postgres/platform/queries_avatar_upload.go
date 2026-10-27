package platform

import _ "embed"

var (
	// queryAvatarUploadReserve embeds a fifteen-minute avatar reservation scoped by
	// tenant/actor/operation/idempotency key. Replay returns old identity only when semantic
	// metadata/version pins match, without refreshing its expiry; actor access, object creation and
	// adoption happen outside this SQL.
	// queryAvatarUploadReserve встраивает пятнадцатиминутное avatar reservation по
	// tenant/actor/operation/idempotency key. Replay возвращает прежнюю identity только при matching
	// semantic metadata/version pins без продления expiry; actor access, object creation и adoption
	// выполняются вне SQL.
	//go:embed sql/avatar_upload_reserve.sql
	queryAvatarUploadReserve string
	// queryAvatarUploadMarkMaterialized embeds exact tenant/reservation/key/digest/size adoption of
	// object version/etag into MATERIALIZED, refreshing expiry and advancing version even for an
	// identical materialized replay. It records descriptor metadata only; object existence/content and
	// authorization are not proven by this update.
	// queryAvatarUploadMarkMaterialized встраивает exact tenant/reservation/key/digest/size adoption
	// object version/etag в MATERIALIZED, продлевая expiry и увеличивая version даже при identical
	// materialized replay. Записывает только descriptor metadata; object existence/content и
	// authorization update не доказывает.
	//go:embed sql/avatar_upload_mark_materialized.sql
	queryAvatarUploadMarkMaterialized string
	// queryAvatarUploadLockReservation embeds an exact tenant/actor/operation/idempotency-key/intent
	// reservation lock and returns adoption/version metadata. It does not check expiry, fresh actor
	// access or agent OCC by itself.
	// queryAvatarUploadLockReservation встраивает exact tenant/actor/operation/idempotency-key/intent
	// reservation lock и возвращает adoption/version metadata. Самостоятельно не проверяет expiry,
	// fresh actor access либо agent OCC.
	//go:embed sql/avatar_upload_lock_reservation.sql
	queryAvatarUploadLockReservation string
	// queryAvatarUploadMarkFinalized embeds MATERIALIZED-to-FINALIZED OCC against exact
	// artifact/object/version/etag pins and records finalization time. Tenant/actor/agent checks must
	// precede use on the locked reservation; this update neither inserts the artifact nor commits its
	// adoption.
	// queryAvatarUploadMarkFinalized встраивает MATERIALIZED-to-FINALIZED OCC по exact
	// artifact/object/version/etag pins и записывает finalization time. Tenant/actor/agent checks
	// выполняются до использования locked reservation; update не добавляет artifact и не фиксирует
	// adoption.
	//go:embed sql/avatar_upload_mark_finalized.sql
	queryAvatarUploadMarkFinalized string
	// queryAvatarUploadInsertAudit embeds successful avatar-upload audit metadata with exact
	// tenant/project/actor/agent and correlation inputs. It must share the owner transaction with real
	// adoption; an audit row alone is not proof of upload or permission.
	// queryAvatarUploadInsertAudit встраивает successful avatar-upload audit metadata с exact
	// tenant/project/actor/agent и correlation inputs. Должен использовать owner transaction реального
	// adoption; одна audit row не доказывает upload либо permission.
	//go:embed sql/avatar_upload_insert_audit.sql
	queryAvatarUploadInsertAudit string
	// queryAvatarUploadInsertIdempotencyReceipt embeds a twenty-four-hour AGENT response receipt for
	// exact tenant/actor/avatar operation/key/digest. It stores the caller-supplied response in the
	// owner transaction and does not independently verify ownership, version or external upload
	// success.
	// queryAvatarUploadInsertIdempotencyReceipt встраивает двадцатичетырёхчасовой AGENT response
	// receipt для exact tenant/actor/avatar operation/key/digest. Сохраняет caller-supplied response в
	// owner transaction, самостоятельно не проверяя ownership, version либо external upload success.
	//go:embed sql/avatar_upload_insert_idempotency_receipt.sql
	queryAvatarUploadInsertIdempotencyReceipt string
	// queryAvatarUploadClaimCompensation embeds exact tenant/object/digest/size compensation claim for
	// reserved/materialized/compensating state while rejecting an artifact-history adoption. Existing
	// descriptors must match except an unmaterialized reservation may acquire them; claiming does not
	// prove deletion or permit broad object cleanup.
	// queryAvatarUploadClaimCompensation встраивает exact tenant/object/digest/size compensation claim
	// для reserved/materialized/compensating state с отказом при artifact-history adoption. Existing
	// descriptors совпадают, кроме возможности получить их для unmaterialized reservation; claim не
	// доказывает deletion и не разрешает broad object cleanup.
	//go:embed sql/avatar_upload_claim_compensation.sql
	queryAvatarUploadClaimCompensation string
	// queryAvatarUploadClaimExpired embeds a bounded global SKIP LOCKED sweep of expired unadopted
	// reservations or compensating claims older than five minutes, advancing cleanup version/time. It
	// is a trusted maintenance path without caller tenant filtering; the returned descriptor/version
	// must bind subsequent cleanup.
	// queryAvatarUploadClaimExpired встраивает bounded global SKIP LOCKED sweep expired unadopted
	// reservations либо compensating claims старше пяти минут с увеличением cleanup version/time. Это
	// trusted maintenance path без caller tenant filtering; returned descriptor/version связывает
	// subsequent cleanup.
	//go:embed sql/avatar_upload_claim_expired.sql
	queryAvatarUploadClaimExpired string
	// queryAvatarUploadRecordCompensationDescriptor embeds OCC recording of version/etag only for an
	// exact compensating reservation whose descriptor is still empty and object key/digest/size match
	// with no artifact adoption. It records observation metadata; external identity/content validation
	// and delete/readback remain outside SQL.
	// queryAvatarUploadRecordCompensationDescriptor встраивает OCC запись version/etag только для
	// exact compensating reservation с ещё пустым descriptor, matching object key/digest/size и без
	// artifact adoption. Записывает observation metadata; external identity/content validation и
	// delete/readback остаются вне SQL.
	//go:embed sql/avatar_upload_record_compensation_descriptor.sql
	queryAvatarUploadRecordCompensationDescriptor string
	// queryAvatarUploadCompleteCompensation embeds COMPENSATING-to-COMPENSATED OCC for the exact
	// key/version/etag/digest only while artifact history still shows no adoption. Caller must prove
	// authoritative object absence and tenant/claim ownership first; the update performs no storage
	// deletion.
	// queryAvatarUploadCompleteCompensation встраивает COMPENSATING-to-COMPENSATED OCC exact
	// key/version/etag/digest только при отсутствии adoption в artifact history. Caller заранее
	// доказывает authoritative object absence и tenant/claim ownership; update не удаляет storage
	// object.
	//go:embed sql/avatar_upload_complete_compensation.sql
	queryAvatarUploadCompleteCompensation string
)
