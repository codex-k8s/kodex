package platform

import _ "embed"

// queryPersonalCredentialTestInsert creates a DUE job from the exact owner binding and immutable snapshot.
// queryPersonalCredentialTestInsert создаёт DUE задание из точной owner binding и immutable snapshot.
//
//go:embed sql/personal_credential_test__insert.sql
var queryPersonalCredentialTestInsert string

// queryPersonalCredentialTestBindingState persists verification metadata with binding version and generation fences.
// queryPersonalCredentialTestBindingState сохраняет metadata проверки с ограждениями binding version и generation.
//
//go:embed sql/personal_credential_test__binding_state.sql
var queryPersonalCredentialTestBindingState string

// queryPersonalCredentialTestCancelBinding closes nonterminal jobs and clears leases when their binding is replaced or revoked.
// queryPersonalCredentialTestCancelBinding закрывает незавершённые задания и leases при замене либо отзыве привязки.
//
//go:embed sql/personal_credential_test__cancel_binding.sql
var queryPersonalCredentialTestCancelBinding string

// queryPersonalCredentialTestCandidates selects a bounded deterministic page of DUE or expired jobs for owner locking.
// queryPersonalCredentialTestCandidates выбирает bounded упорядоченную страницу DUE либо истёкших заданий для owner locking.
//
//go:embed sql/personal_credential_test__candidates.sql
var queryPersonalCredentialTestCandidates string

// queryPersonalCredentialTestLocator resolves tenant-owned job coordinates before locking Connection and binding.
// queryPersonalCredentialTestLocator разрешает tenant-owned координаты задания до блокировки Connection и binding.
//
//go:embed sql/personal_credential_test__locator.sql
var queryPersonalCredentialTestLocator string

// queryPersonalCredentialTestLockBinding locks the exact binding after its Connection without requiring an active user for cleanup.
// queryPersonalCredentialTestLockBinding блокирует точную привязку после Connection, не требуя активного пользователя для cleanup.
//
//go:embed sql/personal_credential_test__lock_binding.sql
var queryPersonalCredentialTestLockBinding string

// queryPersonalCredentialTestLock locks immutable job input and its current lease after the Connection and binding locks.
// queryPersonalCredentialTestLock блокирует immutable input задания и текущую аренду после блокировок Connection и binding.
//
//go:embed sql/personal_credential_test__lock.sql
var queryPersonalCredentialTestLock string

// queryPersonalCredentialTestClaim assigns the first finite lease only to the expected DUE version.
// queryPersonalCredentialTestClaim назначает первую конечную аренду только ожидаемой DUE версии.
//
//go:embed sql/personal_credential_test__claim.sql
var queryPersonalCredentialTestClaim string

// queryPersonalCredentialTestBegin consumes one CLAIMED admission and records its durable timestamp.
// queryPersonalCredentialTestBegin одноразово расходует CLAIMED допуск и сохраняет его timestamp.
//
//go:embed sql/personal_credential_test__begin.sql
var queryPersonalCredentialTestBegin string

// queryPersonalCredentialTestFinish closes one expected job version and clears all live lease coordinates.
// queryPersonalCredentialTestFinish закрывает одну ожидаемую версию задания и очищает все действующие координаты аренды.
//
//go:embed sql/personal_credential_test__finish.sql
var queryPersonalCredentialTestFinish string

// queryPersonalCredentialTestResetBinding releases CHECKING only for the exact still-current binding snapshot.
// queryPersonalCredentialTestResetBinding снимает CHECKING только для точного всё ещё актуального binding snapshot.
//
//go:embed sql/personal_credential_test__reset_binding.sql
var queryPersonalCredentialTestResetBinding string
