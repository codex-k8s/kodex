package platform

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// TestRuntimeCapacityRejectsStaleOrUnboundedObservation proves nil legacy differs from explicit zero capacity.
// Invalid resource amounts, future/stale observations and multiple claims cannot bypass the queue.
// TestRuntimeCapacityRejectsStaleOrUnboundedObservation различает legacy nil и явное отсутствие ёмкости.
// Неверные quantities, будущие/устаревшие снимки и несколько claims не обходят очередь.
func TestRuntimeCapacityRejectsStaleOrUnboundedObservation(t *testing.T) {
	now := time.Now()
	if raw, err := runtimeCapacityJSON(nil, 2, now); err != nil || raw != nil {
		t.Fatal("legacy claim changed")
	}
	fixture := func() *command.RuntimeSchedulingCapacity {
		return &command.RuntimeSchedulingCapacity{ObservedAt: now, Overhead: command.RuntimeNodeCapacity{CPU: 50, Memory: 128 << 20, Pods: 1}}
	}
	raw, err := runtimeCapacityJSON(fixture(), 1, now)
	var decoded map[string]json.RawMessage
	if err != nil || json.Unmarshal(raw, &decoded) != nil || string(decoded["nodes"]) != "[]" {
		t.Fatal("empty nodes became legacy capacity")
	}
	for _, test := range []struct {
		name   string
		change func(*command.RuntimeSchedulingCapacity)
		limit  int32
	}{
		{"stale", func(v *command.RuntimeSchedulingCapacity) { v.ObservedAt = now.Add(-21 * time.Second) }, 1},
		{"future", func(v *command.RuntimeSchedulingCapacity) { v.ObservedAt = now.Add(2 * time.Second) }, 1},
		{"negative", func(v *command.RuntimeSchedulingCapacity) { v.Overhead.CPU = -1 }, 1},
		{"missing pod slot", func(v *command.RuntimeSchedulingCapacity) { v.Overhead.Pods = 0 }, 1},
		{"too many nodes", func(v *command.RuntimeSchedulingCapacity) { v.Nodes = make([]command.RuntimeNodeCapacity, 513) }, 1},
		{"invented availability", func(v *command.RuntimeSchedulingCapacity) { v.Nodes = []command.RuntimeNodeCapacity{{Memory: 1}} }, 1},
		{"batch", func(v *command.RuntimeSchedulingCapacity) {}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := fixture()
			test.change(v)
			if _, err := runtimeCapacityJSON(v, test.limit, now); err == nil {
				t.Fatal("invalid capacity admitted")
			}
		})
	}
}
