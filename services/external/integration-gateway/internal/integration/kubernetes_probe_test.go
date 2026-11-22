package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// kubernetesHealthClaimFixture retains a real shipped definition but deliberately uses an opaque owner test digest different from empty operation input.
// kubernetesHealthClaimFixture сохраняет real shipped definition с opaque owner test digest, намеренно отличным от пустого operation input.
func kubernetesHealthClaimFixture(t *testing.T, adapter *Adapter) *cp.IntegrationConnectionTestClaim {
	t.Helper()
	request, binding := kubernetesRequestFixture(t, adapter, "kubernetes.identity")
	configuration, err := structpb.NewStruct(request.Configuration)
	if err != nil {
		t.Fatal(err)
	}
	return &cp.IntegrationConnectionTestClaim{TestRef: "test_fixture", ConnectionRef: request.ConnectionRef,
		DefinitionKey: request.DefinitionKey, DefinitionVersion: request.DefinitionVersion, DefinitionDigest: request.DefinitionDigest, DefinitionPackage: request.DefinitionPackage,
		PublicConfiguration: configuration, Lease: &cp.WorkLease{Ref: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))},
		CredentialDeliveryKind: cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY,
		BrokerIdentityPurpose:  cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_KUBERNETES_TARGET,
		BrokerIdentityRef:      "identity_fixture", BrokerIdentityRevision: 3, InputDigest: strings.Repeat("a", 64)}
}

// TestKubernetesProbeCarriesOriginalOwnerSnapshot exercises the actual claim adapter and both owner-completed outcomes without generic credential acquisition or empty-input rehashing.
// TestKubernetesProbeCarriesOriginalOwnerSnapshot проверяет actual claim adapter и оба owner-completed outcomes без generic credential acquisition либо empty-input rehashing.
func TestKubernetesProbeCarriesOriginalOwnerSnapshot(t *testing.T) {
	adapter := testAdapter(t)
	claim := kubernetesHealthClaimFixture(t, adapter)
	before := proto.Clone(claim)
	for _, authenticated := range []bool{true, false} {
		broker := &kubernetesBrokerFixture{response: kubernetesHealthFixture(claim.TestRef, authenticated)}
		receipt, err := adapter.ProbeKubernetes(t.Context(), claim, "gateway_fixture", broker)
		if err != nil || !receipt.Attempted || !receipt.Completed || receipt.Authenticated != authenticated || receipt.TestRef != claim.TestRef || broker.calls != 1 {
			t.Fatal("original health claim did not join its owner receipt")
		}
		expected := &sb.ManagedKubernetesInvocationBinding{WorkloadInstance: "gateway_fixture", ConnectionTestRef: claim.TestRef,
			LeaseRef: claim.Lease.Ref, Fence: claim.Lease.Fence, Generation: claim.Lease.Generation, InputDigest: claim.InputDigest}
		if !proto.Equal(broker.binding, expected) || !proto.Equal(claim, before) {
			t.Fatal("health adapter rewrote original owner binding or claim")
		}
	}
}

// TestKubernetesProbeRejectsMixedAuthority checks closed delivery, package and lease metadata before the only broker call.
// TestKubernetesProbeRejectsMixedAuthority проверяет closed delivery, package и lease metadata до единственного broker call.
func TestKubernetesProbeRejectsMixedAuthority(t *testing.T) {
	adapter := testAdapter(t)
	for _, mutate := range []func(*cp.IntegrationConnectionTestClaim){
		func(claim *cp.IntegrationConnectionTestClaim) {
			claim.CredentialDeliveryKind = cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_TOKEN
		},
		func(claim *cp.IntegrationConnectionTestClaim) {
			claim.BrokerIdentityPurpose = cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET
		},
		func(claim *cp.IntegrationConnectionTestClaim) { claim.BrokerIdentityRef = "" },
		func(claim *cp.IntegrationConnectionTestClaim) { claim.BrokerIdentityRevision = 0 },
		func(claim *cp.IntegrationConnectionTestClaim) {
			claim.CredentialRevision = &cp.IntegrationCredentialRevision{Ref: "forged"}
		},
		func(claim *cp.IntegrationConnectionTestClaim) { claim.CredentialMaterializationRef = "forged" },
		func(claim *cp.IntegrationConnectionTestClaim) { claim.DefinitionDigest = strings.Repeat("b", 64) },
		func(claim *cp.IntegrationConnectionTestClaim) { claim.InputDigest = "" },
		func(claim *cp.IntegrationConnectionTestClaim) { claim.Lease = nil },
		func(claim *cp.IntegrationConnectionTestClaim) { claim.Lease.Generation = 0 },
		func(claim *cp.IntegrationConnectionTestClaim) {
			claim.Lease.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
		},
		func(claim *cp.IntegrationConnectionTestClaim) {
			claim.Lease.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
		},
		func(claim *cp.IntegrationConnectionTestClaim) {
			claim.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
		},
	} {
		claim := kubernetesHealthClaimFixture(t, adapter)
		mutate(claim)
		broker := &kubernetesBrokerFixture{}
		receipt, err := adapter.ProbeKubernetes(t.Context(), claim, "gateway_fixture", broker)
		if err == nil || receipt.Attempted || receipt.Completed || receipt.Authenticated || broker.calls != 0 {
			t.Fatal("invalid metadata reached broker or became health success")
		}
	}
}

// TestKubernetesProbeUncertainAckNeverRepeats preserves attempted work and withholds completion after transport failure, gate or malformed reply.
// TestKubernetesProbeUncertainAckNeverRepeats сохраняет attempted work без completion при transport failure, gate либо malformed reply.
func TestKubernetesProbeUncertainAckNeverRepeats(t *testing.T) {
	adapter := testAdapter(t)
	claim := kubernetesHealthClaimFixture(t, adapter)
	for _, broker := range []*kubernetesBrokerFixture{
		{err: errors.New("private upstream token")},
		{err: kubernetesPendingStatus(t, kubernetesPendingFixture(false))},
		{response: kubernetesCompletedFixture()},
		{response: kubernetesHealthFixture("foreign_test", true)},
		{response: &sb.ExecuteManagedKubernetesOperationResponse{}},
	} {
		receipt, err := adapter.ProbeKubernetes(t.Context(), claim, "gateway_fixture", broker)
		if err == nil || !receipt.Attempted || receipt.Completed || receipt.Authenticated || broker.calls != 1 || strings.Contains(err.Error(), "private") {
			t.Fatal("uncertain health acknowledgement replayed or became completion")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	broker := &kubernetesBrokerFixture{}
	receipt, err := adapter.ProbeKubernetes(ctx, claim, "gateway_fixture", broker)
	if err == nil || receipt.Attempted || broker.calls != 0 {
		t.Fatal("canceled probe dispatched")
	}
}
