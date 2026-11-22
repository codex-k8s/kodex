package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// kubernetesSetupWrappedClient isolates typed response-envelope validation from transport authority.
// Other embedded methods are unimplemented and must never be reached by this metadata-only test.
// kubernetesSetupWrappedClient изолирует typed response-envelope validation от transport authority.
// Остальные embedded methods не реализованы и не вызываются этим metadata-only test.
type kubernetesSetupWrappedClient struct {
	cp.ManagedKubernetesIdentitySetupServiceClient
	response *cp.ResolveManagedKubernetesIdentityUploadResponse
}

// ResolveManagedKubernetesIdentityUpload returns an explicit test envelope without network or material.
// ResolveManagedKubernetesIdentityUpload возвращает explicit test envelope без network либо material.
func (client *kubernetesSetupWrappedClient) ResolveManagedKubernetesIdentityUpload(context.Context, *cp.ResolveManagedKubernetesIdentityUploadRequest, ...grpc.CallOption) (*cp.ResolveManagedKubernetesIdentityUploadResponse, error) {
	return client.response, nil
}

// TestKubernetesSetupOwnerRPCWrappedResponse proves that neither absent nested state nor an unknown
// outer/nested envelope can be treated as a completed owner receipt. It proves no signed policy or TLS.
// TestKubernetesSetupOwnerRPCWrappedResponse доказывает, что absent nested state либо unknown
// outer/nested envelope не становятся completed owner receipt. Signed policy и TLS не проверяются.
func TestKubernetesSetupOwnerRPCWrappedResponse(t *testing.T) {
	view := &cp.ManagedKubernetesIdentitySetupView{CanonicalContext: "fixture-context", SetupRef: "ksu_fixture01", ConnectionRef: "int_fixture01", ConnectionVersion: 1, IdentityRef: "kbi_fixture01", IdentityRevision: 1, TargetRef: "target_fixture01", Version: 1,
		State: cp.ManagedKubernetesIdentitySetupState_MANAGED_KUBERNETES_IDENTITY_SETUP_STATE_COMPLETED, ExpiresAt: timestamppb.New(time.Now().UTC().Add(time.Minute)), CredentialPolicy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_PROJECT_SHARED}
	valid := &cp.ResolveManagedKubernetesIdentityUploadResponse{Snapshot: &cp.ManagedKubernetesIdentityUploadSnapshot{SetupRef: view.SetupRef, ReconcileOnly: true, Completed: view}}
	for _, variant := range []string{"valid", "missing", "outer-unknown", "nested-unknown"} {
		t.Run(variant, func(t *testing.T) {
			response := proto.Clone(valid).(*cp.ResolveManagedKubernetesIdentityUploadResponse)
			switch variant {
			case "missing":
				response.Snapshot = nil
			case "outer-unknown":
				response.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
			case "nested-unknown":
				response.Snapshot.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
			}
			owner := &Owner{client: &controlplaneclient.Client{KubernetesIdentities: &kubernetesSetupWrappedClient{response: response}}, claimantID: "fixture-claimant"}
			result, err := owner.ResolveKubernetesIdentityUpload(t.Context(), view.SetupRef, "unused-fixture-digest")
			if variant == "valid" {
				if err != nil || result.Completed == nil || result.Completed.SetupRef != view.SetupRef {
					t.Fatal("typed completed owner wrapper was lost", err)
				}
			} else if !errors.Is(err, targetrepo.ErrSetupUnavailable) || result.Completed != nil {
				t.Fatal("invalid wrapper became owner completion", err)
			}
		})
	}
}
