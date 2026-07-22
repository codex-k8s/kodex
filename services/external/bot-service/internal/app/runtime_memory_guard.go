package app

import (
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

var kubernetesMemoryQuantityPattern = regexp.MustCompile(`^[1-9][0-9]{0,8}(Ki|Mi|Gi|Ti)$`)

func (cfg Config) validateRuntimeMemoryGuard() error {
	if !cfg.RuntimeEnabled {
		return nil
	}
	if !cfg.RuntimeLimitsEnabled {
		return fmt.Errorf("MATTERCODEX_RUNTIME_LIMITS_ENABLED must remain true when agent runtime is enabled")
	}
	nodeMemory, err := parseRuntimeMemoryQuantity("MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY", cfg.RuntimeNodeAllocatableMemory)
	if err != nil {
		return err
	}
	agentBudget, err := parseRuntimeMemoryQuantity("MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET", cfg.RuntimeAgentMemoryBudget)
	if err != nil {
		return err
	}
	systemReserve, err := parseRuntimeMemoryQuantity("MATTERCODEX_RUNTIME_SYSTEM_MEMORY_RESERVE", cfg.RuntimeSystemMemoryReserve)
	if err != nil {
		return err
	}
	sessionRequest, err := parseRuntimeMemoryQuantity("MATTERCODEX_AGENT_SESSION_MEMORY_REQUEST", cfg.AgentSessionMemoryRequest)
	if err != nil {
		return err
	}
	sessionLimit, err := parseRuntimeMemoryQuantity("MATTERCODEX_AGENT_SESSION_MEMORY_LIMIT", cfg.AgentSessionMemoryLimit)
	if err != nil {
		return err
	}
	utilityLimit, err := parseRuntimeMemoryQuantity("MATTERCODEX_AGENT_UTILITY_MEMORY_LIMIT", cfg.AgentUtilityMemoryLimit)
	if err != nil {
		return err
	}
	devShmLimit, err := parseRuntimeMemoryQuantity("MATTERCODEX_AGENT_DEV_SHM_SIZE_LIMIT", cfg.AgentDevShmSizeLimit)
	if err != nil {
		return err
	}
	if sessionRequest.Cmp(sessionLimit) != 0 {
		return fmt.Errorf("session memory request must equal session memory limit")
	}
	if sessionLimit.Cmp(agentBudget) > 0 || utilityLimit.Cmp(agentBudget) > 0 {
		return fmt.Errorf("individual agent memory limit exceeds aggregate agent memory budget")
	}
	if devShmLimit.Cmp(sessionLimit) > 0 {
		return fmt.Errorf("agent dev shm size limit exceeds session memory limit")
	}
	totalReserved := agentBudget.DeepCopy()
	totalReserved.Add(systemReserve)
	if totalReserved.Cmp(nodeMemory) > 0 {
		return fmt.Errorf("aggregate agent memory budget and system reserve exceed node allocatable memory")
	}
	priorityClass := strings.TrimSpace(cfg.AgentWorkloadPriorityClass)
	if len(priorityClass) == 0 || len(priorityClass) > 63 || !kubernetesDNSLabelPattern.MatchString(priorityClass) {
		return fmt.Errorf("MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS is invalid")
	}
	return nil
}

func parseRuntimeMemoryQuantity(name string, value string) (resource.Quantity, error) {
	value = strings.TrimSpace(value)
	if !kubernetesMemoryQuantityPattern.MatchString(value) {
		return resource.Quantity{}, fmt.Errorf("%s must be a positive Kubernetes memory quantity with Ki, Mi, Gi or Ti suffix", name)
	}
	quantity, err := resource.ParseQuantity(value)
	if err != nil || quantity.Sign() <= 0 {
		return resource.Quantity{}, fmt.Errorf("%s is invalid", name)
	}
	return quantity, nil
}
