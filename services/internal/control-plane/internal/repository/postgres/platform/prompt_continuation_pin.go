package platform

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/prompt_continuation_pin_turn.sql
var queryPromptContinuationPinTurn string

//go:embed sql/prompt_continuation_claim_pin.sql
var queryPromptContinuationClaimPin string

func (repository *Repository) checkClaimContinuationPinTx(ctx context.Context, tx pgx.Tx, current scope, nodeRef string) error {
	var expected, task, attachmentRef, sessionRef string
	err := tx.QueryRow(ctx, queryPromptContinuationClaimPin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "node_ref": nodeRef}).Scan(
		&expected, &task, &attachmentRef, &sessionRef, &current.actorID, &current.actorRef, &current.actorName, &current.organizationRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if expected == "" {
		return nil
	}
	if current.actorID == "" {
		return errs.ErrForbidden
	}
	snapshot, err := repository.promptContinuationPreviewTx(ctx, tx, current, sessionRef, query.PromptPreviewContext{Task: task, AttachmentSetRef: attachmentRef})
	if err != nil {
		return err
	}
	if snapshot.ContextPin.DependencyDigest != expected {
		return errs.ErrVersionMismatch
	}
	return nil
}

func (repository *Repository) checkContinuationPreviewPinTx(ctx context.Context, tx pgx.Tx, current scope, payload command.SessionTurnInput) (entity.PromptContextPin, error) {
	if payload.ExpectedPromptContextDigest == "" {
		return entity.PromptContextPin{}, nil
	}
	decoded, err := hex.DecodeString(payload.ExpectedPromptContextDigest)
	if err != nil || len(decoded) != 32 || strings.ToLower(payload.ExpectedPromptContextDigest) != payload.ExpectedPromptContextDigest {
		return entity.PromptContextPin{}, errs.ErrInvalid
	}
	snapshot, err := repository.promptContinuationPreviewTx(ctx, tx, current, payload.SessionRef, query.PromptPreviewContext{Task: payload.Task, AttachmentSetRef: payload.AttachmentSetRef})
	if err != nil {
		return entity.PromptContextPin{}, err
	}
	if snapshot.ContextPin.Digest != payload.ExpectedPromptContextDigest {
		return entity.PromptContextPin{}, errs.ErrVersionMismatch
	}
	return snapshot.ContextPin, nil
}
