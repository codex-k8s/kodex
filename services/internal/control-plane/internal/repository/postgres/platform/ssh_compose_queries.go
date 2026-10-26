package platform

import _ "embed"

// querySSHComposeContext resolves current project and execution coordinates from owner state.
// querySSHComposeContext разрешает project и execution coordinates из owner state.
//
//go:embed sql/ssh_compose__context.sql
var querySSHComposeContext string

// querySSHComposeMerge reads only committed merge evidence in the exact project repository.
// querySSHComposeMerge читает committed merge evidence exact project repository.
//
//go:embed sql/ssh_compose__merge.sql
var querySSHComposeMerge string

// querySSHComposeSourceGrants checks current version-pinned runtime grants before source access.
// querySSHComposeSourceGrants проверяет current version-pinned runtime grants до source access.
//
//go:embed sql/ssh_compose__source_grants.sql
var querySSHComposeSourceGrants string

// querySSHComposePredecessor reads an owner-scoped inspect or deployment snapshot without replay.
// querySSHComposePredecessor читает owner-scoped inspect либо deployment snapshot без replay.
//
//go:embed sql/ssh_compose__predecessor.sql
var querySSHComposePredecessor string

// querySSHComposeStoreSnapshot persists server-assigned immutable execution inputs once.
// querySSHComposeStoreSnapshot однократно сохраняет server-assigned immutable inputs.
//
//go:embed sql/ssh_compose__store_snapshot.sql
var querySSHComposeStoreSnapshot string

// querySSHComposeLease locks one exact live lease and current owner eligibility graph.
// querySSHComposeLease блокирует exact live lease и current owner eligibility graph.
//
//go:embed sql/ssh_compose__lease.sql
var querySSHComposeLease string

// querySSHComposeRenew advances only an already authorized lease without changing its generation.
// querySSHComposeRenew продлевает уже разрешённый lease, не меняя generation.
//
//go:embed sql/ssh_compose__renew.sql
var querySSHComposeRenew string

// querySSHComposeTest checks the exact health-test lease before projecting its key.
// querySSHComposeTest проверяет exact health-test lease до projection ключа.
//
//go:embed sql/ssh_compose__test.sql
var querySSHComposeTest string

// querySSHComposeStoreTestSnapshot pins the first health-test credential revision once.
// querySSHComposeStoreTestSnapshot однократно закрепляет первую health-test credential revision.
//
//go:embed sql/ssh_compose__store_test_snapshot.sql
var querySSHComposeStoreTestSnapshot string

// querySSHComposeRecovery marks an owner-reclaimed uncertain effect as read-only forever.
// querySSHComposeRecovery отмечает owner-reclaimed uncertain effect как read-only навсегда.
//
//go:embed sql/ssh_compose__recovery.sql
var querySSHComposeRecovery string

// querySSHComposeCompletion reads owner-locked completion pins without trusting worker output.
// querySSHComposeCompletion читает owner-locked completion pins, не доверяя worker output.
//
//go:embed sql/ssh_compose__completion.sql
var querySSHComposeCompletion string

// querySSHComposeSecretLock serializes environment binding against credential reservation.
// querySSHComposeSecretLock сериализует environment binding и credential reservation.
//
//go:embed sql/ssh_compose__secret_lock.sql
var querySSHComposeSecretLock string

// querySSHComposeSecretReserve checks retained environment/reveal history after taking the secret lock.
// querySSHComposeSecretReserve проверяет retained environment/reveal history после secret lock.
//
//go:embed sql/ssh_compose__secret_reserve.sql
var querySSHComposeSecretReserve string

// querySSHComposeSecretRedact removes credential hints from older terminal metadata snapshots.
// querySSHComposeSecretRedact удаляет credential hints из прежних terminal metadata snapshots.
//
//go:embed sql/ssh_compose__secret_redact.sql
var querySSHComposeSecretRedact string

// querySSHComposeSecretPurpose resolves monotonic custody for reveal and rotation completion.
// querySSHComposeSecretPurpose разрешает monotonic custody для reveal и rotation completion.
//
//go:embed sql/ssh_compose__secret_purpose.sql
var querySSHComposeSecretPurpose string

// querySSHComposeInvalidate closes dependent leases, gates and deliveries in the dependency owner's transaction.
// querySSHComposeInvalidate закрывает dependent leases, gates и deliveries в dependency owner transaction.
//
//go:embed sql/ssh_compose__invalidate.sql
var querySSHComposeInvalidate string

// querySSHComposeActiveDependency rejects health-test state changes during dependent SSH work.
// querySSHComposeActiveDependency запрещает health-test менять состояние при зависимой SSH работе.
//
//go:embed sql/ssh_compose__active_dependency.sql
var querySSHComposeActiveDependency string

// querySSHComposeInvalidateHealth closes exact SSH health authority and resets its TESTING aggregate atomically.
// querySSHComposeInvalidateHealth атомарно закрывает exact SSH health authority и сбрасывает TESTING aggregate.
//
//go:embed sql/ssh_compose__invalidate_health.sql
var querySSHComposeInvalidateHealth string

// querySSHComposeSecretPin avoids waiting behind secret lifecycle writers after invocation/connection locks; canonical resolution still checks the pinned revision.
// querySSHComposeSecretPin исключает ожидание secret lifecycle writer после invocation/connection locks; canonical resolver проверяет закреплённую revision.
//
//go:embed sql/ssh_compose__secret_pin.sql
var querySSHComposeSecretPin string
