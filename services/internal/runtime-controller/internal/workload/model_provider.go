package workload

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// runtimeModelProvider validates the complete typed owner projection before materializing RunnerInput.
// It rejects unknown Proto fields, nil models and integer overflow; it does not infer endpoint defaults.
// runtimeModelProvider проверяет typed owner projection до materialization RunnerInput.
// Unknown Proto fields, nil models и integer overflow отклоняются; endpoint defaults не выводятся.
func runtimeModelProvider(value *controlplanev1.RuntimeModelProviderConfiguration) (*runtimecontract.ModelProviderConfiguration, error) {
	if value == nil {
		return nil, nil
	}
	if len(value.ProtoReflect().GetUnknown()) != 0 || len(value.GetModels()) > 128 {
		return nil, runtimecontract.ErrModelProviderConfiguration
	}
	config := &runtimecontract.ModelProviderConfiguration{Schema: value.GetSchema(), ManagedBy: value.GetManagedBy(),
		SourceRevision: value.GetSourceRevision(), Engine: value.GetEngine(), EngineVersion: value.GetEngineVersion(),
		ToolMode: value.GetToolMode(), ProviderID: value.GetProviderId(), BaseURL: value.GetBaseUrl(),
		AddressCIDRs: append([]string(nil), value.GetAddressCidrs()...)}
	for _, model := range value.GetModels() {
		if model == nil || len(model.ProtoReflect().GetUnknown()) != 0 || model.GetContextWindowTokens() < 1 ||
			model.GetContextWindowTokens() > 2_000_000 || model.GetMaximumOutputTokens() < 1 ||
			model.GetMaximumOutputTokens() > model.GetContextWindowTokens() {
			return nil, runtimecontract.ErrModelProviderConfiguration
		}
		config.Models = append(config.Models, runtimecontract.ModelProviderModel{ID: model.GetId(),
			ContextWindowTokens: int(model.GetContextWindowTokens()), MaximumOutputTokens: int(model.GetMaximumOutputTokens())})
	}
	if config.Validate() != nil {
		return nil, runtimecontract.ErrModelProviderConfiguration
	}
	return config, nil
}
