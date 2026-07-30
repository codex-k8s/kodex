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

//go:embed sql/verifier__activate_snapshot.sql
var verifierActivateSnapshotSQL string

//go:embed sql/verifier__accept_context.sql
var verifierAcceptContextSQL string

//go:embed sql/verifier__readiness.sql
var verifierReadinessSQL string

//go:embed sql/reservations__delete_expired.sql
var reservationsDeleteExpiredSQL string

type querySet struct {
	proofReserve              string
	contextReserve            string
	verifierActivateSnapshot  string
	verifierAcceptContext     string
	verifierReadiness         string
	reservationsDeleteExpired string
}

func loadQueries() (querySet, error) {
	queries := querySet{
		proofReserve:              proofReserveSQL,
		contextReserve:            contextReserveSQL,
		verifierActivateSnapshot:  verifierActivateSnapshotSQL,
		verifierAcceptContext:     verifierAcceptContextSQL,
		verifierReadiness:         verifierReadinessSQL,
		reservationsDeleteExpired: reservationsDeleteExpiredSQL,
	}
	for _, definition := range []struct {
		name        string
		cardinality string
		body        string
	}{
		{"proof__reserve", "one", queries.proofReserve},
		{"context__reserve", "one", queries.contextReserve},
		{"verifier__activate_snapshot", "one", queries.verifierActivateSnapshot},
		{"verifier__accept_context", "one", queries.verifierAcceptContext},
		{"verifier__readiness", "one", queries.verifierReadiness},
		{"reservations__delete_expired", "exec", queries.reservationsDeleteExpired},
	} {
		header := fmt.Sprintf("-- name: %s :%s", definition.name, definition.cardinality)
		if strings.TrimSpace(definition.body) == "" ||
			!strings.HasPrefix(strings.TrimSpace(definition.body), header) {
			return querySet{}, fmt.Errorf("invalid embedded query %s", definition.name)
		}
	}
	return queries, nil
}
