package grpc

import (
	"encoding/hex"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func providerUsageContextInput(value *cp.ProviderAccountUsageContext) *entity.ProviderAccountUsageContext {
	if value == nil {
		return nil
	}
	return &entity.ProviderAccountUsageContext{Purpose: strings.TrimPrefix(value.Purpose.String(), "PROVIDER_ACCOUNT_USAGE_PURPOSE_"), AgentRef: value.AgentRef, ProviderDefinitionKey: value.ProviderDefinitionKey, Model: value.Model, ReasoningEffort: value.ReasoningEffort, RuntimeProfileRef: value.RuntimeProfileRef}
}

func castProviderAccountUsageRead(value entity.ProviderAccount) (*cp.ProviderAccount, error) {
	result := castProviderAccount(value)
	u := value.Usage
	if u == nil {
		return nil, errs.ErrUnavailable
	}
	if u.AccountVersion != value.Version || u.ObservedAt.IsZero() || !u.ExpiresAt.After(u.ObservedAt) || len(u.ContextDigest) != 64 || u.CatalogRevision != "mcat_"+u.CatalogDigest {
		return nil, errs.ErrUnavailable
	}
	for _, digest := range []string{u.ContextDigest, u.CatalogDigest} {
		if raw, err := hex.DecodeString(digest); err != nil || len(raw) != 32 || strings.ToLower(digest) != digest {
			return nil, errs.ErrUnavailable
		}
	}
	if u.MaximumConcurrentExecutions < 1 || u.ActiveExecutions < 0 || u.Capacity.State == "READY" != (u.ActiveExecutions < u.MaximumConcurrentExecutions) || u.CatalogStatus == nil {
		return nil, errs.ErrUnavailable
	}
	if u.Context == nil && (u.AllowedToSubmit || u.EligibleForSelection || u.ModelCompatibility.State != "NOT_EVALUATED" || u.ActorEligibility.State != "NOT_EVALUATED") {
		return nil, errs.ErrUnavailable
	}
	if u.EligibleForSelection && (u.Lifecycle.State != "READY" || u.Credential.State != "READY" || u.ActorEligibility.State != "READY") {
		return nil, errs.ErrUnavailable
	}
	if u.AllowedToSubmit && (!u.EligibleForSelection || u.ModelCompatibility.State != "READY") {
		return nil, errs.ErrUnavailable
	}
	if cp.ProviderModelCatalogState_value["PROVIDER_MODEL_CATALOG_STATE_"+u.CatalogStatus.State] == 0 {
		return nil, errs.ErrUnavailable
	}
	if u.ProviderHealthScope != "CREDENTIALED_CATALOG_REACHABILITY" {
		return nil, errs.ErrUnavailable
	}
	if u.ProviderHealth.State == "READY" {
		if u.ProviderHealth.Reason != "CREDENTIALED_CATALOG_REACHABLE" || u.ProviderHealthObservedAt == nil || u.ProviderHealthExpiresAt == nil ||
			u.ProviderHealthObservedAt.After(u.ObservedAt) || !u.ProviderHealthExpiresAt.After(u.ObservedAt) || u.CatalogStatus.State != "READY" || u.CatalogStatus.Failure != "NONE" ||
			(u.CatalogStatus.Source != "REMOTE_API" && u.CatalogStatus.Source != "REMOTE_CODEX") || u.CatalogStatus.ObservedAt == nil || u.CatalogStatus.ExpiresAt == nil ||
			!u.ProviderHealthObservedAt.Equal(*u.CatalogStatus.ObservedAt) || !u.ProviderHealthExpiresAt.Equal(*u.CatalogStatus.ExpiresAt) {
			return nil, errs.ErrUnavailable
		}
	} else if u.ProviderHealth.State != "UNKNOWN" || u.ProviderHealth.Reason != "PROVIDER_HEALTH_UNOBSERVED" || u.ProviderHealthObservedAt != nil || u.ProviderHealthExpiresAt != nil {
		return nil, errs.ErrUnavailable
	}
	states := []entity.ProviderUsageDimension{u.Lifecycle, u.Credential, u.ProviderHealth, u.ModelCompatibility, u.Capacity, u.ActorEligibility}
	dimensions := make([]*cp.ProviderUsageDimension, 0, 6)
	for _, dimension := range states {
		state := cp.ProviderUsageState_value["PROVIDER_USAGE_STATE_"+dimension.State]
		reason := cp.ProviderUsageReason_value["PROVIDER_USAGE_REASON_"+dimension.Reason]
		remediation := cp.ProviderUsageRemediation_value["PROVIDER_USAGE_REMEDIATION_"+dimension.Remediation]
		if state == 0 || reason == 0 || remediation == 0 {
			return nil, errs.ErrUnavailable
		}
		dimensions = append(dimensions, &cp.ProviderUsageDimension{State: cp.ProviderUsageState(state), Reason: cp.ProviderUsageReason(reason), Remediation: cp.ProviderUsageRemediation(remediation)})
	}
	operational := cp.ProviderUsageState_value["PROVIDER_USAGE_STATE_"+u.OperationalState]
	if operational == 0 {
		return nil, errs.ErrUnavailable
	}
	usage := &cp.ProviderAccountUsage{AccountVersion: u.AccountVersion, AgentVersion: u.AgentVersion,
		ProviderHealthScope:     cp.ProviderHealthScope_PROVIDER_HEALTH_SCOPE_CREDENTIALED_CATALOG_REACHABILITY,
		RuntimeConfigurationRef: u.RuntimeConfigurationRef, RuntimeConfigurationDigest: u.RuntimeConfigurationDigest,
		Lifecycle: dimensions[0], Credential: dimensions[1], ProviderHealth: dimensions[2], ModelCompatibility: dimensions[3], Capacity: dimensions[4], ActorEligibility: dimensions[5],
		AllowedToSubmit: u.AllowedToSubmit, EligibleForSelection: u.EligibleForSelection, OperationalState: cp.ProviderUsageState(operational), MaximumConcurrentExecutions: u.MaximumConcurrentExecutions, ActiveExecutions: u.ActiveExecutions,
		CatalogStatus: castModelCatalog(entity.ModelCatalog{Status: u.CatalogStatus}).CatalogStatus, CatalogRevision: u.CatalogRevision, CatalogDigest: u.CatalogDigest, ContextDigest: u.ContextDigest, ObservedAt: timestamp(u.ObservedAt), ExpiresAt: timestamp(u.ExpiresAt)}
	if u.ProviderHealthObservedAt != nil {
		usage.ProviderHealthObservedAt = timestamp(*u.ProviderHealthObservedAt)
		usage.ProviderHealthExpiresAt = timestamp(*u.ProviderHealthExpiresAt)
	}
	if u.Context != nil {
		purpose := cp.ProviderAccountUsagePurpose_value["PROVIDER_ACCOUNT_USAGE_PURPOSE_"+u.Context.Purpose]
		if purpose == 0 {
			return nil, errs.ErrUnavailable
		}
		usage.Context = &cp.ProviderAccountUsageContext{Purpose: cp.ProviderAccountUsagePurpose(purpose), AgentRef: u.Context.AgentRef, ProviderDefinitionKey: u.Context.ProviderDefinitionKey, Model: u.Context.Model, ReasoningEffort: u.Context.ReasoningEffort, RuntimeProfileRef: u.Context.RuntimeProfileRef}
	}
	result.Usage = usage
	return result, nil
}
