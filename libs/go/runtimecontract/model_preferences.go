package runtimecontract

import "slices"

// ResolveRuntimeContext combines native defaults with one immutable owner overlay, preserving model ceilings.
// ResolveRuntimeContext объединяет штатные defaults с immutable overlay владельца, сохраняя пределы модели.
func (model ModelProviderModel) ResolveRuntimeContext(raw string) (ModelContextPolicy, error) {
	result := model.EffectiveContextPolicy()
	overlay, err := ParseConfigOverlay(raw)
	if err != nil {
		return result, err
	}
	if overlay.Context != nil {
		if overlay.Context.AutoCompaction != nil {
			result.AutoCompaction = *overlay.Context.AutoCompaction
		}
		if overlay.Context.Prune != nil {
			result.Prune = *overlay.Context.Prune
		}
	}
	return result, nil
}

// ResolveRuntimeModelCache applies non-authorizing intent to a private descriptor copy without changing its route qualification.
// Mandatory route constraints cannot be disabled or widened; strict no-reuse/no-store preferences always require enforcement.
// ResolveRuntimeModelCache применяет намерение к приватной копии descriptor без изменения qualification маршрута.
// Обязательные ограничения нельзя отключить или расширить; no-reuse/no-store всегда требуют исполнения гарантии.
func (config ModelProviderConfiguration) ResolveRuntimeModelCache(modelID, raw string) (EffectiveModelCache, error) {
	baseline, err := config.ResolveModelCache(modelID)
	if err != nil {
		return baseline, err
	}
	overlay, err := ParseConfigOverlay(raw)
	if err != nil || overlay.Cache == nil {
		return baseline, err
	}
	model, found := config.Model(modelID)
	if !found {
		return baseline, ErrModelProviderConfiguration
	}
	policy := ModelCachePolicy{Revision: 1, Optimization: "NO_HINTS", ReuseScope: "SESSION",
		Qualification: ModelCacheQualification{Mechanism: "NONE", Status: "UNKNOWN", Source: "NONE"}}
	if model.CachePolicy != nil {
		policy = *model.CachePolicy
	}
	requested := overlay.Cache
	if (policy.Required || policy.NoReuse || policy.NoStore) && ((requested.Required != nil && !*requested.Required) ||
		(policy.NoReuse && requested.NoReuse != nil && !*requested.NoReuse) ||
		(policy.NoStore && requested.NoStore != nil && !*requested.NoStore) ||
		(requested.Optimization != "" && requested.Optimization != policy.Optimization) ||
		(requested.ReuseScope != "" && cacheScopeWidth(requested.ReuseScope) > cacheScopeWidth(policy.ReuseScope))) {
		baseline.Status, baseline.Reason = "UNSUPPORTED", "MANDATORY_ROUTE_POLICY_CANNOT_WEAKEN"
		return baseline, ErrInferenceCachePolicy
	}
	if requested.Optimization != "" {
		policy.Optimization = requested.Optimization
	}
	if requested.ReuseScope != "" {
		policy.ReuseScope = requested.ReuseScope
	}
	if requested.NoReuse != nil {
		policy.NoReuse = *requested.NoReuse
	}
	if requested.NoStore != nil {
		policy.NoStore = *requested.NoStore
	}
	if requested.Required != nil {
		policy.Required = *requested.Required
	}
	policy.Required = policy.Required || policy.NoReuse || policy.NoStore
	config.Models = slices.Clone(config.Models)
	for index := range config.Models {
		if config.Models[index].ID == modelID {
			config.Models[index].CachePolicy = &policy
		}
	}
	return config.ResolveModelCache(modelID)
}

// cacheScopeWidth orders only the closed reuse scopes; unknown input stays maximally broad and is separately rejected by the parser.
// cacheScopeWidth упорядочивает закрытые reuse scopes; неизвестный ввод считается самым широким и отдельно отклоняется parser.
func cacheScopeWidth(scope string) int {
	switch scope {
	case "SESSION":
		return 1
	case "INITIATOR":
		return 2
	case "PROJECT":
		return 3
	default:
		return 4
	}
}
