package grpc

import (
	"context"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TestKubernetesIdentitySetupTransportRejectsUnknownBeforeOwner covers each registered setup RPC.
// Unknown input never reaches a repository or gains authority from an otherwise empty transport.
// TestKubernetesIdentitySetupTransportRejectsUnknownBeforeOwner покрывает каждый setup RPC.
// Unknown input не достигает repository и не получает authority от пустого transport.
func TestKubernetesIdentitySetupTransportRejectsUnknownBeforeOwner(t *testing.T) {
	server := &Server{}
	ctx := context.Background()
	cases := []struct {
		request proto.Message
		invoke  func() error
	}{
		{&cp.BeginManagedKubernetesIdentitySetupRequest{}, func() error {
			_, err := server.BeginManagedKubernetesIdentitySetup(ctx, &cp.BeginManagedKubernetesIdentitySetupRequest{})
			return err
		}},
		{&cp.GetManagedKubernetesIdentitySetupRequest{}, func() error {
			_, err := server.GetManagedKubernetesIdentitySetup(ctx, &cp.GetManagedKubernetesIdentitySetupRequest{})
			return err
		}},
		{&cp.CancelManagedKubernetesIdentitySetupRequest{}, func() error {
			_, err := server.CancelManagedKubernetesIdentitySetup(ctx, &cp.CancelManagedKubernetesIdentitySetupRequest{})
			return err
		}},
		{&cp.ResolveManagedKubernetesIdentityUploadRequest{}, func() error {
			_, err := server.ResolveManagedKubernetesIdentityUpload(ctx, &cp.ResolveManagedKubernetesIdentityUploadRequest{})
			return err
		}},
		{&cp.ClaimManagedKubernetesIdentityUploadRequest{}, func() error {
			_, err := server.ClaimManagedKubernetesIdentityUpload(ctx, &cp.ClaimManagedKubernetesIdentityUploadRequest{})
			return err
		}},
		{&cp.CompleteManagedKubernetesIdentityUploadRequest{}, func() error {
			_, err := server.CompleteManagedKubernetesIdentityUpload(ctx, &cp.CompleteManagedKubernetesIdentityUploadRequest{})
			return err
		}},
	}
	for _, item := range cases {
		if status.Code(item.invoke()) != codes.Unauthenticated {
			t.Fatal("setup request bypassed exact-method authentication")
		}
		item.request.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
		if managedKubernetesWireValid(item.request) {
			t.Fatal("future setup field reached semantic interpretation")
		}
	}
	request := &cp.CompleteManagedKubernetesIdentityUploadRequest{Outcome: &cp.CompleteManagedKubernetesIdentityUploadRequest_RejectedAcknowledgement{RejectedAcknowledgement: &cp.ManagedKubernetesIdentityAcknowledgementRejection{ResponseSha256: strings.Repeat("a", 64)}}}
	request.GetRejectedAcknowledgement().ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
	if _, err := server.CompleteManagedKubernetesIdentityUpload(ctx, request); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unknown acknowledgement witness reached owner completion")
	}
}

// TestKubernetesIdentitySetupSafeProjectionPreservesHistory rejects partial owner metadata.
// Completed history retains its original expiry and explicit quarantine reason without private pins.
// TestKubernetesIdentitySetupSafeProjectionPreservesHistory отклоняет неполные owner metadata.
// Completed history сохраняет original expiry и quarantine reason без private pins.
func TestKubernetesIdentitySetupSafeProjectionPreservesHistory(t *testing.T) {
	input := entity.KubernetesIdentitySetupView{CanonicalContext: "fixture-context", Ref: "ksu_synthetic", ConnectionRef: "int_synthetic", IdentityRef: "kbi_synthetic", TargetRef: "target_synthetic",
		Version: 3, ConnectionVersion: 4, BindingVersion: 2, BindingGeneration: 2, IdentityRevision: 1, Policy: entity.IntegrationCredentialInitiatorRequired,
		State: enum.KubernetesIdentitySetupCompleted, ReasonCode: "SETUP_ACK_CONFLICT", ExpiresAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	result, err := kubernetesIdentitySetupToProto(input)
	if err != nil || result.GetExpiresAt().AsTime() != input.ExpiresAt || result.GetReasonCode() != input.ReasonCode || result.GetState() != cp.ManagedKubernetesIdentitySetupState_MANAGED_KUBERNETES_IDENTITY_SETUP_STATE_COMPLETED {
		t.Fatal("setup history was refreshed or quarantine classification was lost")
	}
	for _, change := range []func(*entity.KubernetesIdentitySetupView){
		func(item *entity.KubernetesIdentitySetupView) { item.State = "FUTURE" },
		func(item *entity.KubernetesIdentitySetupView) { item.ReasonCode = "private arbitrary error" },
		func(item *entity.KubernetesIdentitySetupView) { item.IdentityRevision = 0 },
		func(item *entity.KubernetesIdentitySetupView) {
			item.Policy = entity.IntegrationCredentialProjectShared
		},
		func(item *entity.KubernetesIdentitySetupView) { item.BindingGeneration = 0 },
	} {
		changed := input
		change(&changed)
		if response, err := kubernetesIdentitySetupToProto(changed); err == nil || response != nil {
			t.Fatal("partial or unregistered metadata crossed setup response boundary")
		}
	}
}

// TestKubernetesIdentityPrivateSnapshotPreservesExactKnownPins excludes replacement material.
// It also proves that completed reconciliation returns safe history without a provision or permit.
// TestKubernetesIdentityPrivateSnapshotPreservesExactKnownPins исключает replacement material.
// Completed reconciliation возвращает safe history без provision либо permit.
func TestKubernetesIdentityPrivateSnapshotPreservesExactKnownPins(t *testing.T) {
	name, err := managedkubernetes.IdentitySecretName("kbi_synthetic_identity", 3)
	if err != nil {
		t.Fatal("derive private fixture name")
	}
	input := entity.KubernetesIdentityUploadSnapshot{SetupRef: "ksu_synthetic", Digest: strings.Repeat("a", 64), ContentBytes: 42, ExpiresAt: time.Now().UTC().Add(time.Minute), ReconcileOnly: true,
		Provision: value.ManagedKubernetesIdentityProvision{IdentityRef: "kbi_synthetic_identity", TargetRef: "target_synthetic", Namespace: "broker-private", Revision: 3, ContentSHA256: strings.Repeat("b", 64)},
		Target:    value.ManagedKubernetesTarget{Ref: "target_synthetic", Revision: 2, CertificateAuthoritySHA256: strings.Repeat("c", 64), AddressIPs: []string{"192.0.2.1"}}}
	input.KnownIdentity = &value.ManagedKubernetesIdentityLocator{IdentityRef: input.Provision.IdentityRef, TargetRef: input.Provision.TargetRef, Revision: 3, Namespace: input.Provision.Namespace,
		Name: name, UID: "76b0b43a-84c2-44d6-81a4-7c477a92ef2e", ResourceVersion: "42", ContentSHA256: input.Provision.ContentSHA256}
	result, err := kubernetesIdentitySnapshotToProto(input)
	if err != nil || result.GetKnownIdentity().GetSecretUid() != input.KnownIdentity.UID {
		t.Fatal("private snapshot lost exact known material identity")
	}
	input.Target.AddressIPs[0] = "192.0.2.2"
	if result.GetTarget().GetAddressIps()[0] != "192.0.2.1" {
		t.Fatal("mutable caller slice changed a projected target")
	}
	input.KnownIdentity.Revision++
	if response, err := kubernetesIdentitySnapshotToProto(input); err == nil || response != nil {
		t.Fatal("replacement revision became an exact known identity")
	}
	input.KnownIdentity = nil
	input.Completed = &entity.KubernetesIdentitySetupView{CanonicalContext: "fixture-context", Ref: input.SetupRef, ConnectionRef: "int_synthetic", IdentityRef: input.Provision.IdentityRef, TargetRef: input.Provision.TargetRef,
		Version: 2, ConnectionVersion: 2, IdentityRevision: 3, Policy: entity.IntegrationCredentialProjectShared, State: enum.KubernetesIdentitySetupCompleted, ExpiresAt: input.ExpiresAt}
	result, err = kubernetesIdentitySnapshotToProto(input)
	if err != nil || result.GetCompleted() == nil || result.GetProvision() != nil || result.GetKnownIdentity() != nil || result.GetTarget() != nil {
		t.Fatal("completed reconciliation exposed private materialization metadata")
	}
}
