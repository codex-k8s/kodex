package platform

import (
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func providerUsageFixture() (providerUsageAccount, providerUsageContext) {
	now := time.Now().UTC()
	expires := now.Add(time.Minute)
	return providerUsageAccount{Ref: "pacc_usage0001", Provider: "openai-codex", State: "AUTHORIZED", Version: 1, Enabled: true, ProviderEnabled: true, CredentialID: "credential-one", Maximum: 2, ObservedAt: now,
			Catalog:       entity.ModelCatalogStatus{State: "READY", Source: "REMOTE_API", Failure: "NONE", ObservedAt: &now, ExpiresAt: &expires},
			CatalogSource: `{"account":"pacc_usage0001","content":"content-one"}`,
			Models:        []platformrepo.ProviderModelCatalogRecord{{ID: "model-one", ReasoningEfforts: []string{"low", "medium"}, DefaultReasoningEffort: "medium"}}},
		providerUsageContext{Input: &entity.ProviderAccountUsageContext{Purpose: "CONFIGURE", AgentRef: "agt_usage00001", ProviderDefinitionKey: "openai-codex", Model: "model-one", RuntimeProfileRef: "profile-one"}, Profile: providerUsageProfile{Ref: "profile-one", Provider: "openai-codex", Model: "model-one", RuntimeRevision: "revision-one", Enabled: true}, ActorAllowed: true, AuthorityDigest: "authority-one"}
}

func TestProviderUsageSeparatesSubmissionAndOperationalEvidence(t *testing.T) {
	for _, name := range []string{"verified reachability", "capacity exhausted", "no context", "model not selected", "empty verified catalog", "permission denied", "credential missing", "provider mismatch", "unsupported effort", "expired catalog", "authorization rejected"} {
		t.Run(name, func(t *testing.T) {
			account, context := providerUsageFixture()
			allowed, selectable := true, true
			modelReason := "AVAILABLE"
			switch name {
			case "capacity exhausted":
				account.Active = account.Maximum
			case "no context":
				context.Input = nil
				allowed = false
				selectable = false
				modelReason = "CONTEXT_REQUIRED"
			case "model not selected":
				context.Input.Model = ""
				allowed = false
				modelReason = "MODEL_REQUIRED"
			case "empty verified catalog":
				account.Models = nil
				allowed = false
				modelReason = "MODEL_UNSUPPORTED"
			case "permission denied":
				context.ActorAllowed = false
				allowed = false
				selectable = false
			case "credential missing":
				account.CredentialID = ""
				allowed = false
				selectable = false
			case "provider mismatch":
				context.Input.ProviderDefinitionKey = "other-provider"
				context.Profile.Provider = "other-provider"
				allowed = false
				selectable = false
				modelReason = "PROVIDER_MISMATCH"
			case "unsupported effort":
				context.Input.ReasoningEffort = "high"
				allowed = false
				modelReason = "EFFORT_UNSUPPORTED"
			case "expired catalog":
				account.Catalog.State = "EXPIRED"
				allowed = false
				modelReason = "CATALOG_EXPIRED"
			case "authorization rejected":
				account.Catalog.State = "FAILED"
				account.Catalog.Source = ""
				account.Catalog.Failure = "AUTHORIZATION_REJECTED"
				allowed = false
				modelReason = "CATALOG_AUTHORIZATION_REJECTED"
			}
			usage, err := providerAccountUsage(account, context)
			if err != nil || usage.AllowedToSubmit != allowed || usage.EligibleForSelection != selectable || usage.ModelCompatibility.Reason != modelReason {
				t.Fatalf("unexpected dimensions: %v", err)
			}
			wantHealth := "READY"
			if name == "credential missing" || name == "expired catalog" || name == "authorization rejected" {
				wantHealth = "UNKNOWN"
			}
			if usage.ProviderHealth.State != wantHealth || usage.ProviderHealthScope != "CREDENTIALED_CATALOG_REACHABILITY" || (usage.ProviderHealthObservedAt != nil) != (wantHealth == "READY") {
				t.Fatal("health observation scope or freshness lost")
			}
			if name == "capacity exhausted" && usage.Capacity.Reason != "CAPACITY_EXHAUSTED" {
				t.Fatal("capacity hidden")
			}
			if name == "authorization rejected" && usage.ModelCompatibility.Remediation != "AUTHORIZE_ACCOUNT" {
				t.Fatal("authorization remediation lost")
			}
		})
	}
}

func TestProviderUsageLaunchPinsAndFreshCredentialBinding(t *testing.T) {
	account, context := providerUsageFixture()
	first, err := providerAccountUsage(account, context)
	if err != nil {
		t.Fatal(err)
	}
	context.Input = &entity.ProviderAccountUsageContext{Purpose: "LAUNCH", AgentRef: "agt_usage00001"}
	context.Agent = providerUsageAgent{Ref: context.Input.AgentRef, Enabled: true, State: "READY", ConfigRef: "rcfg_usage001", Provider: account.Provider, Model: "model-one", Candidates: []entity.ProviderAccountCandidate{{AccountRef: account.Ref, ProviderDefinitionKey: account.Provider, CatalogRevision: first.CatalogRevision, CatalogDigest: first.CatalogDigest, DefaultReasoningEffort: "medium"}}}
	usage, err := providerAccountUsage(account, context)
	if err != nil || !usage.AllowedToSubmit {
		t.Fatal("matching immutable pins denied")
	}
	account.CredentialID = "credential-two"
	refreshed, _ := providerAccountUsage(account, context)
	if refreshed.ContextDigest == usage.ContextDigest || refreshed.CatalogDigest != usage.CatalogDigest {
		t.Fatal("credential and catalog content identity conflated")
	}
	account.Catalog.State = "PENDING"
	pending, _ := providerAccountUsage(account, context)
	if pending.AllowedToSubmit {
		t.Fatal("new credential used without verified observation")
	}
	account.Catalog.State = "READY"
	context.Agent.Candidates = append(context.Agent.Candidates, context.Agent.Candidates[0])
	duplicate, _ := providerAccountUsage(account, context)
	if duplicate.AllowedToSubmit {
		t.Fatal("duplicate selected pin admitted")
	}
}

func TestProviderUsageFreshHealthAndCandidateProfile(t *testing.T) {
	account, context := providerUsageFixture()
	first, err := providerAccountUsage(account, context)
	if err != nil {
		t.Fatal(err)
	}
	account.ObservedAt = account.ObservedAt.Add(time.Second)
	next, err := providerAccountUsage(account, context)
	if err != nil || first.ContextDigest != next.ContextDigest {
		t.Fatal("read clock changed source pin")
	}
	context.Profile.RuntimeRevision = "revision-two"
	next, err = providerAccountUsage(account, context)
	if err != nil || first.ContextDigest == next.ContextDigest {
		t.Fatal("candidate profile revision not pinned")
	}
	context.Profile.Enabled = false
	next, err = providerAccountUsage(account, context)
	if err != nil || next.AllowedToSubmit || next.ModelCompatibility.Reason != "RUNTIME_PROFILE_UNAVAILABLE" {
		t.Fatal("disabled candidate profile admitted")
	}
	for _, state := range []string{"PENDING", "FAILED", "EXPIRED"} {
		account.Catalog.State = state
		next, err = providerAccountUsage(account, context)
		if err != nil || next.ProviderHealth.State != "UNKNOWN" || next.ProviderHealthObservedAt != nil {
			t.Fatal("stale or failed observation reported healthy")
		}
	}
}

func TestProviderUsageCursorRejectsChangedSourceAndExpiry(t *testing.T) {
	token := encodeProviderUsageCursor("source-one", time.Now().UTC(), "pacc_usage0001")
	if _, _, err := decodeProviderUsageCursor("source-one", token); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"actor-changed", "context-changed", "credential-changed", "capacity-changed"} {
		if _, _, err := decodeProviderUsageCursor(source, token); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("changed snapshot accepted")
		}
	}
	if _, _, err := decodeProviderUsageCursor("source-one", "malformed"); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("malformed cursor accepted")
	}
}
