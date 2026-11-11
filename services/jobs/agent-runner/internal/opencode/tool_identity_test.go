package opencode

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// signedToolFixture models a pinned hook envelope with an inert key for negative boundary tests.
// signedToolFixture моделирует pinned hook envelope с неактивным ключом для negative boundary tests.
func signedToolFixture(t *testing.T) ([]byte, string, string) {
	t.Helper()
	key, revision := strings.Repeat("a", 64), strings.Repeat("b", 64)
	payload := `{"session_id":"ses_nativeA","call_id":"call_1","tool":"kodex_invoke_integration","revision":"` + revision + `","input":{"grant_ref":"igr_synthetic","input":{"comment":"synthetic"}}}`
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(payload))
	var input map[string]any
	_ = json.Unmarshal([]byte(`{"grant_ref":"igr_synthetic","input":{"comment":"synthetic"}}`), &input)
	input["__kodex_native"] = map[string]string{"payload": payload, "mac": hex.EncodeToString(mac.Sum(nil))}
	raw, err := json.Marshal(map[string]any{"name": "invoke_integration", "arguments": input})
	if err != nil {
		t.Fatal(err)
	}
	return raw, key, revision
}

// TestManagedToolIdentityRejectsForgedArguments closes mutations, cross-execution proofs and missing hooks.
// It also checks that the controller receives typed original arguments without the private envelope.
// TestManagedToolIdentityRejectsForgedArguments отклоняет mutations, cross-execution proof и missing hook.
// Controller получает typed original arguments без private envelope.
func TestManagedToolIdentityRejectsForgedArguments(t *testing.T) {
	raw, key, revision := signedToolFixture(t)
	clean, id, err := VerifyManagedToolIdentity(raw, "kodex", revision, key)
	if err != nil || len(id) != 70 || strings.Contains(string(clean), "__kodex_native") || !strings.Contains(string(clean), "igr_synthetic") {
		t.Fatal("valid hook identity rejected or private proof forwarded")
	}
	for _, test := range []struct {
		name                  string
		raw                   []byte
		server, revision, key string
	}{
		{"argument", []byte(strings.Replace(string(raw), `"grant_ref":"igr_synthetic"`, `"grant_ref":"igr_foreign"`, 1)), "kodex", revision, key},
		{"method", []byte(strings.Replace(string(raw), `"name":"invoke_integration"`, `"name":"delegate_agent"`, 1)), "kodex", revision, key},
		{"execution", raw, "kodex", strings.Repeat("c", 64), key},
		{"key", raw, "kodex", revision, strings.Repeat("d", 64)},
		{"server", raw, "workspace", revision, key},
		{"missing", []byte(`{"name":"invoke_integration","arguments":{}}`), "kodex", revision, key},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := VerifyManagedToolIdentity(test.raw, test.server, test.revision, test.key); err == nil {
				t.Fatal("forged native tool envelope accepted")
			}
		})
	}
}
