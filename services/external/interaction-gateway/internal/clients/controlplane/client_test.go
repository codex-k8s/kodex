package controlplane

import (
	"errors"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	domaincontrol "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/controlplane"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func TestMappingRPCErrorPreservesSafeWorkspaceStage(t *testing.T) {
	t.Parallel()

	current := status.New(codes.PermissionDenied, "control-plane permission denied")
	withDetail, err := current.WithDetails(&controlplanev1.ErrorDetail{
		Code: "WORKSPACE_MAPPING_PROVIDER_RECEIPT_REJECTED",
	})
	if err != nil {
		t.Fatal(err)
	}
	mapped := mappingRPCError(withDetail.Err())
	if !errors.Is(mapped, domaincontrol.ErrConflict) {
		t.Fatalf("unexpected mapped error: %v", mapped)
	}
	if got := domaincontrol.SafeCode(mapped); got != "WORKSPACE_MAPPING_PROVIDER_RECEIPT_REJECTED" {
		t.Fatalf("unexpected safe code: %q", got)
	}
}
