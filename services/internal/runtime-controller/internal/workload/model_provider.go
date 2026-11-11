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
		ToolMode: value.GetToolMode(), ProviderID: value.GetProviderId(), BaseURL: value.GetBaseUrl(), API: value.GetApi(), RouteKind: value.GetRouteKind(), RouteVersion: value.GetRouteVersion(),
		AddressCIDRs: append([]string(nil), value.GetAddressCidrs()...)}
	for _, model := range value.GetModels() {
		if model == nil || len(model.ProtoReflect().GetUnknown()) != 0 || model.GetContextWindowTokens() < 1 ||
			model.GetContextWindowTokens() > 2_000_000 || model.GetMaximumOutputTokens() < 1 ||
			model.GetMaximumOutputTokens() > model.GetContextWindowTokens() {
			return nil, runtimecontract.ErrModelProviderConfiguration
		}
		policy, err := runtimeModelCachePolicy(model.GetCachePolicy())
		if err != nil {
			return nil, err
		}
		var contextPolicy *runtimecontract.ModelContextPolicy
		if context := model.GetContextPolicy(); context != nil {
			if len(context.ProtoReflect().GetUnknown()) != 0 {
				return nil, runtimecontract.ErrModelProviderConfiguration
			}
			contextPolicy = &runtimecontract.ModelContextPolicy{Revision: int(context.GetRevision()), AutoCompaction: context.GetAutoCompaction(), Prune: context.GetPrune()}
		}
		config.Models = append(config.Models, runtimecontract.ModelProviderModel{ID: model.GetId(), CachePolicy: policy, ContextPolicy: contextPolicy,
			ContextWindowTokens: int(model.GetContextWindowTokens()), MaximumOutputTokens: int(model.GetMaximumOutputTokens())})
	}
	if config.Validate() != nil {
		return nil, runtimecontract.ErrModelProviderConfiguration
	}
	return config, nil
}

// runtimeModelCachePolicy rejects unknown nested fields instead of silently dropping a mandatory guarantee at the controller boundary.
// runtimeModelCachePolicy отклоняет unknown nested fields вместо молчаливой потери mandatory guarantee в controller boundary.
func runtimeModelCachePolicy(value *controlplanev1.RuntimeModelCachePolicy) (*runtimecontract.ModelCachePolicy, error) {
	if value == nil {
		return nil, nil
	}
	q := value.GetQualification()
	if len(value.ProtoReflect().GetUnknown()) != 0 || q == nil || len(q.ProtoReflect().GetUnknown()) != 0 {
		return nil, runtimecontract.ErrInferenceCachePolicy
	}
	return &runtimecontract.ModelCachePolicy{Revision: int(value.GetRevision()), Optimization: value.GetOptimization(), ReuseScope: value.GetReuseScope(), NoReuse: value.GetNoReuse(), NoStore: value.GetNoStore(), Required: value.GetRequired(), Qualification: runtimecontract.ModelCacheQualification{Mechanism: q.GetMechanism(), Status: q.GetStatus(), Source: q.GetSource(), BackendVersion: q.GetBackendVersion(), EvidenceSHA256: q.GetEvidenceSha256(), RouteSHA256: q.GetRouteSha256()}}, nil
}
