// Package kubernetessecret реализует закрытую доставку authority-материалов
// через заранее созданные Kubernetes Secret.
package kubernetessecret

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/codex-k8s/kodex/libs/go/exactkubernetessecret"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

var (
	keySetKeys = []string{
		"private.jwk", "current_private_jwk", "next_private_jwk",
		"current_generation", "next_generation", "source_revision",
		"source_digest_sha256", "previous_public_jwk", "previous_generation",
	}
	manifestTrustKeys      = []string{"bundle.jws", "source_revision", "source_digest_sha256"}
	proofTrustKeys         = []string{"jwks.json", "source_revision", "source_digest_sha256"}
	readbackCredentialKeys = []string{
		"pinned_intent_id", "readback_credential_compact_jws",
		"readback_credential_jti", "readback_credential_digest_sha256",
		"intent_digest_sha256", "expires_at",
	}
	readbackPossessionKeys = []string{
		"possession_private_jwk", "possession_key_kid",
		"possession_key_generation", "possession_key_thumbprint_sha256",
	}
	restoreCredentialKeys = []string{
		"semantic_digest_sha256", "issuance_directive_jti",
		"role_credential_compact_jws", "role_credential_digest_sha256",
		"delivery_receipt_jti", "issued_at",
	}
	restoreACKKeys = []string{
		"semantic_digest_sha256", "issuance_directive_jti", "ack_private_jwk",
		"ack_key_kid", "ack_key_thumbprint_sha256", "role_credential_jti",
		"delivery_receipt_jti", "issued_at",
	}
)

// Delivery предоставляет versioned CAS только для закрытого набора ресурсов.
type Delivery struct {
	clients map[string]*exactkubernetessecret.MapClient
}

// NewPublisherDelivery строит реестр ресурсов из проверенного publisher registry.
func NewPublisherDelivery(registry model.DeliveryTargetRegistry) (*Delivery, error) {
	resources := make(map[string][]string, len(registry.Targets)*8)
	for _, target := range registry.Targets {
		for _, entry := range []struct {
			name string
			keys []string
		}{
			{target.AuthPrivateKeySecret, keySetKeys},
			{target.ManifestTrustSecret, manifestTrustKeys},
			{target.ProofTrustSecret, proofTrustKeys},
			{target.ProofPrivateKeySecret, keySetKeys},
			{target.RestoreCredentialPath, restoreCredentialKeys},
			{target.RestoreACKKeyPath, restoreACKKeys},
			{target.ReadbackCredentialPath, readbackCredentialKeys},
			{target.ReadbackPossessionKeyPath, readbackPossessionKeys},
		} {
			if entry.name == "" {
				continue
			}
			if previous, ok := resources[entry.name]; ok && !equalKeys(previous, entry.keys) {
				return nil, errors.New("Kubernetes Secret purpose is reused with another schema")
			}
			resources[entry.name] = entry.keys
		}
	}
	return New(resources)
}

// NewRuntimeDelivery ограничивает sidecar четырьмя назначенными Secret.
func NewRuntimeDelivery(
	readbackCredential string,
	readbackPossession string,
	restoreCredential string,
	restoreACK string,
	resolverCredential string,
	resolverPossession string,
) (*Delivery, error) {
	resources := map[string][]string{
		readbackCredential: readbackCredentialKeys,
		readbackPossession: readbackPossessionKeys,
		restoreCredential:  restoreCredentialKeys,
		restoreACK:         restoreACKKeys,
	}
	if resolverCredential != "" {
		resources[resolverCredential] = readbackCredentialKeys
	}
	if resolverPossession != "" {
		resources[resolverPossession] = readbackPossessionKeys
	}
	return New(resources)
}

// New создаёт доставку для явно переданного закрытого реестра ресурсов.
func New(resources map[string][]string) (*Delivery, error) {
	if len(resources) == 0 || len(resources) > 256 {
		return nil, errors.New("Kubernetes Secret delivery registry is invalid")
	}
	clients := make(map[string]*exactkubernetessecret.MapClient, len(resources))
	for name, keys := range resources {
		client, err := exactkubernetessecret.NewMap(exactkubernetessecret.MapConfig{
			ResourceName: name, AllowedDataKeys: keys, Timeout: 5 * time.Second,
		})
		if err != nil {
			for _, opened := range clients {
				opened.Close()
			}
			return nil, err
		}
		clients[name] = client
	}
	return &Delivery{clients: clients}, nil
}

// ReadVersioned читает exact Secret и возвращает generation как логическую версию.
func (delivery *Delivery) ReadVersioned(
	ctx context.Context,
	resourceName string,
) (repository.SecretMaterial, bool, error) {
	client, ok := delivery.clients[resourceName]
	if !ok {
		return repository.SecretMaterial{}, false, errors.New("Kubernetes Secret is outside the delivery registry")
	}
	snapshot, err := client.Read(ctx)
	if err != nil {
		return repository.SecretMaterial{}, false, err
	}
	if snapshot.Generation == 0 {
		return repository.SecretMaterial{}, false, nil
	}
	return material(snapshot)
}

// CreateVersioned инициализирует заранее созданный пустой Secret.
func (delivery *Delivery) CreateVersioned(
	ctx context.Context,
	resourceName string,
	data map[string]string,
) (repository.SecretMaterial, error) {
	client, ok := delivery.clients[resourceName]
	if !ok {
		return repository.SecretMaterial{}, errors.New("Kubernetes Secret is outside the delivery registry")
	}
	current, err := client.Read(ctx)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	if current.Generation > 0 {
		result, _, readErr := delivery.ReadVersioned(ctx, resourceName)
		return result, readErr
	}
	updated, err := client.CompareAndSwap(ctx, current.ResourceVersion, 0, encode(data))
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	result, _, err := material(updated)
	return result, err
}

// WriteVersionedCAS заменяет материал при совпадении поколения.
func (delivery *Delivery) WriteVersionedCAS(
	ctx context.Context,
	resourceName string,
	expectedVersion uint64,
	data map[string]string,
) (repository.SecretMaterial, error) {
	client, ok := delivery.clients[resourceName]
	if !ok || expectedVersion == 0 {
		return repository.SecretMaterial{}, errors.New("Kubernetes Secret CAS target is invalid")
	}
	current, err := client.Read(ctx)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	updated, err := client.CompareAndSwap(
		ctx,
		current.ResourceVersion,
		expectedVersion,
		encode(data),
	)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	result, _, err := material(updated)
	return result, err
}

// Close освобождает соединения с Kubernetes API.
func (delivery *Delivery) Close() {
	for _, client := range delivery.clients {
		client.Close()
	}
}

func material(snapshot exactkubernetessecret.MapSnapshot) (repository.SecretMaterial, bool, error) {
	data := make(map[string]string, len(snapshot.Data))
	for key, value := range snapshot.Data {
		data[key] = string(value)
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(data)
	if err != nil {
		return repository.SecretMaterial{}, false, errors.New("digest Kubernetes Secret readback")
	}
	return repository.SecretMaterial{Version: snapshot.Generation, Data: data, Digest: digest}, true, nil
}

func encode(data map[string]string) map[string][]byte {
	result := make(map[string][]byte, len(data))
	for key, value := range data {
		result[key] = []byte(value)
	}
	return result
}

func equalKeys(left, right []string) bool {
	leftCopy, rightCopy := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	if len(leftCopy) != len(rightCopy) {
		return false
	}
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}

var _ repository.SecretDelivery = (*Delivery)(nil)
