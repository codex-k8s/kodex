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
		ToolMode: config.ToolMode, ProviderId: config.ProviderID, BaseUrl: config.BaseURL, Api: config.API, RouteKind: config.RouteKind, RouteVersion: config.RouteVersion,
		AddressCidrs: append([]string(nil), config.AddressCIDRs...)}
	for _, model := range config.Models {
		result.Models = append(result.Models, &controlplanev1.RuntimeModelProviderModel{Id: model.ID,
			ContextWindowTokens: int64(model.ContextWindowTokens), MaximumOutputTokens: int64(model.MaximumOutputTokens), CachePolicy: castModelCachePolicy(model.CachePolicy), ContextPolicy: castModelContextPolicy(model.ContextPolicy)})
	}
	return result, true
}

// castModelCachePolicy preserves the complete nonsecret requested policy and operator attestation without deriving enforcement guarantees.
// castModelCachePolicy сохраняет полную requested policy и operator attestation без самостоятельного вывода guarantees.
func castModelCachePolicy(value *runtimecontract.ModelCachePolicy) *controlplanev1.RuntimeModelCachePolicy {
	if value == nil {
		return nil
	}
	q := value.Qualification
	return &controlplanev1.RuntimeModelCachePolicy{Revision: int32(value.Revision), Optimization: value.Optimization, ReuseScope: value.ReuseScope, NoReuse: value.NoReuse, NoStore: value.NoStore, Required: value.Required, Qualification: &controlplanev1.RuntimeModelCacheQualification{Mechanism: q.Mechanism, Status: q.Status, Source: q.Source, BackendVersion: q.BackendVersion, EvidenceSha256: q.EvidenceSHA256, RouteSha256: q.RouteSHA256}}
}

// castModelContextPolicy retains nil versus an explicit owner choice; defaults are applied only by the shared effective-policy function.
// castModelContextPolicy сохраняет nil и explicit owner choice; defaults применяет только shared effective-policy function.
func castModelContextPolicy(value *runtimecontract.ModelContextPolicy) *controlplanev1.RuntimeModelContextPolicy {
	if value == nil {
		return nil
	}
	return &controlplanev1.RuntimeModelContextPolicy{Revision: int32(value.Revision), AutoCompaction: value.AutoCompaction, Prune: value.Prune}
}

// castModelExecutionPolicies exposes the same policy resolver consumed before inference, without endpoints, credentials or namespaces.
// castModelExecutionPolicies раскрывает тот же policy resolver, что применяется до inference, без endpoints, credentials или namespaces.
func castModelExecutionPolicies(value runtimecontract.ModelProviderConfiguration) []*controlplanev1.ModelExecutionPolicy {
	result := make([]*controlplanev1.ModelExecutionPolicy, 0, len(value.Models))
	for _, model := range value.Models {
		effective, err := value.ResolveModelCache(model.ID)
		context := model.EffectiveContextPolicy()
		result = append(result, &controlplanev1.ModelExecutionPolicy{ModelId: model.ID, ContextWindowTokens: int64(model.ContextWindowTokens), MaximumOutputTokens: int64(model.MaximumOutputTokens), Context: castModelContextPolicy(&context), Eligible: err == nil, Cache: &controlplanev1.ModelCacheResolution{Requested: castModelCachePolicy(effective.Requested), Mechanism: effective.Mechanism, ReuseScope: effective.ReuseScope, Status: effective.Status, Reason: effective.Reason, NoReuse: effective.NoReuse, NoStore: effective.NoStore, ProxyResponseCache: effective.ProxyResponseCache, PhysicalIsolation: effective.PhysicalIsolation}})
	}
	return result
}
