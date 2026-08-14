package resource

import (
	"strings"
	"testing"
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

func TestGatewayPublicTLSHashesSurviveReadinessGrantRotation(t *testing.T) {
	t.Parallel()

	first := value.Principal{
		ActorID: "63dfc7d7-9439-5e8d-8953-24f975da8f32", OrganizationID: "d9b072a0-3980-57c0-a6fe-289b7a608f31",
		Permission: permissionGatewayPublicTLSPrepare, PolicyRevision: 30, AuthorityGeneration: 1,
		CallerWorkload: controlAPIGatewayWorkload, CallerSPIFFEID: controlAPIGatewaySPIFFEID,
		AuthoritySource: "WORKLOAD_READINESS", AuthorityReference: "11111111-1111-4111-8111-111111111111",
		AuthorityRevision: 100, AuthorityDigest: strings.Repeat("1", 64),
	}
	rotated := first
	rotated.CorrelationID = "22222222-2222-4222-8222-222222222222"
	rotated.PolicyRevision = 31
	rotated.AuthorityGeneration = 2
	rotated.AuthorityReference = "33333333-3333-4333-8333-333333333333"
	rotated.AuthorityRevision = 101
	rotated.AuthorityDigest = strings.Repeat("2", 64)
	rotated.AuthorityGrantGeneration = 3

	candidate := domainrepo.GatewayPublicTLSMaterial{
		Generation: 1, CertificateSHA256: strings.Repeat("a", 64),
		NotBefore: time.Date(2026, 8, 10, 20, 23, 7, 0, time.UTC),
		NotAfter:  time.Date(2026, 11, 8, 20, 23, 7, 0, time.UTC),
	}
	firstPrepare, err := gatewayPublicTLSPrepareHash(first, candidate, 0, "")
	if err != nil {
		t.Fatalf("hash first prepare: %v", err)
	}
	rotatedPrepare, err := gatewayPublicTLSPrepareHash(rotated, candidate, 0, "")
	if err != nil {
		t.Fatalf("hash rotated prepare: %v", err)
	}
	if firstPrepare != rotatedPrepare {
		t.Fatal("readiness grant rotation changed public TLS prepare intent")
	}

	first.Permission = permissionGatewayPublicTLSConfirm
	rotated.Permission = permissionGatewayPublicTLSConfirm
	firstConfirm, err := gatewayPublicTLSConfirmHash(first, candidate.Generation, candidate.CertificateSHA256)
	if err != nil {
		t.Fatalf("hash first confirm: %v", err)
	}
	rotatedConfirm, err := gatewayPublicTLSConfirmHash(rotated, candidate.Generation, candidate.CertificateSHA256)
	if err != nil {
		t.Fatalf("hash rotated confirm: %v", err)
	}
	if firstConfirm != rotatedConfirm {
		t.Fatal("readiness grant rotation changed public TLS confirm intent")
	}

	rotated.CallerWorkload = "another-workload"
	differentCaller, err := gatewayPublicTLSConfirmHash(rotated, candidate.Generation, candidate.CertificateSHA256)
	if err != nil {
		t.Fatalf("hash different caller: %v", err)
	}
	if differentCaller == firstConfirm {
		t.Fatal("stable caller identity is missing from public TLS intent")
	}
}
