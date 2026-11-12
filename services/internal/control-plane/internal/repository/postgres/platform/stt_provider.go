package platform

import (
	"encoding/json"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// openAITranscriptionCapabilities checks the same server-owned exact route for settings readiness and secret projection.
// openAITranscriptionCapabilities проверяет один серверный точный маршрут для готовности настроек и выдачи секрета.
func openAITranscriptionCapabilities(raw []byte) bool {
	var capabilities struct {
		ModelProvider json.RawMessage `json:"modelProvider"`
	}
	if json.Unmarshal(raw, &capabilities) != nil {
		return false
	}
	config, err := runtimecontract.DecodeModelProviderConfiguration(capabilities.ModelProvider)
	return err == nil && config.SupportsOpenAITranscription()
}
