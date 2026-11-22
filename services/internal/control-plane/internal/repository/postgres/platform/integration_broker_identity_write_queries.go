package platform

import _ "embed"

// queryBrokerIdentityConnectionLock serializes metadata revision allocation with Connection changes.
// queryBrokerIdentityConnectionLock сериализует metadata revision allocation с Connection changes.
//
//go:embed sql/broker_identity__connection_lock.sql
var queryBrokerIdentityConnectionLock string

// queryBrokerIdentityBindingLock validates the exact current personal oneof before metadata writes.
// queryBrokerIdentityBindingLock проверяет exact current personal oneof до metadata writes.
//
//go:embed sql/broker_identity__binding_lock.sql
var queryBrokerIdentityBindingLock string

// queryBrokerIdentityInsert allocates a new immutable metadata revision under its owner locks.
// queryBrokerIdentityInsert выделяет новую immutable metadata revision под owner locks.
//
//go:embed sql/broker_identity__insert.sql
var queryBrokerIdentityInsert string

// queryBrokerIdentityReadOwned joins an exact identity revision to every already resolved owner pin.
// queryBrokerIdentityReadOwned связывает exact identity revision со всеми resolved owner pins.
//
//go:embed sql/broker_identity__read_owned.sql
var queryBrokerIdentityReadOwned string

// queryBrokerIdentityAttachPersonal advances the personal generation and clears every prior test pin.
// queryBrokerIdentityAttachPersonal увеличивает personal generation и очищает prior test pins.
//
//go:embed sql/broker_identity__attach_personal.sql
var queryBrokerIdentityAttachPersonal string

// queryBrokerIdentityAttachShared changes only the explicitly selected shared metadata source.
// queryBrokerIdentityAttachShared меняет только явно выбранный shared metadata source.
//
//go:embed sql/broker_identity__attach_shared.sql
var queryBrokerIdentityAttachShared string

// queryBrokerIdentityKubernetesFact derives the still-live owner deadline from completed immutable facts.
// The final binding UPDATE checks this database-derived deadline after dependency cancellation.
// queryBrokerIdentityKubernetesFact выводит ещё действующий owner deadline из completed immutable facts.
// Final binding UPDATE проверяет database-derived deadline после dependency cancellation.
//
//go:embed sql/broker_identity__kubernetes_fact.sql
var queryBrokerIdentityKubernetesFact string
