package platform

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// validateSSHComposeCompletion binds successful provider evidence to immutable owner release and live dependencies.
// validateSSHComposeCompletion связывает успешное provider evidence с immutable owner release и live dependencies.
func (repository *Repository) validateSSHComposeCompletion(ctx context.Context, tx pgx.Tx, current scope, invocationID string, payload command.IntegrationInvocationInput) error {
	var operation, instance string
	var raw []byte
	var recovery bool
	if tx.QueryRow(ctx, querySSHComposeCompletion, pgx.StrictNamedArgs{"invocation_id": invocationID}).Scan(&operation, &raw, &recovery, &instance) != nil {
		return errs.ErrUnavailable
	}
	if !strings.HasPrefix(operation, "ssh.compose.") {
		return nil
	}
	if !payload.Success {
		return nil
	}
	var snapshot sshcompose.Snapshot
	if sshcompose.Decode(raw, &snapshot, 32768) != nil {
		return errs.ErrForbidden
	}
	definition, ok := repository.integrationDefinitions["ssh-compose"]
	capability, exists := definition.Capability(operation)
	if !ok || !exists {
		return errs.ErrForbidden
	}
	canonical, err := capability.ValidateOutput([]byte(payload.ResultSummary))
	if err != nil {
		return errs.ErrInvalid
	}
	var output map[string]json.RawMessage
	if json.Unmarshal(canonical, &output) != nil {
		return errs.ErrInvalid
	}
	var targetRef, sha, tree, bundle, image string
	var healthy bool
	_ = json.Unmarshal(output["target_ref"], &targetRef)
	if operation == "ssh.compose.inspect" {
		_ = json.Unmarshal(output["ready"], &healthy)
		_ = json.Unmarshal(output["merged_sha"], &sha)
		_ = json.Unmarshal(output["tree_sha"], &tree)
		_ = json.Unmarshal(output["bundle_sha256"], &bundle)
		if !healthy || targetRef != snapshot.Target.Ref {
			return errs.ErrInvalid
		}
		if snapshot.Source == nil {
			if sha != "" || tree != "" || bundle != "" {
				return errs.ErrInvalid
			}
		} else if sha != snapshot.MergedSHA || !sshComposeCommitPattern.MatchString(tree) || !validRuntimeSecretSHA256(bundle) {
			return errs.ErrInvalid
		}
	} else {
		var encoded string
		if json.Unmarshal(output["receipt"], &encoded) != nil {
			return errs.ErrInvalid
		}
		var receipt sshcompose.Receipt
		if sshcompose.Decode([]byte(encoded), &receipt, 16384) != nil || receipt.Version != sshcompose.ProtocolVersion || receipt.TargetRef != snapshot.Target.Ref || receipt.ProjectRef != snapshot.Target.ProjectRef ||
			receipt.DeploymentRef != snapshot.DeploymentRef || receipt.InputDigest != snapshot.DeploymentInputDigest || receipt.EffectKey != snapshot.DeploymentEffectKey || receipt.MergedSHA != snapshot.MergedSHA ||
			receipt.TreeSHA != snapshot.TreeSHA || receipt.BundleSHA256 != snapshot.BundleSHA256 || receipt.Image != snapshot.Target.RuntimeImage {
			return errs.ErrInvalid
		}
		if operation == "ssh.compose.deploy" {
			_ = json.Unmarshal(output["deployed_sha"], &sha)
			_ = json.Unmarshal(output["bundle_sha256"], &bundle)
			_ = json.Unmarshal(output["image_digest"], &image)
			_ = json.Unmarshal(output["health_ok"], &healthy)
			if targetRef != snapshot.Target.Ref || sha != snapshot.MergedSHA || bundle != snapshot.BundleSHA256 || image != snapshot.Target.RuntimeImage[len(snapshot.Target.RuntimeImage)-71:] ||
				!healthy || !receipt.HealthOK || receipt.State != "SUCCEEDED" || receipt.Generation < 1 || (!recovery && receipt.Generation != payload.Generation) || (recovery && receipt.Generation >= payload.Generation) {
				return errs.ErrInvalid
			}
		}
	}
	_, err = repository.issueSSHComposePermit(ctx, tx, current, platformrepo.SSHComposeLeaseBinding{WorkloadInstance: instance, InvocationRef: payload.InvocationRef, LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}, "", false)
	return err
}
