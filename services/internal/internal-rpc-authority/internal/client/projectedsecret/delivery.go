// Package projectedsecret читает только заранее назначенные workload Secret,
// смонтированные kubelet в фиксированные read-only каталоги.
package projectedsecret

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
)

const generationKey = "_generation"

type binding struct {
	directory string
	keys      []string
}

// Delivery предоставляет workload только read path смонтированных Secret.
type Delivery struct {
	bindings map[string]binding
}

// NewRuntimeDelivery связывает закрытые resource names с фиксированными mount paths.
func NewRuntimeDelivery(
	readbackCredential string,
	readbackPossession string,
	restoreCredential string,
	restoreACK string,
	resolverCredential string,
	resolverPossession string,
) (*Delivery, error) {
	bindings := map[string]binding{
		readbackCredential: {
			directory: "/var/run/secrets/kodex/internal-rpc-authority/readback/credential",
			keys:      []string{"pinned_intent_id", "readback_credential_compact_jws", "readback_credential_jti", "readback_credential_digest_sha256", "intent_digest_sha256", "expires_at"},
		},
		readbackPossession: {
			directory: "/var/run/secrets/kodex/internal-rpc-authority/readback/possession",
			keys:      []string{"possession_private_jwk", "possession_key_kid", "possession_key_generation", "possession_key_thumbprint_sha256"},
		},
		restoreCredential: {
			directory: "/var/run/secrets/kodex/internal-rpc-authority/restore/credential",
			keys:      []string{"semantic_digest_sha256", "issuance_directive_jti", "role_credential_compact_jws", "role_credential_digest_sha256", "delivery_receipt_jti", "issued_at"},
		},
		restoreACK: {
			directory: "/var/run/secrets/kodex/internal-rpc-authority/restore/ack",
			keys:      []string{"semantic_digest_sha256", "issuance_directive_jti", "ack_private_jwk", "ack_key_kid", "ack_key_thumbprint_sha256", "role_credential_jti", "delivery_receipt_jti", "issued_at"},
		},
	}
	if resolverCredential != "" {
		bindings[resolverCredential] = binding{
			directory: "/var/run/secrets/kodex/internal-rpc-authority/readback/resolver-credential",
			keys:      []string{"pinned_intent_id", "readback_credential_compact_jws", "readback_credential_jti", "readback_credential_digest_sha256", "intent_digest_sha256", "expires_at"},
		}
	}
	if resolverPossession != "" {
		bindings[resolverPossession] = binding{
			directory: "/var/run/secrets/kodex/internal-rpc-authority/readback/resolver-possession",
			keys:      []string{"possession_private_jwk", "possession_key_kid", "possession_key_generation", "possession_key_thumbprint_sha256"},
		}
	}
	for name, value := range bindings {
		if name == "" || !filepath.IsAbs(value.directory) || len(value.keys) == 0 {
			return nil, errors.New("projected Secret runtime binding is invalid")
		}
	}
	return &Delivery{bindings: bindings}, nil
}

// ReadVersioned читает один согласованный kubelet snapshot.
func (delivery *Delivery) ReadVersioned(
	_ context.Context,
	resourceName string,
) (repository.SecretMaterial, bool, error) {
	binding, ok := delivery.bindings[resourceName]
	if !ok {
		return repository.SecretMaterial{}, false, errors.New("projected Secret is outside the runtime registry")
	}
	generationRaw, err := readBounded(filepath.Join(binding.directory, generationKey), 64)
	if errors.Is(err, os.ErrNotExist) {
		return repository.SecretMaterial{}, false, nil
	}
	if err != nil {
		return repository.SecretMaterial{}, false, errors.New("read projected Secret generation")
	}
	generation, err := strconv.ParseUint(string(generationRaw), 10, 64)
	if err != nil || generation == 0 {
		return repository.SecretMaterial{}, false, errors.New("projected Secret generation is invalid")
	}
	data := make(map[string]string, len(binding.keys))
	for _, key := range binding.keys {
		raw, readErr := readBounded(filepath.Join(binding.directory, key), 1<<20)
		if readErr != nil {
			return repository.SecretMaterial{}, false, errors.New("read projected Secret field")
		}
		data[key] = string(raw)
	}
	// Повторное чтение generation закрывает смену kubelet symlink между файлами.
	confirmed, err := readBounded(filepath.Join(binding.directory, generationKey), 64)
	if err != nil || string(confirmed) != string(generationRaw) {
		return repository.SecretMaterial{}, false, errors.New("projected Secret snapshot changed during read")
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(data)
	if err != nil {
		return repository.SecretMaterial{}, false, errors.New("digest projected Secret readback")
	}
	return repository.SecretMaterial{Version: generation, Data: data, Digest: digest}, true, nil
}

func (delivery *Delivery) CreateVersioned(context.Context, string, map[string]string) (repository.SecretMaterial, error) {
	return repository.SecretMaterial{}, errors.New("projected Secret delivery is read-only")
}

func (delivery *Delivery) WriteVersionedCAS(context.Context, string, uint64, map[string]string) (repository.SecretMaterial, error) {
	return repository.SecretMaterial{}, errors.New("projected Secret delivery is read-only")
}

func (delivery *Delivery) Close() {}

func readBounded(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, errors.New("projected Secret file is invalid")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || int64(len(raw)) > limit {
		return nil, errors.New("read projected Secret file")
	}
	return raw, nil
}

var _ repository.SecretDelivery = (*Delivery)(nil)
