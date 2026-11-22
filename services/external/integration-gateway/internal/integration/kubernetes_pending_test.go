package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// kubernetesPendingFixture produces the broker's closed typed detail without model-supplied selectors.
// kubernetesPendingFixture создаёт closed typed detail broker без model-supplied selectors.
func kubernetesPendingFixture(reconcile bool) *errdetails.ErrorInfo {
	detail := &errdetails.ErrorInfo{Domain: "kodex.control-plane", Reason: "KUBERNETES_APPROVAL_REQUIRED", Metadata: map[string]string{
		"planRef": "plan_fixture", "childRef": "child_fixture", "gateRef": "gate_fixture",
	}}
	if reconcile {
		detail.Reason = "KUBERNETES_RECONCILIATION_REQUIRED"
		delete(detail.Metadata, "gateRef")
		detail.Metadata["effectRef"], detail.Metadata["effectGeneration"] = "effect_fixture", "2"
	}
	return detail
}

// kubernetesPendingStatus serializes the production detail with an intentionally private diagnostic that must not escape.
// kubernetesPendingStatus сериализует production detail с намеренно private diagnostic, которая не должна выйти наружу.
func kubernetesPendingStatus(t *testing.T, detail *errdetails.ErrorInfo) error {
	t.Helper()
	reply, err := status.New(codes.FailedPrecondition, "private upstream diagnostic").WithDetails(detail)
	if err != nil {
		t.Fatal(err)
	}
	return reply.Err()
}

// TestKubernetesPendingPreservesOriginalCall checks actual adapter dispatch and typed continuation without a second effect or a fabricated completion.
// TestKubernetesPendingPreservesOriginalCall проверяет actual adapter dispatch и typed continuation без второго effect либо fabricated completion.
func TestKubernetesPendingPreservesOriginalCall(t *testing.T) {
	adapter := testAdapter(t)
	request, binding := kubernetesRequestFixture(t, adapter, "kubernetes.patch")
	for _, reconcile := range []bool{false, true} {
		broker := &kubernetesBrokerFixture{err: kubernetesPendingStatus(t, kubernetesPendingFixture(reconcile))}
		response, err := adapter.ExecuteKubernetes(t.Context(), request, binding, broker)
		var pending *KubernetesPending
		if response != nil || !errors.As(err, &pending) || broker.calls != 1 || !proto.Equal(broker.binding, binding) ||
			pending.PlanRef != "plan_fixture" || pending.ChildRef != "child_fixture" || strings.Contains(err.Error(), "private") {
			t.Fatal("pending reply lost original binding or became an effect result")
		}
		if reconcile && (pending.EffectRef != "effect_fixture" || pending.EffectGeneration != 2 || pending.GateRef != "") {
			t.Fatal("reconciliation pins changed")
		}
		if !reconcile && (pending.GateRef != "gate_fixture" || pending.EffectRef != "" || pending.EffectGeneration != 0) {
			t.Fatal("gate acquired effect credit")
		}
		broker.response = kubernetesCompletedFixture()
		_, err = adapter.ExecuteKubernetes(t.Context(), request, binding, broker)
		if errors.As(err, &pending) || !IsUnknownOutcome(err) {
			t.Fatal("mixed result and pending error accepted")
		}
	}
}

// TestKubernetesPendingRejectsUnknownShapes refuses malformed details and never authorizes continuation by message text alone.
// TestKubernetesPendingRejectsUnknownShapes отвергает malformed details и не допускает continuation по одному message text.
func TestKubernetesPendingRejectsUnknownShapes(t *testing.T) {
	for _, reconcile := range []bool{false, true} {
		for _, mutate := range []func(*errdetails.ErrorInfo){
			func(info *errdetails.ErrorInfo) { info.Domain = "example.invalid" },
			func(info *errdetails.ErrorInfo) { info.Reason = "FUTURE" },
			func(info *errdetails.ErrorInfo) { info.Metadata["extra"] = "unexpected" },
			func(info *errdetails.ErrorInfo) { delete(info.Metadata, "childRef") },
			func(info *errdetails.ErrorInfo) { info.Metadata["planRef"] = "raw private text" },
			func(info *errdetails.ErrorInfo) { info.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01}) },
		} {
			info := kubernetesPendingFixture(reconcile)
			mutate(info)
			if kubernetesPendingFromError(kubernetesPendingStatus(t, info)) != nil {
				t.Fatal("unknown pending shape accepted")
			}
		}
	}
	for _, generation := range []string{"", "0", "-1", "02", "+2", "2 ", "9223372036854775808"} {
		info := kubernetesPendingFixture(true)
		info.Metadata["effectGeneration"] = generation
		if kubernetesPendingFromError(kubernetesPendingStatus(t, info)) != nil {
			t.Fatal("malformed effect generation accepted")
		}
	}
	for _, err := range []error{nil, errors.New("managed Kubernetes approval required"), status.Error(codes.FailedPrecondition, "managed Kubernetes approval required")} {
		if kubernetesPendingFromError(err) != nil {
			t.Fatal("message text acquired continuation authority")
		}
	}
	info := kubernetesPendingFixture(false)
	for _, code := range []codes.Code{codes.PermissionDenied, codes.Unavailable} {
		reply, err := status.New(code, "private").WithDetails(info)
		if err != nil || kubernetesPendingFromError(reply.Err()) != nil {
			t.Fatal("unrelated code acquired pending semantics")
		}
	}
	reply, err := status.New(codes.FailedPrecondition, "private").WithDetails(info, info)
	if err != nil || kubernetesPendingFromError(reply.Err()) != nil {
		t.Fatal("duplicate detail accepted")
	}
}

// TestKubernetesResultRequiresFreshOwnerCredit rejects another generation, absent effect pins and expired output authority before bytes leave the adapter.
// TestKubernetesResultRequiresFreshOwnerCredit отвергает другую generation, absent effect pins и expired output authority до выдачи bytes из adapter.
func TestKubernetesResultRequiresFreshOwnerCredit(t *testing.T) {
	adapter := testAdapter(t)
	request, binding := kubernetesRequestFixture(t, adapter, "kubernetes.get")
	for _, mutate := range []func(*sb.ManagedKubernetesSafeResult){
		func(result *sb.ManagedKubernetesSafeResult) { result.EffectRef = "" },
		func(result *sb.ManagedKubernetesSafeResult) { result.EffectGeneration = 0 },
		func(result *sb.ManagedKubernetesSafeResult) { result.InvocationGeneration++ },
		func(result *sb.ManagedKubernetesSafeResult) { result.AuthorizedUntil = nil },
		func(result *sb.ManagedKubernetesSafeResult) {
			result.AuthorizedUntil = timestamppb.New(time.Now().Add(-time.Second))
		},
		func(result *sb.ManagedKubernetesSafeResult) {
			result.AuthorizedUntil = timestamppb.New(time.Now().Add(time.Hour))
		},
		func(result *sb.ManagedKubernetesSafeResult) {
			result.AuthorizedUntil.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
		},
	} {
		response := kubernetesCompletedFixture()
		mutate(response.GetCompleted())
		broker := &kubernetesBrokerFixture{response: response}
		if received, err := adapter.ExecuteKubernetes(t.Context(), request, binding, broker); received != nil || err == nil || broker.calls != 1 {
			t.Fatal("invalid owner credit released a result")
		}
	}
}

// kubernetesHealthFixture encodes only the separate owner health acknowledgement and never includes resource-effect credit.
// kubernetesHealthFixture кодирует лишь отдельное owner health acknowledgement без resource-effect credit.
func kubernetesHealthFixture(testRef string, authenticated bool) *sb.ExecuteManagedKubernetesOperationResponse {
	raw, _ := json.Marshal(map[string]any{"authenticated": authenticated, "resourcePermissionChecks": "PER_OPERATION_REQUIRED"})
	digest := sha256.Sum256(raw)
	result := &sb.ManagedKubernetesSafeResult{EffectRef: testRef, ResultJson: raw, ResultDigest: hex.EncodeToString(digest[:]), RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED}
	if !authenticated {
		result.RemoteOutcome, result.ReasonCode = sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_FAILED, "HEALTH_CHECK_FAILED"
	}
	return &sb.ExecuteManagedKubernetesOperationResponse{Result: &sb.ExecuteManagedKubernetesOperationResponse_Completed{Completed: result}}
}

// TestKubernetesHealthCompletionIsSeparate checks the response validator used by ProbeKubernetes, with no inference of resource authority.
// TestKubernetesHealthCompletionIsSeparate проверяет response validator ProbeKubernetes без вывода resource authority.
func TestKubernetesHealthCompletionIsSeparate(t *testing.T) {
	binding := &sb.ManagedKubernetesInvocationBinding{ConnectionTestRef: "test_fixture", Generation: 1}
	for _, authenticated := range []bool{true, false} {
		response := kubernetesHealthFixture(binding.ConnectionTestRef, authenticated)
		if !validKubernetesResponse(response, binding, integrationpackage.Capability{}) {
			t.Fatal("valid health outcome rejected")
		}
		for _, mutate := range []func(*sb.ManagedKubernetesSafeResult){
			func(result *sb.ManagedKubernetesSafeResult) { result.EffectRef = "foreign_test" },
			func(result *sb.ManagedKubernetesSafeResult) { result.EffectGeneration = 1 },
			func(result *sb.ManagedKubernetesSafeResult) { result.InvocationGeneration = binding.Generation },
			func(result *sb.ManagedKubernetesSafeResult) {
				result.AuthorizedUntil = timestamppb.New(time.Now().Add(time.Second))
			},
			func(result *sb.ManagedKubernetesSafeResult) {
				result.RemoteOutcome = sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_UNKNOWN
			},
		} {
			received := proto.Clone(response).(*sb.ExecuteManagedKubernetesOperationResponse)
			mutate(received.GetCompleted())
			if validKubernetesResponse(received, binding, integrationpackage.Capability{}) {
				t.Fatal("health accepted foreign or contradictory authority")
			}
		}
	}
}
