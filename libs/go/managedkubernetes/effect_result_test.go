package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// TestEffectResultValidation refuses ambiguous/oversized/raw diagnostic results without echoing data.
// TestEffectResultValidation отвергает ambiguous/oversized/raw diagnostic results без вывода data.
func TestEffectResultValidation(t *testing.T) {
	for _, item := range []struct {
		raw             []byte
		outcome, reason string
		valid           bool
	}{
		{[]byte(`{"metadata":{"name":"example"}}`), "SUCCEEDED", "", true},
		{[]byte(`{"partial":true}`), "UNKNOWN", "KUBERNETES_TRANSPORT_UNKNOWN", true},
		{[]byte(`{}`), "FAILED", "KUBERNETES_ACCESS_DENIED", true},
		{[]byte(`{"x":1,"x":2}`), "SUCCEEDED", "", false},
		{[]byte(`null`), "SUCCEEDED", "", false},
		{[]byte(`[]`), "SUCCEEDED", "", false},
		{[]byte("{\"x\":\"\xff\"}"), "SUCCEEDED", "", false},
		{[]byte(`{}`), "SUCCEEDED", "KUBERNETES_TIMEOUT", false},
		{[]byte(`{}`), "FAILED", "provider text with credentials", false},
		{[]byte(`{}`), "UNREGISTERED", "", false},
		{[]byte(`{"x":"` + strings.Repeat("a", MaximumEffectResultBytes) + `"}`), "SUCCEEDED", "", false},
	} {
		hash := sha256.Sum256(item.raw)
		digest := hex.EncodeToString(hash[:])
		if (ValidateEffectResult(item.raw, digest, item.outcome, item.reason) == nil) != item.valid {
			t.Fatal("effect result boundary changed")
		}
		if item.valid && ValidateEffectResult(item.raw, strings.Repeat("0", 64), item.outcome, item.reason) == nil {
			t.Fatal("result bytes were not bound to their digest")
		}
	}
}
