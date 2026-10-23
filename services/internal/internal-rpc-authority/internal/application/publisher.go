package application

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// Publisher exposes typed application entry points for the domain publisher without becoming a separate
// owner of issuance, delivery or graph readiness.
// Publisher предоставляет типизированные прикладные входы доменной публикации, не становясь отдельным
// владельцем выпуска, доставки или готовности графа.
type Publisher struct {
	service *service.Publisher
}

// NewPublisher wraps the supplied domain service without validation or external work; composition must
// supply the initialized nonnil dependency.
// NewPublisher оборачивает переданный доменный сервис без проверки и внешней работы; композиция должна
// передать настроенную непустую зависимость.
func NewPublisher(serviceValue *service.Publisher) *Publisher {
	return &Publisher{service: serviceValue}
}

// Publish forwards controller identity, signed directive and idempotency key unchanged to domain
// issuance/delivery, preserving the domain result and error.
// Publish без изменения передаёт личность контроллера, подписанную директиву и ключ идемпотентности
// доменному выпуску и доставке, сохраняя результат и ошибку.
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

// Ready delegates publisher readiness to the domain rather than treating wrapper construction as proof
// of a working publication path.
// Ready передаёт проверку готовности публикации домену, не считая создание обёртки доказательством
// рабочего пути.
func (application *Publisher) Ready(ctx context.Context) error {
	return application.service.Ready(ctx)
}

// PublishReadbackMaterials delegates ordinary readback-material issuance and delivery to the domain;
// returned private possession material remains sensitive.
// PublishReadbackMaterials передаёт домену выпуск и доставку материалов обычной проверки доставки;
// возвращаемый закрытый материал владения остаётся конфиденциальным.
func (application *Publisher) PublishReadbackMaterials(
	ctx context.Context,
) ([]model.PublishedReadbackMaterial, error) {
	return application.service.PublishReadbackMaterials(ctx)
}

// PublishAuthorityGraph delegates full authority graph publication and returns its exact domain result
// without synthesizing publication success.
// PublishAuthorityGraph передаёт домену публикацию полного графа полномочий и возвращает точный
// результат, не создавая фиктивный успех.
func (application *Publisher) PublishAuthorityGraph(
	ctx context.Context,
) (model.AuthoritySnapshotPublication, error) {
	return application.service.PublishAuthorityGraph(ctx)
}

// Registry returns the domain publisher's configured registry; this wrapper adds no validation, cloning
// or immutability enforcement.
// Registry возвращает настроенный реестр доменной публикации; обёртка не добавляет проверки, копирования
// или защиты от изменения.
func (application *Publisher) Registry() model.DeliveryTargetRegistry {
	return application.service.Registry()
}

// SignerGeneration returns the domain publisher's active signer generation without changing key material
// or promoting a generation.
// SignerGeneration возвращает активное поколение подписанта доменной публикации без изменения ключей или
// продвижения поколения.
func (application *Publisher) SignerGeneration() uint64 {
	return application.service.SignerGeneration()
}
