package app

import "testing"

func TestRuntimeMemoryGuardValidation(t *testing.T) {
	if _, err := parseRuntimeMemoryQuantity("MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY", "131861544Ki"); err != nil {
		t.Fatalf("allocatable Ki quantity: %v", err)
	}
	valid := Config{
		RuntimeEnabled:               true,
		RuntimeLimitsEnabled:         true,
		RuntimeNodeAllocatableMemory: "120Gi",
		RuntimeAgentMemoryBudget:     "80Gi",
		RuntimeSystemMemoryReserve:   "40Gi",
		AgentSessionMemoryRequest:    "8Gi",
		AgentSessionMemoryLimit:      "8Gi",
		AgentUtilityMemoryLimit:      "4Gi",
		AgentDevShmSizeLimit:         "2Gi",
		AgentWorkloadPriorityClass:   "matter-codex-agent-workload",
	}
	if err := valid.validateRuntimeMemoryGuard(); err != nil {
		t.Fatalf("valid memory guard: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "guard выключен", mutate: func(cfg *Config) { cfg.RuntimeLimitsEnabled = false }},
		{name: "aggregate budget отсутствует", mutate: func(cfg *Config) { cfg.RuntimeAgentMemoryBudget = "" }},
		{name: "quantity некорректен", mutate: func(cfg *Config) { cfg.RuntimeNodeAllocatableMemory = "120G" }},
		{name: "scheduler request ниже limit", mutate: func(cfg *Config) { cfg.AgentSessionMemoryRequest = "4Gi" }},
		{name: "aggregate invariant нарушен", mutate: func(cfg *Config) { cfg.RuntimeAgentMemoryBudget = "81Gi" }},
		{name: "dev shm превышает limit", mutate: func(cfg *Config) { cfg.AgentDevShmSizeLimit = "9Gi" }},
		{name: "priority class некорректен", mutate: func(cfg *Config) { cfg.AgentWorkloadPriorityClass = "Invalid class" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid
			test.mutate(&cfg)
			if err := cfg.validateRuntimeMemoryGuard(); err == nil {
				t.Fatal("validateRuntimeMemoryGuard() error = nil")
			}
		})
	}
}

func TestRuntimeMemoryQuantityRangeBoundary(t *testing.T) {
	tests := []struct {
		value     string
		wantError bool
	}{
		{value: "8388607Ti"},
		{value: "8388608Ti", wantError: true},
		{value: "999999999Ti", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			_, err := parseRuntimeMemoryQuantity("MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY", test.value)
			if (err != nil) != test.wantError {
				t.Fatalf("parseRuntimeMemoryQuantity(%q) error = %v, wantError=%t", test.value, err, test.wantError)
			}
		})
	}

	boundary := Config{
		RuntimeEnabled:               true,
		RuntimeLimitsEnabled:         true,
		RuntimeNodeAllocatableMemory: "8388607Ti",
		RuntimeAgentMemoryBudget:     "8388606Ti",
		RuntimeSystemMemoryReserve:   "1Ti",
		AgentSessionMemoryRequest:    "1Ti",
		AgentSessionMemoryLimit:      "1Ti",
		AgentUtilityMemoryLimit:      "1Ti",
		AgentDevShmSizeLimit:         "1Ti",
		AgentWorkloadPriorityClass:   "matter-codex-agent-workload",
	}
	if err := boundary.validateRuntimeMemoryGuard(); err != nil {
		t.Fatalf("boundary validateRuntimeMemoryGuard() error = %v", err)
	}
	boundary.RuntimeNodeAllocatableMemory = "8388608Ti"
	if err := boundary.validateRuntimeMemoryGuard(); err == nil {
		t.Fatal("validateRuntimeMemoryGuard() accepted quantity above the common range")
	}
}
