package authority

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed sql/proof__reserve.sql
var proofReserveSQL string

//go:embed sql/context__reserve.sql
var contextReserveSQL string

//go:embed sql/continuation__reserve.sql
var continuationReserveSQL string

//go:embed sql/verifier__activate_snapshot.sql
var verifierActivateSnapshotSQL string

//go:embed sql/verifier__accept_context.sql
var verifierAcceptContextSQL string

//go:embed sql/verifier__freshness.sql
var verifierFreshnessSQL string

//go:embed sql/verifier__readiness.sql
var verifierReadinessSQL string

//go:embed sql/context_reservations__delete_expired.sql
var contextReservationsDeleteExpiredSQL string

//go:embed sql/proof_reservations__delete_expired.sql
var proofReservationsDeleteExpiredSQL string

//go:embed sql/context__register_issued.sql
var contextRegisterIssuedSQL string

//go:embed sql/context_bindings__cleanup.sql
var contextBindingsCleanupSQL string

//go:embed sql/context__issued_readback.sql
var contextIssuedReadbackSQL string

type querySet struct {
	contextIssuedReadback            string
	contextRegisterIssued            string
	contextBindingsCleanup           string
	proofReserve                     string
	contextReserve                   string
	continuationReserve              string
	verifierActivateSnapshot         string
	verifierAcceptContext            string
	verifierReadiness                string
	verifierFreshness                string
	contextReservationsDeleteExpired string
	proofReservationsDeleteExpired   string
}

func loadQueries() (querySet, error) {
	queries := querySet{
		contextIssuedReadback:            contextIssuedReadbackSQL,
		contextRegisterIssued:            contextRegisterIssuedSQL,
		contextBindingsCleanup:           contextBindingsCleanupSQL,
		proofReserve:                     proofReserveSQL,
		contextReserve:                   contextReserveSQL,
		continuationReserve:              continuationReserveSQL,
		verifierActivateSnapshot:         verifierActivateSnapshotSQL,
		verifierAcceptContext:            verifierAcceptContextSQL,
		verifierReadiness:                verifierReadinessSQL,
		verifierFreshness:                verifierFreshnessSQL,
		contextReservationsDeleteExpired: contextReservationsDeleteExpiredSQL,
		proofReservationsDeleteExpired:   proofReservationsDeleteExpiredSQL,
	}
	for _, definition := range []struct {
		name        string
		cardinality string
		body        string
	}{
		{"context__issued_readback", "one", queries.contextIssuedReadback},
		{"context__register_issued", "one", queries.contextRegisterIssued},
		{"context_bindings__cleanup", "one", queries.contextBindingsCleanup},
		{"proof__reserve", "one", queries.proofReserve},
		{"context__reserve", "one", queries.contextReserve},
		{"continuation__reserve", "one", queries.continuationReserve},
		{"verifier__activate_snapshot", "one", queries.verifierActivateSnapshot},
		{"verifier__accept_context", "one", queries.verifierAcceptContext},
		{"verifier__readiness", "one", queries.verifierReadiness},
		{"verifier__freshness", "one", queries.verifierFreshness},
		{
			"context_reservations__delete_expired",
			"exec",
			queries.contextReservationsDeleteExpired,
		},
		{
			"proof_reservations__delete_expired",
			"exec",
			queries.proofReservationsDeleteExpired,
		},
	} {
		header := fmt.Sprintf("-- name: %s :%s", definition.name, definition.cardinality)
		if strings.TrimSpace(definition.body) == "" ||
			!strings.HasPrefix(strings.TrimSpace(definition.body), header) {
			return querySet{}, fmt.Errorf("invalid embedded query %s", definition.name)
		}
	}
	return queries, nil
}
