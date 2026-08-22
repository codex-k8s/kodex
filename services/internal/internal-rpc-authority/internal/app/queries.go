package app

import _ "embed"

//go:embed sql/database_credential__record_session_readback.sql
var queryRecordDatabaseCredentialSessionReadback string
