
package generated

type PlatformResourceKind uint

const (
  PlatformResourceKindProject PlatformResourceKind = iota
  PlatformResourceKindAgent
  PlatformResourceKindArtifact
  PlatformResourceKindInstructions
  PlatformResourceKindWorkflow
  PlatformResourceKindSchedule
  PlatformResourceKindIntegrationConnection
  PlatformResourceKindIntegrationGrant
  PlatformResourceKindMembership
  PlatformResourceKindSystemAssistant
  PlatformResourceKindRoleImageRecipe
)

// Value returns the value of the enum.
func (op PlatformResourceKind) Value() any {
	if op >= PlatformResourceKind(len(PlatformResourceKindValues)) {
		return nil
	}
	return PlatformResourceKindValues[op]
}

var PlatformResourceKindValues = []any{"PROJECT","AGENT","ARTIFACT","INSTRUCTIONS","WORKFLOW","SCHEDULE","INTEGRATION_CONNECTION","INTEGRATION_GRANT","MEMBERSHIP","SYSTEM_ASSISTANT","ROLE_IMAGE_RECIPE"}
var ValuesToPlatformResourceKind = map[any]PlatformResourceKind{
  PlatformResourceKindValues[PlatformResourceKindProject]: PlatformResourceKindProject,
  PlatformResourceKindValues[PlatformResourceKindAgent]: PlatformResourceKindAgent,
  PlatformResourceKindValues[PlatformResourceKindArtifact]: PlatformResourceKindArtifact,
  PlatformResourceKindValues[PlatformResourceKindInstructions]: PlatformResourceKindInstructions,
  PlatformResourceKindValues[PlatformResourceKindWorkflow]: PlatformResourceKindWorkflow,
  PlatformResourceKindValues[PlatformResourceKindSchedule]: PlatformResourceKindSchedule,
  PlatformResourceKindValues[PlatformResourceKindIntegrationConnection]: PlatformResourceKindIntegrationConnection,
  PlatformResourceKindValues[PlatformResourceKindIntegrationGrant]: PlatformResourceKindIntegrationGrant,
  PlatformResourceKindValues[PlatformResourceKindMembership]: PlatformResourceKindMembership,
  PlatformResourceKindValues[PlatformResourceKindSystemAssistant]: PlatformResourceKindSystemAssistant,
  PlatformResourceKindValues[PlatformResourceKindRoleImageRecipe]: PlatformResourceKindRoleImageRecipe,
}
