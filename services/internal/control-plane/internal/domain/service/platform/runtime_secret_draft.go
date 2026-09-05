package platform

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (service *Service) GetRuntimeSecretDraft(ctx context.Context, p value.Principal, ref string) (entity.RuntimeSecretDraft, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecretDraft{}, err
	}
	if !runtimeDiffReference.MatchString(ref) {
		return entity.RuntimeSecretDraft{}, errs.ErrInvalid
	}
	return service.repository.GetRuntimeSecretDraft(ctx, p, ref)
}

func (service *Service) PrepareRuntimeSecretDraft(ctx context.Context, p value.Principal, input repository.RuntimeSecretDraftPrepareInput) (entity.RuntimeSecretDraftOperationReceipt, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecretDraftOperationReceipt{}, err
	}
	if p.CallerWorkload != "control-api-gateway" {
		return entity.RuntimeSecretDraftOperationReceipt{}, errs.ErrForbidden
	}
	if !p.InteractiveAuthenticationIsFresh(time.Now().UTC(), 5*time.Minute) {
		return entity.RuntimeSecretDraftOperationReceipt{}, errs.ErrFreshAuthenticationRequired
	}
	switch input.Kind {
	case "SAVE", "VALIDATE", "PUBLISH", "DISCARD":
	default:
		return entity.RuntimeSecretDraftOperationReceipt{}, errs.ErrInvalid
	}
	input.Mutation.Operation = "runtime-secret-draft." + strings.ToLower(input.Kind)
	input.Mutation.IntentDigest = ""
	input.Mutation.IntentDigest = digest(input)
	if input.Mutation.Validate() != nil {
		return entity.RuntimeSecretDraftOperationReceipt{}, errs.ErrInvalid
	}
	if input.Kind == "SAVE" {
		if len(input.ExpectedContentSHA256) != 64 || input.ValueType != "STRING" && input.ValueType != "BINARY" && input.ValueType != "JSON" {
			return entity.RuntimeSecretDraftOperationReceipt{}, errs.ErrInvalid
		}
		if input.SecretRef == "" {
			if !strings.HasPrefix(input.ProjectRef, "prj_") || !runtimeDiffReference.MatchString(input.ProjectRef) || strings.TrimSpace(input.Name) != input.Name || utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 120 || len(input.Description) > 1000 || input.Mutation.ExpectedVersion != nil {
				return entity.RuntimeSecretDraftOperationReceipt{}, errs.ErrInvalid
			}
		} else if !strings.HasPrefix(input.SecretRef, "sec_") || !runtimeDiffReference.MatchString(input.SecretRef) || input.Mutation.ExpectedVersion == nil || input.ProjectRef != "" || input.Name != "" || input.Description != "" {
			return entity.RuntimeSecretDraftOperationReceipt{}, errs.ErrInvalid
		}
	} else if !runtimeDiffReference.MatchString(input.DraftRef) || input.Mutation.ExpectedVersion == nil || input.Kind == "PUBLISH" && input.ExpectedSecretVersion < 1 {
		return entity.RuntimeSecretDraftOperationReceipt{}, errs.ErrInvalid
	}
	return service.repository.PrepareRuntimeSecretDraft(ctx, p, input)
}

func (service *Service) ConsumeRuntimeSecretDraft(ctx context.Context, p value.Principal, input repository.RuntimeSecretDraftWorkInput) (entity.RuntimeSecretDraftWork, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecretDraftWork{}, err
	}
	return service.repository.ConsumeRuntimeSecretDraft(ctx, p, input)
}
func (service *Service) FinishRuntimeSecretDraft(ctx context.Context, p value.Principal, input repository.RuntimeSecretDraftWorkInput) (entity.RuntimeSecretDraftResult, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecretDraftResult{}, err
	}
	return service.repository.FinishRuntimeSecretDraft(ctx, p, input)
}
func (service *Service) ListRuntimeSecretDraftRecovery(ctx context.Context, p value.Principal, page query.Page) ([]entity.RuntimeSecretDraftWork, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuntimeSecretDraftRecovery(ctx, p, page)
}
func (service *Service) CheckRuntimeSecretDraftWork(ctx context.Context, p value.Principal) error {
	p, err := service.principal(ctx, p)
	if err != nil {
		return err
	}
	return service.repository.CheckRuntimeSecretDraftWork(ctx, p)
}
