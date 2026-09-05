package platform

import (
	"testing"
	"time"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

func TestModelCatalogContentIdentityExcludesFreshnessAndCredentialButBindsCapabilities(t *testing.T) {
	task := platformrepo.ProviderModelCatalogTask{OrganizationID: "tenant", AccountRef: "pacc_account", AccountVersion: 1, CredentialRef: "credential", CredentialRevision: 1, AuthorizationMethod: "API_KEY"}
	observation := platformrepo.ProviderModelCatalogObservation{AccountRef: task.AccountRef, CredentialRef: task.CredentialRef, ObservedAt: time.Now(), Source: "REMOTE_API", Failure: "NONE", Models: []platformrepo.ProviderModelCatalogRecord{{ID: "model", DefaultReasoningEffort: "adaptive", ReasoningEfforts: []string{"adaptive"}}, {ID: "non-reasoning"}}}
	_, content, receipt, err := canonicalModelCatalogObservation(task, observation)
	if err != nil {
		t.Fatal(err)
	}
	refreshed := observation
	refreshed.ObservedAt = observation.ObservedAt.Add(time.Minute)
	_, nextContent, nextReceipt, err := canonicalModelCatalogObservation(task, refreshed)
	if err != nil || content != nextContent || receipt == nextReceipt {
		t.Fatal("freshness changed content pin or lost receipt identity")
	}
	rotated := task
	rotated.CredentialRevision++
	_, changed, _, err := canonicalModelCatalogObservation(rotated, observation)
	if err != nil || changed != content {
		t.Fatal("operational credential refresh changed capabilities pin")
	}
	observation.Models[0].ReasoningEfforts = append(observation.Models[0].ReasoningEfforts, "high")
	_, changed, _, err = canonicalModelCatalogObservation(task, observation)
	if err != nil || changed == content {
		t.Fatal("capabilities not bound")
	}
}

func TestModelCatalogObservationFailsClosedWithoutRemoteEvidence(t *testing.T) {
	task := platformrepo.ProviderModelCatalogTask{AccountRef: "account", CredentialRef: "credential", AuthorizationMethod: "API_KEY"}
	base := platformrepo.ProviderModelCatalogObservation{AccountRef: task.AccountRef, CredentialRef: task.CredentialRef, ObservedAt: time.Now(), Failure: "UNVERIFIED_SOURCE"}
	if _, _, _, err := canonicalModelCatalogObservation(task, base); err != nil {
		t.Fatal(err)
	}
	base.Models = []platformrepo.ProviderModelCatalogRecord{{ID: "cached-model"}}
	if _, _, _, err := canonicalModelCatalogObservation(task, base); err == nil {
		t.Fatal("cached fallback became capabilities")
	}
	base.Failure = "NONE"
	base.Source = "REMOTE_API"
	base.Models = nil
	if _, _, _, err := canonicalModelCatalogObservation(task, base); err != nil {
		t.Fatal("empty authoritative catalog rejected")
	}
	base.Models = []platformrepo.ProviderModelCatalogRecord{{ID: "non-reasoning", DefaultReasoningEffort: "none"}}
	if _, _, _, err := canonicalModelCatalogObservation(task, base); err == nil {
		t.Fatal("invented non-reasoning effort accepted")
	}
}
