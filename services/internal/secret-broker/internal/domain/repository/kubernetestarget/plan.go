package kubernetestarget

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// OperationChild contains immutable owner-selected input, never caller-assigned dispatch authority.
// State is readback only; execution still requires fresh Begin for this exact child.
// OperationChild содержит immutable owner-selected input без caller-assigned dispatch authority.
// State является readback; исполнение требует fresh Begin именно для этого child.
type OperationChild struct {
	Ref, Operation, InputDigest, State string
	Resource                           managedkubernetes.ResourceAddress
	Input                              managedkubernetes.OperationInput `json:"-"`
	NormalizedInput                    []byte                           `json:"-"`
	NodeTransport                      *value.KubernetesNodeTransport
	Observation                        *managedkubernetes.ChildCandidate
}

// String protects private child bodies from ordinary diagnostic formatting.
// String защищает private child bodies при обычном диагностическом форматировании.
func (OperationChild) String() string { return "[managed Kubernetes operation child]" }

// GoString retains the same boundary for detailed diagnostic formatting.
// GoString сохраняет ту же границу для подробного диагностического форматирования.
func (child OperationChild) GoString() string { return child.String() }

// OperationPlan is a bounded ordered set of owner children with an immutable plan pin.
// It cannot be used as a grant or reorder dependent actions by resource name.
// OperationPlan является bounded ordered set owner children с immutable plan pin.
// Он не является grant и не позволяет переставлять зависимые действия по имени ресурса.
type OperationPlan struct {
	Ref, Digest string
	Revision    int64
	Children    []OperationChild
}

// OperationPlanner submits observed metadata and receives owner-derived commands without target effects.
// OperationPlanner передаёт observed metadata и получает owner-derived commands без target effects.
type OperationPlanner interface {
	// PrepareKubernetesChildren binds one finite proposal to the original immutable owner snapshot.
	// PrepareKubernetesChildren связывает finite proposal с original immutable owner snapshot.
	PrepareKubernetesChildren(context.Context, managedkubernetes.Binding, value.KubernetesOperationSnapshot, []managedkubernetes.ChildCandidate) (OperationPlan, error)
}
