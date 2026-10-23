package caster

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// TestScopedMetadataClosedWireMapping retains organization/project scope metadata in draft/published
// casters and rejects the selected unknown or incorrectly cased scopes, without invoking owner authority.
// TestScopedMetadataClosedWireMapping сохраняет organization/project scope metadata в draft/published
// casters и отвергает выбранные неизвестные или scope с неверным регистром без вызова owner authority.
func TestScopedMetadataClosedWireMapping(t *testing.T) {
	now := time.Now().UTC()
	for _, scope := range []runtimesecret.ScopeKind{runtimesecret.ScopeOrganization, runtimesecret.ScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			project := ""
			if scope == runtimesecret.ScopeProject {
				project = "prj_fixture"
			}
			draft := value.SecretDraft{ScopeKind: scope, OrganizationRef: "org_fixture", ProjectRef: project, Ref: "draft_fixture", SecretRef: "sec_fixture", Version: 2, Generation: 1, SecretVersion: 1,
				ValueType: "STRING", State: "DRAFT", CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour)}
			secret := &value.PublishedSecret{ScopeKind: scope, OrganizationRef: "org_fixture", ProjectRef: project, Ref: "sec_fixture", Version: 2, Revision: 1, ValueType: "STRING", Status: "ACTIVE", CreatedAt: now, UpdatedAt: now}
			wire, err := SecretDraft(draft)
			if err != nil || wire.GetScopeKind().String() != "RUNTIME_RESOURCE_SCOPE_KIND_"+string(scope) || wire.GetOrganizationRef() != draft.OrganizationRef || wire.GetProjectRef() != project {
				t.Fatal("draft caster changed scope")
			}
			published, err := PublishedSecret(secret)
			if err != nil || published.GetScopeKind() != wire.GetScopeKind() || published.GetOrganizationRef() != secret.OrganizationRef || published.GetProjectRef() != project {
				t.Fatal("published caster changed scope")
			}
			for _, invalid := range []runtimesecret.ScopeKind{"", "UNKNOWN", "SYSTEM", "project"} {
				draft.ScopeKind = invalid
				secret.ScopeKind = invalid
				if _, err := SecretDraft(draft); err == nil {
					t.Fatal("draft caster accepted unknown scope")
				}
				if _, err := PublishedSecret(secret); err == nil {
					t.Fatal("published caster accepted unknown scope")
				}
			}
		})
	}
}

// TestMetadataTimestampsPreserveOrderAndRejectIncompletePair checks exact ordered conversion
// and rejects zero/out-of-range members without returning a partially usable metadata pair.
// TestMetadataTimestampsPreserveOrderAndRejectIncompletePair проверяет точный порядок перевода
// и отвергает zero/out-of-range элементы без частично пригодной metadata pair.
func TestMetadataTimestampsPreserveOrderAndRejectIncompletePair(t *testing.T) {
	created := time.Date(2026, 10, 1, 9, 0, 0, 123, time.UTC)
	updated := created.Add(time.Hour)
	outside := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name             string
		created, updated time.Time
		valid            bool
	}{
		{"valid", created, updated, true},
		{"no chronology policy", updated, created, true},
		{"missing creation", time.Time{}, updated, false},
		{"missing update", created, time.Time{}, false},
		{"creation outside range", outside, updated, false},
		{"update outside range", created, outside, false},
		{"both missing", time.Time{}, time.Time{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			gotCreated, gotUpdated, err := metadataTimestamps(test.created, test.updated)
			if test.valid {
				if err != nil || !gotCreated.AsTime().Equal(test.created) || !gotUpdated.AsTime().Equal(test.updated) {
					t.Fatal("metadata timestamp pair lost values or ordering")
				}
				return
			}
			if err != errDraftMetadata || gotCreated != nil || gotUpdated != nil {
				t.Fatal("invalid metadata timestamp pair escaped closed validation")
			}
		})
	}
}
