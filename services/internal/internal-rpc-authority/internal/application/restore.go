package application

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

// RestoreController предоставляет прикладную координацию восстановления.
type RestoreController struct {
	domain *service.RestoreController
}

// NewRestoreController создаёт прикладную границу controller.
func NewRestoreController(domain *service.RestoreController) *RestoreController {
	return &RestoreController{domain: domain}
}

// Prepare создаёт либо повторяет pinned intent восстановления.
func (application *RestoreController) Prepare(
	ctx context.Context,
	command model.PrepareRestoreCommand,
) (model.RestoreState, error) {
	return application.domain.Prepare(ctx, command)
}

// GetDirective выдаёт точную role-bound директиву.
func (application *RestoreController) GetDirective(
	ctx context.Context,
	peer service.RestorePeer,
	roleCredential string,
	observedRevision uint64,
) (service.RestoreDirectiveResult, error) {
	return application.domain.GetDirective(
		ctx,
		peer,
		roleCredential,
		observedRevision,
	)
}

// Acknowledge проверяет и сохраняет одноразовый ACK роли.
func (application *RestoreController) Acknowledge(
	ctx context.Context,
	peer service.RestorePeer,
	roleCredential string,
	directive string,
	ack string,
	idempotencyKey string,
) (service.RestoreACKResult, error) {
	return application.domain.Acknowledge(
		ctx,
		peer,
		roleCredential,
		directive,
		ack,
		idempotencyKey,
	)
}

// Complete завершает восстановление после полного набора подтверждений.
func (application *RestoreController) Complete(
	ctx context.Context,
	command model.CompleteRestoreCommand,
) (model.RestoreState, error) {
	return application.domain.Complete(ctx, command)
}

// Recover восстанавливает координацию после сбоя процесса.
func (application *RestoreController) Recover(ctx context.Context) error {
	return application.domain.Recover(ctx)
}

// Ready возвращает проверенное устойчивое состояние координации.
func (application *RestoreController) Ready(
	ctx context.Context,
) (model.RestoreState, error) {
	return application.domain.Ready(ctx)
}

// RoleTrustMetadata возвращает обслуживаемые метаданные доверия ролей.
func (application *RestoreController) RoleTrustMetadata() model.RestoreRoleTrustMetadata {
	return application.domain.RoleTrustMetadata()
}

// SignerGeneration возвращает поколение signer controller.
func (application *RestoreController) SignerGeneration() uint64 {
	return application.domain.SignerGeneration()
}
