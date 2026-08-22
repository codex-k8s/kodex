package app

import (
	"errors"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/provider"
)

func TestSafeCodeDoesNotExposeUnknownError(t *testing.T) {
	t.Parallel()

	if got := safeCode(errors.New("provider secret response")); got != "RUNTIME_UNAVAILABLE" {
		t.Fatalf("safeCode() = %q", got)
	}
	if got := safeCode(&provider.SafeError{Code: "PROVIDER_RATE_LIMITED"}); got != "PROVIDER_RATE_LIMITED" {
		t.Fatalf("safeCode() = %q", got)
	}
}

func TestStableIdempotencyKey(t *testing.T) {
	t.Parallel()

	first := stableIdempotencyKey("lease", "complete")
	if first == "" || first != stableIdempotencyKey("lease", "complete") || first == stableIdempotencyKey("lease", "retry") {
		t.Fatalf("stableIdempotencyKey() is not stable and intent-bound")
	}
}

func TestBoundedResult(t *testing.T) {
	t.Parallel()

	if got := bounded("  abcdef  ", 4); got != "abcd" {
		t.Fatalf("bounded() = %q", got)
	}
}
