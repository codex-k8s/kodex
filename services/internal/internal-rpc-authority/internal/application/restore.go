package application

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
)

type RestoreController struct {
	domain *service.RestoreController
}

func NewRestoreController(domain *service.RestoreController) *RestoreController {
	return &RestoreController{domain: domain}
}

func (application *RestoreController) Prepare(
	ctx context.Context,
	command model.PrepareRestoreCommand,
) (model.RestoreState, error) {
	return application.domain.Prepare(ctx, command)
}

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

func (application *RestoreController) Complete(
	ctx context.Context,
	command model.CompleteRestoreCommand,
) (model.RestoreState, error) {
	return application.domain.Complete(ctx, command)
}

func (application *RestoreController) Recover(ctx context.Context) error {
	return application.domain.Recover(ctx)
}

func (application *RestoreController) Ready(
	ctx context.Context,
) (model.RestoreState, error) {
	return application.domain.Ready(ctx)
}

func (application *RestoreController) RoleTrustMetadata() model.RestoreRoleTrustMetadata {
	return application.domain.RoleTrustMetadata()
}

func (application *RestoreController) SignerGeneration() uint64 {
	return application.domain.SignerGeneration()
}
