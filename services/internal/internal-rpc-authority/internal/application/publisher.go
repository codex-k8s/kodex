package application

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
)

type Publisher struct {
	service *service.Publisher
}

func NewPublisher(serviceValue *service.Publisher) *Publisher {
	return &Publisher{service: serviceValue}
}

func (application *Publisher) Publish(
	ctx context.Context,
	controller service.ControllerIdentity,
	directiveCompact string,
	idempotencyKey string,
) (model.PublishedCredential, error) {
	return application.service.Publish(
		ctx,
		controller,
		directiveCompact,
		idempotencyKey,
	)
}

func (application *Publisher) Ready(ctx context.Context) error {
	return application.service.Ready(ctx)
}

func (application *Publisher) PublishReadbackMaterials(
	ctx context.Context,
) ([]model.PublishedReadbackMaterial, error) {
	return application.service.PublishReadbackMaterials(ctx)
}

func (application *Publisher) Registry() model.DeliveryTargetRegistry {
	return application.service.Registry()
}

func (application *Publisher) SignerGeneration() uint64 {
	return application.service.SignerGeneration()
}
