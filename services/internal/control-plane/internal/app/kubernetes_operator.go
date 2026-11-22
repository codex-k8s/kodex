package app

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	platformrepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
)

// validKubernetesOperatorInput requires an explicit public artifact digest and canonical absolute path.
// Missing configuration disables this feature rather than selecting another credential/target source.
// validKubernetesOperatorInput требует explicit public artifact digest и canonical absolute path.
// Отсутствие configuration отключает feature, не выбирая другой credential/target source.
func validKubernetesOperatorInput(config Config) bool {
	if config.KubernetesOperatorRegistryFile == "" {
		return config.KubernetesOperatorRegistryHash == ""
	}
	path := config.KubernetesOperatorRegistryFile
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n") &&
		managedkubernetes.ValidDigest(config.KubernetesOperatorRegistryHash)
}

// configureKubernetesOperator loads a bounded nonsecret ConfigMap projection with its exact file hash.
// ConfigMap symlinks are allowed; one open file descriptor binds the actual bytes before decoding.
// It never reads kubeconfig, private keys, ambient context, caller URLs or target credentials.
// configureKubernetesOperator читает bounded nonsecret ConfigMap projection с exact file hash.
// ConfigMap symlinks допустимы; один открытый file descriptor связывает байты до decoding.
// Не читает kubeconfig, private keys, ambient context, caller URLs либо target credentials.
func configureKubernetesOperator(repository *platformrepository.Repository, config Config) error {
	if !validKubernetesOperatorInput(config) {
		return value.ErrKubernetesOperator
	}
	if config.KubernetesOperatorRegistryFile == "" {
		return nil
	}
	file, err := os.Open(config.KubernetesOperatorRegistryFile)
	if err != nil {
		return value.ErrKubernetesOperator
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > value.MaximumKubernetesOperatorBytes {
		return value.ErrKubernetesOperator
	}
	raw, err := io.ReadAll(io.LimitReader(file, value.MaximumKubernetesOperatorBytes+1))
	if err != nil || len(raw) > value.MaximumKubernetesOperatorBytes {
		return value.ErrKubernetesOperator
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != config.KubernetesOperatorRegistryHash {
		return value.ErrKubernetesOperator
	}
	registry, err := value.DecodeKubernetesOperatorRegistry(raw, time.Now().UTC())
	if err != nil {
		return value.ErrKubernetesOperator
	}
	return repository.ConfigureKubernetesOperator(registry)
}
