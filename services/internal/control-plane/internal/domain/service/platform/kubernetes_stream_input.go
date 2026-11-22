package platform

import (
	"bytes"
	"context"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesStreamInputOwner requires the existing authenticated public gateway operation; an absent owner repository stays unavailable rather than inventing an in-memory input queue.
// kubernetesStreamInputOwner требует existing authenticated public gateway operation; без owner repository путь недоступен, in-memory input queue не создаётся.
func (service *Service) kubernetesStreamInputOwner(ctx context.Context, p value.Principal, permission string) (platformrepo.KubernetesStreamInputRepository, value.Principal, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, p, err
	}
	if p.CallerWorkload != "control-api-gateway" || p.Permission != permission {
		return nil, p, errs.ErrForbidden
	}
	repository, available := service.repository.(platformrepo.KubernetesStreamInputRepository)
	if !available {
		return nil, p, errs.ErrUnavailable
	}
	return repository, p, nil
}

// ListKubernetesStreamInputs delegates every bounded exact-node page to the common current original-root visibility rule; submissions additionally require live execution authority at the owner.
// ListKubernetesStreamInputs передаёт каждую bounded exact-node page общему current original-root visibility rule; для submissions owner дополнительно требует live execution authority.
func (service *Service) ListKubernetesStreamInputs(ctx context.Context, p value.Principal, input query.KubernetesStreamInputList) (entity.KubernetesStreamInputPage, error) {
	repository, p, err := service.kubernetesStreamInputOwner(ctx, p, "platform.integrations.kubernetes.input.list")
	if err != nil {
		return entity.KubernetesStreamInputPage{}, err
	}
	if !validKubernetesSetupRef(input.RunRef, "run_") || !validKubernetesSetupRef(input.NodeRef, "nod_") || input.Page.Size < 0 || input.Page.Size > 50 || len(input.Page.Token) > 2048 || !utf8.ValidString(input.Page.Token) || strings.ContainsAny(input.Page.Token, "\x00\r\n") {
		return entity.KubernetesStreamInputPage{}, errs.ErrInvalid
	}
	return repository.ListKubernetesStreamInputs(ctx, p, input)
}

// GetKubernetesStreamInput reads exact metadata or one original submission key without recovering payload, current execution credit or an arbitrary first child.
// GetKubernetesStreamInput читает exact metadata либо original submission key без восстановления payload, current execution credit либо выбора произвольного первого child.
func (service *Service) GetKubernetesStreamInput(ctx context.Context, p value.Principal, input query.KubernetesStreamInputRead) (entity.KubernetesStreamInputReadback, error) {
	repository, p, err := service.kubernetesStreamInputOwner(ctx, p, "platform.integrations.kubernetes.input.get")
	if err != nil {
		return entity.KubernetesStreamInputReadback{}, err
	}
	if !validKubernetesSetupRef(input.InvocationRef, "inv_") || !validKubernetesSetupRef(input.StreamRef, "kst_") || len(input.StreamRef) > 96 || input.SubmissionIdempotencyKey != "" && !validKubernetesSetupIdempotency(input.SubmissionIdempotencyKey) {
		return entity.KubernetesStreamInputReadback{}, errs.ErrInvalid
	}
	return repository.GetKubernetesStreamInput(ctx, p, input)
}

// SubmitKubernetesStreamInput forwards bounded validated bytes once under exact owner OCC; sequence, source eligibility and private storage remain owner assigned.
// The temporary owned copy is cleared after the synchronous owner operation, including errors; no retry or detached payload retention is introduced.
// SubmitKubernetesStreamInput передаёт bounded validated bytes один раз под exact owner OCC; sequence, source eligibility и private storage назначает owner.
// Temporary owned copy очищается после synchronous owner operation, включая ошибки; retry либо detached payload retention не создаются.
func (service *Service) SubmitKubernetesStreamInput(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesStreamInputSubmit) (entity.KubernetesStreamInputReadback, error) {
	repository, p, err := service.kubernetesStreamInputOwner(ctx, p, "platform.integrations.kubernetes.input.submit")
	if err != nil {
		return entity.KubernetesStreamInputReadback{}, err
	}
	if !validKubernetesSetupRef(input.InvocationRef, "inv_") || !validKubernetesSetupRef(input.StreamRef, "kst_") || len(input.StreamRef) > 96 || !validKubernetesSetupMutation(mutation, false) || !input.ValidPayload() {
		return entity.KubernetesStreamInputReadback{}, errs.ErrInvalid
	}
	input.Payload = bytes.Clone(input.Payload)
	defer clear(input.Payload)
	return repository.SubmitKubernetesStreamInput(ctx, p, mutation, input)
}

// StopKubernetesStream forwards closure intent for one existing tool graph; actor/root visibility remains mandatory while expired execution credentials cannot block safe closure.
// StopKubernetesStream передаёт closure intent одного existing tool graph; actor/root visibility обязательна, но expired execution credentials не блокируют безопасное закрытие.
func (service *Service) StopKubernetesStream(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesStreamInputStop) (entity.KubernetesStreamInputView, error) {
	repository, p, err := service.kubernetesStreamInputOwner(ctx, p, "platform.integrations.kubernetes.input.stop")
	if err != nil {
		return entity.KubernetesStreamInputView{}, err
	}
	if !validKubernetesSetupRef(input.InvocationRef, "inv_") || !validKubernetesSetupRef(input.StreamRef, "kst_") || len(input.StreamRef) > 96 || !validKubernetesSetupMutation(mutation, false) {
		return entity.KubernetesStreamInputView{}, errs.ErrInvalid
	}
	return repository.StopKubernetesStream(ctx, p, mutation, input)
}
