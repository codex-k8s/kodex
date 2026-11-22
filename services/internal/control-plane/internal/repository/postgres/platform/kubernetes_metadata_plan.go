package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// kubernetesMetadataChild binds a registered addressless read to the exact original target grant; it fabricates neither a resource observation nor discovery content.
// kubernetesMetadataChild связывает зарегистрированный безадресный read с exact original target grant; не выдумывает наблюдение ресурса либо discovery content.
func kubernetesMetadataChild(owner kubernetesOperationOwner) (entity.ManagedKubernetesChildEffect, error) {
	var empty entity.ManagedKubernetesChildEffect
	var scope map[string]string
	parsed, canonical, err := managedkubernetes.DecodeOperationInput(owner.candidate.operation, owner.normalized)
	if !integrationpackage.KubernetesMetadataRead(owner.candidate.operation) || owner.candidate.risk != "READ" || owner.candidate.approvalPolicy != "NONE" || err != nil || string(canonical) != string(owner.normalized) || parsed.Resource != "" ||
		json.Unmarshal(owner.candidate.resourceScope, &scope) != nil || len(scope) != 2 || scope["namespace"] != "@cluster" || !managedkubernetes.ValidLocator(scope["target_ref"]) {
		return empty, errs.ErrForbidden
	}
	digest := sha256.Sum256(canonical)
	return entity.ManagedKubernetesChildEffect{Operation: owner.candidate.operation, NormalizedInputJSON: canonical, InputDigest: hex.EncodeToString(digest[:]), State: "READY"}, nil
}
