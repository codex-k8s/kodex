package platform

import (
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// acceptWorkflowDeploymentResult accepts only owner-read SSH receipts for the exact
// published target and accepted merge dependency; model text never proves health.
// acceptWorkflowDeploymentResult принимает только owner-read SSH receipt точной
// опубликованной цели и принятого merge; текст модели не доказывает health.
func acceptWorkflowDeploymentResult(requirement entity.WorkflowResultRequirement, declaration entity.WorkflowDeclaredResult, proof workflowIntegrationResult, inputs []workflowResultDependency) (entity.WorkflowAcceptedResult, string) {
	var values map[string]string
	if requirement.Kind != "DEPLOYMENT" || requirement.Operation != "ssh.compose.deploy" || proof.Operation != requirement.Operation || json.Unmarshal(proof.Scope, &values) != nil {
		return entity.WorkflowAcceptedResult{}, workflowReceiptScopeMismatch
	}
	target, err := sshcompose.TargetFromResourceScope(values)
	if err != nil || target.Ref != requirement.TargetRef || target.Repository != requirement.Repository {
		return entity.WorkflowAcceptedResult{}, workflowReceiptScopeMismatch
	}
	subject, found := workflowResultSubject(inputs, requirement.SubjectStepKey)
	if !found || subject.Kind != "INTEGRATION_RECEIPT" || subject.Repository != target.Repository {
		return entity.WorkflowAcceptedResult{}, workflowReviewSubjectMismatch
	}
	if workflowResultSHA([]byte(proof.Summary)) != proof.Digest {
		return entity.WorkflowAcceptedResult{}, workflowReceiptInvalid
	}
	_, valid := workflowResultObject([]byte(proof.Summary), []string{"receipt", "target_ref", "deployed_sha", "bundle_sha256", "image_digest", "health_ok"}, []string{"receipt", "target_ref", "deployed_sha", "bundle_sha256", "image_digest", "health_ok"})
	var output struct {
		Receipt     string `json:"receipt"`
		TargetRef   string `json:"target_ref"`
		SHA         string `json:"deployed_sha"`
		Bundle      string `json:"bundle_sha256"`
		ImageDigest string `json:"image_digest"`
		Health      *bool  `json:"health_ok"`
	}
	if !valid || json.Unmarshal([]byte(proof.Summary), &output) != nil || output.Health == nil || !*output.Health {
		return entity.WorkflowAcceptedResult{}, workflowReceiptInvalid
	}
	var receipt sshcompose.Receipt
	receiptKeys := []string{"version", "targetRef", "projectRef", "deploymentRef", "effectKey", "inputDigest", "mergedSha", "treeSha", "bundleSha256", "image", "generation", "expiresAt", "deadline", "state", "healthOk", "safeErrorCode", "startedAt", "finishedAt"}
	_, valid = workflowResultObject([]byte(output.Receipt), []string{"version", "targetRef", "projectRef", "deploymentRef", "effectKey", "inputDigest", "mergedSha", "treeSha", "bundleSha256", "image", "generation", "state", "healthOk", "startedAt", "finishedAt"}, receiptKeys)
	if !valid || sshcompose.Decode([]byte(output.Receipt), &receipt, 16384) != nil || receipt.Version != sshcompose.ProtocolVersion ||
		receipt.State != "SUCCEEDED" || !receipt.HealthOK || receipt.SafeErrorCode != "" || receipt.Generation < 1 || receipt.StartedAt < 1 || receipt.FinishedAt < receipt.StartedAt ||
		receipt.TargetRef != target.Ref || receipt.ProjectRef != target.ProjectRef || receipt.Image != target.RuntimeImage ||
		output.TargetRef != target.Ref || output.SHA != subject.HeadSHA || receipt.MergedSHA != subject.HeadSHA ||
		!sshComposeCommitPattern.MatchString(receipt.TreeSHA) || !validRuntimeSecretSHA256(receipt.BundleSHA256) || output.Bundle != receipt.BundleSHA256 ||
		!validRuntimeSecretSHA256(receipt.InputDigest) || proof.ProviderRef != "ssh-compose:"+target.Ref+":"+receipt.DeploymentRef ||
		output.ImageDigest != target.RuntimeImage[len(target.RuntimeImage)-71:] {
		return entity.WorkflowAcceptedResult{}, workflowReceiptInvalid
	}
	return entity.WorkflowAcceptedResult{Key: requirement.Key, Kind: requirement.Kind, Repository: requirement.Repository, ReceiptRef: declaration.ReceiptRef,
		Digest: proof.Digest, HeadSHA: receipt.MergedSHA, TargetRef: target.Ref, BundleSHA256: receipt.BundleSHA256, ImageDigest: output.ImageDigest, HealthOK: true}, ""
}
