package codexappserver

import (
	"bytes"
	"strings"
	"testing"
)

func TestMaskAccountReturnsOnlyBoundedMetadata(t *testing.T) {
	t.Parallel()

	if got := maskAccount("owner@example.test"); got != "o***@example.test" {
		t.Fatalf("unexpected masked account: %q", got)
	}
	for _, invalid := range []string{"", "owner", "@example.test"} {
		if got := maskAccount(invalid); got != "configured" {
			t.Fatalf("invalid account was reflected: %q", got)
		}
	}
}

func TestBoundedWriterDoesNotRetainProviderDiagnostic(t *testing.T) {
	t.Parallel()

	var target bytes.Buffer
	writer := &boundedWriter{target: &target, remaining: 8}
	raw := []byte("token=super-secret-provider-value")
	count, err := writer.Write(raw)
	if err != nil || count != len(raw) || target.Len() != 8 {
		t.Fatalf("bounded writer result is invalid: %d %d %v", count, target.Len(), err)
	}
	if strings.Contains(target.String(), "super-secret-provider-value") {
		t.Fatal("provider diagnostic exceeded the bounded buffer")
	}
}
