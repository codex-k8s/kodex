package managedkubernetes

import (
	"math"
	"testing"
)

// TestPortForwardRetentionRejectsForeignAndUnboundedSets keeps the protected snapshot finite, canonical and tied to its exact OPEN incarnation.
// TestPortForwardRetentionRejectsForeignAndUnboundedSets сохраняет protected snapshot конечным, canonical и связанным с exact OPEN incarnation.
func TestPortForwardRetentionRejectsForeignAndUnboundedSets(t *testing.T) {
	f := portForwardConnectionFixture(PortForwardConnectionOpen)
	opening := PortForwardConnectionIdentity{ConnectionRef: f.ConnectionRef, Generation: f.Generation, Slot: f.Slot}
	for _, mode := range []string{"valid", "empty", "capacity", "prefix", "zero", "overflow", "slot", "missing", "future", "duplicate", "unordered"} {
		t.Run(mode, func(t *testing.T) {
			set := PortForwardRetentionSet{Connections: []PortForwardConnectionIdentity{opening}}
			switch mode {
			case "empty":
				set.Connections = nil
			case "capacity":
				set.Connections = make([]PortForwardConnectionIdentity, MaximumPortForwardConnections+1)
			case "prefix":
				set.Connections[0].ConnectionRef = "inv_foreign"
			case "zero":
				set.Connections[0].Generation = 0
			case "overflow":
				set.Connections[0].Generation = math.MaxInt64 + 1
			case "slot":
				set.Connections[0].Slot = 2
			case "missing":
				set.Connections[0].Generation--
			case "future":
				set.Connections[0].Generation++
			case "duplicate":
				set.Connections = append(set.Connections, opening)
				set.Connections[1].Generation++
			case "unordered":
				set.Connections = append(set.Connections, opening)
			}
			if (set.Validate(2, &opening) == nil) != (mode == "valid") {
				t.Fatal("invalid owner snapshot admitted", mode)
			}
		})
	}
	set := PortForwardRetentionSet{Connections: []PortForwardConnectionIdentity{opening}}
	copy := set.Clone()
	copy.Connections[0].Generation++
	if set.Connections[0] != opening || set.Validate(0, nil) == nil || set.Validate(MaximumForwardPorts+1, nil) == nil {
		t.Fatal("snapshot ownership or slot bounds changed")
	}
}
