package service

import (
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

func TestMatchesObservedCaller(t *testing.T) {
	const (
		callerSPIFFEID = "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway"
		targetSPIFFEID = "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane"
	)
	claims := model.AuthorizationClaims{
		Caller: model.Workload{SPIFFEID: callerSPIFFEID},
		Target: model.Workload{SPIFFEID: targetSPIFFEID},
	}

	if !matchesObservedCaller(claims, callerSPIFFEID) {
		t.Fatal("observed caller SPIFFE ID was rejected")
	}
	if matchesObservedCaller(claims, targetSPIFFEID) {
		t.Fatal("target SPIFFE ID was accepted as the observed caller")
	}
}
