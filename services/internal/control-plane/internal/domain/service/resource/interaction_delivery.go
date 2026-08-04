package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

type runtimeResultArtifact struct {
	ID, SHA256, Name, MediaType string
	Version                     uint64
	Payload                     []byte
}

func validateRuntimeOutputs(outputs []RuntimeOutput) error {
	if len(outputs) == 0 || len(outputs) > 32 || outputs[0].Kind != "FINAL_MARKDOWN" ||
		outputs[0].Sequence != 1 {
		return errs.ErrInvalidInput
	}
	seen := make(map[string]struct{}, len(outputs))
	kindTotals := make(map[string]uint32, 3)
	kindCounts := make(map[string]uint32, 3)
	totalBytes := 0
	for _, output := range outputs {
		digest := sha256.Sum256(output.ArtifactPayload)
		if output.Kind != "FINAL_MARKDOWN" && output.Kind != "FILE" && output.Kind != "IMAGE" {
			return errs.ErrInvalidInput
		}
		if value.ValidateID(output.ArtifactID) != nil || output.ArtifactVersion != 1 ||
			output.ArtifactSHA256 != hex.EncodeToString(digest[:]) || output.ArtifactName == "" ||
			len(output.ArtifactName) > 255 || strings.ContainsAny(output.ArtifactName, "/\\\x00\r\n") ||
			output.ArtifactMediaType == "" || len(output.ArtifactMediaType) > 255 ||
			len(output.ArtifactPayload) == 0 || len(output.ArtifactPayload) > 512<<10 ||
			output.Sequence == 0 || output.Total == 0 || output.Sequence > output.Total {
			return errs.ErrInvalidInput
		}
		if output.Kind == "FINAL_MARKDOWN" && (output.ArtifactMediaType != "text/markdown" ||
			len(output.ArtifactPayload) > 60<<10 || !utf8.Valid(output.ArtifactPayload)) {
			return errs.ErrInvalidInput
		}
		if output.Kind == "IMAGE" && !strings.HasPrefix(output.ArtifactMediaType, "image/") {
			return errs.ErrInvalidInput
		}
		key := output.Kind + ":" + strconv.FormatUint(uint64(output.Sequence), 10)
		if _, duplicate := seen[key]; duplicate {
			return errs.ErrInvalidInput
		}
		seen[key] = struct{}{}
		if kindTotals[output.Kind] != 0 && kindTotals[output.Kind] != output.Total {
			return errs.ErrInvalidInput
		}
		kindTotals[output.Kind], kindCounts[output.Kind] = output.Total, kindCounts[output.Kind]+1
		totalBytes += len(output.ArtifactPayload)
	}
	for kind, total := range kindTotals {
		if kindCounts[kind] != total {
			return errs.ErrInvalidInput
		}
	}
	if totalBytes > 512<<10 {
		return errs.ErrInvalidInput
	}
	return nil
}

func (service *Service) materializeRuntimeOutputs(ctx context.Context, tx domainrepo.Transaction,
	principal value.Principal, execution RuntimeExecution, session, turn entity.Resource, spec entity.TurnSpec,
	outputs []RuntimeOutput, now time.Time) error {
	primary := outputs[0]
	if err := service.enqueueInteractionTerminalDelivery(ctx, tx, session, turn, spec,
		&runtimeResultArtifact{ID: primary.ArtifactID, Version: primary.ArtifactVersion,
			SHA256: primary.ArtifactSHA256, Name: primary.ArtifactName,
			MediaType: primary.ArtifactMediaType, Payload: slices.Clone(primary.ArtifactPayload)}); err != nil {
		return err
	}
	for _, output := range outputs[1:] {
		evidence := sha256.Sum256([]byte(strings.Join([]string{execution.ID, execution.TurnID,
			output.ArtifactSHA256, execution.ImmutableInputSHA256}, "\x00")))
		artifact, err := entity.New(output.ArtifactID, principal.OrganizationID, principal.ProjectID,
			execution.SessionID, principal.ActorID, enum.KindArtifact, output.ArtifactName,
			entity.ArtifactSpec{ArtifactKind: "runtime-output", Direction: "OUTPUT",
				StorageRef: "control-plane-inline:" + output.ArtifactID, SizeBytes: uint64(len(output.ArtifactPayload)),
				MediaType: output.ArtifactMediaType, SHA256: output.ArtifactSHA256, ScanStatus: "CLEAN",
				RetentionPolicyRef: "policy://runtime-result", ScanPolicyRevision: 1,
				ScanEvidenceSHA256: hex.EncodeToString(evidence[:]), ScannerWorkloadID: "agent-runner", ScannedAt: now}, now)
		if err != nil {
			return errs.ErrStateConflict
		}
		if err := tx.Insert(ctx, artifact); err != nil {
			return err
		}
		if err := service.appendMutationRecords(ctx, tx, principal, "materialize_runtime_output", artifact); err != nil {
			return err
		}
		kinds := []string{"PUBLISH_ARTIFACT"}
		if output.Kind == "FINAL_MARKDOWN" {
			kinds = []string{"FINAL_MARKDOWN"}
		}
		for _, kind := range kinds {
			work := domainrepo.InteractionDeliveryWork{ID: uuid.NewSHA1(uuid.NameSpaceURL,
				[]byte("control-plane:interaction-delivery:"+turn.ID+":"+strconv.FormatUint(turn.Version, 10)+
					":"+kind+":"+output.ArtifactID)).String(), OrganizationID: turn.OrganizationID,
				ProjectID: turn.ProjectID, ActorID: turn.OwnerActorID, SessionID: session.ID,
				SessionVersion: session.Version, TurnID: turn.ID, TurnVersion: turn.Version, Attempt: spec.Attempt,
				RuntimeRevisionID: execution.RuntimeRevisionID, RuntimeRevisionVersion: execution.RuntimeRevisionVersion,
				ImmutableInputSHA256: execution.ImmutableInputSHA256, Kind: kind,
				LifecycleState: string(turn.State), Outcome: spec.Outcome, ArtifactID: output.ArtifactID,
				ArtifactVersion: output.ArtifactVersion, ArtifactSHA256: output.ArtifactSHA256,
				ArtifactName: output.ArtifactName, ArtifactStorageRef: "control-plane-inline:" + output.ArtifactID,
				ArtifactSizeBytes: uint64(len(output.ArtifactPayload)), ArtifactMediaType: output.ArtifactMediaType,
				InlinePayload: slices.Clone(output.ArtifactPayload)}
			if err := tx.EnqueueInteractionDelivery(ctx, work); err != nil {
				return err
			}
		}
	}
	return nil
}

func inlinePayload(artifact *runtimeResultArtifact) []byte {
	if artifact == nil {
		return nil
	}
	return artifact.Payload
}

func (service *Service) enqueueInteractionStateDeliveries(ctx context.Context, tx domainrepo.Transaction,
	session, turn entity.Resource, spec entity.TurnSpec, identity string, kinds ...string) error {
	revision, err := tx.Get(ctx, turn.OrganizationID, turn.ProjectID, spec.RuntimeRevisionID)
	if err != nil || revision.Kind != enum.KindRuntimeRevision {
		if err != nil {
			return err
		}
		return errs.ErrStateConflict
	}
	for _, kind := range kinds {
		work := domainrepo.InteractionDeliveryWork{ID: uuid.NewSHA1(uuid.NameSpaceURL,
			[]byte("control-plane:interaction-delivery:"+turn.ID+":"+identity+":"+kind)).String(),
			OrganizationID: turn.OrganizationID, ProjectID: turn.ProjectID, ActorID: turn.OwnerActorID,
			SessionID: session.ID, SessionVersion: session.Version, TurnID: turn.ID, TurnVersion: turn.Version,
			Attempt: spec.Attempt, RuntimeRevisionID: revision.ID, RuntimeRevisionVersion: revision.Version,
			ImmutableInputSHA256: spec.EffectiveInputSHA256, Kind: kind,
			LifecycleState: string(turn.State), Outcome: spec.Outcome}
		if err := tx.EnqueueInteractionDelivery(ctx, work); err != nil {
			return err
		}
	}
	return nil
}

// enqueueInteractionTerminalDelivery материализует owner-produced работу в той
// же транзакции, которая сделала Turn terminal. Gateway не реконструирует
// business state и получает только точный immutable lineage.
func (service *Service) enqueueInteractionTerminalDelivery(
	ctx context.Context,
	tx domainrepo.Transaction,
	session entity.Resource,
	turn entity.Resource,
	spec entity.TurnSpec,
	inline *runtimeResultArtifact,
) error {
	revision, err := tx.Get(ctx, turn.OrganizationID, turn.ProjectID, spec.RuntimeRevisionID)
	if err != nil || revision.Kind != enum.KindRuntimeRevision {
		if err != nil {
			return err
		}
		return errs.ErrStateConflict
	}
	kinds := []string{"STATUS", "RUN_CARD"}
	var artifact entity.Resource
	var artifactSpec entity.ArtifactSpec
	if turn.State == enum.StateFailed {
		kinds = append(kinds, "INCIDENT")
	}
	if spec.ResultArtifactID != "" {
		if inline != nil && inline.ID == spec.ResultArtifactID {
			digest := sha256.Sum256(inline.Payload)
			if inline.Version != spec.ResultArtifactVersion || inline.SHA256 != spec.ResultArtifactSHA256 ||
				hex.EncodeToString(digest[:]) != inline.SHA256 || inline.MediaType != "text/markdown" {
				return errs.ErrStateConflict
			}
			artifact = entity.Resource{ID: inline.ID, Name: inline.Name, Version: inline.Version}
			artifactSpec = entity.ArtifactSpec{SHA256: inline.SHA256, StorageRef: "control-plane-inline:" + inline.ID,
				SizeBytes: uint64(len(inline.Payload)), MediaType: inline.MediaType}
		} else {
			artifact, err = tx.Get(ctx, turn.OrganizationID, turn.ProjectID, spec.ResultArtifactID)
			if err != nil {
				return err
			}
			var ok bool
			artifactSpec, ok = artifact.Spec.(entity.ArtifactSpec)
			if !ok || artifact.Version != spec.ResultArtifactVersion || artifactSpec.SHA256 != spec.ResultArtifactSHA256 {
				return errs.ErrStateConflict
			}
		}
		kinds = append(kinds, "FINAL_MARKDOWN", "PUBLISH_ARTIFACT")
	}
	for _, kind := range kinds {
		work := domainrepo.InteractionDeliveryWork{
			ID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("control-plane:interaction-delivery:"+turn.ID+":"+
				strconv.FormatUint(turn.Version, 10)+":"+kind)).String(),
			OrganizationID: turn.OrganizationID, ProjectID: turn.ProjectID, ActorID: turn.OwnerActorID,
			SessionID: session.ID, SessionVersion: session.Version, TurnID: turn.ID, TurnVersion: turn.Version,
			Attempt: spec.Attempt, RuntimeRevisionID: revision.ID, RuntimeRevisionVersion: revision.Version,
			ImmutableInputSHA256: spec.EffectiveInputSHA256, Kind: kind,
			LifecycleState: string(turn.State), Outcome: spec.Outcome,
		}
		if kind == "FINAL_MARKDOWN" || kind == "PUBLISH_ARTIFACT" {
			work.ArtifactID, work.ArtifactVersion, work.ArtifactSHA256 = spec.ResultArtifactID,
				spec.ResultArtifactVersion, spec.ResultArtifactSHA256
			work.ArtifactName, work.ArtifactStorageRef = artifact.Name, artifactSpec.StorageRef
			work.ArtifactSizeBytes, work.ArtifactMediaType = artifactSpec.SizeBytes, artifactSpec.MediaType
			work.InlinePayload = slices.Clone(inlinePayload(inline))
		}
		if err := tx.EnqueueInteractionDelivery(ctx, work); err != nil {
			return err
		}
	}
	return nil
}
