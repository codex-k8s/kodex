package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesToolOwner requires the registered public gateway operation without impersonating a private worker.
// kubernetesToolOwner требует зарегистрированную операцию публичного gateway без подмены приватного worker.
func (service *Service) kubernetesToolOwner(ctx context.Context, p value.Principal, permission string) (port.KubernetesToolRepository, value.Principal, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, p, err
	}
	if p.CallerWorkload != "control-api-gateway" || p.Permission != permission {
		return nil, p, errs.ErrForbidden
	}
	repository, ok := service.repository.(port.KubernetesToolRepository)
	if !ok {
		return nil, p, errs.ErrUnavailable
	}
	return repository, p, nil
}

// ListKubernetesTools validates only locators and delegates authority for each page to the owner.
// ListKubernetesTools проверяет форму ссылок и передаёт авторизацию каждой страницы владельцу.
func (service *Service) ListKubernetesTools(ctx context.Context, p value.Principal, input query.KubernetesToolList) (entity.KubernetesToolPage, error) {
	repository, p, err := service.kubernetesToolOwner(ctx, p, "platform.integrations.kubernetes.tool.list")
	if err != nil {
		return entity.KubernetesToolPage{}, err
	}
	if !validKubernetesSetupRef(input.RunRef, "run_") || !validKubernetesSetupRef(input.NodeRef, "nod_") || input.Page.Size < 0 || input.Page.Size > 50 || input.Page.Token != "" && !validKubernetesSetupRef(input.Page.Token, "inv_") {
		return entity.KubernetesToolPage{}, errs.ErrInvalid
	}
	return repository.ListKubernetesTools(ctx, p, input)
}

// GetKubernetesTool reads the current owner view without requiring an existing stream or execution credential.
// GetKubernetesTool читает состояние владельца без требования существующего потока или ключа исполнения.
func (service *Service) GetKubernetesTool(ctx context.Context, p value.Principal, inv string) (entity.KubernetesToolView, error) {
	repository, p, err := service.kubernetesToolOwner(ctx, p, "platform.integrations.kubernetes.tool.get")
	if err != nil {
		return entity.KubernetesToolView{}, err
	}
	if !validKubernetesSetupRef(inv, "inv_") {
		return entity.KubernetesToolView{}, errs.ErrInvalid
	}
	return repository.GetKubernetesTool(ctx, p, inv)
}

// StopKubernetesTool requests closure of one invocation; it grants no approval and never cancels the whole run.
// StopKubernetesTool запрашивает закрытие одного вызова, не выдаёт approval и не отменяет весь запуск.
func (service *Service) StopKubernetesTool(ctx context.Context, p value.Principal, mutation value.Mutation, inv string) (entity.KubernetesToolView, error) {
	repository, p, err := service.kubernetesToolOwner(ctx, p, "platform.integrations.kubernetes.tool.stop")
	if err != nil {
		return entity.KubernetesToolView{}, err
	}
	if !validKubernetesSetupRef(inv, "inv_") || !validKubernetesSetupMutation(mutation, false) {
		return entity.KubernetesToolView{}, errs.ErrInvalid
	}
	return repository.StopKubernetesTool(ctx, p, mutation, inv)
}
