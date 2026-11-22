package entity

import (
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// KubernetesOwnerGateChild exposes one owner-selected immutable action without executable input.
// Plan/target hashes describe the approval boundary; this metadata never grants effect credit.
// KubernetesOwnerGateChild описывает одно immutable действие владельца без executable input.
// Hashes plan/target описывают approval boundary; metadata не выдаёт effect credit.
type KubernetesOwnerGateChild struct {
	Preview                                                          *managedkubernetes.GateEffectPreview
	PlanRef, ChildRef, Operation, PlanDigest, InputDigest, TargetRef string
	TargetRevision                                                   int64
	Resource                                                         managedkubernetes.ResourceAddress
	IntendedName, IntendedGenerateName                               string
}

// Valid checks the finite public description, never actor eligibility, approval or effect authority.
// CREATE uses its original collection observation and selected intended name; other effects keep
// the exact named observed incarnation. No opaque input body is inspected by this projection.
// Valid проверяет конечное public описание, не actor eligibility, approval либо effect authority.
// CREATE сохраняет collection observation и intended name; остальные effects — точную именованную
// observed incarnation. Projection не разбирает opaque input body.
func (child KubernetesOwnerGateChild) Valid() bool {
	operation, known := integrationpackage.KubernetesOperationByKey(child.Operation)
	if child.Preview == nil || child.Preview.InputDigest != child.InputDigest || child.Preview.ValidateForOperation(child.Operation, managedkubernetes.GatePreviewDetailBytes) != nil || !known || (operation.Execution != integrationpackage.KubernetesResource && operation.Execution != integrationpackage.KubernetesStream) ||
		!managedkubernetes.ValidLocator(child.PlanRef) || len(child.PlanRef) > 96 || !managedkubernetes.ValidLocator(child.ChildRef) || len(child.ChildRef) > 96 ||
		!managedkubernetes.ValidLocator(child.TargetRef) || len(child.TargetRef) > 96 || child.TargetRevision < 1 || child.TargetRevision > 9007199254740991 ||
		!managedkubernetes.ValidDigest(child.PlanDigest) || !managedkubernetes.ValidDigest(child.InputDigest) || child.Resource.Validate() != nil || child.Resource.AllNamespaces {
		return false
	}
	resource := child.Resource
	if child.Operation == "kubernetes.port-forward" && (resource.Group != "" || resource.Version != "v1" || resource.Resource != "pods" || resource.Subresource != "" || !resource.Namespaced) {
		return false
	}
	if child.Operation == "kubernetes.run" && (resource.Group != "" || resource.Version != "v1" || resource.Resource != "pods" || !resource.Namespaced || child.IntendedName == "" || child.IntendedGenerateName != "") {
		return false
	}
	if operation.Verb == "create" {
		if resource.Name != "" || resource.UID != "" || resource.ResourceVersion != "" || resource.Subresource != "" || (child.IntendedName == "") == (child.IntendedGenerateName == "") {
			return false
		}
		resource.Name = child.IntendedName
		if resource.Name == "" {
			resource.Name = child.IntendedGenerateName
		}
		return resource.Validate() == nil
	}
	return resource.Name != "" && resource.UID != "" && resource.ResourceVersion != "" && child.IntendedName == "" && child.IntendedGenerateName == ""
}
