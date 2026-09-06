package entity

import "time"

type ProviderAccountUsageContext struct {
	Purpose, AgentRef, ProviderDefinitionKey, Model, ReasoningEffort, RuntimeProfileRef string
}

type ProviderUsageDimension struct {
	State, Reason, Remediation string
}

type ProviderAccountUsage struct {
	Context                                                                               *ProviderAccountUsageContext
	AccountVersion, AgentVersion                                                          int64
	RuntimeConfigurationRef, RuntimeConfigurationDigest                                   string
	Lifecycle, Credential, ProviderHealth, ModelCompatibility, Capacity, ActorEligibility ProviderUsageDimension
	AllowedToSubmit, EligibleForSelection                                                 bool
	OperationalState                                                                      string
	MaximumConcurrentExecutions, ActiveExecutions                                         int64
	CatalogStatus                                                                         *ModelCatalogStatus
	CatalogRevision, CatalogDigest, ContextDigest                                         string
	ObservedAt, ExpiresAt                                                                 time.Time
	ProviderHealthScope                                                                   string
	ProviderHealthObservedAt, ProviderHealthExpiresAt                                     *time.Time
}
