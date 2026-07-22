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
