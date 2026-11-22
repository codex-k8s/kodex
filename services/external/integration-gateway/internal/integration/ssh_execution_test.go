package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// sshInvocationBrokerFixture is a typed controlled RPC seam, not proof of owner PostgreSQL or protected transport authentication.
// sshInvocationBrokerFixture является typed controlled RPC seam, не proof owner PostgreSQL либо protected transport authentication.
type sshInvocationBrokerFixture struct {
	response       *sb.ManagedSSHExecutionResult
	expected       *sb.ManagedSSHExecutionBinding
	execute, reads int
	err            error
}

// ProbeSSHIdentity rejects unrelated health dispatch from a finite invocation fixture.
// ProbeSSHIdentity отклоняет unrelated health dispatch из finite invocation fixture.
func (*sshInvocationBrokerFixture) ProbeSSHIdentity(context.Context, *sb.ProbeSSHIdentityRequest, ...grpc.CallOption) (*sb.ProbeSSHIdentityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unexpected health RPC")
}

// ExecuteSSHIdentity records one unary attempt without reminting a claim or retrying the effect.
// ExecuteSSHIdentity учитывает одну unary attempt без remint claim либо повторного effect.
func (f *sshInvocationBrokerFixture) ExecuteSSHIdentity(_ context.Context, request *sb.ExecuteSSHIdentityRequest, _ ...grpc.CallOption) (*sb.ExecuteSSHIdentityResponse, error) {
	f.execute++
	if f.expected != nil && !proto.Equal(request.GetBinding(), f.expected) {
		return nil, status.Error(codes.PermissionDenied, "unexpected SSH execution binding")
	}
	return &sb.ExecuteSSHIdentityResponse{Result: f.response}, f.err
}

// testSSHBrokerCatalogOperation verifies that every shipped SSH operation admits a typed owner claim and dispatches exactly one broker call bound to that claim.
// testSSHBrokerCatalogOperation проверяет допуск типизированного owner claim каждой поставляемой SSH-операции и ровно один вызов брокера с теми же координатами.
func testSSHBrokerCatalogOperation(t *testing.T, definition integrationpackage.Package, capability integrationpackage.Capability) {
	t.Helper()
	inputs := map[string]string{
		"ssh.identity.inspect": `{}`,
		"ssh.target.inspect":   `{}`,
		"ssh.system.read":      `{}`,
		"ssh.execute":          `{"command":"printf synthetic","maximum_output":64}`,
		"ssh.process.list":     `{"limit":1}`,
		"ssh.file.list":        `{"resource":"documents","limit":1}`,
		"ssh.file.stat":        `{"resource":"documents","path":"state"}`,
		"ssh.file.read":        `{"resource":"documents","path":"state","expected":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","limit":1}`,
		"ssh.file.create":      `{"resource":"documents","path":"state","content_base64":"dGVzdA=="}`,
		"ssh.file.replace":     `{"resource":"documents","path":"state","content_base64":"dGVzdA==","expected":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
		"ssh.file.delete":      `{"resource":"documents","path":"state","expected":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
		"ssh.service.read":     `{"resource":"worker"}`,
		"ssh.service.restart":  `{"resource":"worker"}`,
		"ssh.service.reload":   `{"resource":"worker"}`,
		"ssh.receipt.read":     `{"receipt_invocation_ref":"inv_previous"}`,
		"ssh.journal.read":     `{"resource":"worker","limit":1}`,
	}
	var input map[string]any
	if json.Unmarshal([]byte(inputs[capability.Operation]), &input) != nil {
		t.Fatal("advertised SSH operation lacks component scenario")
	}
	risk, validRisk := cp.IntegrationRisk_value["INTEGRATION_RISK_"+capability.Risk]
	policy, validPolicy := cp.IntegrationApprovalPolicy_value["INTEGRATION_APPROVAL_POLICY_"+capability.ApprovalPolicy]
	if !validRisk || !validPolicy {
		t.Fatal("advertised SSH operation lacks typed risk or approval policy")
	}
	adapter := testAdapter(t)
	claim := sshPurposeClaimFixture(t, definition, capability, input, cp.IntegrationRisk(risk), cp.IntegrationApprovalPolicy(policy))
	if _, _, _, _, err := adapter.validateInvocation(RequestFromInvocation(claim)); err == nil {
		t.Fatal("generic TOKEN path admitted SSH broker identity")
	}
	instance := "gateway_fixture"
	prepared, err := adapter.PrepareSSHInvocation(claim, instance)
	if err != nil || prepared == nil {
		t.Fatal("advertised SSH operation rejected typed claim", err)
	}
	if !prepared.ExpiresAt().Equal(claim.Lease.ExpiresAt.AsTime()) {
		t.Fatal("typed SSH dispatch changed owner lease expiry")
	}
	expected := &sb.ManagedSSHExecutionBinding{WorkloadInstance: instance, InvocationRef: claim.InvocationRef,
		LeaseRef: claim.Lease.Ref, Fence: claim.Lease.Fence, Generation: claim.Lease.Generation, InputDigest: claim.InputDigest}
	_, response := sshInvocationResultFixture()
	response.Binding = expected
	broker := &sshInvocationBrokerFixture{response: response, expected: expected}
	result, err := prepared.Execute(t.Context(), broker)
	if err != nil || !result.Attempted || !result.Receipt.Recorded || !result.Receipt.Applied ||
		result.Receipt.Observation.ResultDigest != response.ResultDigest || broker.execute != 1 || broker.reads != 0 {
		t.Fatal("typed SSH dispatch failed to return the original owner-bound receipt", err)
	}
}

// ReconcileSSHIdentity records metadata reads separately and never calls the execution method.
// ReconcileSSHIdentity учитывает metadata reads отдельно и не вызывает execution method.
func (f *sshInvocationBrokerFixture) ReconcileSSHIdentity(context.Context, *sb.ReconcileSSHIdentityRequest, ...grpc.CallOption) (*sb.ReconcileSSHIdentityResponse, error) {
	f.reads++
	return &sb.ReconcileSSHIdentityResponse{Result: f.response}, f.err
}

// sshInvocationResultFixture supplies exact opaque original coordinates and an independently hashed bounded result for reply-boundary tests.
// sshInvocationResultFixture задаёт exact opaque original coordinates и independently hashed bounded result для reply-boundary tests.
func sshInvocationResultFixture() (*SSHInvocation, *sb.ManagedSSHExecutionResult) {
	invocation := &SSHInvocation{binding: managedssh.Binding{WorkloadInstance: "gateway_fixture", InvocationRef: "inv_fixture", LeaseRef: "lease_fixture", Fence: "fence_fixture", Generation: 1, InputDigest: strings.Repeat("a", 64)}, expires: time.Now().Add(30 * time.Second)}
	raw := []byte(`{"state":"observed"}`)
	sum := sha256.Sum256(raw)
	result := &sb.ManagedSSHExecutionResult{Binding: sshInvocationWireBinding(invocation.binding), AttemptRef: "sxe_fixture", SnapshotDigest: strings.Repeat("b", 64), State: "SUCCEEDED", OwnerState: "SUCCEEDED", Recorded: true, Applied: true, Outcome: "SUCCEEDED", ResultDigest: hex.EncodeToString(sum[:]), ResultJson: raw}
	return invocation, result
}

// TestSSHInvocationReplyRequiresExactOwnerAndResult refuses changed original bindings, future states and uncredited output over the actual purpose decoder.
// TestSSHInvocationReplyRequiresExactOwnerAndResult отвергает changed original bindings, future states и uncredited output через actual purpose decoder.
func TestSSHInvocationReplyRequiresExactOwnerAndResult(t *testing.T) {
	invocation, result := sshInvocationResultFixture()
	if _, err := invocation.sshInvocationReply(result, false); err != nil {
		t.Fatal("valid owner receipt rejected", err)
	}
	for _, mutate := range []func(*sb.ManagedSSHExecutionResult){
		func(r *sb.ManagedSSHExecutionResult) { r.Binding.InvocationRef = "inv_foreign" }, func(r *sb.ManagedSSHExecutionResult) { r.Binding.Fence = "foreign_fence" },
		func(r *sb.ManagedSSHExecutionResult) { r.Binding.Generation++ }, func(r *sb.ManagedSSHExecutionResult) { r.Binding.InputDigest = strings.Repeat("c", 64) },
		func(r *sb.ManagedSSHExecutionResult) { r.OwnerState = "RUNNING"; r.Recorded = false }, func(r *sb.ManagedSSHExecutionResult) { r.Outcome = "FUTURE" },
		func(r *sb.ManagedSSHExecutionResult) { r.Applied = false }, func(r *sb.ManagedSSHExecutionResult) { r.ResultJson = []byte(`{"state":"changed"}`) },
		func(r *sb.ManagedSSHExecutionResult) { r.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01}) },
	} {
		changed := proto.Clone(result).(*sb.ManagedSSHExecutionResult)
		mutate(changed)
		if _, err := invocation.sshInvocationReply(changed, false); err == nil {
			t.Fatal("changed reply became owner receipt")
		}
	}
	if _, err := invocation.sshInvocationReply(result, true); err == nil {
		t.Fatal("reconciliation exposed output")
	}
}

// TestSSHInvocationUncertainDeliveryNeverRepeatsExecute separates one uncertain RPC from explicit metadata reconciliation and preserves malformed-success refusal.
// TestSSHInvocationUncertainDeliveryNeverRepeatsExecute отделяет uncertain RPC от explicit metadata reconciliation и сохраняет malformed-success refusal.
func TestSSHInvocationUncertainDeliveryNeverRepeatsExecute(t *testing.T) {
	invocation, result := sshInvocationResultFixture()
	broker := &sshInvocationBrokerFixture{err: status.Error(codes.Unavailable, "synthetic lost ACK")}
	actual, err := invocation.Execute(t.Context(), broker)
	if err == nil || !actual.Attempted || broker.execute != 1 || broker.reads != 0 {
		t.Fatal("uncertain delivery retried execution")
	}
	broker.err = nil
	broker.response = proto.Clone(result).(*sb.ManagedSSHExecutionResult)
	broker.response.ResultJson = nil
	actual, err = invocation.Reconcile(t.Context(), broker)
	if err != nil || !actual.Receipt.Recorded || !actual.Receipt.Applied || len(actual.Receipt.Observation.Result) != 0 || broker.execute != 1 || broker.reads != 1 {
		t.Fatal("readback lost original owner receipt", err)
	}
	broker.response.Binding.InvocationRef = "inv_foreign"
	_, err = invocation.Execute(t.Context(), broker)
	var safe *SafeError
	if !errors.As(err, &safe) || safe.Code != "INTEGRATION_RESPONSE_INVALID" || broker.execute != 2 || broker.reads != 1 {
		t.Fatal("damaged success was not distinguished from transport loss", err)
	}
}

// TestSSHInvocationShippedSourceUsesOriginalTypedClaim accepts actual READ, SCOPED and EACH BI claims only through their closed purpose boundary; generic TOKEN admission and contradictory claim pins remain rejected before any broker RPC.
// TestSSHInvocationShippedSourceUsesOriginalTypedClaim допускает actual READ, SCOPED и EACH BI claims лишь через closed purpose boundary; generic TOKEN admission и contradictory claim pins отклоняются до broker RPC.
func TestSSHInvocationShippedSourceUsesOriginalTypedClaim(t *testing.T) {
	adapter, err := New(Config{CredentialDirectory: t.TempDir(), ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8080", OpenAPIProxyURL: "http://egress-gateway-openapi.kodex-system.svc.cluster.local:8083", SyntheticBaseURL: "http://integration-synthetic.kodex-system.svc.cluster.local:8080", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		key, operation string
		input          map[string]any
		risk           cp.IntegrationRisk
		policy         cp.IntegrationApprovalPolicy
	}{
		{integrationpackage.SSHManagedDefinitionKey, "ssh.target.inspect", map[string]any{}, cp.IntegrationRisk_INTEGRATION_RISK_READ, cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE},
		{integrationpackage.SSHManagedDefinitionKey, "ssh.file.create", map[string]any{"resource": "documents", "path": "state", "content_base64": "dGVzdA=="}, cp.IntegrationRisk_INTEGRATION_RISK_WRITE, cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_HUMAN_SCOPED},
		{integrationpackage.SSHExecuteDefinitionKey, "ssh.execute", map[string]any{"command": "printf synthetic", "maximum_output": 64}, cp.IntegrationRisk_INTEGRATION_RISK_SENSITIVE, cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_HUMAN_EACH_EFFECT},
	} {
		t.Run(scenario.operation, func(t *testing.T) {
			definition := definitions[scenario.key]
			capability, ok := definition.CapabilityByOperation(scenario.operation)
			if !ok {
				t.Fatal("canonical SSH capability missing")
			}
			claim := sshPurposeClaimFixture(t, definition, capability, scenario.input, scenario.risk, scenario.policy)
			if _, _, _, _, err := adapter.validateInvocation(RequestFromInvocation(claim)); err == nil {
				t.Fatal("generic TOKEN path admitted broker identity")
			}
			if prepared, err := adapter.PrepareSSHInvocation(claim, "gateway_fixture"); err != nil || prepared == nil {
				t.Fatal("actual SSH source rejected exact typed invocation", err)
			}
			for _, change := range []func(*cp.IntegrationInvocationClaim){
				func(c *cp.IntegrationInvocationClaim) {
					c.BrokerIdentityPurpose = cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_KUBERNETES_TARGET
				},
				func(c *cp.IntegrationInvocationClaim) { c.CredentialRevision = &cp.IntegrationCredentialRevision{} },
				func(c *cp.IntegrationInvocationClaim) { c.GrantVersion = 0 },
				func(c *cp.IntegrationInvocationClaim) { c.InputDigest = strings.Repeat("f", 64) },
				func(c *cp.IntegrationInvocationClaim) { c.ResourcePermissionProfile = 0 },
			} {
				changed := proto.Clone(claim).(*cp.IntegrationInvocationClaim)
				change(changed)
				if prepared, err := adapter.PrepareSSHInvocation(changed, "gateway_fixture"); err == nil || prepared != nil {
					t.Fatal("contradictory claim acquired SSH execution")
				}
			}
		})
	}
}

// sshPurposeClaimFixture constructs the exact invocation envelope accepted by the broker-owned path, using the shipped package and independently computed scope and input digests.
// sshPurposeClaimFixture создаёт конверт вызова для broker-owned пути из поставляемого пакета и отдельно вычисленных digest области и аргументов.
func sshPurposeClaimFixture(t *testing.T, definition integrationpackage.Package, capability integrationpackage.Capability, arguments map[string]any, risk cp.IntegrationRisk, policy cp.IntegrationApprovalPolicy) *cp.IntegrationInvocationClaim {
	t.Helper()
	raw, err := json.Marshal(definition)
	if err != nil || definition.Spec.Readiness != "READY" {
		t.Fatal("actual SSH source is unavailable", err)
	}
	config, _ := structpb.NewStruct(map[string]any{"target_ref": "target_fixture"})
	input, _ := structpb.NewStruct(arguments)
	scope, err := capability.ResourceScopeValues(map[string]string{"target_ref": "target_fixture"})
	if err != nil {
		t.Fatal(err)
	}
	encodedScope, _ := json.Marshal(scope)
	encodedInput, _ := json.Marshal(arguments)
	canonical, err := capability.ValidateInput(encodedInput)
	if err != nil {
		t.Fatal("invalid synthetic SSH input", err)
	}
	scopeSum, inputSum := sha256.Sum256(encodedScope), sha256.Sum256(canonical)
	return &cp.IntegrationInvocationClaim{InvocationRef: "inv_fixture", ConnectionRef: "int_fixture", DefinitionKey: definition.Metadata.Key, DefinitionVersion: definition.Metadata.Version, DefinitionDigest: definition.Digest, DefinitionPackage: raw,
		CapabilityKey: capability.Key, Operation: capability.Operation, Risk: risk, ApprovalPolicy: policy,
		PublicConfiguration: config, BoundedInput: input, ResourceScope: &cp.IntegrationResourceScope{Kind: cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_SSH_TARGET, Values: scope, Digest: hex.EncodeToString(scopeSum[:])},
		EffectKey: "effect_fixture", InputDigest: hex.EncodeToString(inputSum[:]), GrantRef: "grant_fixture", GrantVersion: 1, WorkMode: cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE, CredentialDeliveryKind: cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY,
		BrokerIdentityPurpose: cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET, BrokerIdentityRef: "bri_fixture", BrokerIdentityRevision: 1,
		ResourcePermissionProfile: cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_SSH_SCOPED_RESOURCE_V1,
		Lease:                     &cp.WorkLease{Ref: "lease_fixture", Fence: "fence_fixture", Generation: 1, ExpiresAt: timestamppb.New(time.Now().Add(30 * time.Second))}}
}
