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

// GetRuntimeSecretDraft resolves the principal and checks the reference's runtimeDiffReference shape before repository draft lookup; syntax matching is not ownership verification.
// GetRuntimeSecretDraft разрешает субъекта и проверяет форму ссылки через runtimeDiffReference до чтения черновика в репозитории; совпадение синтаксиса не проверяет владение.
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

// PrepareRuntimeSecretDraft fixes ScopeKind to PROJECT before entering the shared preparation guards; the caller cannot select organization scope through this entry point.
// PrepareRuntimeSecretDraft фиксирует ScopeKind как PROJECT перед общей подготовкой; вызывающий не может выбрать организационную область через эту точку входа.
func (service *Service) PrepareRuntimeSecretDraft(ctx context.Context, p value.Principal, input repository.RuntimeSecretDraftPrepareInput) (entity.RuntimeSecretDraftOperationReceipt, error) {
	input.ScopeKind = "PROJECT"
	return service.prepareRuntimeSecretDraft(ctx, p, input)
}

// PrepareOrganizationRuntimeSecretDraft permits only SAVE of a new organization draft with empty secret, project and draft references, then fixes organization scope before shared preparation. A supplied project locator cannot redirect this specialized command.
// PrepareOrganizationRuntimeSecretDraft допускает только SAVE нового организационного черновика с пустыми ссылками секрета, проекта и черновика, затем фиксирует организационную область до общей подготовки. Переданный адрес проекта не может перенаправить специализированную команду.
func (service *Service) PrepareOrganizationRuntimeSecretDraft(ctx context.Context, p value.Principal, input repository.RuntimeSecretDraftPrepareInput) (entity.RuntimeSecretDraftOperationReceipt, error) {
	if input.Kind != "SAVE" || input.SecretRef != "" || input.ProjectRef != "" || input.DraftRef != "" {
		return entity.RuntimeSecretDraftOperationReceipt{}, errs.ErrInvalid
	}
	input.ScopeKind = "ORGANIZATION"
	return service.prepareRuntimeSecretDraft(ctx, p, input)
}

// prepareRuntimeSecretDraft resolves the principal, requires the gateway and five-minute interactive freshness, validates the closed action and mutation intent, then applies action-specific create/update/draft/publication preconditions. SAVE checks digest length rather than recomputing content; publication requires an impact-plan reference and at most one thousand selected items before owner preparation.
// prepareRuntimeSecretDraft разрешает субъекта, требует шлюз и интерактивную аутентификацию не старше пяти минут, проверяет закрытое действие и намерение изменения, затем предусловия создания, обновления, черновика и публикации для действия. SAVE проверяет длину дайджеста без пересчёта содержимого; публикация требует ссылку плана влияния и не более тысячи выбранных элементов до подготовки владельцем.
func (service *Service) prepareRuntimeSecretDraft(ctx context.Context, p value.Principal, input repository.RuntimeSecretDraftPrepareInput) (entity.RuntimeSecretDraftOperationReceipt, error) {
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
			validScope := input.ScopeKind == "ORGANIZATION" && input.ProjectRef == "" || input.ScopeKind == "PROJECT" && strings.HasPrefix(input.ProjectRef, "prj_") && runtimeDiffReference.MatchString(input.ProjectRef)
			if !validScope || strings.TrimSpace(input.Name) != input.Name || utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 120 || len(input.Description) > 1000 || input.Mutation.ExpectedVersion != nil {
				return entity.RuntimeSecretDraftOperationReceipt{}, errs.ErrInvalid
			}
		} else if !strings.HasPrefix(input.SecretRef, "sec_") || !runtimeDiffReference.MatchString(input.SecretRef) || input.Mutation.ExpectedVersion == nil || input.ProjectRef != "" || input.Name != "" || input.Description != "" {
			return entity.RuntimeSecretDraftOperationReceipt{}, errs.ErrInvalid
		}
	} else if !runtimeDiffReference.MatchString(input.DraftRef) || input.Mutation.ExpectedVersion == nil || input.Kind == "PUBLISH" && input.ExpectedSecretVersion < 1 {
		return entity.RuntimeSecretDraftOperationReceipt{}, errs.ErrInvalid
	}
	if input.Kind == "PUBLISH" && (!runtimeDiffReference.MatchString(input.ImpactPlanRef) || len(input.SelectedItemRefs) > 1000) {
		return entity.RuntimeSecretDraftOperationReceipt{}, errs.ErrInvalid
	}
	return service.repository.PrepareRuntimeSecretDraft(ctx, p, input)
}

// ConsumeRuntimeSecretDraft resolves the principal and delegates consumption of the exact draft work input to the owner repository; this wrapper does not decrypt or materialize the draft itself.
// ConsumeRuntimeSecretDraft разрешает субъекта и передаёт потребление точного входа работы черновика владельцу в репозитории; сама обёртка не расшифровывает и не материализует черновик.
func (service *Service) ConsumeRuntimeSecretDraft(ctx context.Context, p value.Principal, input repository.RuntimeSecretDraftWorkInput) (entity.RuntimeSecretDraftWork, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecretDraftWork{}, err
	}
	return service.repository.ConsumeRuntimeSecretDraft(ctx, p, input)
}

// FinishRuntimeSecretDraft resolves the principal and submits the worker's completion/failure input to repository reconciliation rather than treating its payload as an accepted publication.
// FinishRuntimeSecretDraft разрешает субъекта и передаёт вход завершения или ошибки исполнителя на сопоставление репозиторию, не считая payload принятой публикацией.
func (service *Service) FinishRuntimeSecretDraft(ctx context.Context, p value.Principal, input repository.RuntimeSecretDraftWorkInput) (entity.RuntimeSecretDraftResult, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecretDraftResult{}, err
	}
	return service.repository.FinishRuntimeSecretDraft(ctx, p, input)
}

// ListRuntimeSecretDraftRecovery resolves the principal and pages recovery work from the repository without claiming, retrying or completing it here.
// ListRuntimeSecretDraftRecovery разрешает субъекта и читает страницы работы восстановления из репозитория без захвата, повтора или завершения здесь.
func (service *Service) ListRuntimeSecretDraftRecovery(ctx context.Context, p value.Principal, page query.Page) ([]entity.RuntimeSecretDraftWork, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuntimeSecretDraftRecovery(ctx, p, page)
}

// CheckRuntimeSecretDraftWork resolves the principal before invoking the repository's draft-work check; it is not the global service readiness path or a materializer call.
// CheckRuntimeSecretDraftWork разрешает субъекта перед проверкой работы черновиков в репозитории; это не глобальный путь готовности сервиса и не вызов материализатора.
func (service *Service) CheckRuntimeSecretDraftWork(ctx context.Context, p value.Principal) error {
	p, err := service.principal(ctx, p)
	if err != nil {
		return err
	}
	return service.repository.CheckRuntimeSecretDraftWork(ctx, p)
}

// PrepareRuntimeSecretDraftImpact resolves the principal, requires gateway authority and fresh interactive authentication, and binds the impact intent to reference and expected version. Reference shape, version presence and mutation validation precede repository preparation; no draft is published here.
// PrepareRuntimeSecretDraftImpact разрешает субъекта, требует полномочия шлюза и свежую интерактивную аутентификацию и связывает намерение влияния со ссылкой и ожидаемой версией. Форма ссылки, наличие версии и проверка изменения предшествуют подготовке в репозитории; черновик здесь не публикуется.
func (service *Service) PrepareRuntimeSecretDraftImpact(ctx context.Context, p value.Principal, ref string, mutation value.Mutation) (entity.RuntimeSecretDraftImpactPlan, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecretDraftImpactPlan{}, err
	}
	if p.CallerWorkload != "control-api-gateway" {
		return entity.RuntimeSecretDraftImpactPlan{}, errs.ErrForbidden
	}
	if !p.InteractiveAuthenticationIsFresh(time.Now().UTC(), 5*time.Minute) {
		return entity.RuntimeSecretDraftImpactPlan{}, errs.ErrFreshAuthenticationRequired
	}
	mutation.Operation = "runtime-secret-draft.impact.prepare"
	mutation.IntentDigest = digest(struct {
		Ref     string
		Version *int64
	}{ref, mutation.ExpectedVersion})
	if !runtimeDiffReference.MatchString(ref) || mutation.ExpectedVersion == nil || mutation.Validate() != nil {
		return entity.RuntimeSecretDraftImpactPlan{}, errs.ErrInvalid
	}
	return service.repository.PrepareRuntimeSecretDraftImpact(ctx, p, ref, mutation)
}

// GetRuntimeSecretDraftImpact resolves the principal, validates the impact reference and requires search of at most eight hundred bytes without NUL, then passes the original search and page to repository lookup.
// GetRuntimeSecretDraftImpact разрешает субъекта, проверяет ссылку влияния и требует поиск не длиннее восьмисот байт без NUL, затем передаёт исходный поиск и страницу чтению репозитория.
func (service *Service) GetRuntimeSecretDraftImpact(ctx context.Context, p value.Principal, ref, search string, page query.Page) (entity.RuntimeSecretDraftImpactPage, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecretDraftImpactPage{}, err
	}
	if !runtimeDiffReference.MatchString(ref) || len(search) > 800 || strings.ContainsRune(search, 0) {
		return entity.RuntimeSecretDraftImpactPage{}, errs.ErrInvalid
	}
	return service.repository.GetRuntimeSecretDraftImpact(ctx, p, ref, search, page)
}
