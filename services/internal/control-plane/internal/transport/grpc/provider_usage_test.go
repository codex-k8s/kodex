package grpc

import (
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func providerUsageReadFixture() entity.ProviderAccount {
	now := time.Now().UTC()
	expires := now.Add(time.Minute)
	ready := entity.ProviderUsageDimension{State: "READY", Reason: "AVAILABLE", Remediation: "NONE"}
	return entity.ProviderAccount{Ref: "pacc_usage0001", Version: 1, State: "AUTHORIZED", Enabled: true, Usage: &entity.ProviderAccountUsage{
		AccountVersion: 1, AgentVersion: 1, Context: &entity.ProviderAccountUsageContext{Purpose: "CONFIGURE", AgentRef: "agt_usage00001", ProviderDefinitionKey: "openai-codex", Model: "model-one"},
		Lifecycle: ready, Credential: ready, ProviderHealth: entity.ProviderUsageDimension{State: "READY", Reason: "CREDENTIALED_CATALOG_REACHABLE", Remediation: "NONE"}, ProviderHealthScope: "CREDENTIALED_CATALOG_REACHABILITY", ProviderHealthObservedAt: &now, ProviderHealthExpiresAt: &expires, ModelCompatibility: ready, Capacity: ready, ActorEligibility: ready,
		AllowedToSubmit: true, EligibleForSelection: true, OperationalState: "READY", MaximumConcurrentExecutions: 2, CatalogStatus: &entity.ModelCatalogStatus{State: "READY", Source: "REMOTE_API", Failure: "NONE", ObservedAt: &now, ExpiresAt: &expires}, CatalogDigest: strings.Repeat("a", 64), CatalogRevision: "mcat_" + strings.Repeat("a", 64), ContextDigest: strings.Repeat("b", 64), ObservedAt: now, ExpiresAt: now.Add(10 * time.Second)}}
}

func TestProviderUsageReadCasterClosesMalformedOwnerProjection(t *testing.T) {
	value, err := castProviderAccountUsageRead(providerUsageReadFixture())
	if err != nil || !value.Usage.AllowedToSubmit || value.Usage.ProviderHealth.State != cp.ProviderUsageState_PROVIDER_USAGE_STATE_READY {
		t.Fatalf("usage projection: %v", err)
	}
	for name, mutate := range map[string]func(*entity.ProviderAccountUsage){
		"unknown state":        func(u *entity.ProviderAccountUsage) { u.ActorEligibility.State = "invented" },
		"unknown reason":       func(u *entity.ProviderAccountUsage) { u.Capacity.Reason = "invented" },
		"unknown remediation":  func(u *entity.ProviderAccountUsage) { u.Credential.Remediation = "invented" },
		"missing catalog":      func(u *entity.ProviderAccountUsage) { u.CatalogStatus = nil },
		"wrong version":        func(u *entity.ProviderAccountUsage) { u.AccountVersion++ },
		"wrong digest":         func(u *entity.ProviderAccountUsage) { u.ContextDigest = strings.Repeat("Z", 64) },
		"wrong capacity":       func(u *entity.ProviderAccountUsage) { u.ActiveExecutions = 2 },
		"false admission":      func(u *entity.ProviderAccountUsage) { u.ActorEligibility.State = "BLOCKED" },
		"no context authority": func(u *entity.ProviderAccountUsage) { u.Context = nil },
		"unscoped health":      func(u *entity.ProviderAccountUsage) { u.ProviderHealthScope = "PROVIDER_WIDE" },
		"stale health":         func(u *entity.ProviderAccountUsage) { u.CatalogStatus.State = "PENDING" },
		"wrong health observation": func(u *entity.ProviderAccountUsage) {
			shifted := u.ObservedAt.Add(-time.Second)
			u.ProviderHealthObservedAt = &shifted
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := providerUsageReadFixture()
			mutate(fixture.Usage)
			if _, err := castProviderAccountUsageRead(fixture); err == nil {
				t.Fatal("malformed owner usage exposed")
			}
		})
	}
}
