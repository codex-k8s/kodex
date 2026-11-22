package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/util/validation"
)

// PodNodeWitness contains broker-observed Pod placement and an operator-attested node identity.
// A proposal may omit NodeUID; the owner must bind it to one registered transport before admission.
// PodNodeWitness содержит наблюдаемое размещение Pod и подтверждённую оператором identity узла.
// Proposal может не содержать NodeUID; owner связывает его с единственным transport до допуска.
type PodNodeWitness struct {
	NodeName  string `json:"nodeName"`
	NodeUID   string `json:"nodeUID"`
	Container string `json:"container"`
}

// ValidateFor accepts only an exact present core Pod and complete bound placement.
// Structural validity grants no permission and does not prove an atomic NodeUID precondition.
// ValidateFor допускает только exact present core Pod и полное привязанное placement.
// Structural validity не даёт permission и не доказывает atomic NodeUID precondition.
func (witness PodNodeWitness) ValidateFor(resource ResourceAddress) error {
	id, err := uuid.Parse(witness.NodeUID)
	if witness.validatePlacement(resource) != nil || err != nil || id == uuid.Nil || id.String() != witness.NodeUID {
		return ErrInvalidResourceAddress
	}
	return nil
}

// validatePlacement checks observed Pod coordinates without inventing a NodeUID from its nodeName.
// validatePlacement проверяет координаты Pod, не выводя NodeUID из его nodeName.
func (witness PodNodeWitness) validatePlacement(resource ResourceAddress) error {
	if resource.Validate() != nil || resource.Group != "" || resource.Version != "v1" || resource.Resource != "pods" || !resource.Namespaced || resource.AllNamespaces || resource.Name == "" || resource.UID == "" || resource.ResourceVersion == "" ||
		resource.Subresource != "" || len(validation.IsDNS1123Subdomain(witness.NodeName)) != 0 || len(validation.IsDNS1123Label(witness.Container)) != 0 {
		return ErrInvalidResourceAddress
	}
	return nil
}

// ChildCandidate is a bounded observed proposal, not a caller-selected child operation or actor.
// NamespaceUID remains an optional observation witness; PodWitness has no URLs or credentials.
// ChildCandidate — bounded observed proposal, не caller-selected child operation либо actor.
// NamespaceUID остаётся optional observation witness; PodWitness не содержит URL либо credentials.
type ChildCandidate struct {
	ObservationState       ObservationState `json:"observationState"`
	Resource               ResourceAddress  `json:"resource"`
	ObservedResourceDigest string           `json:"observedResourceDigest"`
	RegisteredSchemaDigest string           `json:"registeredSchemaDigest"`
	NamespaceUID           string           `json:"namespaceUID,omitempty"`
	PodWitness             *PodNodeWitness  `json:"podWitness,omitempty"`
}

// Validate preserves the shared observation rules and rejects placement on non-present or non-Pod rows.
// Validate сохраняет общие observation rules и отвергает placement для non-present либо non-Pod rows.
func (candidate ChildCandidate) Validate() error {
	observation := ResourceObservation{State: candidate.ObservationState, Resource: candidate.Resource, ObservedResourceDigest: candidate.ObservedResourceDigest, RegisteredSchemaDigest: candidate.RegisteredSchemaDigest, NamespaceUID: candidate.NamespaceUID}
	if observation.Validate() != nil {
		return ErrInvalidResourceAddress
	}
	if candidate.PodWitness != nil && (candidate.ObservationState != ObservationPresent || candidate.PodWitness.ValidateFor(candidate.Resource) != nil) {
		return ErrInvalidResourceAddress
	}
	return nil
}

// ValidateProposal permits an unbound node UID only in a structurally valid observed Pod proposal.
// It grants no execution authority; stored children and stream admission must still use Validate.
// ValidateProposal допускает отсутствие UID узла только в структурно корректном proposal Pod.
// Метод не даёт полномочий; сохранённые children и stream admission используют строгий Validate.
func (candidate ChildCandidate) ValidateProposal() error {
	base := candidate
	base.PodWitness = nil
	if base.Validate() != nil {
		return ErrInvalidResourceAddress
	}
	if candidate.PodWitness == nil {
		return nil
	}
	witness := *candidate.PodWitness
	if candidate.ObservationState != ObservationPresent || witness.validatePlacement(candidate.Resource) != nil ||
		(witness.NodeUID != "" && witness.ValidateFor(candidate.Resource) != nil) {
		return ErrInvalidResourceAddress
	}
	return nil
}

// ChildProposalDigest binds one finite exact set to the immutable owner snapshot in a stable order.
// Duplicate resource identity, unknown observations and oversized sets fail before hashing. Input
// slices are not sorted in place; the witness and every resource/schema pin participate in the hash.
// ChildProposalDigest связывает конечный exact set с immutable owner snapshot в устойчивом порядке.
// Duplicate resource identity, unknown observations и oversized sets отвергаются до hash. Input
// slices не сортируются на месте; witness и все resource/schema pins входят в hash.
func ChildProposalDigest(snapshotDigest string, candidates []ChildCandidate) (string, error) {
	if !ValidDigest(snapshotDigest) || len(candidates) > 256 {
		return "", ErrInvalidResourceAddress
	}
	ordered := append([]ChildCandidate{}, candidates...)
	type coordinate struct {
		group, version, resource, namespace, name, subresource string
		namespaced, all                                        bool
	}
	seen := make(map[coordinate]struct{}, len(ordered))
	for _, candidate := range ordered {
		if candidate.ValidateProposal() != nil {
			return "", ErrInvalidResourceAddress
		}
		address := candidate.Resource
		key := coordinate{address.Group, address.Version, address.Resource, address.Namespace, address.Name, address.Subresource, address.Namespaced, address.AllNamespaces}
		if _, exists := seen[key]; exists {
			return "", ErrInvalidResourceAddress
		}
		seen[key] = struct{}{}
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, _ := json.Marshal(ordered[i].Resource)
		right, _ := json.Marshal(ordered[j].Resource)
		return string(left) < string(right)
	})
	raw, err := json.Marshal(struct {
		Domain         string           `json:"domain"`
		SnapshotDigest string           `json:"snapshotDigest"`
		Candidates     []ChildCandidate `json:"candidates"`
	}{"kodex.managed-kubernetes.children/v1", snapshotDigest, ordered})
	if err != nil {
		return "", ErrInvalidResourceAddress
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
