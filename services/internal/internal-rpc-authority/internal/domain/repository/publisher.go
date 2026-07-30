package repository

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
)

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
	PublisherReady(context.Context) error
}

type SecretMaterial struct {
	Version uint64
	Data    map[string]string
	Digest  string
}

type SecretDelivery interface {
	ReadKV2(context.Context, string) (SecretMaterial, bool, error)
	CreateKV2(context.Context, string, map[string]string) (SecretMaterial, error)
	WriteKV2CAS(
		context.Context,
		string,
		uint64,
		map[string]string,
	) (SecretMaterial, error)
}
