package controlplaneapi

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var agentBotMappingProofNamespace = uuid.MustParse("64e49c9e-7613-50dc-b5cc-95ffecdad937")

// AgentBotMappingProofRef строит непрозрачное доказательство exact current
// WorkspaceMattermostMapping tuple без переноса raw provider Team ID.
func AgentBotMappingProofRef(mappingID string, resourceVersion, mappingGeneration,
	providerEffectVersion, providerEffectGeneration uint64,
) (string, error) {
	if uuid.Validate(mappingID) != nil || resourceVersion == 0 || mappingGeneration == 0 ||
		providerEffectVersion == 0 || providerEffectGeneration == 0 {
		return "", errors.New("agent bot mapping proof tuple is invalid")
	}
	tuple := fmt.Sprintf("agent-bot-mapping-proof-v1\x00%s\x00%d\x00%d\x00%d\x00%d", mappingID,
		resourceVersion, mappingGeneration, providerEffectVersion, providerEffectGeneration)
	return uuid.NewSHA1(agentBotMappingProofNamespace, []byte(tuple)).String(), nil
}
