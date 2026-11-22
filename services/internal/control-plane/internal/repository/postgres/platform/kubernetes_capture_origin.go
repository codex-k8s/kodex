package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesCaptureOrigin resolves one exact adopted revision's safe provenance.
// queryKubernetesCaptureOrigin разрешает safe provenance одной exact adopted revision.
//
//go:embed sql/kubernetes_capture__origin.sql
var queryKubernetesCaptureOrigin string

// readKubernetesCaptureOrigin refuses missing or mismatched provenance of a captured head rather
// than presenting it as an ordinary complete file. Ordinary artifacts return nil without authority.
// Caller eligibility remains mandatory before this representation projection.
// readKubernetesCaptureOrigin отклоняет missing либо mismatched provenance captured head вместо
// выдачи как ordinary complete file. Ordinary artifacts возвращают nil без выдачи authority.
// Caller eligibility обязательна до representation projection.
func readKubernetesCaptureOrigin(ctx context.Context, runner queryRunner, current scope, artifactRef, revisionRef string) (*entity.KubernetesArtifactCaptureOrigin, error) {
	var captured bool
	var raw, digest, sourceRaw, snapshotDigest, kind string
	err := runner.QueryRow(ctx, queryKubernetesCaptureOrigin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "artifact_ref": artifactRef, "revision_ref": revisionRef}).Scan(&captured, &raw, &digest, &sourceRaw, &snapshotDigest, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	if !captured {
		if raw != "" {
			return nil, errs.ErrUnavailable
		}
		return nil, nil
	}
	var origin entity.KubernetesArtifactCaptureOrigin
	var source kubernetesCaptureSource
	sum := sha256.Sum256([]byte(sourceRaw))
	if len(raw) > 16384 || len(sourceRaw) > 16384 || hex.EncodeToString(sum[:]) != snapshotDigest || internalrpcauth.DecodeStrictJSON([]byte(raw), &origin) != nil || internalrpcauth.DecodeStrictJSON([]byte(sourceRaw), &source) != nil || !validKubernetesCaptureSource(source) || !validKubernetesCaptureOrigin(origin, source, digest, kind) {
		return nil, errs.ErrUnavailable
	}
	return &origin, nil
}

// validKubernetesCaptureOrigin compares the saved public facts with immutable private provenance.
// Source identities and scopes themselves are never copied to the public result.
// validKubernetesCaptureOrigin сравнивает saved public facts с immutable private provenance.
// Source identities и scopes не копируются в public result.
func validKubernetesCaptureOrigin(origin entity.KubernetesArtifactCaptureOrigin, source kubernetesCaptureSource, digest, kind string) bool {
	if origin.Validate() != nil || origin.InvocationRef != source.InvocationRef || origin.StreamRef != source.StreamRef || origin.StreamGeneration != source.Generation || origin.CaptureGeneration != source.CaptureGeneration || origin.Operation != source.Operation || !captureOutputChannel(origin.Channel) || origin.Kind != kind || origin.SourceDigest != digest || !managedkubernetes.ValidDigest(digest) || origin.Resource != source.Resource || origin.TargetRef != source.TargetRef || origin.TargetRevision != source.TargetRevision || origin.ConnectionRef != source.ConnectionRef || !origin.SealedAt.Equal(source.SealedAt) || origin.OwnerState != source.OwnerState || origin.NativeTerminalKnown != source.NativeTerminalKnown || origin.TransportJoined != source.TransportJoined || origin.AcknowledgedPrefix > source.AcknowledgedWatermark || origin.OutputWatermark != source.OutputWatermark || origin.DroppedBytes != source.LossBytes {
		return false
	}
	if origin.Kind != "FULL" && origin.Kind != "PARTIAL" {
		return false
	}
	if !origin.NativeTerminalKnown && (origin.LocalState != "UNKNOWN" || origin.RemoteOutcome != "UNKNOWN" || origin.ExitCode != nil || origin.ReasonCode != "") {
		return false
	}
	if source.TerminalReceipt != nil {
		receipt := source.TerminalReceipt
		if origin.LocalState != receipt.LocalState || origin.RemoteOutcome != receipt.RemoteOutcome || origin.ReasonCode != receipt.ReasonCode || (origin.ExitCode == nil) != (receipt.ExitCode == nil) || origin.ExitCode != nil && *origin.ExitCode != *receipt.ExitCode {
			return false
		}
	}
	if origin.Kind == "FULL" && (!origin.NativeTerminalKnown || !origin.TransportJoined || origin.LocalState != "COMPLETED" || origin.OwnerState != "COMPLETED" || origin.AcknowledgedPrefix != origin.OutputWatermark || origin.DroppedBytes != 0) {
		return false
	}
	return true
}
