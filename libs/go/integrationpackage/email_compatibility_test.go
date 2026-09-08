package integrationpackage

import (
	"encoding/json"
	"testing"
)

func TestManagedEmailCredentialAndImmutableCompatibility(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	current := definitions["email"]
	if current.Spec.Credential != nil || current.RequiresConnectionCredential() {
		t.Fatal("managed mailbox requires generic credential")
	}
	legacy, ok := ResolveShippedRevision(current, "1.4.0", legacyEmailDigest)
	if !ok || legacy.Digest == current.Digest || legacy.Spec.Credential == nil || legacy.RequiresConnectionCredential() {
		t.Fatal("legacy exact pins lost")
	}
	if ValidateExecutableRevision(legacy, current) != nil {
		t.Fatal("legacy reader rejected")
	}
	if _, ok := ResolveShippedRevision(current, "1.4.0", current.Digest); ok {
		t.Fatal("version/digest substitution accepted")
	}
	for _, origin := range []string{OriginUI, OriginGit} {
		candidate := legacy
		candidate.Metadata.Origin = origin
		raw, _ := json.Marshal(candidate)
		candidate, err = Parse(raw)
		if err != nil || ValidateExecutableRevision(candidate, current) != nil {
			t.Fatal("managed legacy revision rejected")
		}
		candidate.Spec.Credential.SecretKey = "other"
		raw, _ = json.Marshal(candidate)
		candidate, err = Parse(raw)
		if err != nil || ValidateExecutableRevision(candidate, current) == nil {
			t.Fatal("unknown credential accepted")
		}
	}
	legacy.Spec.Capabilities[0].Name += "changed"
	raw, _ := json.Marshal(legacy)
	modified, err := Parse(raw)
	if err != nil || ValidateExecutableRevision(modified, current) == nil {
		t.Fatal("modified shipped revision accepted")
	}
	if !definitions["github"].RequiresConnectionCredential() {
		t.Fatal("provider credential boundary changed")
	}
}
