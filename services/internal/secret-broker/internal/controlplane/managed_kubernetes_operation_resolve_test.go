package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// resolveClientFixture replaces only the authenticated RPC response, not owner authority or target I/O.
// resolveClientFixture заменяет только authenticated RPC response, не owner authority либо target I/O.
type resolveClientFixture struct {
	frameClientFixture
	response *cp.ResolveManagedKubernetesOperationResponse
	request  *cp.ResolveManagedKubernetesOperationRequest
	calls    int
}

// ResolveManagedKubernetesOperation records the exact original claim and never retries the request.
// ResolveManagedKubernetesOperation сохраняет exact original claim без повторения request.
func (client *resolveClientFixture) ResolveManagedKubernetesOperation(_ context.Context, request *cp.ResolveManagedKubernetesOperationRequest, _ ...grpc.CallOption) (*cp.ResolveManagedKubernetesOperationResponse, error) {
	client.calls++
	client.request = request
	return client.response, client.err
}

// operationResolveFixture supplies a synthetic immutable ConfigMap read with private identity metadata.
// operationResolveFixture задаёт synthetic immutable ConfigMap read с private identity metadata.
func operationResolveFixture(t *testing.T) (*Owner, *resolveClientFixture, managedkubernetes.Binding) {
	t.Helper()
	owner, base, binding, _ := frameAdapterFixture()
	_, canonical, err := managedkubernetes.DecodeOperationInput("kubernetes.get", []byte(`{"group":"","version":"v1","resource":"configmaps","namespace":"synthetic","name":"example"}`))
	if err != nil {
		t.Fatal(err)
	}
	name, err := managedkubernetes.IdentitySecretName("identity_synthetic", 2)
	if err != nil {
		t.Fatal(err)
	}
	client := &resolveClientFixture{frameClientFixture: *base, response: &cp.ResolveManagedKubernetesOperationResponse{SnapshotDigest: strings.Repeat("a", 64), Operation: "kubernetes.get", Risk: "READ", ApprovalPolicy: "NONE", IdentityRef: "identity_synthetic", IdentityGeneration: 3, SubjectDigest: strings.Repeat("b", 64), Target: &cp.ManagedKubernetesTargetSnapshot{TargetRef: "target_synthetic", Revision: 4, ServerOrigin: "https://kubernetes.example.invalid", TlsServerName: "kubernetes.example.invalid", CertificateAuthoritySha256: strings.Repeat("c", 64), AddressIps: []string{"192.0.2.10"}, EmbeddedContext: "synthetic", ClusterUid: "33efc8c9-9fca-4991-97f3-a1258e718ad6", KubeconfigSourceOrigins: []string{"https://kubernetes.example.invalid"}}, IdentityLocator: &cp.ManagedKubernetesIdentityLocator{IdentityRef: "identity_synthetic", Revision: 2, TargetRef: "target_synthetic", Namespace: "kodex-identity", SecretName: name, SecretUid: "a102d741-c15b-410c-987c-f9e45716bb88", SecretResourceVersion: "42", ContentSha256: strings.Repeat("d", 64)}, NormalizedInputJson: canonical, Resource: &cp.ManagedKubernetesResourceAddress{Version: "v1", Resource: "configmaps", Namespace: "synthetic", Name: "example", Namespaced: true}, ExpiresAt: timestamppb.New(time.Now().Add(25 * time.Second)), ProtectedNamespaces: []string{"kodex-system"}}}
	owner.client.KubernetesOperations = client
	return owner, client, binding
}

// TestOperationResolvePreservesClaimAndImmutableCopies proves the adapter does not change lineage or retain mutable RPC buffers.
// TestOperationResolvePreservesClaimAndImmutableCopies доказывает сохранение lineage и отсутствие mutable RPC buffers.
func TestOperationResolvePreservesClaimAndImmutableCopies(t *testing.T) {
	owner, client, binding := operationResolveFixture(t)
	result, err := owner.ResolveKubernetesOperation(t.Context(), binding)
	if err != nil || result.Operation != "kubernetes.get" || result.Resource == nil || result.Identity.Revision != 2 || result.IdentityGeneration != 3 || client.calls != 1 || !proto.Equal(client.request.Binding, kubernetesHealthBindingToProto(binding)) {
		t.Fatal("owner claim or selected identity changed")
	}
	client.response.Target.AddressIps[0] = "192.0.2.99"
	client.response.ProtectedNamespaces[0] = "foreign"
	client.response.NormalizedInputJson[0] = '!'
	client.response.Resource.Name = "foreign"
	if result.Target.AddressPins[0] != "192.0.2.10" || result.ProtectedNamespaces[0] != "kodex-system" || result.Resource.Name != "example" || result.NormalizedInput[0] != '{' {
		t.Fatal("RPC mutation changed resolved snapshot")
	}
	formatted := fmt.Sprintf("%v %#v", result, result)
	if strings.Contains(formatted, "configmaps") || strings.Contains(formatted, result.Identity.Name) {
		t.Fatal("private snapshot disclosed by diagnostics")
	}
	result.Input.Body = json.RawMessage(`{"private":"synthetic-private-business-content"}`)
	result.NormalizedInput = []byte(`{"private":"synthetic-private-business-content"}`)
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil || strings.Contains(string(raw), "synthetic-private-business-content") || strings.Contains(string(raw), `"Input"`) || strings.Contains(string(raw), `"NormalizedInput"`) {
		t.Fatal("private input escaped through snapshot JSON serialization")
	}
}

// TestOperationStreamResolvePreservesExactSelector refuses fallback to a live parent after stream denial.
// TestOperationStreamResolvePreservesExactSelector запрещает fallback к живому parent после stream denial.
func TestOperationStreamResolvePreservesExactSelector(t *testing.T) {
	for _, scenario := range []string{"active", "closed", "missing-plan", "invalid", "empty", "oversized"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding := operationResolveFixture(t)
			client.response.PlanRef, client.response.PlanRevision, client.response.PlanDigest = "plan_synthetic", 1, strings.Repeat("e", 64)
			streamRef := "stream_synthetic"
			switch scenario {
			case "closed":
				client.err = errors.New("synthetic stream denial")
			case "missing-plan":
				client.response.PlanRef, client.response.PlanRevision, client.response.PlanDigest = "", 0, ""
			case "invalid":
				streamRef = "stream\nforeign"
			case "empty":
				streamRef = ""
			case "oversized":
				streamRef = strings.Repeat("a", 97)
			}
			result, err := owner.ResolveKubernetesStream(t.Context(), binding, streamRef)
			if scenario == "active" {
				if err != nil || result.PlanRef != "plan_synthetic" || client.request.StreamRef != streamRef || !proto.Equal(client.request.Binding, kubernetesHealthBindingToProto(binding)) {
					t.Fatal("exact stream selector or original binding lost")
				}
			} else if err == nil || result.Digest != "" {
				t.Fatal("invalid stream acquired snapshot")
			}
			expected := 1
			if scenario == "invalid" || scenario == "empty" || scenario == "oversized" {
				expected = 0
			}
			if client.calls != expected {
				t.Fatal("stream read retried or invalid selector reached RPC")
			}
			if expected != 0 && client.request.StreamRef != streamRef {
				t.Fatal("stream denial fell back to base operation")
			}
		})
	}
}

// TestOperationResolveRejectsMalformedOwnerReplies covers closed policy, scope and nested wire boundaries.
// TestOperationResolveRejectsMalformedOwnerReplies проверяет closed policy, scope и nested wire boundaries.
func TestOperationResolveRejectsMalformedOwnerReplies(t *testing.T) {
	for _, scenario := range []string{"nil", "lost", "unknown", "target-unknown", "identity-unknown", "timestamp-unknown", "resource-unknown", "unknown-operation", "invalid-json", "noncanonical", "risk", "approval", "digest", "subject", "identity-ref", "identity-target", "identity-name", "identity-generation", "expired", "missing-time", "mixed-address", "foreign-resource", "missing-resource", "incomplete-plan", "reconcile-no-plan", "protected-duplicate", "protected-invalid", "policy-hash-only", "policy-mismatch", "policy-duplicate", "http-target"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding := operationResolveFixture(t)
			r := client.response
			switch scenario {
			case "nil":
				client.response = nil
			case "lost":
				client.err = errors.New("synthetic owner failure")
			case "unknown":
				r.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 1})
			case "target-unknown":
				r.Target.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 1})
			case "identity-unknown":
				r.IdentityLocator.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 1})
			case "timestamp-unknown":
				r.ExpiresAt.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 1})
			case "resource-unknown":
				r.Resource.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 1})
			case "unknown-operation":
				r.Operation = "kubernetes.arbitrary"
			case "invalid-json":
				r.NormalizedInputJson = []byte(`{"group":"","group":"other"}`)
			case "noncanonical":
				r.NormalizedInputJson = append([]byte(" "), r.NormalizedInputJson...)
			case "risk":
				r.Risk = "WRITE"
			case "approval":
				r.ApprovalPolicy = "HUMAN_SCOPED"
			case "digest":
				r.SnapshotDigest = ""
			case "subject":
				r.SubjectDigest = ""
			case "identity-ref":
				r.IdentityRef = "identity_foreign"
			case "identity-target":
				r.IdentityLocator.TargetRef = "target_foreign"
			case "identity-name":
				r.IdentityLocator.SecretName = "foreign"
			case "identity-generation":
				r.IdentityGeneration = 0
			case "expired":
				r.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
			case "missing-time":
				r.ExpiresAt = nil
			case "mixed-address":
				r.Discovery = &cp.ManagedKubernetesDiscoveryAddress{Kind: cp.ManagedKubernetesDiscoveryKind_MANAGED_KUBERNETES_DISCOVERY_KIND_VERSION}
			case "foreign-resource":
				r.Resource.Name = "foreign"
			case "missing-resource":
				r.Resource = nil
			case "incomplete-plan":
				r.PlanRef = "plan_synthetic"
			case "reconcile-no-plan":
				r.ReconcileOnly = true
			case "protected-duplicate":
				r.ProtectedNamespaces = append(r.ProtectedNamespaces, r.ProtectedNamespaces[0])
			case "protected-invalid":
				r.ProtectedNamespaces = []string{"*"}
			case "policy-hash-only":
				r.Target.ResourcePolicySha256 = strings.Repeat("a", 64)
			case "policy-mismatch":
				r.Target.ResourcePolicyJson = []byte(`{}`)
				r.Target.ResourcePolicySha256 = strings.Repeat("a", 64)
			case "policy-duplicate":
				r.Target.ResourcePolicyJson = []byte(`{"storage":[],"storage":[]}`)
			case "http-target":
				r.Target.ServerOrigin = "http://example.invalid"
			}
			result, err := owner.ResolveKubernetesOperation(t.Context(), binding)
			if err == nil || result.Digest != "" || client.calls != 1 {
				t.Fatal("malformed owner reply became usable or was retried")
			}
		})
	}
}

// TestOperationResolveDiscoveryOfflineAndPlanEvolution preserves supported address variants and fresh lease evolution.
// TestOperationResolveDiscoveryOfflineAndPlanEvolution сохраняет supported address variants и fresh lease evolution.
func TestOperationResolveDiscoveryOfflineAndPlanEvolution(t *testing.T) {
	for _, operation := range []string{"kubernetes.version", "kubernetes.config.metadata"} {
		t.Run(operation, func(t *testing.T) {
			owner, client, binding := operationResolveFixture(t)
			r := client.response
			r.Operation = operation
			r.Resource = nil
			r.NormalizedInputJson = []byte(`{}`)
			if operation == "kubernetes.version" {
				r.Discovery = &cp.ManagedKubernetesDiscoveryAddress{Kind: cp.ManagedKubernetesDiscoveryKind_MANAGED_KUBERNETES_DISCOVERY_KIND_VERSION}
			}
			first, err := owner.ResolveKubernetesOperation(t.Context(), binding)
			if err != nil {
				t.Fatal("valid address variant refused")
			}
			r.PlanRef = "plan_synthetic"
			r.PlanRevision = 1
			r.PlanDigest = strings.Repeat("e", 64)
			r.ReconcileOnly = true
			r.ExpiresAt = timestamppb.New(r.ExpiresAt.AsTime().Add(time.Second))
			next, err := owner.ResolveKubernetesOperation(t.Context(), binding)
			if err != nil || next.Digest != first.Digest || !next.ExpiresAt.After(first.ExpiresAt) || next.PlanRef == "" || !next.ReconcileOnly {
				t.Fatal("lease or assigned plan corrupted immutable snapshot")
			}
		})
	}
}

// TestOperationResolveNodePinsAndPolicyCopies retains private policy and serving-key metadata without granting a stream.
// TestOperationResolveNodePinsAndPolicyCopies сохраняет private policy и serving-key metadata без stream grant.
func TestOperationResolveNodePinsAndPolicyCopies(t *testing.T) {
	owner, client, binding := operationResolveFixture(t)
	r := client.response
	policy := managedkubernetes.ResourcePolicy{Controllers: []managedkubernetes.ControllerQualification{{Group: "custom.example.invalid", Version: "v1", Resource: "workers", Kind: "Worker", SchemaSHA256: strings.Repeat("a", 64), ControllerKind: "Deployment", ControllerNamespace: "controllers", ControllerName: "worker-controller", ControllerUID: "11111111-1111-4111-8111-111111111111", ControllerContainer: "manager", ControllerImageDigest: strings.Repeat("b", 64), TemplateSHA256: strings.Repeat("c", 64), Consumer: managedkubernetes.ControllerConsumerDataOnlyV1, Extractor: "NONE"}}}
	r.Target.ResourcePolicyJson, _ = json.Marshal(policy)
	_, policyHash, err := managedkubernetes.DecodeResourcePolicy(r.Target.ResourcePolicyJson)
	if err != nil {
		t.Fatal(err)
	}
	r.Target.ResourcePolicySha256 = policyHash
	ca := []byte("synthetic-certificate-metadata-not-a-TLS-fixture")
	hash := sha256.Sum256(ca)
	r.NodeTransport = &cp.ManagedKubernetesNodeTransportSnapshot{PinRef: "node_pin_synthetic", Generation: 1, TargetRef: r.Target.TargetRef, TargetRevision: r.Target.Revision, NodeName: "synthetic-node", NodeUid: "2fbad8aa-1b5d-4f34-a2b6-60793435c6a3", ServerOrigin: "https://node.example.invalid:10250", TlsServerName: "node.example.invalid", CertificateAuthorityPem: ca, CertificateAuthoritySha256: hex.EncodeToString(hash[:]), ServerPublicKeySha256: strings.Repeat("e", 64), AddressIps: []string{"192.0.2.11"}, NotBefore: timestamppb.New(time.Now().Add(-time.Minute)), ExpiresAt: timestamppb.New(time.Now().Add(time.Hour)), Protocol: string(managedkubernetes.WebSocketV5)}
	result, err := owner.ResolveKubernetesOperation(t.Context(), binding)
	if err != nil || result.Target.ResourcePolicy == nil || result.Target.ResourcePolicySHA256 != policyHash || result.NodeTransport == nil {
		t.Fatal("pinned private metadata lost")
	}
	r.NodeTransport.CertificateAuthorityPem[0] = 'X'
	r.NodeTransport.AddressIps[0] = "192.0.2.99"
	if result.NodeTransport.CertificateAuthorityPEM[0] == 'X' || result.NodeTransport.AddressIPs[0] != "192.0.2.11" {
		t.Fatal("node metadata buffer retained")
	}
	if _, err := owner.ResolveKubernetesOperation(t.Context(), binding); err == nil {
		t.Fatal("changed node trust bytes accepted under old hash")
	}
}

// TestOperationResolveRejectsHealthAndInvalidClaimBeforeRPC prevents using the read as an identity selector.
// TestOperationResolveRejectsHealthAndInvalidClaimBeforeRPC запрещает применение read как identity selector.
func TestOperationResolveRejectsHealthAndInvalidClaimBeforeRPC(t *testing.T) {
	owner, client, binding := operationResolveFixture(t)
	binding.InvocationRef = ""
	binding.ConnectionTestRef = "health_synthetic"
	if _, err := owner.ResolveKubernetesOperation(t.Context(), binding); err == nil || client.calls != 0 {
		t.Fatal("health claim reached operation owner")
	}
}

// TestOperationResolveKeepsRegisteredEachFloor rejects scoped approval even when generic namespaced writes allow it.
// TestOperationResolveKeepsRegisteredEachFloor запрещает scoped approval, даже когда generic namespaced write его допускает.
func TestOperationResolveKeepsRegisteredEachFloor(t *testing.T) {
	owner, client, binding := operationResolveFixture(t)
	r := client.response
	r.Operation, r.Risk, r.ApprovalPolicy = "kubernetes.apply", "WRITE", "HUMAN_EACH_EFFECT"
	_, canonical, err := managedkubernetes.DecodeOperationInput(r.Operation, []byte(`{"group":"","version":"v1","resource":"configmaps","namespace":"synthetic","name":"example","body":{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"example","namespace":"synthetic"}},"options":{"fieldManager":"kodex"}}`))
	if err != nil {
		t.Fatal(err)
	}
	r.NormalizedInputJson = canonical
	if _, err := owner.ResolveKubernetesOperation(t.Context(), binding); err != nil {
		t.Fatal("registered gated write refused")
	}
	r.ApprovalPolicy = "HUMAN_SCOPED"
	if _, err := owner.ResolveKubernetesOperation(t.Context(), binding); err == nil {
		t.Fatal("registered EACH floor weakened")
	}
}

// TestOperationResolveSeparatesClusterAndAllNamespaces refuses widening an ordinary cluster read into namespace fan-out.
// TestOperationResolveSeparatesClusterAndAllNamespaces запрещает расширять ordinary cluster read до namespace fan-out.
func TestOperationResolveSeparatesClusterAndAllNamespaces(t *testing.T) {
	for _, operation := range []string{"kubernetes.list", "kubernetes.watch", "kubernetes.list.all-namespaces", "kubernetes.watch.all-namespaces"} {
		t.Run(operation, func(t *testing.T) {
			owner, client, binding := operationResolveFixture(t)
			r := client.response
			r.Operation = operation
			raw := `{"group":"","version":"v1","resource":"configmaps","namespace":""}`
			if strings.Contains(operation, "watch") {
				raw = `{"group":"","version":"v1","resource":"configmaps","namespace":"","options":{"timeoutSeconds":30}}`
			}
			_, canonical, err := managedkubernetes.DecodeOperationInput(operation, []byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			r.NormalizedInputJson = canonical
			for _, all := range []bool{false, true} {
				r.Resource = &cp.ManagedKubernetesResourceAddress{Version: "v1", Resource: "configmaps", Namespaced: all, AllNamespaces: all}
				_, err := owner.ResolveKubernetesOperation(t.Context(), binding)
				if (err == nil) != (all == strings.HasSuffix(operation, ".all-namespaces")) {
					t.Fatal("ordinary cluster scope and explicit all-namespaces scope were confused")
				}
			}
		})
	}
}
