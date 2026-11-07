package platform

import _ "embed"

// queryPersonalCredentialConnection resolves and locks the exact visible Connection for a self operation.
// queryPersonalCredentialConnection разрешает и блокирует точный видимый Connection собственной операции.
//
//go:embed sql/personal_credential__connection.sql
var queryPersonalCredentialConnection string

// queryPersonalCredentialReadOwned embeds the addressed, principal-qualified metadata read.
// queryPersonalCredentialReadOwned встраивает адресное чтение metadata с привязкой к principal.
//
//go:embed sql/personal_credential__read_owned.sql
var queryPersonalCredentialReadOwned string

// queryPersonalCredentialLockOwned locks the same owner row used by revoke and call-start decisions.
// queryPersonalCredentialLockOwned блокирует ту же owner row, которую используют revoke и начало вызова.
//
//go:embed sql/personal_credential__lock_owned.sql
var queryPersonalCredentialLockOwned string

// queryPersonalCredentialCreate creates only an empty self-owned binding under exact Connection pins.
// queryPersonalCredentialCreate создаёт только пустую собственную привязку с точными pins Connection.
//
//go:embed sql/personal_credential__create.sql
var queryPersonalCredentialCreate string

// queryPersonalCredentialInsertRevision appends immutable metadata after the caller locks its binding.
// queryPersonalCredentialInsertRevision добавляет immutable metadata после блокировки собственной привязки.
//
//go:embed sql/personal_credential__insert_revision.sql
var queryPersonalCredentialInsertRevision string

// queryPersonalCredentialReplace switches only the owned current revision and clears previous eligibility.
// queryPersonalCredentialReplace меняет только собственную текущую ревизию и сбрасывает прежнюю eligibility.
//
//go:embed sql/personal_credential__replace.sql
var queryPersonalCredentialReplace string

// queryPersonalCredentialRevoke retains revision history while advancing the self-owned fence.
// queryPersonalCredentialRevoke сохраняет историю ревизий и увеличивает собственное ограждение.
//
//go:embed sql/personal_credential__revoke.sql
var queryPersonalCredentialRevoke string
