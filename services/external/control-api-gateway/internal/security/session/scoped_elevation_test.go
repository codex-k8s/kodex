package session

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRuntimeSecretElevationRequiresClosedOwnerTuple(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name, scope, organization, project string
		valid                              bool
	}{
		{"organization", "ORGANIZATION", "org_example", "", true},
		{"project", "PROJECT", "org_example", "prj_example", true},
		{"missing scope", "", "org_example", "prj_example", false},
		{"unknown scope", "TENANT", "org_example", "", false},
		{"missing organization", "PROJECT", "", "prj_example", false},
		{"organization with project", "ORGANIZATION", "org_example", "prj_example", false},
		{"project without project", "PROJECT", "org_example", "", false},
		{"invalid organization", "ORGANIZATION", "org/example", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			purpose := &LoginPurpose{Kind: ElevationKindRuntimeSecretReveal, ScopeKind: test.scope, OrganizationRef: test.organization, ProjectRef: test.project, SecretRef: "sec_example"}
			if validLoginPurpose(purpose) != test.valid {
				t.Fatal("login purpose owner tuple validation mismatch")
			}
			elevation := &Elevation{Kind: purpose.Kind, ScopeKind: purpose.ScopeKind, OrganizationRef: purpose.OrganizationRef, ProjectRef: purpose.ProjectRef, SecretRef: purpose.SecretRef, ExpiresAt: now.Add(time.Minute).Unix()}
			if validElevation(elevation, now, now.Add(time.Hour)) != test.valid {
				t.Fatal("session elevation owner tuple validation mismatch")
			}
			purpose.ReceiptRef = "receipt_example"
			elevation.ReceiptRef = purpose.ReceiptRef
			if validLoginPurpose(purpose) || validElevation(elevation, now, now.Add(time.Hour)) {
				t.Fatal("mixed-purpose elevation was accepted")
			}
		})
	}
}

func TestOrganizationElevationScopeIsSealedAndNotRenewed(t *testing.T) {
	key := filepath.Join(t.TempDir(), "key.hex")
	writeKey(t, key, strings.Repeat("55", 32))
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	store, err := New(Config{CurrentKeyFile: key, TTL: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return now }
	purpose := &Elevation{Kind: ElevationKindRuntimeSecretReveal, ScopeKind: "ORGANIZATION", OrganizationRef: "org_example", SecretRef: "sec_example", ExpiresAt: now.Add(time.Minute).Unix()}
	_, encoded, _, err := store.IssueWithElevation(uuid.NewString(), uuid.NewString(), uuid.NewString(), 3, "header.payload.signature", now.Add(time.Hour), purpose)
	if err != nil {
		t.Fatal(err)
	}
	purpose.ScopeKind, purpose.OrganizationRef, purpose.ProjectRef = "PROJECT", "org_other", "prj_other"
	opened, err := store.Open(encoded)
	if err != nil || opened.Elevation == nil || opened.Elevation.ScopeKind != "ORGANIZATION" || opened.Elevation.OrganizationRef != "org_example" || opened.Elevation.ProjectRef != "" {
		t.Fatal("sealed organization elevation changed owner tuple")
	}
	store.now = func() time.Time { return now.Add(2*time.Minute + 10*time.Second) }
	renewed, _, _, err := store.Renew(opened, now.Add(time.Hour))
	if err != nil || renewed.Elevation != nil {
		t.Fatal("renewal extended expired organization elevation")
	}
}

func TestEmailReconciliationCannotCarryRuntimeSecretScope(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	purpose := &LoginPurpose{Kind: ElevationKindEmailReconciliation, ReceiptRef: "receipt_example", ReceiptVersion: 3, ReceiptDigest: strings.Repeat("a", 64)}
	if !validLoginPurpose(purpose) {
		t.Fatal("exact email receipt purpose rejected")
	}
	for _, target := range []struct{ scope, organization string }{{"ORGANIZATION", "org_example"}, {"", "org_example"}, {"PROJECT", ""}} {
		purpose.ScopeKind, purpose.OrganizationRef = target.scope, target.organization
		elevation := &Elevation{Kind: purpose.Kind, ScopeKind: target.scope, OrganizationRef: target.organization, ReceiptRef: purpose.ReceiptRef, ReceiptVersion: purpose.ReceiptVersion, ReceiptDigest: purpose.ReceiptDigest, ExpiresAt: now.Add(time.Minute).Unix()}
		if validLoginPurpose(purpose) || validElevation(elevation, now, now.Add(time.Hour)) {
			t.Fatal("email receipt purpose accepted mixed runtime resource scope")
		}
	}
}
