package platform

import (
	"strings"
	"testing"
)

func TestEmailCredentialValueBoundsAndNoImplicitTrimming(t *testing.T) {
	for _, test := range []struct {
		kind, value string
		valid       bool
	}{
		{"AUTH_SECRET", "  synthetic password  ", true}, {"USERNAME", "user@example.test", true},
		{"AUTH_SECRET", "", false}, {"AUTH_SECRET", "abc\x00def", false}, {"AUTH_SECRET", "abc\r\ndef", false},
		{"USERNAME", strings.Repeat("x", 321), false}, {"AUTH_SECRET", strings.Repeat("x", (16<<10)+1), false},
		{"CA_CERTIFICATE", "not a certificate", false}, {"CA_CERTIFICATE", "-----BEGIN PRIVATE KEY-----\ninvalid\n-----END PRIVATE KEY-----", false},
		{"TOKEN", "synthetic", false},
	} {
		if got := validEmailCredentialValue(test.kind, []byte(test.value)); got != test.valid {
			t.Fatalf("kind %s: got %v", test.kind, got)
		}
	}
}
