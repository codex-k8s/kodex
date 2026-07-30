package session

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed sql/session__identity.sql
var sessionIdentitySQL string

//go:embed sql/issuer__assume.sql
var issuerAssumeSQL string

//go:embed sql/verifier__assume.sql
var verifierAssumeSQL string

//go:embed sql/database_credential_reconciler__assume.sql
var databaseCredentialReconcilerAssumeSQL string

//go:embed sql/publisher__assume.sql
var publisherAssumeSQL string

//go:embed sql/readback_attestor__assume.sql
var readbackAttestorAssumeSQL string

//go:embed sql/restore_controller__assume.sql
var restoreControllerAssumeSQL string

type querySet struct {
	sessionIdentity                    string
	issuerAssume                       string
	verifierAssume                     string
	databaseCredentialReconcilerAssume string
	publisherAssume                    string
	readbackAttestorAssume             string
	restoreControllerAssume            string
}

func loadQueries() (querySet, error) {
	queries := querySet{
		sessionIdentity:                    sessionIdentitySQL,
		issuerAssume:                       issuerAssumeSQL,
		verifierAssume:                     verifierAssumeSQL,
		databaseCredentialReconcilerAssume: databaseCredentialReconcilerAssumeSQL,
		publisherAssume:                    publisherAssumeSQL,
		readbackAttestorAssume:             readbackAttestorAssumeSQL,
		restoreControllerAssume:            restoreControllerAssumeSQL,
	}
	for _, definition := range []struct {
		name        string
		cardinality string
		body        string
	}{
		{"session__identity", "one", queries.sessionIdentity},
		{"issuer__assume", "exec", queries.issuerAssume},
		{"verifier__assume", "exec", queries.verifierAssume},
		{
			"database_credential_reconciler__assume",
			"exec",
			queries.databaseCredentialReconcilerAssume,
		},
		{"publisher__assume", "exec", queries.publisherAssume},
		{"readback_attestor__assume", "exec", queries.readbackAttestorAssume},
		{"restore_controller__assume", "exec", queries.restoreControllerAssume},
	} {
		header := fmt.Sprintf("-- name: %s :%s", definition.name, definition.cardinality)
		if strings.TrimSpace(definition.body) == "" ||
			!strings.HasPrefix(strings.TrimSpace(definition.body), header) {
			return querySet{}, fmt.Errorf("invalid embedded query %s", definition.name)
		}
	}
	return queries, nil
}
