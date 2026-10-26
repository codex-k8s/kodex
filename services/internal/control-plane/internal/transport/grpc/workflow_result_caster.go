package grpc

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// castWorkflowResultRequirements copies the pinned predicate contract without assigning evidence.
// castWorkflowResultRequirements копирует закреплённый контракт условий без назначения доказательств.
func castWorkflowResultRequirements(values []entity.WorkflowResultRequirement) []*controlplanev1.WorkflowResultRequirement {
	result := make([]*controlplanev1.WorkflowResultRequirement, 0, len(values))
	for _, value := range values {
		result = append(result, &controlplanev1.WorkflowResultRequirement{Key: value.Key, Kind: value.Kind, FileName: value.FileName, MediaType: value.MediaType, Repository: value.Repository, SubjectStepKey: value.SubjectStepKey, Operation: value.Operation, TargetRef: value.TargetRef})
	}
	return result
}

// castWorkflowAttemptResults emits only safe owner evidence, excluding internal runtime row IDs.
// castWorkflowAttemptResults передаёт безопасные доказательства без внутренних runtime row IDs.
func castWorkflowAttemptResults(values []entity.WorkflowAttemptResult) []*controlplanev1.WorkflowAttemptResult {
	result := make([]*controlplanev1.WorkflowAttemptResult, 0, len(values))
	for _, value := range values {
		item := &controlplanev1.WorkflowAttemptResult{Attempt: value.Attempt, RunRef: value.RunRef, Accepted: value.Accepted, Reason: value.Reason}
		for _, evidence := range value.Results {
			item.Results = append(item.Results, &controlplanev1.WorkflowAcceptedResult{Key: evidence.Key, Kind: evidence.Kind, ArtifactRef: evidence.ArtifactRef, ArtifactRevision: evidence.ArtifactRevision, Digest: evidence.Digest, ReceiptRef: evidence.ReceiptRef, Repository: evidence.Repository, Number: evidence.Number, HeadSha: evidence.HeadSHA, Verdict: evidence.Verdict, TargetRef: evidence.TargetRef, BundleSha256: evidence.BundleSHA256, ImageDigest: evidence.ImageDigest, HealthOk: evidence.HealthOK})
		}
		result = append(result, item)
	}
	return result
}
