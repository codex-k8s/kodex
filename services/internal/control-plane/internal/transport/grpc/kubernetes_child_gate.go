package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// castKubernetesOwnerGateChild projects only a complete safe description with no executable input.
// A nil result is invalid upstream data when the source Gate declares a Kubernetes child.
// castKubernetesOwnerGateChild выдаёт только полное безопасное описание без executable input.
// Nil при заявленном Kubernetes child означает неверные upstream data.
func castKubernetesOwnerGateChild(child *entity.KubernetesOwnerGateChild) *cp.KubernetesOwnerGateChild {
	if child == nil || !child.Valid() {
		return nil
	}
	address := child.Resource
	preview := &cp.KubernetesOwnerGatePreview{InputDigest: child.Preview.InputDigest, SecretMaterialRef: child.Preview.SecretMaterialRef, Limitations: append([]string(nil), child.Preview.Limitations...)}
	for _, option := range child.Preview.Options {
		preview.Options = append(preview.Options, &cp.KubernetesOwnerGatePreviewOption{Key: option.Key, Kind: option.Kind, ValueJson: option.ValueJSON, Redacted: option.Redacted})
	}
	for _, change := range child.Preview.Changes {
		preview.Changes = append(preview.Changes, &cp.KubernetesOwnerGatePreviewChange{Operation: change.Operation, Path: change.Path, From: change.From, ValueKind: change.ValueKind, ValueJson: change.ValueJSON, Redacted: change.Redacted})
	}
	return &cp.KubernetesOwnerGateChild{PlanRef: child.PlanRef, ChildRef: child.ChildRef, Operation: child.Operation, PlanDigest: child.PlanDigest, InputDigest: child.InputDigest, TargetRef: child.TargetRef, TargetRevision: child.TargetRevision, IntendedName: child.IntendedName, IntendedGenerateName: child.IntendedGenerateName,
		Preview: preview, Resource: &cp.KubernetesOwnerGateResource{Group: address.Group, Version: address.Version, Resource: address.Resource, Namespace: address.Namespace, Name: address.Name, Uid: address.UID, ResourceVersion: address.ResourceVersion, Subresource: address.Subresource, Namespaced: address.Namespaced, AllNamespaces: address.AllNamespaces}}
}
