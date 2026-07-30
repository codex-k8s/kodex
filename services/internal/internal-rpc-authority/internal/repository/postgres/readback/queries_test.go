package readback

import (
	"strings"
	"testing"
)

func TestReadbackNamedSQLИспользуетТолькоOwnerFunctions(t *testing.T) {
	if err := validateQueries(); err != nil {
		t.Fatal(err)
	}
	for name, query := range map[string]string{
		"issue":   issueChallengeSQL,
		"consume": consumeChallengeSQL,
	} {
		upper := strings.ToUpper(query)
		if strings.Contains(upper, "INSERT INTO") ||
			strings.Contains(upper, "UPDATE INTERNAL_RPC_AUTHORITY") ||
			strings.Contains(upper, "DELETE FROM") {
			t.Fatalf("%s query retains direct DML: %s", name, query)
		}
		if !strings.Contains(query, "_authority_readback_attestation_challenge(") {
			t.Fatalf("%s query does not call exact owner function", name)
		}
	}
	if strings.Contains(strings.ToUpper(loadChallengeSQL), "FOR UPDATE") {
		t.Fatal("runtime principal still locks challenge outside owner function")
	}
}
