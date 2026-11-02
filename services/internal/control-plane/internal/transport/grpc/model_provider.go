package grpc

import (
	"encoding/json"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// castSnapshotModelProvider closes malformed owner snapshots instead of omitting configured transport pins.
// Absent legacy configuration remains nil; this read projection does not grant credential access.
// castSnapshotModelProvider отклоняет неверный owner snapshot вместо потери configured transport pins.
// Отсутствующая legacy configuration остаётся nil; read projection не выдаёт credential access.
func castSnapshotModelProvider(values map[string]any) (*controlplanev1.RuntimeModelProviderConfiguration, bool) {
	value := values["modelProvider"]
	if value == nil {
		return nil, true
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	config, err := runtimecontract.DecodeModelProviderConfiguration(raw)
	if err != nil {
		return nil, false
	}
	result := &controlplanev1.RuntimeModelProviderConfiguration{Schema: config.Schema, ManagedBy: config.ManagedBy,
		SourceRevision: config.SourceRevision, Engine: config.Engine, EngineVersion: config.EngineVersion,
		ToolMode: config.ToolMode, ProviderId: config.ProviderID, BaseUrl: config.BaseURL,
		AddressCidrs: append([]string(nil), config.AddressCIDRs...)}
	for _, model := range config.Models {
		result.Models = append(result.Models, &controlplanev1.RuntimeModelProviderModel{Id: model.ID,
			ContextWindowTokens: int64(model.ContextWindowTokens), MaximumOutputTokens: int64(model.MaximumOutputTokens)})
	}
	return result, true
}
