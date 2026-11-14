package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// qualificationFixture creates synthetic operator evidence with no live target, key or claim; permission and provider checks remain separately exercised by consumer tests.
// qualificationFixture создаёт synthetic operator evidence без live target, key либо claim; permission/provider checks отдельно проверяются consumer tests.
func qualificationFixture(kind, keyDigest string) operatorQualification {
	now := time.Now().UTC().Truncate(time.Second)
	profile := operatorQualification{Kind: kind, Origin: "https://grafana.example.invalid", ProviderVersion: "13.2.0", OrganizationID: "1", CredentialSHA256: keyDigest, ImageSHA256: strings.Repeat("a", 64), ConfigurationSHA256: strings.Repeat("b", 64), WriterInventorySHA256: strings.Repeat("c", 64), RuntimeReadbackSHA256: strings.Repeat("d", 64), QualifiedAt: now.Add(-time.Minute).Format(time.RFC3339), ValidUntil: now.Add(time.Hour).Format(time.RFC3339)}
	for _, name := range []string{"deployment-readback", "target-config", "writer-inventory"} {
		profile.Proofs = append(profile.Proofs, operatorQualificationProof{Case: name, RequestSHA256: strings.Repeat("e", 64), ResponseSHA256: strings.Repeat("f", 64), HTTPStatus: 200})
	}
	switch kind {
	case qualificationGrafanaAnnotations:
		profile.DashboardUID = "fixture-dashboard"
		profile.WriterMode = "LEGACY_SQL_ANNOTATIONS"
		profile.AnnotationStoreBackend = "legacy-sql"
		profile.AnnotationAPIMigrationPhase = "off"
	case qualificationGrafanaRuleDelete:
		profile.WriterMode = "API_ONLY_ORGANIZATION_ALERT_RULES"
	case qualificationLokiRuler:
		profile.Origin = "https://loki.example.invalid"
		profile.ProviderVersion = "3.6.21"
		profile.OrganizationID = ""
		profile.TenantID = "fixture-tenant"
		profile.Namespace = "fixture-rules"
		profile.WriterMode = "API_ONLY_RULER_NAMESPACE"
		profile.RulerStorageBackend = "S3"
		for _, entry := range []struct {
			name, key, tenant, namespace string
			status                       int
		}{
			{"tenant-positive-a", keyDigest, profile.TenantID, "", 200}, {"tenant-positive-b", strings.Repeat("9", 64), "other-tenant", "", 200},
			{"tenant-cross-a-b", keyDigest, "other-tenant", "", 403}, {"tenant-cross-b-a", strings.Repeat("9", 64), profile.TenantID, "", 403},
			{"ruler-create", keyDigest, profile.TenantID, profile.Namespace, 202}, {"ruler-update", keyDigest, profile.TenantID, profile.Namespace, 202}, {"ruler-delete", keyDigest, profile.TenantID, profile.Namespace, 202},
		} {
			profile.Proofs = append(profile.Proofs, operatorQualificationProof{Case: entry.name, RequestSHA256: strings.Repeat("e", 64), ResponseSHA256: strings.Repeat("f", 64), HTTPStatus: entry.status, CredentialSHA256: entry.key, TenantID: entry.tenant, Namespace: entry.namespace})
		}
	}
	return profile
}

// installQualificationFixture mounts read-only synthetic bytes with an exact process pin, preserving the production separation from Connection/request configuration.
// installQualificationFixture устанавливает read-only synthetic bytes с exact process pin, сохраняя production separation от Connection/request configuration.
func installQualificationFixture(t *testing.T, adapter *Adapter, profiles ...operatorQualification) string {
	t.Helper()
	raw, err := json.Marshal(operatorQualificationDocument{Schema: "kodex-observability-qualifications/v1", Profiles: profiles})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "profiles.json")
	if os.WriteFile(path, raw, 0400) != nil {
		t.Fatal("cannot create synthetic operator profile")
	}
	digest := sha256.Sum256(raw)
	adapter.observabilityQualificationFile = path
	adapter.observabilityQualificationSHA256 = hex.EncodeToString(digest[:])
	return path
}

// TestOperatorQualificationPinsAndWithdrawal rejects origin/resource/key drift, writable files, tampered bytes, duplicate documents, unknown fields and withdrawal without making any HTTP request.
// TestOperatorQualificationPinsAndWithdrawal отклоняет origin/resource/key drift, writable files, tampered bytes, duplicate documents, unknown fields и withdrawal без HTTP request.
func TestOperatorQualificationPinsAndWithdrawal(t *testing.T) {
	for _, scenario := range []string{"ready", "key", "origin", "organization", "dashboard", "expired", "future", "long-validity", "withdrawn", "writable", "digest", "duplicate", "unknown-json", "case-alias"} {
		t.Run(scenario, func(t *testing.T) {
			key := strings.Repeat("1", 64)
			profile := qualificationFixture(qualificationGrafanaAnnotations, key)
			request := Request{Credential: &CredentialRevision{ContentSHA256: key}}
			configuration := map[string]string{"base_url": profile.Origin, "organization_id": "1", "dashboard_uid": "fixture-dashboard"}
			switch scenario {
			case "expired":
				profile.ValidUntil = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
			case "future":
				profile.QualifiedAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
				profile.ValidUntil = time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
			case "long-validity":
				profile.ValidUntil = time.Now().Add(25 * time.Hour).UTC().Format(time.RFC3339)
			}
			adapter := &Adapter{}
			path := installQualificationFixture(t, adapter, profile)
			switch scenario {
			case "key":
				request.Credential.ContentSHA256 = strings.Repeat("2", 64)
			case "origin":
				configuration["base_url"] = "https://foreign.example.invalid"
			case "organization":
				configuration["organization_id"] = "2"
			case "dashboard":
				configuration["dashboard_uid"] = "foreign-dashboard"
			case "withdrawn":
				if os.Remove(path) != nil {
					t.Fatal("cannot withdraw fixture")
				}
			case "writable":
				if os.Chmod(path, 0600) != nil {
					t.Fatal("cannot change fixture mode")
				}
			case "digest":
				adapter.observabilityQualificationSHA256 = strings.Repeat("3", 64)
			case "duplicate":
				installQualificationFixture(t, adapter, profile, profile)
			case "unknown-json", "case-alias":
				raw, _ := os.ReadFile(path)
				keyName := "unknown"
				if scenario == "case-alias" {
					keyName = "Schema"
				}
				raw = append([]byte(`{"`+keyName+`":"untrusted",`), raw[1:]...)
				if os.Chmod(path, 0600) != nil || os.WriteFile(path, raw, 0400) != nil || os.Chmod(path, 0400) != nil {
					t.Fatal("cannot alter fixture")
				}
				digest := sha256.Sum256(raw)
				adapter.observabilityQualificationSHA256 = hex.EncodeToString(digest[:])
			}
			_, err := adapter.operatorQualificationFor(request, configuration, qualificationGrafanaAnnotations)
			if (err == nil) != (scenario == "ready") {
				t.Fatal("operator qualification scope/validity mismatch", err)
			}
			if err != nil && err.Error() != providerCapabilityUnsupportedCode {
				t.Fatal("operator evidence failure became a credential failure", err)
			}
		})
	}
}

// TestOperatorQualificationClosedModes checks that a checkbox-like mode, missing deployment pins, mixed-writer organization or incomplete/invalid two-tenant matrix cannot qualify a destructive capability.
// TestOperatorQualificationClosedModes проверяет, что checkbox-like mode, missing deployment pins, mixed-writer organization либо incomplete/invalid two-tenant matrix не квалифицируют destructive capability.
func TestOperatorQualificationClosedModes(t *testing.T) {
	now := time.Now().UTC()
	for _, kind := range []string{qualificationGrafanaAnnotations, qualificationGrafanaRuleDelete, qualificationLokiRuler} {
		profile := qualificationFixture(kind, strings.Repeat("1", 64))
		if !validOperatorQualification(profile, now) {
			t.Fatal("synthetic complete qualification rejected")
		}
		profile.WriterMode = "ENABLED"
		if validOperatorQualification(profile, now) {
			t.Fatal("ordinary checkbox qualified a target")
		}
	}
	for _, scenario := range []string{"one-key", "one-tenant", "401-only", "missing-write", "foreign-namespace", "missing-config", "unknown-backend"} {
		profile := qualificationFixture(qualificationLokiRuler, strings.Repeat("1", 64))
		switch scenario {
		case "one-key":
			profile.Proofs[4].CredentialSHA256 = profile.CredentialSHA256
		case "one-tenant":
			profile.Proofs[4].TenantID = profile.TenantID
		case "401-only":
			profile.Proofs[5].HTTPStatus = 401
		case "missing-write":
			profile.Proofs = profile.Proofs[:len(profile.Proofs)-1]
		case "foreign-namespace":
			profile.Proofs[len(profile.Proofs)-1].Namespace = "other-rules"
		case "missing-config":
			profile.ConfigurationSHA256 = ""
		case "unknown-backend":
			profile.RulerStorageBackend = "LOCAL"
		}
		if validOperatorQualification(profile, now) {
			t.Fatal("unqualified ruler target accepted:", scenario)
		}
	}
}

// TestOperatorQualificationProcessInput allows only the exact paired optional process pins; neither a product JSON path nor a digest alone activates qualification.
// TestOperatorQualificationProcessInput допускает только exact paired optional process pins; product JSON path либо отдельный digest qualification не активируют.
func TestOperatorQualificationProcessInput(t *testing.T) {
	if ValidateObservabilityQualificationConfiguration("", "") != nil || ValidateObservabilityQualificationConfiguration(ObservabilityQualificationFile, strings.Repeat("1", 64)) != nil {
		t.Fatal("valid optional process pin rejected")
	}
	for _, value := range []struct{ path, digest string }{{"/etc/shadow", strings.Repeat("1", 64)}, {ObservabilityQualificationFile, ""}, {"", strings.Repeat("1", 64)}, {ObservabilityQualificationFile, strings.Repeat("0", 64)}} {
		if ValidateObservabilityQualificationConfiguration(value.path, value.digest) == nil {
			t.Fatal("arbitrary/incomplete process pin accepted")
		}
	}
}

// TestOperatorQualificationCrossLanguageGolden consumes the same immutable synthetic bytes as the installer and validates all three native profiles at an explicit fixture clock; it does not claim current/live operator evidence.
// TestOperatorQualificationCrossLanguageGolden читает те же immutable synthetic bytes, что installer, и проверяет три native profiles при explicit fixture clock; current/live operator evidence не утверждается.
func TestOperatorQualificationCrossLanguageGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "contracts", "integrations", "observability-qualifications", "v1", "synthetic-golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "3a0e8c4c753e05d5bb19319db428b5abbc1be64bef52e815d131204bdafd6e67" {
		t.Fatal("operator cross-language bytes changed")
	}
	var document operatorQualificationDocument
	if json.Unmarshal(raw, &document) != nil || document.Schema != "kodex-observability-qualifications/v1" || len(document.Profiles) != 3 {
		t.Fatal("operator golden shape changed")
	}
	now := time.Date(2026, time.November, 14, 12, 0, 0, 0, time.UTC)
	for _, profile := range document.Profiles {
		if !validOperatorQualification(profile, now) {
			t.Fatal("installer golden is incompatible with native profile", profile.Kind)
		}
	}
}
