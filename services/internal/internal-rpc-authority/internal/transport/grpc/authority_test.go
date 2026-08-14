package authoritygrpc

import (
	"testing"

	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
)

func TestAuthoritySourcePreservesSupportedPolicySources(t *testing.T) {
	t.Parallel()

	tests := map[string]internalrpcauthorityv1.AuthoritySource{
		"WORKLOAD_READINESS": internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_WORKLOAD_READINESS,
		"PROVIDER_READBACK":  internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_PROVIDER_READBACK,
		"GIT_RECONCILIATION": internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_GIT_RECONCILIATION,
		"RUNTIME_EXECUTION":  internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_RUNTIME_EXECUTION,
	}
	for source, expected := range tests {
		source, expected := source, expected
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if actual := authoritySource(source); actual != expected {
				t.Fatalf("authority source mismatch: got %s, want %s", actual, expected)
			}
		})
	}
}

func TestAuthoritySourceRejectsUnknownValue(t *testing.T) {
	t.Parallel()
	if actual := authoritySource("UNKNOWN"); actual != internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_UNSPECIFIED {
		t.Fatalf("unknown authority source must remain unspecified: %s", actual)
	}
}
