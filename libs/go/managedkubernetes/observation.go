package managedkubernetes

import "github.com/google/uuid"

// ObservationState distinguishes a concrete object, proven absence and a scoped collection.
// These are discovery facts only; the owner independently admits the operation and each effect.
// ObservationState разделяет конкретный объект, доказанное отсутствие и scoped collection.
// Это только discovery facts; owner отдельно допускает operation и каждый effect.
type ObservationState string

const (
	// ObservationPresent requires exact immutable UID and observed resource version.
	// ObservationPresent требует точный immutable UID и наблюдённый resource version.
	ObservationPresent ObservationState = "PRESENT"
	// ObservationAbsent represents a named object's absence without inventing UID or RV.
	// ObservationAbsent задаёт отсутствие именованного объекта без выдуманных UID либо RV.
	ObservationAbsent ObservationState = "ABSENT"
	// ObservationCollection represents an explicit namespace/GVR scope, never all namespaces.
	// ObservationCollection задаёт explicit namespace/GVR scope, не все namespaces.
	ObservationCollection ObservationState = "COLLECTION"
	// ObservationUnversionedRead is a named read-only API view without a fabricated storage UID/RV.
	// Only registered READ/NONE handling may use it; it never admits mutation or Pod stream access.
	// ObservationUnversionedRead — named read-only API view без выдуманных storage UID/RV.
	// Допустим только registered READ/NONE; mutation либо Pod stream access не разрешается.
	ObservationUnversionedRead ObservationState = "UNVERSIONED_READ"
)

// ResourceObservation carries bounded broker discovery metadata into one owner proposal.
// Absence must originate from an authenticated exact-target NotFound observation, not a denied
// or failed read. Its digest binds that observation; it does not authorize an upsert or replay.
// ResourceObservation переносит bounded broker discovery metadata в один owner proposal.
// Отсутствие выводится из authenticated exact-target NotFound, не denied либо failed read.
// Digest связывает observation, но не разрешает upsert либо replay.
type ResourceObservation struct {
	State                                          ObservationState
	Resource                                       ResourceAddress
	ObservedResourceDigest, RegisteredSchemaDigest string
	// NamespaceUID is optional observed evidence, never an atomic namespace fence. Empty evidence
	// neither proves stability nor denies ordinary name-scoped RBAC, including CREATE. A known
	// different UID requires new owner admission; cluster/all-namespace scopes have no scalar UID.
	// NamespaceUID — optional observed evidence, не atomic namespace fence. Пустое evidence
	// не доказывает стабильность и не запрещает ordinary name-scoped RBAC, включая CREATE.
	// Известный иной UID требует нового owner admission; cluster/all-namespace не имеют scalar UID.
	NamespaceUID string `json:"namespaceUID,omitempty"`
}

// Validate rejects ambiguous presence and fabricated item preconditions before owner use.
// Operation-specific eligibility and fresh discovery provenance remain owner responsibilities.
// Validate отклоняет ambiguous presence и выдуманные item preconditions до owner use.
// Operation-specific eligibility и fresh discovery provenance проверяет owner.
func (observation ResourceObservation) Validate() error {
	address := observation.Resource
	if address.Validate() != nil || !ValidDigest(observation.ObservedResourceDigest) || !ValidDigest(observation.RegisteredSchemaDigest) {
		return ErrInvalidResourceAddress
	}
	if observation.NamespaceUID != "" {
		id, err := uuid.Parse(observation.NamespaceUID)
		if !address.Namespaced || address.AllNamespaces || err != nil || id == uuid.Nil || id.String() != observation.NamespaceUID {
			return ErrInvalidResourceAddress
		}
	}
	switch observation.State {
	case ObservationPresent:
		if address.Name != "" && address.UID != "" && address.ResourceVersion != "" {
			return nil
		}
	case ObservationAbsent:
		if address.Name != "" && address.UID == "" && address.ResourceVersion == "" && address.Subresource == "" {
			return nil
		}
	case ObservationCollection:
		if address.Name == "" && address.UID == "" && address.ResourceVersion == "" && address.Subresource == "" {
			return nil
		}
	case ObservationUnversionedRead:
		if address.Name != "" && address.UID == "" && address.ResourceVersion == "" && !address.AllNamespaces && address.Subresource == "" && !unversionedSensitiveResource(address) {
			return nil
		}
	}
	return ErrInvalidResourceAddress
}

// unversionedSensitiveResource keeps credential/Pod/authorization objects out of the computed
// view exception. Their missing storage pins are malformed evidence, never a projection bypass.
// unversionedSensitiveResource исключает credential/Pod/authorization objects из computed view
// exception. Missing storage pins у них — malformed evidence, не обход projection.
func unversionedSensitiveResource(address ResourceAddress) bool {
	if address.Group == "" && (address.Resource == "secrets" || address.Resource == "pods" || address.Resource == "serviceaccounts") {
		return true
	}
	switch address.Group {
	case "authentication.k8s.io", "authorization.k8s.io", "rbac.authorization.k8s.io", "certificates.k8s.io":
		return true
	default:
		return false
	}
}
