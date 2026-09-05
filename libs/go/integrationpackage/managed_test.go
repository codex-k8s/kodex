package integrationpackage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManagedRevisionPreservesShippedAndRestrictsExecution(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for key, baseline := range definitions {
		if err := ValidateExecutableRevision(baseline, baseline); err != nil {
			t.Fatalf("shipped %s: %v", key, err)
		}
		for _, origin := range []string{OriginUI, OriginGit} {
			raw, _ := json.Marshal(baseline)
			candidate, canonical, err := NormalizeManagedRevision(raw, origin, definitions)
			if err != nil || candidate.Metadata.Origin != origin || candidate.Digest == baseline.Digest || len(canonical) == 0 {
				t.Fatalf("normalize %s/%s: %v", key, origin, err)
			}
			candidate.Spec.Name = "Настроенная интеграция"
			candidate.Metadata.Version = "3.0.0"
			// Удаляем effect capabilities, сохраняя обязательный health operation.
			capabilities := candidate.Spec.Capabilities[:0:0]
			for _, capability := range candidate.Spec.Capabilities {
				if capability.Risk != "READ" {
					continue
				}
				capability.ApprovalPolicy = string(ApprovalHumanEachEffect)
				capability.Execution.TimeoutSeconds = 1
				capability.Execution.MaxAttempts = 1
				capabilities = append(capabilities, capability)
			}
			candidate.Spec.Capabilities = capabilities
			candidate = reparseManaged(t, candidate)
			if err := ValidateExecutableRevision(candidate, baseline); err != nil {
				t.Fatalf("restricted %s/%s: %v", key, origin, err)
			}
		}
	}
}

func TestManagedRevisionRejectsContractExpansionAndStaleDigest(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	baseline := definitions["github"]
	for name, mutate := range map[string]func(*Package){
		"adapter":        func(p *Package) { p.Spec.Adapter = "GITLAB" },
		"credential":     func(p *Package) { p.Spec.Credential.SecretKey = "other" },
		"network":        func(p *Package) { p.Spec.NetworkDestinations[0].Hostname = "other.example" },
		"new operation":  func(p *Package) { p.Spec.Capabilities[0].Operation = "github.unregistered.read" },
		"new capability": func(p *Package) { p.Spec.Capabilities[0].Key = "github.unregistered.read" },
		"output": func(p *Package) {
			p.Spec.Capabilities[0].OutputFields[0].Required = !p.Spec.Capabilities[0].OutputFields[0].Required
		},
		"scope":                 func(p *Package) { p.Spec.Capabilities[0].ResourceScope.ConnectionFields = []string{"owner"} },
		"configuration removed": func(p *Package) { p.Spec.ConfigurationFields = p.Spec.ConfigurationFields[1:] },
		"health timeout":        func(p *Package) { p.Spec.HealthCheck.TimeoutSeconds++ },
		"attempts":              func(p *Package) { p.Spec.Capabilities[0].Execution.MaxAttempts++ },
		"timeout":               func(p *Package) { p.Spec.Capabilities[0].Execution.TimeoutSeconds++ },
		"backoff":               func(p *Package) { p.Spec.Capabilities[0].Execution.RetryBackoffMilliseconds-- },
		"weaken gate": func(p *Package) {
			for i := range p.Spec.Capabilities {
				if p.Spec.Capabilities[i].Risk != "READ" {
					p.Spec.Capabilities[i].ApprovalPolicy = "NONE"
					return
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := reparseManaged(t, baseline)
			candidate.Metadata.Origin = OriginUI
			mutate(&candidate)
			raw, _ := json.Marshal(candidate)
			candidate, err := Parse(raw)
			if err == nil && ValidateExecutableRevision(candidate, baseline) == nil {
				t.Fatal("expanded contract accepted")
			}
		})
	}
	stale := reparseManaged(t, baseline)
	stale.Spec.Name = "Изменение без нового digest"
	if ValidateExecutableRevision(stale, baseline) == nil {
		t.Fatal("stale digest accepted")
	}
	spoofed := reparseManaged(t, stale)
	if ValidateExecutableRevision(spoofed, baseline) == nil {
		t.Fatal("modified shipped identity accepted")
	}
	if _, _, err := NormalizeManagedRevision([]byte(shippedYAML["github.yaml"]), Origin, definitions); err == nil {
		t.Fatal("caller selected shipped owner")
	}
	if _, _, err := NormalizeManagedRevision([]byte(strings.Replace(shippedYAML["github.yaml"], "key: github", "key: unregistered", 1)), OriginUI, definitions); err == nil {
		t.Fatal("unregistered adapter key accepted")
	}
}

func TestManagedFieldsOnlyNarrow(t *testing.T) {
	base := []Field{{Key: "value", Type: "STRING", Format: "PLAIN", Required: true, MaximumLength: 120, AllowedValues: []string{"a", "b"}}}
	narrow := []Field{{Key: "value", Type: "STRING", Format: "PLAIN", Required: true, MaximumLength: 10, AllowedValues: []string{"a"}}}
	if !narrowFields(narrow, base) {
		t.Fatal("narrow string rejected")
	}
	for _, change := range []func(*Field){
		func(f *Field) { f.Required = false }, func(f *Field) { f.MaximumLength = 121 },
		func(f *Field) { f.AllowEmpty = true }, func(f *Field) { f.AllowedValues = nil },
		func(f *Field) { f.AllowedValues = []string{"c"} }, func(f *Field) { f.Format = "IDENTIFIER" },
	} {
		field := narrow[0]
		change(&field)
		if narrowFields([]Field{field}, base) {
			t.Fatal("expanded string accepted")
		}
	}
	integers := []Field{{Key: "limit", Type: "INTEGER", Minimum: 2, Maximum: 100}}
	if !narrowFields([]Field{{Key: "limit", Type: "INTEGER", Minimum: 3, Maximum: 50}}, integers) {
		t.Fatal("narrow integer rejected")
	}
	for _, field := range []Field{{Key: "limit", Type: "INTEGER", Minimum: 1, Maximum: 50}, {Key: "limit", Type: "INTEGER", Minimum: 3}, {Key: "limit", Type: "INTEGER", Minimum: 3, Maximum: 101}} {
		if narrowFields([]Field{field}, integers) {
			t.Fatal("expanded integer accepted")
		}
	}
}

func reparseManaged(t *testing.T, value Package) Package {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
