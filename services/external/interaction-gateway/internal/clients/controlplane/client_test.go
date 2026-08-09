package controlplane

import (
	"testing"

	domaincontrol "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/controlplane"
)

func TestAgentBotReceiptAdapterDropsPrivateCredentialCoordinates(t *testing.T) {
	t.Parallel()
	receipt := providerReceiptToProto(domaincontrol.ProviderEffectReceipt{
		TargetKind:               "agent_bot_identity",
		CredentialBindingID:      "11111111-1111-4111-8111-111111111111",
		CredentialBindingVersion: 7,
		CredentialBindingSHA256:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if receipt.GetCredentialBindingId() != "" || receipt.GetCredentialBindingVersion() != 0 ||
		receipt.GetCredentialBindingSha256() != "" {
		t.Fatalf("Agent bot adapter emitted private credential coordinates: %#v", receipt)
	}
}
