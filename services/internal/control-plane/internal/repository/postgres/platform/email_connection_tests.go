package platform

import _ "embed"

// queryEmailConnectionTestSetup requires a current accepted owner-bound mailbox before the
// command changes health state; an unfinished setup remains a conflict, not a queued test.
// queryEmailConnectionTestSetup требует текущий опубликованный owner-bound mailbox до смены
// health state; незавершённая настройка даёт conflict вместо постановки проверки в очередь.
//
//go:embed sql/email_connection_test__setup.sql
var queryEmailConnectionTestSetup string

// queryEmailConnectionTestMailboxMatches binds the requested health mailbox to the original
// server-assigned test snapshot and current owner eligibility without repinning during Begin.
// queryEmailConnectionTestMailboxMatches связывает запрошенный health mailbox с исходным снимком
// проверки, назначенным сервером, и текущим владельцем без переназначения pin при Begin.
//
//go:embed sql/email_connection_test__mailbox_matches.sql
var queryEmailConnectionTestMailboxMatches string
