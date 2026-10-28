package runtimecontract

import "slices"

// ExecutionSnapshotTool names the bounded diagnostic read of the current execution.
// ExecutionSnapshotTool называет ограниченное диагностическое чтение текущего исполнения.
const ExecutionSnapshotTool = "get_execution_snapshot"

// RepositoryBundlePrepareTool names the specialized metadata-only publication tool in the closed producer/consumer catalog.
// RepositoryBundlePrepareTool задаёт имя специализированного инструмента публикации по метаданным в закрытом каталоге производителя и потребителя.
const RepositoryBundlePrepareTool = "prepare_repository_bundle_publication"

// RepositoryBundlePublishOperation binds catalog eligibility to the exact repository publication capability and operation.
// RepositoryBundlePublishOperation связывает допуск в каталог с точными capability и operation публикации репозитория.
const RepositoryBundlePublishOperation = "github.repository.bundle.publish"

// RuntimeRepositoryBundleAvailable advertises preparation only for an ordinary project turn with an exact human-approved write grant; owner-side fresh checks still authorize each invocation.
// RuntimeRepositoryBundleAvailable показывает подготовку только обычному проектному ходу с точным WRITE-разрешением и обязательным Human Gate; каждый вызов дополнительно проверяется владельцем состояния.
func RuntimeRepositoryBundleAvailable(input RunnerInput) bool {
	if input.Mode != RunnerModeTurn || input.AssistantScope != AssistantScopeNone || input.ProjectRef == "" {
		return false
	}
	for _, grant := range input.IntegrationGrants {
		if grant.CapabilityKey == RepositoryBundlePublishOperation && grant.Operation == RepositoryBundlePublishOperation && grant.Risk == "WRITE" && grant.ApprovalPolicy == "HUMAN_EACH_EFFECT" {
			return true
		}
	}
	return false
}

// RuntimeExecutionSnapshotAvailable exposes diagnostic read capability only for an active turn;
// control-plane RecordRunToolCall still checks the fresh lease before returning a result.
// RuntimeExecutionSnapshotAvailable открывает диагностическое чтение только для активного хода;
// control-plane RecordRunToolCall всё равно проверяет свежую аренду до выдачи результата.
func RuntimeExecutionSnapshotAvailable(input RunnerInput) bool {
	return input.Mode == RunnerModeTurn && input.LeaseRef != "" && input.LeaseFence != "" &&
		input.LeaseGeneration > 0 && BindProviderProcessObservation(input, "0.0.0").Validate() == nil
}

// RuntimeWorkflowLaunchAvailable binds producer and consumer to one ordinary immutable profile;
// the control plane checks current authority before the launch effect.
// RuntimeWorkflowLaunchAvailable связывает производителя и потребителя с одним обычным неизменным
// профилем; control plane проверяет текущие полномочия перед запуском.
func RuntimeWorkflowLaunchAvailable(input RunnerInput) bool {
	return input.Mode == RunnerModeTurn && input.AssistantScope == AssistantScopeNone &&
		input.ProjectRef != "" && slices.Contains(input.Capabilities, "platform.run.launch")
}

// RuntimeFileToolsAvailable checks an exact published VFS catalog in immutable execution input;
// this does not independently grant file authority.
// RuntimeFileToolsAvailable проверяет точный опубликованный каталог VFS в неизменном входе;
// сама по себе эта проверка не выдаёт полномочия на файлы.
func RuntimeFileToolsAvailable(input RunnerInput) bool {
	return input.Mode == RunnerModeTurn && input.ProjectRef != "" && input.LeaseRef != "" &&
		input.LeaseFence != "" && input.LeaseGeneration > 0 && input.FileCatalog != nil && input.FileCatalog.Validate() == nil
}

// RuntimeMCPToolNames returns the closed consumer tool set for the validated input;
// runtime-controller owns producer schemas and compares its actual tools/list with this profile.
// RuntimeMCPToolNames возвращает закрытый набор инструментов потребителя для проверенного входа;
// runtime-controller владеет схемами производителя и сверяет фактический tools/list с профилем.
func RuntimeMCPToolNames(input RunnerInput) []string {
	if ValidateManagedMCPProfiles(input) != nil {
		return nil
	}
	result := []string{"propose_run_metadata"}
	if RuntimeExecutionSnapshotAvailable(input) {
		result = append(result, ExecutionSnapshotTool)
	}
	if RuntimeFileToolsAvailable(input) {
		result = append(result, FileToolSearch, FileToolMetadata, FileToolPreview, FileToolManifest, FileToolRead)
	}
	if input.IsAssistant() {
		result = append(result, "get_configuration_catalog", "find_platform_resources", "read_task_session", "propose_configuration_plan", "propose_assistant_metadata")
	}
	if len(input.DelegationTargets) != 0 {
		result = append(result, "delegate_agent")
	}
	if RuntimeWorkflowLaunchAvailable(input) {
		result = append(result, "get_workflow_catalog", "launch_workflow")
	}
	if len(input.IntegrationGrants) != 0 {
		result = append(result, "get_integration_catalog", "invoke_integration")
	}
	if RuntimeRepositoryBundleAvailable(input) {
		result = append(result, RepositoryBundlePrepareTool)
	}
	if len(input.ManagedMCPProfiles) != 0 {
		result = append(result, Context7ResolveTool, Context7QueryTool)
	}
	return result
}
