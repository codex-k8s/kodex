
package generated

type ArtifactSource uint

const (
  ArtifactSourceControlCenter ArtifactSource = iota
  ArtifactSourceAgentResult
  ArtifactSourceIntegrationResult
  ArtifactSourceKnowledgeSource
  ArtifactSourceInteractionAttachment
)

// Value returns the value of the enum.
func (op ArtifactSource) Value() any {
	if op >= ArtifactSource(len(ArtifactSourceValues)) {
		return nil
	}
	return ArtifactSourceValues[op]
}

var ArtifactSourceValues = []any{"CONTROL_CENTER","AGENT_RESULT","INTEGRATION_RESULT","KNOWLEDGE_SOURCE","INTERACTION_ATTACHMENT"}
var ValuesToArtifactSource = map[any]ArtifactSource{
  ArtifactSourceValues[ArtifactSourceControlCenter]: ArtifactSourceControlCenter,
  ArtifactSourceValues[ArtifactSourceAgentResult]: ArtifactSourceAgentResult,
  ArtifactSourceValues[ArtifactSourceIntegrationResult]: ArtifactSourceIntegrationResult,
  ArtifactSourceValues[ArtifactSourceKnowledgeSource]: ArtifactSourceKnowledgeSource,
  ArtifactSourceValues[ArtifactSourceInteractionAttachment]: ArtifactSourceInteractionAttachment,
}
