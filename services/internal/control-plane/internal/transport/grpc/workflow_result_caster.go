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
		result = append(result, &controlplanev1.WorkflowResultRequirement{Key: value.Key, Kind: value.Kind, FileName: value.FileName, MediaType: value.MediaType, Repository: value.Repository, SubjectStepKey: value.SubjectStepKey, SubjectResultKey: value.SubjectResultKey, Operation: value.Operation, TargetRef: value.TargetRef})
	}
	return result
}

// castWorkflowAttemptResults emits only safe owner evidence, excluding internal runtime row IDs.
// castWorkflowAttemptResults передаёт безопасные доказательства без внутренних runtime row IDs.
func castWorkflowAttemptResults(values []entity.WorkflowAttemptResult) []*controlplanev1.WorkflowAttemptResult {
	result := make([]*controlplanev1.WorkflowAttemptResult, 0, len(values))
	for _, value := range values {
		item := &controlplanev1.WorkflowAttemptResult{Attempt: value.Attempt, RunRef: value.RunRef, Accepted: value.Accepted, Reason: value.Reason, EffectiveState: value.EffectiveState}
		for _, evidence := range value.Results {
			item.Results = append(item.Results, &controlplanev1.WorkflowAcceptedResult{Key: evidence.Key, Kind: evidence.Kind, ArtifactRef: evidence.ArtifactRef, ArtifactRevision: evidence.ArtifactRevision, Digest: evidence.Digest, ReceiptRef: evidence.ReceiptRef, Repository: evidence.Repository, Number: evidence.Number, HeadSha: evidence.HeadSHA, Verdict: evidence.Verdict, TargetRef: evidence.TargetRef, BundleSha256: evidence.BundleSHA256, ImageDigest: evidence.ImageDigest, HealthOk: evidence.HealthOK})
		}
		result = append(result, item)
	}
	return result
}

// castWorkflowReworkPreview copies only public explanatory data and no owner pins.
// castWorkflowReworkPreview копирует лишь public explanatory data без owner pins.
func castWorkflowReworkPreview(value *entity.WorkflowReworkPreview) *controlplanev1.WorkflowReworkPreview {
	if value == nil {
		return nil
	}
	result := &controlplanev1.WorkflowReworkPreview{Schema: value.Schema, SourceStepKey: value.SourceStepKey, SourceAttempt: value.SourceAttempt, BlockReason: value.BlockReason}
	for _, step := range value.Authors {
		result.Authors = append(result.Authors, &controlplanev1.WorkflowReworkStepPreview{StepKey: step.StepKey, PreviousAttempt: step.PreviousAttempt, NextAttempt: step.NextAttempt})
	}
	for _, step := range value.Rechecks {
		result.Rechecks = append(result.Rechecks, &controlplanev1.WorkflowReworkStepPreview{StepKey: step.StepKey, PreviousAttempt: step.PreviousAttempt, NextAttempt: step.NextAttempt})
	}
	return result
}
