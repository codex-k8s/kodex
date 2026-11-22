package integration

import (
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// kubernetesInvocationClaimFixture carries the same production package and canonical operation pins used by the existing adapter tests.
// kubernetesInvocationClaimFixture передаёт production package и canonical operation pins существующих adapter tests.
func kubernetesInvocationClaimFixture(t *testing.T, adapter *Adapter) *cp.IntegrationInvocationClaim {
	t.Helper()
	request, binding := kubernetesRequestFixture(t, adapter, "kubernetes.get")
	configuration, err := structpb.NewStruct(request.Configuration)
	if err != nil {
		t.Fatal(err)
	}
	input, err := structpb.NewStruct(request.Input)
	if err != nil {
		t.Fatal(err)
	}
	return &cp.IntegrationInvocationClaim{InvocationRef: binding.InvocationRef, ConnectionRef: request.ConnectionRef,
		DefinitionKey: request.DefinitionKey, DefinitionVersion: request.DefinitionVersion, DefinitionDigest: request.DefinitionDigest, DefinitionPackage: request.DefinitionPackage,
		PublicConfiguration: configuration, BoundedInput: input, InputDigest: request.InputDigest, EffectKey: request.EffectKey,
		CapabilityKey: request.CapabilityKey, Operation: request.Operation, Risk: cp.IntegrationRisk_INTEGRATION_RISK_READ, ApprovalPolicy: cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE,
		GrantRef: request.GrantRef, GrantVersion: request.GrantVersion, ResourcePermissionProfile: cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_KUBERNETES_SCOPED_RESOURCE_V1,
		ResourceScope:          &cp.IntegrationResourceScope{Kind: cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_KUBERNETES_TARGET, Values: request.ResourceScope, Digest: request.ResourceScopeDigest},
		WorkMode:               cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE,
		Lease:                  &cp.WorkLease{Ref: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))},
		CredentialDeliveryKind: cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY,
		BrokerIdentityPurpose:  cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_KUBERNETES_TARGET, BrokerIdentityRef: "identity_fixture", BrokerIdentityRevision: 3}
}

// TestKubernetesCommonClaimReachesBrokerOnly proves the common claim can reach real adapter validation without acquiring a generic credential or email execution binding.
// TestKubernetesCommonClaimReachesBrokerOnly доказывает common claim → actual adapter validation без generic credential либо email execution binding.
func TestKubernetesCommonClaimReachesBrokerOnly(t *testing.T) {
	adapter := testAdapter(t)
	for _, mode := range []cp.IntegrationInvocationWorkMode{cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE, cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY} {
		claim := kubernetesInvocationClaimFixture(t, adapter)
		claim.WorkMode = mode
		before := proto.Clone(claim)
		request, binding, err := PrepareKubernetesInvocation(claim, "gateway_fixture")
		if err != nil || request.EmailExecution != nil || request.Credential != nil || !proto.Equal(before, claim) {
			t.Fatal("purpose preparation altered the owner claim or introduced another credential lane")
		}
		claim.ResourceScope.Values["namespace"] = "foreign"
		claim.BoundedInput.Fields["namespace"] = structpb.NewStringValue("foreign")
		claim.Lease.Fence = "foreign_fence"
		claim.DefinitionPackage[0] ^= 1
		broker := &kubernetesBrokerFixture{response: kubernetesCompletedFixture()}
		response, err := adapter.ExecuteKubernetes(t.Context(), request, binding, broker)
		if err != nil || response == nil || broker.calls != 1 || binding.Fence == claim.Lease.Fence || request.ResourceScope["namespace"] == "foreign" {
			t.Fatal("copied common claim lost original pins or did not reach bounded broker dispatch")
		}
	}
}

// TestKubernetesCommonClaimRejectsForeignPurpose refuses mixed representation and expired initial claims before creating an activity input.
// TestKubernetesCommonClaimRejectsForeignPurpose отвергает mixed representation и expired initial claims до создания activity input.
func TestKubernetesCommonClaimRejectsForeignPurpose(t *testing.T) {
	adapter := testAdapter(t)
	for _, mutate := range []func(*cp.IntegrationInvocationClaim){
		func(claim *cp.IntegrationInvocationClaim) {
			claim.CredentialRevision = &cp.IntegrationCredentialRevision{Ref: "forged"}
		},
		func(claim *cp.IntegrationInvocationClaim) { claim.CredentialMaterializationRef = "forged" },
		func(claim *cp.IntegrationInvocationClaim) { claim.SshComposeSnapshot = []byte("foreign") },
		func(claim *cp.IntegrationInvocationClaim) { claim.SshComposePermit = []byte("foreign") },
		func(claim *cp.IntegrationInvocationClaim) {
			claim.CredentialDeliveryKind = cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_TOKEN
		},
		func(claim *cp.IntegrationInvocationClaim) {
			claim.BrokerIdentityPurpose = cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET
		},
		func(claim *cp.IntegrationInvocationClaim) { claim.BrokerIdentityRef = "" },
		func(claim *cp.IntegrationInvocationClaim) { claim.BrokerIdentityRevision = 0 },
		func(claim *cp.IntegrationInvocationClaim) { claim.WorkMode = cp.IntegrationInvocationWorkMode(99) },
		func(claim *cp.IntegrationInvocationClaim) {
			claim.Lease.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
		},
		func(claim *cp.IntegrationInvocationClaim) { claim.InputDigest = "" },
	} {
		claim := kubernetesInvocationClaimFixture(t, adapter)
		mutate(claim)
		if _, binding, err := PrepareKubernetesInvocation(claim, "gateway_fixture"); err == nil || binding != nil {
			t.Fatal("invalid initial claim created purpose activity input")
		}
	}
}
