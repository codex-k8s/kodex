package entity

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPersistedAuthorizationNeverContainsProviderLoginID(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(ProviderAuthorization{ID: "authorization", State: "PENDING"})
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.ToLower(string(raw))
	if strings.Contains(payload, "loginid") || strings.Contains(payload, "login_id") {
		t.Fatal("private provider loginId entered persistent authorization payload")
	}
}
