package controlplane

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// filePreparationClientFixture controls owner compatibility and one source response without storage or native effects.
// filePreparationClientFixture задаёт owner compatibility и source response без storage либо native effects.
type filePreparationClientFixture struct {
	frameClientFixture
	readiness *cp.CheckManagedKubernetesOwnerReadinessResponse
	response  *cp.PrepareManagedKubernetesFileInputResponse
	calls     int
	request   *cp.PrepareManagedKubernetesFileInputRequest
}

// CheckManagedKubernetesOwnerReadiness returns the exact synthetic compatibility conjunction.
// CheckManagedKubernetesOwnerReadiness возвращает exact synthetic compatibility conjunction.
func (f *filePreparationClientFixture) CheckManagedKubernetesOwnerReadiness(context.Context, *cp.CheckManagedKubernetesOwnerReadinessRequest, ...grpc.CallOption) (*cp.CheckManagedKubernetesOwnerReadinessResponse, error) {
	return f.readiness, nil
}

// PrepareManagedKubernetesFileInput records one protected request and never retries it.
// PrepareManagedKubernetesFileInput фиксирует protected request и не повторяет его.
func (f *filePreparationClientFixture) PrepareManagedKubernetesFileInput(_ context.Context, r *cp.PrepareManagedKubernetesFileInputRequest, _ ...grpc.CallOption) (*cp.PrepareManagedKubernetesFileInputResponse, error) {
	f.calls++
	f.request = r
	return f.response, nil
}

// TestFilePreparationRequiresEveryKnownContract closes absent/future compatibility before any preparation request.
// TestFilePreparationRequiresEveryKnownContract закрывает absent/future compatibility до preparation request.
func TestFilePreparationRequiresEveryKnownContract(t *testing.T) {
	for _, mode := range []string{"valid", "not_ready", "delivery_missing", "output_missing", "input_missing", "file_missing", "file_future", "unknown", "expiry", "foreign_child", "missing_timestamp", "oversized_origin"} {
		t.Run(mode, func(t *testing.T) {
			owner, base, binding, _ := frameAdapterFixture()
			now := time.Now()
			until := now.Add(10 * time.Second)
			plan := targetrepo.OperationPlan{Ref: "kpl_synthetic", Revision: 1, Digest: strings.Repeat("a", 64)}
			options, _ := json.Marshal(managedkubernetes.CopyProfile{ArtifactRef: "art_synthetic", ArtifactSHA256: strings.Repeat("b", 64), TransferFormat: "FILE", RemotePath: "/tmp/source", MaximumBytes: 4096, TimeoutSeconds: 30})
			child := targetrepo.OperationChild{Ref: "kch_synthetic", Operation: "kubernetes.cp.upload", Input: managedkubernetes.OperationInput{Version: "v1", Resource: "pods", Options: options}}
			snapshot := value.KubernetesOperationSnapshot{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ExpiresAt: now.Add(30 * time.Second)}
			f := &filePreparationClientFixture{frameClientFixture: *base, readiness: &cp.CheckManagedKubernetesOwnerReadinessResponse{Ready: true, DeliveryContractRevision: 1, OutputContractRevision: 1, InputContractRevision: 1, FileInputContractRevision: 1}, response: &cp.PrepareManagedKubernetesFileInputResponse{AuthorizedUntil: timestamppb.New(until), View: &cp.ManagedKubernetesFileInputView{SourceRef: "kfs_synthetic", InvocationRef: binding.InvocationRef, PlanRef: plan.Ref, ChildRef: child.Ref, State: cp.ManagedKubernetesFileInputState_MANAGED_KUBERNETES_FILE_INPUT_STATE_READY, Version: 1, ArtifactRef: "art_synthetic", ArtifactRevisionRef: "arv_synthetic", ArtifactSha256: strings.Repeat("b", 64), ArtifactSizeBytes: 4, TransferFormat: "FILE", NormalizedSha256: strings.Repeat("c", 64), NormalizedSizeBytes: 2048, NormalizationRevision: 1, Reason: cp.ManagedKubernetesFileInputReason_MANAGED_KUBERNETES_FILE_INPUT_REASON_NONE, CreatedAt: timestamppb.New(now.Add(-time.Second)), HardDeadline: timestamppb.New(now.Add(time.Minute))}}}
			switch mode {
			case "not_ready":
				f.readiness.Ready = false
			case "delivery_missing":
				f.readiness.DeliveryContractRevision = 0
			case "output_missing":
				f.readiness.OutputContractRevision = 0
			case "input_missing":
				f.readiness.InputContractRevision = 0
			case "file_missing":
				f.readiness.FileInputContractRevision = 0
			case "file_future":
				f.readiness.FileInputContractRevision = 2
			case "unknown":
				f.readiness.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
			case "expiry":
				f.response.AuthorizedUntil = timestamppb.New(now.Add(-time.Second))
			case "foreign_child":
				f.response.View.ChildRef = "kch_foreign00"
			case "missing_timestamp":
				f.response.View.HardDeadline = nil
			case "oversized_origin":
				f.response.View.ArtifactSizeBytes = 16<<20 + 1
			}
			owner.client.KubernetesOperations = f
			result, err := owner.PrepareKubernetesFileInput(t.Context(), binding, snapshot, plan, child)
			if mode == "valid" {
				if err != nil || result.SourceRef != "kfs_synthetic" || f.calls != 1 || f.request.ChildRef != child.Ref || f.request.Binding.Fence != binding.Fence {
					t.Fatal("exact FILE preparation rejected", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid FILE compatibility/source admitted")
			}
			if mode != "expiry" && mode != "foreign_child" && mode != "missing_timestamp" && mode != "oversized_origin" && f.calls != 0 {
				t.Fatal("incompatible owner received preparation")
			}
		})
	}
}
