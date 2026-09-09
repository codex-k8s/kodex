package repository

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// PublisherStore сохраняет выданные материалы и намерения проверки доставки.
type PublisherStore interface {
	LoadPublishedCredential(
		context.Context,
		string,
	) (model.PublishedCredential, bool, error)
	SavePublishedCredential(
		context.Context,
		model.PublishedCredential,
	) (model.PublishedCredential, error)
	PinReadbackIntent(context.Context, model.ReadbackIntent) (model.ReadbackIntent, error)
	LoadSnapshotHistory(context.Context) (model.AuthoritySnapshotHistory, error)
	LoadSnapshotPublication(
		context.Context,
		uint64,
		string,
	) (model.AuthoritySnapshotPublication, bool, error)
	PrepareRotation(
		context.Context,
		model.AuthorityRotationIntent,
	) error
	LoadOrPrepareRotationOperation(
		context.Context,
		model.AuthorityRotationOperation,
	) (model.AuthorityRotationOperation, error)
	LoadRotationOperation(
		context.Context,
		string,
	) (model.AuthorityRotationOperation, bool, error)
	PrepareRotationPhase(
		context.Context,
		model.AuthorityRotationOperation,
		string,
		model.AuthorityRotationIntent,
	) error
	AdvanceRotationOperation(
		context.Context,
		model.AuthorityRotationOperation,
		string,
		model.AuthoritySnapshotPublication,
	) (model.AuthorityRotationOperation, error)
	BeginRotationDelivery(
		context.Context,
		model.AuthorityRotationIntent,
	) error
	MarkRotationDelivered(
		context.Context,
		model.AuthorityRotationIntent,
	) error
	AppendSnapshot(
		context.Context,
		model.AuthoritySnapshotPublication,
		int,
	) (model.AuthoritySnapshotPublication, error)
	SnapshotPublicationReady(
		context.Context,
		model.AuthoritySnapshotPublication,
		[]model.SnapshotReadbackTarget,
	) error
	PublisherReady(context.Context) error
}

// SecretMaterial содержит версию и digest фактически прочитанного секрета.
type SecretMaterial struct {
	Version uint64
	Data    map[string]string
	Digest  string
}

// SecretDelivery доставляет versioned KV-материал с CAS.
type SecretDelivery interface {
	ReadVersioned(context.Context, string) (SecretMaterial, bool, error)
	CreateVersioned(context.Context, string, map[string]string) (SecretMaterial, error)
	WriteVersionedCAS(
		context.Context,
		string,
		uint64,
		map[string]string,
	) (SecretMaterial, error)
}

// SnapshotDelivery атомарно обновляет заранее созданный Secret снимка.
type SnapshotDelivery interface {
	Publish(
		context.Context,
		model.AuthoritySnapshotPublication,
	) (model.AuthoritySnapshotPublication, error)
	Read(
		context.Context,
	) (model.AuthoritySnapshotPublication, error)
	Close()
}

// AuthorityGraphLifecycle публикует и readback-проверяет полный graph.
type AuthorityGraphLifecycle interface {
	Publish(context.Context) (model.AuthoritySnapshotPublication, error)
	Ready(context.Context, model.AuthoritySnapshotPublication) error
}
