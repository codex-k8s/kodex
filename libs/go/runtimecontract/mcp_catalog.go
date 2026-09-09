package runtimecontract

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
	result := []string{"propose_run_metadata"}
	if RuntimeFileToolsAvailable(input) {
		result = append(result, FileToolSearch, FileToolMetadata, FileToolPreview, FileToolManifest)
	}
	if input.SystemAssistant {
		result = append(result, "get_configuration_catalog", "propose_configuration_plan", "propose_assistant_metadata")
	}
	if len(input.DelegationTargets) != 0 {
		result = append(result, "delegate_agent")
	}
	if len(input.IntegrationGrants) != 0 {
		result = append(result, "invoke_integration")
	}
	return result
}
