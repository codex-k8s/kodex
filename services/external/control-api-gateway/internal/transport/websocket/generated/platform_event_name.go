
package generated

type PlatformEventName uint

const (
  PlatformEventNameProjectChanged PlatformEventName = iota
  PlatformEventNameAgentChanged
  PlatformEventNameArtifactChanged
  PlatformEventNameInstructionsPublished
  PlatformEventNameWorkflowChanged
  PlatformEventNameScheduleChanged
  PlatformEventNameIntegrationConnectionChanged
  PlatformEventNameIntegrationGrantChanged
  PlatformEventNameMembershipChanged
  PlatformEventNameSystemAssistantChanged
  PlatformEventNameRoleImageRecipeChanged
)

// Value returns the value of the enum.
func (op PlatformEventName) Value() any {
	if op >= PlatformEventName(len(PlatformEventNameValues)) {
		return nil
	}
	return PlatformEventNameValues[op]
}

var PlatformEventNameValues = []any{"PROJECT_CHANGED","AGENT_CHANGED","ARTIFACT_CHANGED","INSTRUCTIONS_PUBLISHED","WORKFLOW_CHANGED","SCHEDULE_CHANGED","INTEGRATION_CONNECTION_CHANGED","INTEGRATION_GRANT_CHANGED","MEMBERSHIP_CHANGED","SYSTEM_ASSISTANT_CHANGED","ROLE_IMAGE_RECIPE_CHANGED"}
var ValuesToPlatformEventName = map[any]PlatformEventName{
  PlatformEventNameValues[PlatformEventNameProjectChanged]: PlatformEventNameProjectChanged,
  PlatformEventNameValues[PlatformEventNameAgentChanged]: PlatformEventNameAgentChanged,
  PlatformEventNameValues[PlatformEventNameArtifactChanged]: PlatformEventNameArtifactChanged,
  PlatformEventNameValues[PlatformEventNameInstructionsPublished]: PlatformEventNameInstructionsPublished,
  PlatformEventNameValues[PlatformEventNameWorkflowChanged]: PlatformEventNameWorkflowChanged,
  PlatformEventNameValues[PlatformEventNameScheduleChanged]: PlatformEventNameScheduleChanged,
  PlatformEventNameValues[PlatformEventNameIntegrationConnectionChanged]: PlatformEventNameIntegrationConnectionChanged,
  PlatformEventNameValues[PlatformEventNameIntegrationGrantChanged]: PlatformEventNameIntegrationGrantChanged,
  PlatformEventNameValues[PlatformEventNameMembershipChanged]: PlatformEventNameMembershipChanged,
  PlatformEventNameValues[PlatformEventNameSystemAssistantChanged]: PlatformEventNameSystemAssistantChanged,
  PlatformEventNameValues[PlatformEventNameRoleImageRecipeChanged]: PlatformEventNameRoleImageRecipeChanged,
}
