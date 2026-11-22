package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// kubernetesBrokerFixture observes the real typed client boundary without provider credentials.
// Other broker methods remain unavailable; this fixture proves dispatch, not an actual cluster effect.
// kubernetesBrokerFixture наблюдает real typed client boundary без provider credentials.
// Другие broker methods недоступны; fixture доказывает dispatch, не actual cluster effect.
type kubernetesBrokerFixture struct {
	sb.ManagedKubernetesServiceClient
	calls    int
	binding  *sb.ManagedKubernetesInvocationBinding
	response *sb.ExecuteManagedKubernetesOperationResponse
	err      error
}

// ExecuteManagedKubernetesOperation records one immutable binding and returns the selected reply.
// ExecuteManagedKubernetesOperation записывает один immutable binding и возвращает selected reply.
func (fixture *kubernetesBrokerFixture) ExecuteManagedKubernetesOperation(_ context.Context, request *sb.ExecuteManagedKubernetesOperationRequest, _ ...grpc.CallOption) (*sb.ExecuteManagedKubernetesOperationResponse, error) {
	fixture.calls++
	fixture.binding = proto.Clone(request.Binding).(*sb.ManagedKubernetesInvocationBinding)
	return fixture.response, fixture.err
}

// TestKubernetesEveryTypedOperation dispatches every shipped command through the broker-only port.
// It does not simulate owner SSAR, actual GVR effects or stream output credit and cannot qualify them.
// TestKubernetesEveryTypedOperation направляет каждую shipped command через broker-only port.
// Не имитирует owner SSAR, actual GVR effects либо stream output credit и не квалифицирует их.
func TestKubernetesEveryTypedOperation(t *testing.T) {
	adapter := testAdapter(t)
	for _, operation := range integrationpackage.KubernetesOperations() {
		t.Run(operation.Key, func(t *testing.T) {
			request, binding := kubernetesRequestFixture(t, adapter, operation.Key)
			broker := &kubernetesBrokerFixture{response: kubernetesCompletedFixture()}
			if operation.Execution == integrationpackage.KubernetesStream {
				broker.response = kubernetesStreamFixture(binding)
			}
			response, err := adapter.ExecuteKubernetes(t.Context(), request, binding, broker)
			if err != nil || response == nil || broker.calls != 1 || !proto.Equal(binding, broker.binding) {
				t.Fatalf("typed command dispatch failed: %v", err)
			}
		})
	}
}

// TestKubernetesScopeAndCredentialRefusedBeforeDispatch rejects forged payload authority, generic
// credentials and stale/different input pins before the first broker RPC.
// TestKubernetesScopeAndCredentialRefusedBeforeDispatch отклоняет forged payload authority,
// generic credentials и stale/different input pins до первого broker RPC.
func TestKubernetesScopeAndCredentialRefusedBeforeDispatch(t *testing.T) {
	adapter := testAdapter(t)
	for _, mutate := range []func(*Request, *sb.ManagedKubernetesInvocationBinding){
		func(request *Request, _ *sb.ManagedKubernetesInvocationBinding) {
			request.Credential = &CredentialRevision{SecretRef: "forged"}
		},
		func(request *Request, _ *sb.ManagedKubernetesInvocationBinding) {
			request.ResourcePermissionProfile = ""
		},
		func(request *Request, _ *sb.ManagedKubernetesInvocationBinding) { request.GrantRef = "" },
		func(request *Request, _ *sb.ManagedKubernetesInvocationBinding) {
			request.ResourceScope["namespace"] = "other"
		},
		func(request *Request, _ *sb.ManagedKubernetesInvocationBinding) {
			request.ResourceScope["extra"] = "other"
		},
		func(request *Request, _ *sb.ManagedKubernetesInvocationBinding) {
			request.Configuration["target_ref"] = "other_target"
		},
		func(request *Request, _ *sb.ManagedKubernetesInvocationBinding) {
			request.Input["binding"] = map[string]any{"fence": "forged"}
		},
		func(request *Request, _ *sb.ManagedKubernetesInvocationBinding) {
			request.DefinitionDigest = strings.Repeat("0", 64)
		},
		func(_ *Request, binding *sb.ManagedKubernetesInvocationBinding) {
			binding.InputDigest = strings.Repeat("0", 64)
		},
		func(_ *Request, binding *sb.ManagedKubernetesInvocationBinding) { binding.Fence = "" },
		func(_ *Request, binding *sb.ManagedKubernetesInvocationBinding) {
			binding.ConnectionTestRef = "forged_test"
		},
	} {
		request, binding := kubernetesRequestFixture(t, adapter, "kubernetes.get")
		mutate(&request, binding)
		broker := &kubernetesBrokerFixture{response: kubernetesCompletedFixture()}
		if response, err := adapter.ExecuteKubernetes(t.Context(), request, binding, broker); err == nil || response != nil || broker.calls != 0 {
			t.Fatal("invalid request reached broker dispatch")
		}
	}
}

// TestKubernetesHealthIsOnlySelectedIdentity refuses health claims on the business-input path; ProbeKubernetes owns the opaque health snapshot.
// TestKubernetesHealthIsOnlySelectedIdentity отвергает health claims на business-input path; opaque health snapshot принадлежит ProbeKubernetes.
func TestKubernetesHealthIsOnlySelectedIdentity(t *testing.T) {
	adapter := testAdapter(t)
	request, binding := kubernetesRequestFixture(t, adapter, "kubernetes.identity")
	binding.ConnectionTestRef, binding.InvocationRef = "test_fixture", ""
	request.GrantRef, request.GrantVersion = "", 0
	broker := &kubernetesBrokerFixture{response: kubernetesHealthFixture(binding.ConnectionTestRef, true)}
	if response, err := adapter.ExecuteKubernetes(t.Context(), request, binding, broker); err == nil || response != nil || broker.calls != 0 {
		t.Fatal("health claim entered business-input execution")
	}
	broker.response = kubernetesStreamFixture(binding)
	if response, err := adapter.ExecuteKubernetes(t.Context(), request, binding, broker); err == nil || response != nil {
		t.Fatal("health accepted a stream")
	}
	request, binding = kubernetesRequestFixture(t, adapter, "kubernetes.patch")
	binding.ConnectionTestRef, binding.InvocationRef = "test_fixture", ""
	request.GrantRef, request.GrantVersion = "", 0
	broker.calls = 0
	if _, err := adapter.ExecuteKubernetes(t.Context(), request, binding, broker); err == nil || broker.calls != 0 {
		t.Fatal("health obtained mutation authority")
	}
}

// TestKubernetesMutationUnknownHasNoRetry preserves uncertainty after lost/malformed broker replies
// and exposes neither the raw upstream diagnostic nor a second Execute RPC.
// TestKubernetesMutationUnknownHasNoRetry сохраняет uncertainty после lost/malformed broker replies
// и не раскрывает raw upstream diagnostic либо второй Execute RPC.
func TestKubernetesMutationUnknownHasNoRetry(t *testing.T) {
	adapter := testAdapter(t)
	for _, operation := range integrationpackage.KubernetesOperations() {
		if operation.Risk == integrationpackage.RiskRead {
			continue
		}
		request, binding := kubernetesRequestFixture(t, adapter, operation.Key)
		for _, broker := range []*kubernetesBrokerFixture{
			{err: errors.New("private kubeconfig token and URL diagnostic")},
			{response: &sb.ExecuteManagedKubernetesOperationResponse{}},
		} {
			response, err := adapter.ExecuteKubernetes(t.Context(), request, binding, broker)
			if response != nil || !IsUnknownOutcome(err) || broker.calls != 1 || strings.Contains(err.Error(), "private") {
				t.Fatalf("mutation uncertainty changed for %s", operation.Key)
			}
		}
	}
}

// TestKubernetesResponsePinsRejectsPartialAndUnknownShapes checks safe result hashes and stream
// metadata before any completed bytes or descriptor are handed to the dedicated owner lane.
// TestKubernetesResponsePinsRejectsPartialAndUnknownShapes проверяет safe result hashes и stream
// metadata до передачи completed bytes либо descriptor в dedicated owner lane.
func TestKubernetesResponsePinsRejectsPartialAndUnknownShapes(t *testing.T) {
	adapter := testAdapter(t)
	request, binding := kubernetesRequestFixture(t, adapter, "kubernetes.get")
	for _, mutate := range []func(*sb.ManagedKubernetesSafeResult){
		func(result *sb.ManagedKubernetesSafeResult) { result.ResultDigest = strings.Repeat("0", 64) },
		func(result *sb.ManagedKubernetesSafeResult) {
			result.ResultJson = []byte(`{"x":1,"x":2}`)
			digest := sha256.Sum256(result.ResultJson)
			result.ResultDigest = hex.EncodeToString(digest[:])
		},
		func(result *sb.ManagedKubernetesSafeResult) {
			result.RemoteOutcome = sb.ManagedKubernetesRemoteOutcome(999)
		},
		func(result *sb.ManagedKubernetesSafeResult) { result.ReasonCode = "raw upstream text" },
		func(result *sb.ManagedKubernetesSafeResult) { result.ResultJson = make([]byte, (64<<10)+1) },
	} {
		response := kubernetesCompletedFixture()
		mutate(response.GetCompleted())
		broker := &kubernetesBrokerFixture{response: response}
		if actual, err := adapter.ExecuteKubernetes(t.Context(), request, binding, broker); actual != nil || err == nil || broker.calls != 1 {
			t.Fatal("invalid completed projection escaped")
		}
	}
	request, binding = kubernetesRequestFixture(t, adapter, "kubernetes.watch")
	for _, mutate := range []func(*sb.ManagedKubernetesStreamDescriptor){
		func(stream *sb.ManagedKubernetesStreamDescriptor) { stream.InvocationRef = "foreign_invocation" },
		func(stream *sb.ManagedKubernetesStreamDescriptor) { stream.Generation++ },
		func(stream *sb.ManagedKubernetesStreamDescriptor) {
			stream.ExpiresAt.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
		},
		func(stream *sb.ManagedKubernetesStreamDescriptor) {
			stream.ExpiresAt = timestamppb.New(time.Now().Add(-time.Minute))
		},
		func(stream *sb.ManagedKubernetesStreamDescriptor) { stream.MaximumFrameBytes = 0 },
		func(stream *sb.ManagedKubernetesStreamDescriptor) {
			stream.OutputChannels = []sb.ManagedKubernetesChannel{sb.ManagedKubernetesChannel(999)}
		},
		func(stream *sb.ManagedKubernetesStreamDescriptor) {
			stream.InputChannels = []sb.ManagedKubernetesChannel{sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN}
		},
	} {
		response := kubernetesStreamFixture(binding)
		mutate(response.GetStream())
		if actual, err := adapter.ExecuteKubernetes(t.Context(), request, binding, &kubernetesBrokerFixture{response: response}); actual != nil || err == nil {
			t.Fatal("invalid stream projection escaped")
		}
	}
}

// kubernetesRequestFixture creates synthetic owner pins around real shipped schema validation.
// None of these locators are credentials or authorization for a live Kubernetes target.
// kubernetesRequestFixture создаёт synthetic owner pins вокруг real shipped schema validation.
// Locators не являются credentials либо authorization для live Kubernetes target.
func kubernetesRequestFixture(t *testing.T, adapter *Adapter, operation string) (Request, *sb.ManagedKubernetesInvocationBinding) {
	t.Helper()
	definition := adapter.definitions[integrationpackage.KubernetesDefinitionKey]
	capability, exists := definition.Capability(operation)
	if !exists {
		t.Fatal("missing shipped Kubernetes command")
	}
	input, namespace := kubernetesInputFixture(operation)
	configuration := map[string]string{"target_ref": "target_fixture"}
	scope, err := capability.KubernetesResourceScope(configuration, namespace)
	if err != nil {
		t.Fatal(err)
	}
	encodedScope, _ := json.Marshal(scope)
	scopeDigest := sha256.Sum256(encodedScope)
	encodedInput, _ := json.Marshal(input)
	canonicalInput, err := capability.ValidateInput(encodedInput)
	if err != nil {
		t.Fatalf("invalid fixture for %s: %v", operation, err)
	}
	inputDigest := sha256.Sum256(canonicalInput)
	definitionPackage, _ := json.Marshal(definition)
	request := Request{DefinitionPackage: definitionPackage, DefinitionKey: definition.Metadata.Key, DefinitionVersion: definition.Metadata.Version,
		DefinitionDigest: definition.Digest, ConnectionRef: "connection_fixture", CapabilityKey: capability.Key,
		Operation: operation, Risk: capability.Risk, ApprovalPolicy: capability.ApprovalPolicy, ResourceKind: capability.ResourceScope.Kind,
		ResourceScope: scope, ResourceScopeDigest: hex.EncodeToString(scopeDigest[:]), Input: input, InputDigest: hex.EncodeToString(inputDigest[:]),
		Configuration: map[string]any{"target_ref": "target_fixture"}, EffectKey: "effect_fixture", GrantRef: "grant_fixture", GrantVersion: 1,
		ResourcePermissionProfile: integrationpackage.CredentialResourcePermissionKubernetesScopedV1}
	binding := &sb.ManagedKubernetesInvocationBinding{WorkloadInstance: "gateway_fixture", InvocationRef: "invocation_fixture", LeaseRef: "lease_fixture", Fence: "fence_fixture", Generation: 1, InputDigest: request.InputDigest}
	return request, binding
}

// kubernetesInputFixture enumerates representative command data; the production parser checks it.
// It qualifies gateway routing only; real GVR plans and external effects belong to broker fixtures.
// kubernetesInputFixture перечисляет representative command data, проверяемые production parser.
// Квалифицирует только gateway routing; real GVR plans и external effects — broker fixtures.
func kubernetesInputFixture(operation string) (map[string]any, string) {
	if slices.Contains([]string{"kubernetes.discovery", "kubernetes.version", "kubernetes.cluster-info", "kubernetes.identity", "kubernetes.config.metadata", "kubernetes.help"}, operation) {
		return map[string]any{}, "@cluster"
	}
	if operation == "kubernetes.kustomize" {
		return map[string]any{"options": map[string]any{"artifactRef": "artifact_fixture", "artifactSHA256": strings.Repeat("a", 64), "entryPath": "base"}}, "@cluster"
	}
	input := map[string]any{"group": "apps", "version": "v1", "resource": "deployments", "namespace": "example", "name": "web",
		"uid": "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8", "resourceVersion": "17"}
	namespace := "example"
	collection := slices.Contains([]string{"kubernetes.list", "kubernetes.list.all-namespaces", "kubernetes.events", "kubernetes.watch", "kubernetes.watch.all-namespaces", "kubernetes.create", "kubernetes.top", "kubernetes.run", "kubernetes.explain", "kubernetes.access"}, operation)
	if collection {
		delete(input, "name")
		delete(input, "uid")
		delete(input, "resourceVersion")
	}
	if strings.HasSuffix(operation, ".all-namespaces") {
		input["namespace"], namespace = "", "@all-namespaces"
	}
	if slices.Contains([]string{"kubernetes.cordon", "kubernetes.uncordon", "kubernetes.taint", "kubernetes.drain", "kubernetes.certificate.approve", "kubernetes.certificate.deny"}, operation) {
		input["namespace"], namespace = "", "@cluster"
	}
	if strings.HasPrefix(operation, "kubernetes.status.") {
		input["subresource"] = "status"
	}
	if operation == "kubernetes.scale" {
		input["subresource"] = "scale"
	}
	if slices.Contains([]string{"kubernetes.create", "kubernetes.replace", "kubernetes.apply", "kubernetes.edit", "kubernetes.status.update", "kubernetes.diff", "kubernetes.auth.reconcile"}, operation) {
		metadata := map[string]any{"name": "web", "namespace": "example"}
		if operation != "kubernetes.create" {
			metadata["uid"], metadata["resourceVersion"] = input["uid"], input["resourceVersion"]
		}
		input["body"] = map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": metadata, "spec": map[string]any{}}
	}
	options := map[string]any{}
	switch operation {
	case "kubernetes.watch", "kubernetes.watch.all-namespaces", "kubernetes.logs", "kubernetes.rollout.status", "kubernetes.drain", "kubernetes.attach":
		options["timeoutSeconds"] = 10
	case "kubernetes.apply":
		options["fieldManager"] = "kodex"
	case "kubernetes.patch", "kubernetes.status.patch":
		input["body"], options["patchType"] = map[string]any{"metadata": map[string]any{"labels": map[string]any{"mode": "safe"}}}, "MERGE"
	case "kubernetes.scale":
		options["replicas"] = 1
	case "kubernetes.wait":
		options["condition"], options["timeoutSeconds"] = "Ready", 10
	case "kubernetes.access":
		options["verb"] = "get"
	case "kubernetes.run":
		options["podName"], options["image"] = "web", "example.invalid/image@sha256:"+strings.Repeat("a", 64)
	case "kubernetes.expose":
		options["serviceName"], options["ports"] = "web", []any{map[string]any{"port": 8080, "targetPort": 8080}}
	case "kubernetes.autoscale":
		options["maximumReplicas"] = 3
	case "kubernetes.set.image":
		options["assignments"] = map[string]any{"web": "example.invalid/image@sha256:" + strings.Repeat("a", 64)}
	case "kubernetes.set.selector":
		options["selectorAssignments"] = map[string]any{"app": "web"}
	case "kubernetes.set.serviceaccount":
		options["serviceAccount"] = "web"
	case "kubernetes.set.subject":
		options["subjects"] = []any{map[string]any{"kind": "User", "name": "example"}}
	case "kubernetes.rollout.undo":
		options["revision"] = 1
	case "kubernetes.exec":
		options["command"], options["timeoutSeconds"] = []string{"echo", "example"}, 10
	case "kubernetes.debug":
		options["image"], options["timeoutSeconds"] = "example.invalid/image@sha256:"+strings.Repeat("a", 64), 10
	case "kubernetes.cp.upload", "kubernetes.cp.download":
		input["group"], input["version"], input["resource"] = "", "v1", "pods"
		options["transferFormat"], options["remotePath"], options["maximumBytes"], options["timeoutSeconds"] = "FILE", "/tmp/example", 1024, 10
		if operation == "kubernetes.cp.upload" {
			options["artifactRef"], options["artifactSHA256"] = "artifact_fixture", strings.Repeat("a", 64)
		}
	case "kubernetes.port-forward":
		options["ports"], options["timeoutSeconds"] = []any{map[string]any{"port": 8080, "targetPort": 8080}}, 10
	}
	if len(options) > 0 {
		input["options"] = options
	}
	return input, namespace
}

// kubernetesCompletedFixture supplies a valid bounded safe projection with an exact content digest.
// kubernetesCompletedFixture возвращает valid bounded safe projection с exact content digest.
func kubernetesCompletedFixture() *sb.ExecuteManagedKubernetesOperationResponse {
	raw := []byte(`{"metadata":"safe"}`)
	digest := sha256.Sum256(raw)
	completed := &sb.ManagedKubernetesSafeResult{ResultJson: raw, ResultDigest: hex.EncodeToString(digest[:]), RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED}
	completed.EffectRef, completed.EffectGeneration, completed.InvocationGeneration = "effect_fixture", 1, 1
	completed.AuthorizedUntil = timestamppb.New(time.Now().Add(20 * time.Second))
	return &sb.ExecuteManagedKubernetesOperationResponse{Result: &sb.ExecuteManagedKubernetesOperationResponse_Completed{Completed: completed}}
}

// kubernetesStreamFixture returns only stream metadata; it grants no output credits or completion.
// kubernetesStreamFixture возвращает только stream metadata, не output credits либо completion.
func kubernetesStreamFixture(binding *sb.ManagedKubernetesInvocationBinding) *sb.ExecuteManagedKubernetesOperationResponse {
	stream := &sb.ManagedKubernetesStreamDescriptor{StreamRef: "stream_fixture", InvocationRef: binding.InvocationRef, Generation: binding.Generation,
		ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)), MaximumFrameBytes: 32768, MaximumTotalBytes: 1 << 20,
		OutputChannels: []sb.ManagedKubernetesChannel{sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT}}
	return &sb.ExecuteManagedKubernetesOperationResponse{Result: &sb.ExecuteManagedKubernetesOperationResponse_Stream{Stream: stream}}
}
