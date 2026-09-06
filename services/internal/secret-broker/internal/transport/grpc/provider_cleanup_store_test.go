package grpc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/providercredential"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type unusedCleanupAppServer struct{ providercredential.AppServer }

func TestProviderCleanupStoreServiceTransportDistinguishesLostWriteACK(t *testing.T) {
	for _, lostACK := range []bool{false, true} {
		client := fake.NewSimpleClientset()
		client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			secret := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret).DeepCopy()
			secret.UID, secret.ResourceVersion = "61000000-0000-4000-8000-000000000001", "1"
			err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), secret, secret.Namespace)
			return true, secret, err
		})
		store, err := kubernetesstore.New(client, "kodex-runtime")
		if err != nil {
			t.Fatal(err)
		}
		attempt := kubernetesstore.ProviderAuthorizationAttempt{AttemptRef: "pauth_cleanup1234", AccountRef: "pacc_cleanup1234", State: "PENDING", VerificationURI: "https://example.invalid/device", UserCode: "SYNTHETIC", ExpiresAt: time.Now().Add(time.Minute)}
		digest := sha256.Sum256([]byte(attempt.AttemptRef + "\x00" + attempt.AccountRef))
		attempt.MaterializerAttemptRef = "pmat_" + hex.EncodeToString(digest[:16])
		attempt, _, err = store.CreateProviderAuthorizationAttempt(t.Context(), attempt)
		if err != nil {
			t.Fatal(err)
		}
		client.PrependReactor("update", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			secret := action.(k8stesting.UpdateAction).GetObject().(*corev1.Secret).DeepCopy()
			if !lostACK {
				return true, nil, apierrors.NewConflict(corev1.Resource("secrets"), secret.Name, errors.New("synthetic CAS rejection"))
			}
			secret.ResourceVersion = "2"
			if err := client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("secrets"), secret, secret.Namespace); err != nil {
				t.Fatal(err)
			}
			return true, nil, apierrors.NewServiceUnavailable("synthetic lost write acknowledgement")
		})
		service, err := providercredential.New(t.Context(), store, &unusedCleanupAppServer{}, providercredential.DefaultDeviceAuthorizationTTL())
		if err != nil {
			t.Fatal(err)
		}
		request := &cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest{TaskRef: "pcct_cleanup1234", AccountRef: attempt.AccountRef, LeaseGeneration: 2,
			RecoveryIdentity: &cp.ProviderCredentialCleanupRecoveryIdentity{TaskRef: "pcct_cleanup1234", LeaseGeneration: 1},
			TargetKind:       cp.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_AUTHORIZATION_ATTEMPT,
			PendingObject:    &cp.ProviderAuthorizationObjectDescriptor{AccountRef: attempt.AccountRef, AuthorizationAttemptRef: attempt.AttemptRef, MaterializerAttemptRef: attempt.MaterializerAttemptRef, Uid: attempt.SecretUID, ResourceVersion: attempt.ResourceVersion}}
		server := &Server{providerCredentials: service}
		_, err = server.CleanupProviderCredential(t.Context(), request)
		result := status.Convert(err)
		if lostACK {
			if result.Code() != codes.Unavailable || len(result.Details()) != 0 {
				t.Fatal("committed write acquired no-effect proof")
			}
			// Следующий exact вызов читает fence; второй Update не нужен.
			response, err := server.CleanupProviderCredential(t.Context(), request)
			if err != nil || response.GetTerminalReceipt() == "" {
				t.Fatalf("fenced recovery failed: %v", err)
			}
		} else if result.Code() != codes.FailedPrecondition || len(result.Details()) != 1 {
			t.Fatal("CAS rejection lost typed proof")
		}
		_ = service.Close()
	}
}
