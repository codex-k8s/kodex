package kubernetestarget

import (
	"errors"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured/unstructuredscheme"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/managedfields"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// reconstructResourceApply uses Kubernetes' native structured-merge field manager with the
// observed managedFields and registered OpenAPI3 models. This preserves ownership/conflict and
// omission semantics for policy inspection; the actual wire request remains native SSA with
// its original fieldManager/force and observed UID/RV, never a whole-object JSON Patch fallback.
// Kubernetes v1.36.3 patcher.applyPatch rejects a supplied UID when the current object is absent:
// https://github.com/kubernetes/kubernetes/blob/v1.36.3/staging/src/k8s.io/apiserver/pkg/endpoints/handlers/patch.go#L538-L563
// reconstructResourceApply использует native structured-merge field manager Kubernetes с
// observed managedFields и registered OpenAPI3 models. Ownership/conflict/omission semantics
// сохраняются для policy inspection; actual wire request остаётся native SSA с исходными
// fieldManager/force и observed UID/RV, не whole-object JSON Patch fallback.
// Kubernetes v1.36.3 patcher.applyPatch отклоняет supplied UID при отсутствии current object;
// источник указан выше. Это сохраняет PRESENT fence при deletion race, без SSA upsert.
func (prepared *PreparedResourceOperation) reconstructResourceApply(applied map[string]any, options map[string]any) (map[string]any, error) {
	managerName, ok := options["fieldManager"].(string)
	if !ok || managerName == "" || prepared.current == nil {
		return nil, errors.New(resourceOperationInvalid)
	}
	converter, err := managedfields.NewTypeConverter(prepared.schema.models, false)
	if err != nil {
		return nil, errors.New(resourceOperationInvalid)
	}
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(prepared.schema.kind, &unstructured.Unstructured{})
	create := unstructuredscheme.NewUnstructuredCreator()
	defaults := unstructuredscheme.NewUnstructuredDefaulter()
	var manager *managedfields.FieldManager
	if clientgoscheme.Scheme.Recognizes(prepared.schema.kind) {
		manager, err = managedfields.NewDefaultFieldManager(converter, runtime.UnsafeObjectConvertor(scheme), defaults, create,
			prepared.schema.kind, prepared.schema.kind.GroupVersion(), prepared.address.Subresource, nil)
	} else {
		manager, err = managedfields.NewDefaultCRDFieldManager(converter, runtime.UnsafeObjectConvertor(scheme), defaults, create,
			prepared.schema.kind, prepared.schema.kind.GroupVersion(), prepared.address.Subresource, nil)
	}
	if err != nil {
		return nil, errors.New(resourceOperationInvalid)
	}
	currentObject, err := cloneResourceObject(prepared.current)
	if err != nil {
		return nil, err
	}
	appliedObject, err := cloneResourceObject(applied)
	if err != nil {
		return nil, err
	}
	current := &unstructured.Unstructured{Object: currentObject}
	current.SetGroupVersionKind(prepared.schema.kind)
	proposal := &unstructured.Unstructured{Object: appliedObject}
	proposal.SetGroupVersionKind(prepared.schema.kind)
	force, _ := options["forceConflicts"].(bool)
	merged, err := manager.Apply(current, proposal, managerName, force)
	if err != nil {
		return nil, errors.New(resourceOperationInvalid)
	}
	result, ok := merged.(*unstructured.Unstructured)
	if !ok || result == nil {
		return nil, errors.New(resourceOperationInvalid)
	}
	return result.Object, nil
}
