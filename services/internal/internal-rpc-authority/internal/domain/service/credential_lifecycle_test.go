package service

import (
	"strings"
	"testing"
)

func TestCompleteChangedDigestSetAcceptsExactPreMaterializedSet(t *testing.T) {
	t.Parallel()

	digestA := strings.Repeat("a", 64)
	digestB := strings.Repeat("b", 64)
	before := map[string]string{"publisher": digestA, "readback": digestB}
	after := map[string]string{"publisher": digestA, "readback": digestB}
	if !completeChangedDigestSet(before, after, nil) {
		t.Fatal("exact pre-materialized digest set was rejected")
	}
	after["publisher"] = strings.Repeat("c", 64)
	if completeChangedDigestSet(before, after, nil) {
		t.Fatal("changed pre-materialized digest set was accepted")
	}
}
