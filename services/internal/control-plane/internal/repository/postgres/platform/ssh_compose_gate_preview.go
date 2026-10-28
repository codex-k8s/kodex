package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// querySSHComposeGatePreview reads only safe immutable release coordinates after gate eligibility.
// querySSHComposeGatePreview читает только безопасные immutable координаты после eligibility Gate.
//
//go:embed sql/ssh_compose__gate_preview.sql
var querySSHComposeGatePreview string

// projectSSHComposeGatePreview exposes the owner snapshot without secret or private execution pins.
// projectSSHComposeGatePreview показывает owner snapshot без secret и private execution pins.
func projectSSHComposeGatePreview(ctx context.Context, runner queryRunner, current scope, gateRef string, intent *entity.IntegrationIntent) error {
	var targetRef, sha, tree, bundle, image string
	if err := runner.QueryRow(ctx, querySSHComposeGatePreview, pgx.StrictNamedArgs{"organization_id": current.organizationID, "gate_ref": gateRef}).Scan(&targetRef, &sha, &tree, &bundle, &image); err != nil {
		return errs.ErrUnavailable
	}
	return appendSSHComposeGatePreview(intent, targetRef, sha, tree, bundle, image)
}

// appendSSHComposeGatePreview adds fixed bounded metadata and never upgrades an incomplete preview.
// appendSSHComposeGatePreview добавляет fixed bounded metadata, не повышая неполный preview до полного.
func appendSSHComposeGatePreview(intent *entity.IntegrationIntent, targetRef, sha, tree, bundle, image string) error {
	if intent == nil || intent.Operation != "ssh.compose.deploy" || intent.EffectPreview == nil {
		return errs.ErrUnavailable
	}
	target, err := sshcompose.TargetFromResourceScope(intent.ResourceScope)
	if err != nil || target.Ref != targetRef || target.RuntimeImage != image || !sshComposeCommitPattern.MatchString(sha) ||
		!sshComposeCommitPattern.MatchString(tree) || !validRuntimeSecretSHA256(bundle) {
		return errs.ErrUnavailable
	}
	fields, ok := intent.EffectPreview["fields"].([]any)
	if !ok || len(fields) > 60 {
		return errs.ErrUnavailable
	}
	for _, value := range []struct{ key, text string }{{"merged_sha", sha}, {"tree_sha", tree}, {"bundle_sha256", bundle}, {"runtime_image", image}} {
		fields = append(fields, map[string]any{"key": value.key, "type": "STRING", "value": value.text, "bytes": len(value.text), "opaque": false, "truncated": false})
	}
	intent.EffectPreview["fields"] = fields
	return nil
}
