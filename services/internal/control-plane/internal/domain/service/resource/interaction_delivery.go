package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/google/uuid"
)

type runtimeResultArtifact struct {
	ID, SHA256, Name, MediaType string
	Version                     uint64
	Payload                     []byte
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
