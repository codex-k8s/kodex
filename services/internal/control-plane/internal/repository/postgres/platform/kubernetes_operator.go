package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ConfigureKubernetesOperator detaches validated public metadata before bootstrap and workers.
// It issues no target authority: owner registration must persist the source and retirement checks.
// Reconfiguration in a running repository is forbidden; a new projection needs a new process.
// ConfigureKubernetesOperator отделяет public metadata до bootstrap и workers.
// Не выдаёт target authority: owner registration сохраняет source и проверяет retirement.
// Reconfiguration работающего repository запрещена; новая projection требует нового process.
func (repository *Repository) ConfigureKubernetesOperator(registry value.KubernetesOperatorRegistry) error {
	canonical, err := json.Marshal(registry)
	if err != nil {
		return value.ErrKubernetesOperator
	}
	detached, err := value.DecodeKubernetesOperatorRegistry(canonical, time.Now().UTC())
	if err != nil {
		return value.ErrKubernetesOperator
	}
	hash := sha256.Sum256(canonical)
	digest := hex.EncodeToString(hash[:])
	if repository.kubernetesOperator != nil {
		if repository.kubernetesOperatorDigest == digest {
			return nil
		}
		return value.ErrKubernetesOperator
	}
	repository.kubernetesOperator, repository.kubernetesOperatorDigest = &detached, digest
	return nil
}

// kubernetesOperatorPayload hashes canonical public metadata, never private credential material.
// kubernetesOperatorPayload хеширует canonical public metadata, не private credential material.
func kubernetesOperatorPayload(metadata any) (string, string, error) {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return "", "", value.ErrKubernetesOperator
	}
	hash := sha256.Sum256(raw)
	return string(raw), hex.EncodeToString(hash[:]), nil
}
