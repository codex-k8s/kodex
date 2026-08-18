package grpc

import (
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestProviderEffectReceiptFromProtoPreservesCanonicalAuthorityDigest(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, 8, 18, 10, 0, 0, 123_000_000, time.UTC)
	original := value.ProviderEffectReceipt{
		ContractVersion:     1,
		Issuer:              "https://interaction-gateway.mattercodex-system.svc.cluster.local/authority/provider-readback",
		Audience:            "urn:mattercodex:provider-readback:mattermost",
		Purpose:             "MATTERMOST_PROVIDER_READBACK_RECEIPT",
		WorkloadID:          "interaction-gateway",
		CallerSPIFFEID:      "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway",
		FullMethod:          controlplanev1.ControlPlaneService_ManageWorkspaceMattermostMapping_FullMethodName,
		ActorID:             "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		OrganizationID:      "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		ProjectID:           "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		WorkspaceID:         "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		ProviderTeamRef:     "provider-team-one",
		ProviderObjectRef:   "provider-team-one",
		Action:              "bind",
		Effect:              "workspace_mattermost_mapping",
		EffectVersion:       7,
		EffectGeneration:    7,
		EffectSHA256:        "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		ReceiptID:           "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
		ReceiptRevision:     7,
		IssuedAt:            issuedAt,
		NotBefore:           issuedAt,
		ExpiresAt:           issuedAt.Add(2 * time.Minute),
		MaskedStatus:        "active",
		Eligible:            true,
		TargetKind:          "workspace_mattermost_mapping",
		TargetResourceID:    "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		TargetStableKey:     "workspace-cccccccccccc4ccc8ccccccccccccccc",
		CommandIntentSHA256: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	}
	reconstructed, err := providerEffectReceiptFromProto(&controlplanev1.ProviderEffectReadbackReceipt{
		ContractVersion: original.ContractVersion,
		Issuer:          original.Issuer, Purpose: original.Purpose, WorkloadId: original.WorkloadID,
		CallerSpiffeId: original.CallerSPIFFEID, FullMethod: original.FullMethod,
		ActorId: original.ActorID, OrganizationId: original.OrganizationID, ProjectId: original.ProjectID,
		WorkspaceId: original.WorkspaceID, ProviderTeamRef: original.ProviderTeamRef,
		ProviderObjectRef: original.ProviderObjectRef, Action: original.Action, Effect: original.Effect,
		EffectVersion: original.EffectVersion, EffectGeneration: original.EffectGeneration,
		EffectSha256: original.EffectSHA256, ReceiptId: original.ReceiptID,
		ReceiptRevision: original.ReceiptRevision, IssuedAt: timestamppb.New(original.IssuedAt),
		NotBefore: timestamppb.New(original.NotBefore), ExpiresAt: timestamppb.New(original.ExpiresAt),
		MaskedStatus: original.MaskedStatus, Eligible: original.Eligible, TargetKind: original.TargetKind,
		TargetResourceId: original.TargetResourceID, TargetStableKey: original.TargetStableKey,
		CommandIntentSha256: original.CommandIntentSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := internalrpcauth.CanonicalJSONSHA256(original)
	if err != nil {
		t.Fatal(err)
	}
	gotDigest, err := internalrpcauth.CanonicalJSONSHA256(reconstructed)
	if err != nil {
		t.Fatal(err)
	}
	if gotDigest != wantDigest {
		t.Fatalf("provider receipt digest changed across protobuf boundary: got %s want %s", gotDigest, wantDigest)
	}
}

func TestWorkspaceMattermostMappingActionIsClosedAndNormalized(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action controlplanev1.WorkspaceMattermostMappingAction
		want   string
	}{
		{name: "bind", action: controlplanev1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_BIND, want: "bind"},
		{name: "relink", action: controlplanev1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_RELINK, want: "relink"},
		{name: "unlink", action: controlplanev1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_UNLINK, want: "unlink"},
		{name: "unspecified", action: controlplanev1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_UNSPECIFIED},
		{name: "unknown", action: controlplanev1.WorkspaceMattermostMappingAction(99)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := workspaceMattermostMappingAction(test.action); got != test.want {
				t.Fatalf("unexpected normalized action: got %q want %q", got, test.want)
			}
		})
	}
}
