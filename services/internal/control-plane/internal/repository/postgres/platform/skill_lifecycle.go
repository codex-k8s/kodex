package platform

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) transitionSkillBundle(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.SkillBundleInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	var id, state string
	var version int64
	if err := tx.QueryRow(ctx, querySkillBundleLock, current.organizationID, payload.BundleRef).Scan(&id, &version, &state); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion != version {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if state == "PURGED" {
		return commandOutcome{}, errs.ErrConflict
	}
	bundle, err := scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, payload.BundleRef))
	if err != nil {
		return commandOutcome{}, err
	}
	draft := bundle.DraftRevision
	targetState, targetRevisionState := state, ""
	clearDraft, publish, review := false, false, false
	switch input.Kind {
	case command.ReviewSkillBundleDraft, command.PublishSkillBundleDraft, command.DiscardSkillBundleDraft:
		if state != "ACTIVE" || draft == nil || draft.Ref != payload.RevisionRef || draft.Digest != payload.ExpectedDigest {
			return commandOutcome{}, errs.ErrConflict
		}
		if input.Kind == command.DiscardSkillBundleDraft {
			targetRevisionState, clearDraft = "DISCARDED", true
		} else {
			if draft.ScanState != "CLEAN" || draft.ScanEngine == "" || draft.ScanDigest != skillScanDigest(draft.Digest, draft.ScanEngine) || draft.ScannedAt == nil || time.Since(*draft.ScannedAt) > 24*time.Hour {
				return commandOutcome{}, errs.ErrConflict
			}
			if input.Kind == command.ReviewSkillBundleDraft {
				if draft.State != "VALIDATED" || !utf8.ValidString(payload.Comment) || len([]rune(payload.Comment)) > 2000 {
					return commandOutcome{}, errs.ErrConflict
				}
				switch payload.Decision {
				case "APPROVE":
					targetRevisionState = "APPROVED"
				case "REJECT":
					targetRevisionState = "REJECTED"
				default:
					return commandOutcome{}, errs.ErrInvalid
				}
				review = true
			} else {
				if draft.State != "APPROVED" || draft.ReviewedBy == "" || draft.ReviewedAt == nil {
					return commandOutcome{}, errs.ErrConflict
				}
				for _, file := range draft.Files {
					if _, err := repository.readSkillFile(ctx, tx, current, bundle.projectID, file); err != nil {
						return commandOutcome{}, err
					}
				}
				targetRevisionState, clearDraft, publish = "PUBLISHED", true, true
			}
		}
	case command.ArchiveSkillBundle:
		if state != "ACTIVE" {
			return commandOutcome{}, errs.ErrConflict
		}
		targetState = "ARCHIVED"
		if draft != nil {
			targetRevisionState, clearDraft = "DISCARDED", true
		}
	case command.RestoreSkillBundle:
		if state != "ARCHIVED" {
			return commandOutcome{}, errs.ErrConflict
		}
		targetState = "ACTIVE"
	case command.PurgeSkillBundle:
		if state != "ARCHIVED" || draft != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		targetState = "PURGED"
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
	if targetRevisionState != "" {
		tag, err := tx.Exec(ctx, querySkillRevisionTransition, pgx.StrictNamedArgs{"organization_id": current.organizationID, "bundle_id": id, "revision_ref": draft.Ref, "digest": draft.Digest, "source_state": draft.State, "target_state": targetRevisionState, "review": review, "actor_id": current.actorID, "comment": payload.Comment})
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	tag, err := tx.Exec(ctx, querySkillBundleTransition, pgx.StrictNamedArgs{"organization_id": current.organizationID, "bundle_id": id, "version": version, "state": targetState, "publish": publish, "clear_draft": clearDraft})
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if targetState != "ACTIVE" {
		if _, err := tx.Exec(ctx, querySkillBundleDisableBindings, current.organizationID, id); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	if targetState == "PURGED" {
		if _, err := tx.Exec(ctx, querySkillBundlePurge, current.organizationID, id); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	bundle, err = scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, payload.BundleRef))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{SkillBundle: &bundle.SkillBundle}, resourceKind: "SKILL_BUNDLE", resourceRef: bundle.Ref, projectID: bundle.projectID, projectRef: bundle.ProjectRef, summary: "i18n:SKILL_BUNDLE_CHANGED"}, nil
}
