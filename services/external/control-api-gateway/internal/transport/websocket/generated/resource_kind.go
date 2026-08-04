
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
)

// Value returns the value of the enum.
func (op ResourceKind) Value() any {
	if op >= ResourceKind(len(ResourceKindValues)) {
		return nil
	}
	return ResourceKindValues[op]
}

var ResourceKindValues = []any{"PROJECT","TEAM","CHAT","ROLE","PROMPT_PROFILE","CREDENTIAL_BINDING","REPOSITORY_WORKSPACE","INTEGRATION","RUNTIME_REVISION","SESSION","TURN","PROCESS_RUN","SCHEDULE","OWNER_GATE","MEMORY_RECORD","WORK_CLAIM","ARTIFACT"}
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
}
