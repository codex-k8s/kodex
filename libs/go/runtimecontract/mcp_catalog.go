package runtimecontract

import "slices"

// RuntimeWorkflowLaunchAvailable связывает producer и consumer с одним exact
// ordinary-профилем immutable input. Текущую authority перед эффектом проверяет CP.
func RuntimeWorkflowLaunchAvailable(input RunnerInput) bool {
	return input.Mode == RunnerModeTurn && input.AssistantScope == AssistantScopeNone &&
		input.ProjectRef != "" && slices.Contains(input.Capabilities, "platform.run.launch")
}

// RuntimeFileToolsAvailable сохраняет exact профиль опубликованного VFS catalog.
// Это проверка immutable execution input, не самостоятельный источник authority.
func RuntimeFileToolsAvailable(input RunnerInput) bool {
	return input.Mode == RunnerModeTurn && input.ProjectRef != "" && input.LeaseRef != "" &&
		input.LeaseFence != "" && input.LeaseGeneration > 0 && input.FileCatalog != nil && input.FileCatalog.Validate() == nil
}

// RuntimeMCPToolNames — закрытый ожидаемый набор инструментов для данного input.
// Producer schema остаются у runtime-controller; wire regression проверяет его
// фактический tools/list против этого consumer-профиля.
func RuntimeMCPToolNames(input RunnerInput) []string {
	if ValidateManagedMCPProfiles(input) != nil {
		return nil
	}
	result := []string{"propose_run_metadata"}
	if RuntimeFileToolsAvailable(input) {
		result = append(result, FileToolSearch, FileToolMetadata, FileToolPreview, FileToolManifest)
	}
	if input.IsAssistant() {
		result = append(result, "get_configuration_catalog", "find_platform_resources", "propose_configuration_plan", "propose_assistant_metadata")
	}
	if len(input.DelegationTargets) != 0 {
		result = append(result, "delegate_agent")
	}
	if RuntimeWorkflowLaunchAvailable(input) {
		result = append(result, "launch_workflow")
	}
	if len(input.IntegrationGrants) != 0 {
		result = append(result, "get_integration_catalog", "invoke_integration")
	}
	if len(input.ManagedMCPProfiles) != 0 {
		result = append(result, Context7ResolveTool, Context7QueryTool)
	}
	return result
}
