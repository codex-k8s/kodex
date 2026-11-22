package managedkubernetes

import "math"

// PortForwardConnectionIdentity names one server-assigned incarnation; it contains no target selector, payload or new effect authority.
// PortForwardConnectionIdentity называет server-assigned incarnation; target selector, payload либо новая effect authority отсутствуют.
type PortForwardConnectionIdentity struct {
	ConnectionRef string
	Generation    uint64
	Slot          byte
}

// PortForwardRetentionSet is an owner-selected snapshot of connections whose native join evidence may still be needed.
// It is issued only by protected INPUT OPEN admission; callers cannot supply it through the public KPF envelope.
// PortForwardRetentionSet — owner-selected snapshot connections, для которых ещё может потребоваться native join evidence.
// Его выдаёт только protected INPUT OPEN admission; public KPF envelope не принимает этот snapshot от caller.
type PortForwardRetentionSet struct {
	Connections []PortForwardConnectionIdentity
}

// Validate checks bounded, generation-ordered identities without minting admission; an optional OPEN requires exact inclusion and forbids future generations.
// Validate проверяет bounded generation-ordered identities без выдачи admission; optional OPEN требует exact inclusion и запрещает будущие поколения.
func (set PortForwardRetentionSet) Validate(slots int, opening *PortForwardConnectionIdentity) error {
	if len(set.Connections) < 1 || len(set.Connections) > MaximumPortForwardConnections || slots < 1 || slots > MaximumForwardPorts {
		return ErrInvalidOperationInput
	}
	var previous uint64
	found := opening == nil
	for i, item := range set.Connections {
		if !ValidPortForwardConnectionRef(item.ConnectionRef) || item.Generation < 1 || item.Generation > math.MaxInt64 || item.Generation <= previous || int(item.Slot) >= slots {
			return ErrInvalidOperationInput
		}
		for _, prior := range set.Connections[:i] {
			if prior.ConnectionRef == item.ConnectionRef {
				return ErrInvalidOperationInput
			}
		}
		if opening != nil {
			if item.Generation > opening.Generation {
				return ErrInvalidOperationInput
			}
			if item == *opening {
				found = true
			}
		}
		previous = item.Generation
	}
	if !found {
		return ErrInvalidOperationInput
	}
	return nil
}

// Clone retains no borrowed identity slice; it never changes the original owner-selected set.
// Clone не сохраняет borrowed identity slice и не изменяет original owner-selected set.
func (set PortForwardRetentionSet) Clone() PortForwardRetentionSet {
	return PortForwardRetentionSet{Connections: append([]PortForwardConnectionIdentity(nil), set.Connections...)}
}
