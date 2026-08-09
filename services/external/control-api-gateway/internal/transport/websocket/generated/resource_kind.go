
package generated

type ResourceKind uint

const (
  ResourceKindProject ResourceKind = iota
  ResourceKindTeam
  ResourceKindChat
  ResourceKindRole
  ResourceKindPromptProfile
  ResourceKindCredentialBinding
  ResourceKindRepositoryWorkspace
  ResourceKindIntegration
  ResourceKindRuntimeRevision
  ResourceKindSession
  ResourceKindTurn
  ResourceKindProcessRun
  ResourceKindSchedule
  ResourceKindOwnerGate
  ResourceKindMemoryRecord
  ResourceKindWorkClaim
  ResourceKindArtifact
  ResourceKindRoleImageRecipe
  ResourceKindImageBuild
  ResourceKindImageArtifact
  ResourceKindRoleDefinition
  ResourceKindAgent
  ResourceKindAgentAssignment
  ResourceKindInstructionSet
  ResourceKindProviderConnectionReference
  ResourceKindProviderPool
  ResourceKindWorkspaceBackup
  ResourceKindWorkspaceRestore
  ResourceKindWorkspaceMattermostMapping
)

// Value returns the value of the enum.
func (op ResourceKind) Value() any {
	if op >= ResourceKind(len(ResourceKindValues)) {
		return nil
	}
	return ResourceKindValues[op]
}

var ResourceKindValues = []any{"PROJECT","TEAM","CHAT","ROLE","PROMPT_PROFILE","CREDENTIAL_BINDING","REPOSITORY_WORKSPACE","INTEGRATION","RUNTIME_REVISION","SESSION","TURN","PROCESS_RUN","SCHEDULE","OWNER_GATE","MEMORY_RECORD","WORK_CLAIM","ARTIFACT","ROLE_IMAGE_RECIPE","IMAGE_BUILD","IMAGE_ARTIFACT","ROLE_DEFINITION","AGENT","AGENT_ASSIGNMENT","INSTRUCTION_SET","PROVIDER_CONNECTION_REFERENCE","PROVIDER_POOL","WORKSPACE_BACKUP","WORKSPACE_RESTORE","WORKSPACE_MATTERMOST_MAPPING"}
var ValuesToResourceKind = map[any]ResourceKind{
  ResourceKindValues[ResourceKindProject]: ResourceKindProject,
  ResourceKindValues[ResourceKindTeam]: ResourceKindTeam,
  ResourceKindValues[ResourceKindChat]: ResourceKindChat,
  ResourceKindValues[ResourceKindRole]: ResourceKindRole,
  ResourceKindValues[ResourceKindPromptProfile]: ResourceKindPromptProfile,
  ResourceKindValues[ResourceKindCredentialBinding]: ResourceKindCredentialBinding,
  ResourceKindValues[ResourceKindRepositoryWorkspace]: ResourceKindRepositoryWorkspace,
  ResourceKindValues[ResourceKindIntegration]: ResourceKindIntegration,
  ResourceKindValues[ResourceKindRuntimeRevision]: ResourceKindRuntimeRevision,
  ResourceKindValues[ResourceKindSession]: ResourceKindSession,
  ResourceKindValues[ResourceKindTurn]: ResourceKindTurn,
  ResourceKindValues[ResourceKindProcessRun]: ResourceKindProcessRun,
  ResourceKindValues[ResourceKindSchedule]: ResourceKindSchedule,
  ResourceKindValues[ResourceKindOwnerGate]: ResourceKindOwnerGate,
  ResourceKindValues[ResourceKindMemoryRecord]: ResourceKindMemoryRecord,
  ResourceKindValues[ResourceKindWorkClaim]: ResourceKindWorkClaim,
  ResourceKindValues[ResourceKindArtifact]: ResourceKindArtifact,
  ResourceKindValues[ResourceKindRoleImageRecipe]: ResourceKindRoleImageRecipe,
  ResourceKindValues[ResourceKindImageBuild]: ResourceKindImageBuild,
  ResourceKindValues[ResourceKindImageArtifact]: ResourceKindImageArtifact,
  ResourceKindValues[ResourceKindRoleDefinition]: ResourceKindRoleDefinition,
  ResourceKindValues[ResourceKindAgent]: ResourceKindAgent,
  ResourceKindValues[ResourceKindAgentAssignment]: ResourceKindAgentAssignment,
  ResourceKindValues[ResourceKindInstructionSet]: ResourceKindInstructionSet,
  ResourceKindValues[ResourceKindProviderConnectionReference]: ResourceKindProviderConnectionReference,
  ResourceKindValues[ResourceKindProviderPool]: ResourceKindProviderPool,
  ResourceKindValues[ResourceKindWorkspaceBackup]: ResourceKindWorkspaceBackup,
  ResourceKindValues[ResourceKindWorkspaceRestore]: ResourceKindWorkspaceRestore,
  ResourceKindValues[ResourceKindWorkspaceMattermostMapping]: ResourceKindWorkspaceMattermostMapping,
}
