package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

var (
	//go:embed sql/email_receipt_insert.sql
	queryEmailReceiptInsert string
	//go:embed sql/email_receipt_update.sql
	queryEmailReceiptUpdate string
	//go:embed sql/email_receipt_authorization_ref.sql
	queryEmailReceiptAuthorizationRef string
	//go:embed sql/email_report_recovery_source.sql
	queryEmailReportRecoverySource string
	//go:embed sql/email_report_expire_source.sql
	queryEmailReportExpireSource string
)

func (repository *Repository) authorizeEmailReport(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (string, entity.EmailAuthorization, emailReceiptOwner, error) {
	var decision entity.EmailAuthorization
	var owner emailReceiptOwner
	payload, ok := input.Payload.(command.EmailEffectReportInput)
	if !ok || emailpolicy.ValidateExternalReceipt(payload.ExternalReceiptRef, payload.ExternalReceiptDigest) != nil ||
		!emailpolicy.ValidDigest(payload.SemanticInputDigest) || emailpolicy.ValidateReceiptTransition(payload.Outcome, payload.Outcome) != nil ||
		emailpolicy.ValidateExecutionBinding(payload.Binding, time.Now(), true) != nil || payload.Binding.InvocationRef == "" {
		return "", decision, owner, errs.ErrInvalid
	}
	if input.Principal.CallerWorkload != "email-bridge" || current.authorityProjectID != "" {
		return "", decision, owner, errs.ErrForbidden
	}
	var ref string
	var rawInput, rawDecision []byte
	err := tx.QueryRow(ctx, queryEmailAuthorizationGet, current.organizationID, payload.Binding.InvocationRef,
		payload.Binding.LeaseRef, payload.Binding.Generation, emailFenceDigest(payload.Binding)).Scan(&ref, &rawInput, &rawDecision)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", decision, owner, errs.ErrForbidden
	}
	if err != nil {
		return "", decision, owner, errs.ErrUnavailable
	}
	var authorizedQuery query.EmailAuthorization
	if json.Unmarshal(rawInput, &authorizedQuery) != nil || json.Unmarshal(rawDecision, &decision) != nil {
		return "", decision, owner, errs.ErrUnavailable
	}
	if decision.SemanticInputDigest != payload.SemanticInputDigest || !decision.Binding.ExpiresAt.Equal(payload.Binding.ExpiresAt) ||
		!decision.Allowed || decision.AgentRef == "" || !api.IsMutation(api.Operation(decision.Operation)) || emailpolicy.ValidateEffectKey(decision.EffectKey) != nil {
		return "", decision, owner, errs.ErrForbidden
	}
	owner, err = readEmailReceipt(ctx, tx, current, "", payload.Binding.InvocationRef)
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return "", decision, owner, err
	}
	if err == nil {
		var authorizationRef string
		if err := tx.QueryRow(ctx, queryEmailReceiptAuthorizationRef, owner.id).Scan(&authorizationRef); err != nil {
			return "", decision, owner, errs.ErrForbidden
		}
		if authorizationRef != ref || owner.receipt.ExternalReceiptRef != payload.ExternalReceiptRef ||
			owner.receipt.ExternalReceiptDigest != payload.ExternalReceiptDigest || owner.receipt.SemanticInputDigest != payload.SemanticInputDigest {
			return "", decision, owner, errs.ErrForbidden
		}
		// Exact replay уже сохранённого факта не выдаёт разрешение на новый write.
		if owner.receipt.Outcome == payload.Outcome || payload.Outcome == emailpolicy.OutcomeUnknown {
			return ref, decision, owner, nil
		}
	}
	if !payload.Binding.ExpiresAt.After(time.Now()) {
		// Просроченный source допускает только запись факта по прежней authorization.
		var state string
		if err := tx.QueryRow(ctx, queryEmailReportRecoverySource, current.organizationID, ref).Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", decision, owner, errs.ErrForbidden
			}
			return "", decision, owner, errs.ErrUnavailable
		}
		if owner.id == "" && state == "SUCCEEDED" {
			return "", decision, owner, errs.ErrConflict
		}
		return ref, decision, owner, nil
	}
	if !decision.ExpiresAt.After(time.Now()) {
		return "", decision, owner, errs.ErrForbidden
	}
	authorizedQuery.Binding.Fence = payload.Binding.Fence
	currentDecision, _, err := repository.emailAuthorization(ctx, tx, current, authorizedQuery)
	if err != nil {
		return "", decision, owner, err
	}
	currentDecision.Binding.Fence = ""
	currentDecision.ExpiresAt = decision.ExpiresAt
	actual, _ := json.Marshal(currentDecision)
	expected, _ := json.Marshal(decision)
	if !bytes.Equal(actual, expected) {
		return "", decision, owner, errs.ErrForbidden
	}
	return ref, decision, owner, nil
}

func (repository *Repository) reportEmailEffect(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	ref, authorization, owner, err := repository.authorizeEmailReport(ctx, tx, current, input)
	if err != nil {
		return commandOutcome{}, err
	}
	payload := input.Payload.(command.EmailEffectReportInput)
	if err := emailpolicy.ValidateReceiptTransition(owner.receipt.Outcome, payload.Outcome); err != nil {
		return commandOutcome{}, err
	}
	if !payload.Binding.ExpiresAt.After(time.Now()) {
		if _, err := tx.Exec(ctx, queryEmailReportExpireSource, current.organizationID, ref); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	if owner.id != "" && owner.receipt.Outcome != payload.Outcome {
		decision, err := readEmailDecision(ctx, tx, current, owner.receipt.Ref, "")
		if err != nil {
			return commandOutcome{}, err
		}
		if decision != nil && decision.Outcome != payload.Outcome {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	if owner.id == "" {
		receiptRef, err := newRef("emrc")
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryEmailReceiptInsert, receiptRef, ref, payload.ExternalReceiptRef, payload.ExternalReceiptDigest,
			payload.SemanticInputDigest, authorization.EffectKey, authorization.MailboxRef, authorization.ConfigurationRevision); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else if owner.receipt.Outcome != payload.Outcome {
		if _, err := tx.Exec(ctx, queryEmailReceiptUpdate, owner.id, owner.receipt.Version, payload.Outcome); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	}
	owner, err = readEmailReceipt(ctx, tx, current, "", payload.Binding.InvocationRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{EmailReceipt: &owner.receipt}, projectID: owner.projectID, projectRef: owner.receipt.ProjectRef,
		resourceKind: "EMAIL_EFFECT_RECEIPT", resourceRef: owner.receipt.Ref, summary: "i18n:EMAIL_EFFECT_REPORTED"}, nil
}
