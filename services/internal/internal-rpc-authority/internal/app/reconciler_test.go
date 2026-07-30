package app

import (
	"strings"
	"testing"
)

func TestCredentialRegisteredSetIsClosedAndDeterministic(t *testing.T) {
	t.Parallel()

	registered := credentialRegisteredSet(9, strings.Repeat("b", 64))
	if len(registered.Generations) != 8 {
		t.Fatalf("unexpected registered generation count: %d", len(registered.Generations))
	}
	first := deterministicLifecycleRequestID(registered.SourceDigest)
	second := deterministicLifecycleRequestID(registered.SourceDigest)
	if first != second || !runtimeUUIDPattern.MatchString(first) {
		t.Fatalf("deterministic lifecycle request id mismatch: %q %q", first, second)
	}
}
