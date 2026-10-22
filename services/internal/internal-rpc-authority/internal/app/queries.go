package app

import _ "embed"

// queryRecordDatabaseCredentialSessionReadback embeds named database-session readback SQL as package data. Declaring this string does not execute a statement, configure a session or prove its immutable session_user identity.
// queryRecordDatabaseCredentialSessionReadback встраивает именованный SQL чтения сессии БД как данные пакета. Объявление строки не выполняет запрос, не настраивает сессию и не доказывает неизменяемую идентичность session_user.
//
//go:embed sql/database_credential__record_session_readback.sql
var queryRecordDatabaseCredentialSessionReadback string
