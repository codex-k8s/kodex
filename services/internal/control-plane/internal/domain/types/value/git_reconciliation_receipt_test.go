package value

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGitReconciliationReceiptRequiresExactProtectedTarget(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	receipt := GitReconciliationReceipt{
		ContractVersion: 1,
		Issuer:          "https://integration-gateway.mattercodex-system.svc.cluster.local/authority/git-reconciliation",
		Purpose:         "GIT_RECONCILIATION_RECEIPT",
		WorkloadID:      "integration-gateway",
		CallerSPIFFEID:  "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway",
		FullMethod:      "/controlplane.v1.ControlPlaneService/ReconcileGitInstructionSet",
		ActorID:         uuid.NewString(), OrganizationID: uuid.NewString(), ProjectID: uuid.NewString(),
		TargetKind: "instruction_set", TargetResourceID: uuid.NewString(), TargetStableKey: "agent-instructions",
		SourceRef: "git://repository/config/agent.md", SourceRevision: 7,
		SourceSHA256: strings.Repeat("a", 64), CommandIntentSHA256: strings.Repeat("b", 64),
		ReceiptID: uuid.NewString(), ReceiptRevision: 3,
		IssuedAt: now, NotBefore: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := receipt.Validate(now); err != nil {
		t.Fatalf("exact Git receipt was rejected: %v", err)
	}
	receipt.FullMethod = "/controlplane.v1.ControlPlaneService/ManageInstructionSet"
	if err := receipt.Validate(now); err == nil {
		t.Fatal("owner-facing method was accepted as Git reconciliation authority")
	}
}
