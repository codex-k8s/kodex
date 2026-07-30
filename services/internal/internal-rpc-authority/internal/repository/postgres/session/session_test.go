package session

import (
	"strings"
	"testing"
)

func TestLoadQueries(t *testing.T) {
	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("loadQueries() error = %v", err)
	}
	for name, body := range map[string]string{
		"session":    queries.sessionIdentity,
		"issuer":     queries.issuerAssume,
		"verifier":   queries.verifierAssume,
		"reconciler": queries.databaseCredentialReconcilerAssume,
	} {
		if strings.TrimSpace(body) == "" {
			t.Fatalf("%s query is empty", name)
		}
	}
}

func TestAssumeQueryRejectsUnknownCapability(t *testing.T) {
	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("loadQueries() error = %v", err)
	}
	if _, err := assumeQuery(queries, "internal_rpc_authority_unknown"); err == nil {
		t.Fatal("assumeQuery() accepted unknown capability")
	}
}
