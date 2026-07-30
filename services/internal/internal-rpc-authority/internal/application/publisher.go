package application

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

// Publisher предоставляет прикладные операции публикации credentials.
type Publisher struct {
	service *service.Publisher
}

// NewPublisher создаёт прикладную границу publisher.
func NewPublisher(serviceValue *service.Publisher) *Publisher {
	return &Publisher{service: serviceValue}
}

// Publish выпускает и доставляет один credential восстановления.
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

// Ready проверяет полный путь publisher.
func (application *Publisher) Ready(ctx context.Context) error {
	return application.service.Ready(ctx)
}

// PublishReadbackMaterials публикует материалы обычной проверки доставки.
func (application *Publisher) PublishReadbackMaterials(
	ctx context.Context,
) ([]model.PublishedReadbackMaterial, error) {
	return application.service.PublishReadbackMaterials(ctx)
}

// PublishAuthorityGraph публикует полный снимок и все ключевые роли.
func (application *Publisher) PublishAuthorityGraph(
	ctx context.Context,
) (model.AuthoritySnapshotPublication, error) {
	return application.service.PublishAuthorityGraph(ctx)
}

// Registry возвращает проверенный неизменяемый реестр целей.
func (application *Publisher) Registry() model.DeliveryTargetRegistry {
	return application.service.Registry()
}

// SignerGeneration возвращает обслуживаемое поколение signer.
func (application *Publisher) SignerGeneration() uint64 {
	return application.service.SignerGeneration()
}
