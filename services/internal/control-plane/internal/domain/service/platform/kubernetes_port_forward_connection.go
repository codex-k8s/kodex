package platform

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesPortForwardOwner reuses the existing authenticated public INPUT boundary and requires the same owner repository plus its connection extension.
// It never changes the principal permission or substitutes a generic connection/credential owner.
// kubernetesPortForwardOwner использует existing authenticated public INPUT boundary и требует тот же owner repository с connection extension.
// Principal permission не меняется; generic connection/credential owner не подставляется.
func (service *Service) kubernetesPortForwardOwner(ctx context.Context, p value.Principal, permission string) (platformrepo.KubernetesPortForwardConnectionRepository, value.Principal, error) {
	_, p, err := service.kubernetesStreamInputOwner(ctx, p, permission)
	if err != nil {
		return nil, p, err
	}
	repository, available := service.repository.(platformrepo.KubernetesPortForwardConnectionRepository)
	if !available {
		return nil, p, errs.ErrUnavailable
	}
	return repository, p, nil
}

// validKubernetesPortForwardStream bounds public selectors without treating their possession as original-root authority.
// validKubernetesPortForwardStream ограничивает public selectors без превращения владения строкой в original-root authority.
func validKubernetesPortForwardStream(invocation, stream string) bool {
	return validKubernetesSetupRef(invocation, "inv_") && validKubernetesSetupRef(stream, "kst_") && len(stream) <= 96
}

// OpenKubernetesPortForwardConnection delegates one explicit slot under original owner OCC/idempotency; only the repository assigns the incarnation and queues OPEN.
// OpenKubernetesPortForwardConnection передаёт explicit slot под original owner OCC/idempotency; incarnation и queued OPEN назначает лишь repository.
func (service *Service) OpenKubernetesPortForwardConnection(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesPortForwardConnectionOpen) (entity.KubernetesPortForwardConnectionReadback, error) {
	repository, p, err := service.kubernetesPortForwardOwner(ctx, p, "platform.integrations.kubernetes.port-forward.open")
	if err != nil {
		return entity.KubernetesPortForwardConnectionReadback{}, err
	}
	if !validKubernetesPortForwardStream(input.InvocationRef, input.StreamRef) || input.Slot >= managedkubernetes.MaximumForwardPorts || !validKubernetesSetupMutation(mutation, false) {
		return entity.KubernetesPortForwardConnectionReadback{}, errs.ErrInvalid
	}
	return repository.OpenKubernetesPortForwardConnection(ctx, p, mutation, input)
}

// GetKubernetesPortForwardConnection reads one exact connection or original Open receipt after a lost ACK; it never resends private bytes.
// GetKubernetesPortForwardConnection читает exact connection либо original Open receipt после lost ACK; private bytes не повторяются.
func (service *Service) GetKubernetesPortForwardConnection(ctx context.Context, p value.Principal, input query.KubernetesPortForwardConnectionRead) (entity.KubernetesPortForwardConnectionReadback, error) {
	repository, p, err := service.kubernetesPortForwardOwner(ctx, p, "platform.integrations.kubernetes.port-forward.get")
	if err != nil {
		return entity.KubernetesPortForwardConnectionReadback{}, err
	}
	if !validKubernetesPortForwardStream(input.InvocationRef, input.StreamRef) || (input.ConnectionRef == "") == (input.OpenIdempotencyKey == "") || input.ConnectionRef != "" && !managedkubernetes.ValidPortForwardConnectionRef(input.ConnectionRef) || input.OpenIdempotencyKey != "" && !validKubernetesSetupIdempotency(input.OpenIdempotencyKey) {
		return entity.KubernetesPortForwardConnectionReadback{}, errs.ErrInvalid
	}
	return repository.GetKubernetesPortForwardConnection(ctx, p, input)
}

// ListKubernetesPortForwardConnections discovers bounded current/history metadata within the exact original stream; hidden cursors cannot broaden that scope.
// ListKubernetesPortForwardConnections обнаруживает bounded current/history metadata внутри exact original stream; скрытые cursors не расширяют scope.
func (service *Service) ListKubernetesPortForwardConnections(ctx context.Context, p value.Principal, input query.KubernetesPortForwardConnectionList) (entity.KubernetesPortForwardConnectionPage, error) {
	repository, p, err := service.kubernetesPortForwardOwner(ctx, p, "platform.integrations.kubernetes.port-forward.list")
	if err != nil {
		return entity.KubernetesPortForwardConnectionPage{}, err
	}
	if !validKubernetesPortForwardStream(input.InvocationRef, input.StreamRef) || input.Page.Size < 0 || input.Page.Size > 32 || input.Page.Token != "" && !managedkubernetes.ValidPortForwardConnectionRef(input.Page.Token) {
		return entity.KubernetesPortForwardConnectionPage{}, errs.ErrInvalid
	}
	return repository.ListKubernetesPortForwardConnections(ctx, p, input)
}

// CloseKubernetesPortForwardConnection requests exact owner closure; revoked execution authority cannot authorize a new native effect and instead uses original parent Stop/cleanup.
// CloseKubernetesPortForwardConnection запрашивает exact owner closure; отозванная execution authority не разрешает новый native effect, используется original parent Stop/cleanup.
func (service *Service) CloseKubernetesPortForwardConnection(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesPortForwardConnectionClose) (entity.KubernetesPortForwardConnectionReadback, error) {
	repository, p, err := service.kubernetesPortForwardOwner(ctx, p, "platform.integrations.kubernetes.port-forward.close")
	if err != nil {
		return entity.KubernetesPortForwardConnectionReadback{}, err
	}
	if !validKubernetesPortForwardStream(input.InvocationRef, input.StreamRef) || !managedkubernetes.ValidPortForwardConnectionRef(input.ConnectionRef) || !validKubernetesSetupMutation(mutation, false) {
		return entity.KubernetesPortForwardConnectionReadback{}, errs.ErrInvalid
	}
	return repository.CloseKubernetesPortForwardConnection(ctx, p, mutation, input)
}
