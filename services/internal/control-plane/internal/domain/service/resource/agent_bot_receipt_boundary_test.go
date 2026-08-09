package resource

import (
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

func TestAgentBotReceiptBoundaryAcceptsOnlyOpaqueRefsWithoutCredentialEvidence(t *testing.T) {
	t.Parallel()
	receipt := value.ProviderEffectReceipt{
		ProviderTeamRef:   "11111111-1111-4111-8111-111111111111",
		ProviderObjectRef: "22222222-2222-4222-8222-222222222222",
	}
	if !validAgentBotReceiptBoundary(receipt) {
		t.Fatal("opaque Agent bot receipt refs were rejected")
	}
	privateVariants := []value.ProviderEffectReceipt{receipt, receipt, receipt, receipt, receipt, receipt, receipt}
	privateVariants[0].ProviderTeamRef = "raw-mattermost-team-id"
	privateVariants[1].CredentialBindingID = "33333333-3333-4333-8333-333333333333"
	privateVariants[2].CredentialBindingVersion = 7
	privateVariants[3].CredentialBindingSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	privateVariants[4].SecretRef = "private/vault/path"
	privateVariants[5].SecretVersion = 9
	privateVariants[6].SecretContentSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for index, candidate := range privateVariants {
		if validAgentBotReceiptBoundary(candidate) {
			t.Fatalf("private Agent bot receipt variant %d was accepted", index)
		}
	}
}
