package recovery

import (
	"context"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"google.golang.org/protobuf/proto"
)

type scopedProjectionRecoveryOwner struct {
	*fakeOwner
	requests []*controlplanev1.ValidateRuntimeCredentialProjectionRequest
}

func (owner *scopedProjectionRecoveryOwner) ValidateRuntimeCredentialProjection(ctx context.Context, request *controlplanev1.ValidateRuntimeCredentialProjectionRequest) (bool, error) {
	owner.requests = append(owner.requests, proto.Clone(request).(*controlplanev1.ValidateRuntimeCredentialProjectionRequest))
	return owner.fakeOwner.ValidateRuntimeCredentialProjection(ctx, request)
}

func TestScopedAssistantRecoveryPreservesAuthorityAndRevokesOnlyExactProjection(t *testing.T) {
	for _, scope := range []string{"SYSTEM", "PROJECT"} {
		t.Run(scope, func(t *testing.T) {
			now := time.Now().UTC()
			manifest := kubernetesstore.CredentialProjectionManifest{
				Authority: kubernetesstore.ProjectionAuthority{
					RPCProfile: transportprofile.TrustedCluster, ActorID: "c20ac176-c0ca-499f-91a4-6fc65c4ef30e", TenantID: "71adb021-5229-4903-9f75-9fd34797665a",
					ProjectID: "e92277a1-c5d0-4d40-af73-54c34a256ef5", SourceRevision: 4, SourceDigestSHA256: strings.Repeat("a", 64),
					CallerWorkloadID: "runtime-controller", CallerFullMethod: "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeRuntimeCredentials",
					CallerCredentialRevision: 1, ExpiresAt: now.Add(time.Minute),
				},
				WorkloadInstance: "scoped-recovery-worker", LeaseRef: "lease_scoped_current", Generation: 4,
				RuntimeRevisionRef: "rrev_scoped_current", RuntimeRevisionDigest: strings.Repeat("a", 64), SessionRef: "ses_scoped_current", TurnRef: "turn_scoped_current",
				Attempt: 1, InputDigest: strings.Repeat("b", 64), ExpiresAt: now.Add(30 * time.Second),
				ProviderCredential: kubernetesstore.ProviderProjectionBinding{AccountRef: "pacc_scoped_current", CredentialRevisionRef: "pcr_scoped_current", CredentialRevision: 1,
					SecretName: "provider-scoped-current", SecretUID: "provider-uid", SecretResourceVersion: "11", ContentSHA256: strings.Repeat("c", 64)},
				RuntimeSecrets: []kubernetesstore.RuntimeSecretProjectionBinding{{Name: "TOKEN", SecretRef: "sec_scoped_current", Revision: 1, Namespace: "kodex-runtime", SecretName: "runtime-scoped-current-r1",
					SecretKey: "value", SecretUID: "runtime-uid", SecretResourceVersion: "12", ContentSHA256: strings.Repeat("d", 64)}},
			}
			if scope == "SYSTEM" {
				manifest.Authority.ProjectID = ""
				manifest.Authority.CallerFullMethod = "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeSystemAssistantCredentials"
			}
			projection := kubernetesstore.CredentialProjection{Namespace: "kodex-runtime", SecretName: "projection-scoped-current", SecretUID: "projection-uid", SecretResourceVersion: "13", ContentSHA256: strings.Repeat("e", 64), Manifest: manifest}
			foreign := projection
			foreign.SecretName = "projection-scoped-foreign"
			foreign.Manifest.LeaseRef = "lease_scoped_foreign"
			foreign.Manifest.SessionRef = "ses_scoped_foreign"
			owner := &scopedProjectionRecoveryOwner{fakeOwner: &fakeOwner{projectionValidity: map[string]bool{manifest.LeaseRef: true, foreign.Manifest.LeaseRef: true}}}
			store := &fakeStore{projections: []kubernetesstore.CredentialProjection{projection, foreign}}
			reconciler := newTestReconciler(t, owner, store)
			if err := reconciler.EnableCredentialProjectionRecovery(owner, store); err != nil {
				t.Fatal(err)
			}
			if err := reconciler.ReconcileOnce(t.Context()); err != nil || len(store.deletedProjections) != 0 || len(owner.requests) != 2 {
				t.Fatalf("current scoped recovery failed: %v", err)
			}
			request := owner.requests[0]
			if request.GetAuthority().GetProjectId() != manifest.Authority.ProjectID || request.GetAuthority().GetRpcProfile() != transportprofile.TrustedCluster ||
				request.GetAuthority().GetCallerFullMethod() != manifest.Authority.CallerFullMethod || request.GetAuthority().GetSourceDigestSha256() != manifest.RuntimeRevisionDigest ||
				request.GetLeaseRef() != manifest.LeaseRef || request.GetRuntimeRevisionRef() != manifest.RuntimeRevisionRef || request.GetSessionRef() != manifest.SessionRef || request.GetTurnRef() != manifest.TurnRef ||
				request.GetAttempt() != manifest.Attempt || request.GetGeneration() != manifest.Generation || len(request.GetRuntimeSecrets()) != 1 ||
				request.GetRuntimeSecrets()[0].GetSecretUid() != manifest.RuntimeSecrets[0].SecretUID || request.GetRuntimeSecrets()[0].GetSecretResourceVersion() != manifest.RuntimeSecrets[0].SecretResourceVersion ||
				request.GetRuntimeSecrets()[0].GetContentSha256() != manifest.RuntimeSecrets[0].ContentSHA256 || !request.GetAuthority().GetExpiresAt().AsTime().Equal(manifest.Authority.ExpiresAt) {
				t.Fatal("recovery widened or lost exact scoped authority/descriptors")
			}
			owner.projectionValidity[manifest.LeaseRef] = false
			if err := reconciler.ReconcileOnce(t.Context()); err != nil || len(store.deletedProjections) != 1 || store.deletedProjections[0] != projection.SecretName {
				t.Fatalf("revoke crossed independent projection: %v", err)
			}
		})
	}
}
