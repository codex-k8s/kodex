package platform

import (
	"encoding/json"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// runtimeCapacityJSON validates bounded fresh scheduling observations before candidate selection.
// Nil preserves old callers; an explicitly empty node list remains a closed queue, never legacy.
// runtimeCapacityJSON проверяет ограниченный свежий снимок до выбора кандидата.
// nil сохраняет старых клиентов; явно пустой список оставляет очередь закрытой без legacy fallback.
func runtimeCapacityJSON(input *command.RuntimeSchedulingCapacity, limit int32, now time.Time) ([]byte, error) {
	if input == nil {
		return nil, nil
	}
	if limit != 1 || len(input.Nodes) > 512 || input.ObservedAt.IsZero() || input.ObservedAt.After(now.Add(time.Second)) || now.Sub(input.ObservedAt) > 20*time.Second {
		return nil, errs.ErrInvalid
	}
	valid := func(v command.RuntimeNodeCapacity) bool {
		return v.CPU >= 0 && v.CPU <= 1000000000 && v.Memory >= 0 && v.Memory <= 1<<60 && v.Ephemeral >= 0 && v.Ephemeral <= 1<<60 && v.Pods >= 0 && v.Pods <= 1000000
	}
	if !valid(input.Overhead) || input.Overhead.Pods != 1 || input.Overhead.TotalCPU != 0 || input.Overhead.TotalMemory != 0 || input.Overhead.TotalEphemeral != 0 || input.Overhead.TotalPods != 0 {
		return nil, errs.ErrInvalid
	}
	for _, node := range input.Nodes {
		total := command.RuntimeNodeCapacity{CPU: node.TotalCPU, Memory: node.TotalMemory, Ephemeral: node.TotalEphemeral, Pods: node.TotalPods}
		if !valid(node) || !valid(total) || node.CPU > total.CPU || node.Memory > total.Memory || node.Ephemeral > total.Ephemeral || node.Pods > total.Pods {
			return nil, errs.ErrInvalid
		}
	}
	nodes := input.Nodes
	if nodes == nil {
		nodes = []command.RuntimeNodeCapacity{}
	}
	return json.Marshal(struct {
		Nodes    []command.RuntimeNodeCapacity `json:"nodes"`
		Overhead command.RuntimeNodeCapacity   `json:"overhead"`
	}{nodes, input.Overhead})
}
