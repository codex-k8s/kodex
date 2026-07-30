package authority

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed sql/replay__reserve.sql
var replayReserveSQL string

//go:embed sql/verifier__activate_snapshot.sql
var verifierActivateSnapshotSQL string

//go:embed sql/verifier__accept_context.sql
var verifierAcceptContextSQL string

//go:embed sql/verifier__readiness.sql
var verifierReadinessSQL string

//go:embed sql/replay__delete_expired.sql
var replayDeleteExpiredSQL string

type querySet struct {
	replayReserve            string
	verifierActivateSnapshot string
	verifierAcceptContext    string
	verifierReadiness        string
	replayDeleteExpired      string
}

func loadQueries() (querySet, error) {
	queries := querySet{
		replayReserve:            replayReserveSQL,
		verifierActivateSnapshot: verifierActivateSnapshotSQL,
		verifierAcceptContext:    verifierAcceptContextSQL,
		verifierReadiness:        verifierReadinessSQL,
		replayDeleteExpired:      replayDeleteExpiredSQL,
	}
	for _, definition := range []struct {
		name        string
		cardinality string
		body        string
	}{
		{"replay__reserve", "one", queries.replayReserve},
		{"verifier__activate_snapshot", "one", queries.verifierActivateSnapshot},
		{"verifier__accept_context", "one", queries.verifierAcceptContext},
		{"verifier__readiness", "one", queries.verifierReadiness},
		{"replay__delete_expired", "exec", queries.replayDeleteExpired},
	} {
		header := fmt.Sprintf("-- name: %s :%s", definition.name, definition.cardinality)
		if strings.TrimSpace(definition.body) == "" ||
			!strings.HasPrefix(strings.TrimSpace(definition.body), header) {
			return querySet{}, fmt.Errorf("invalid embedded query %s", definition.name)
		}
	}
	return queries, nil
}
