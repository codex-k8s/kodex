package integrationpackage

import (
	"encoding/json"
	"testing"
)

func TestShippedRevisionRequiresExactCurrentPins(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	current := definitions["email"]
	if current.Spec.Credential != nil || current.RequiresConnectionCredential() {
		t.Fatal("managed mailbox requires generic credential")
	}
	if _, ok := ResolveShippedRevision(current, current.Metadata.Version, current.Digest); !ok {
		t.Fatal("current exact revision rejected")
	}
	for _, version := range []string{"1.4.0", "1.4.1"} {
		if _, ok := ResolveShippedRevision(current, version, current.Digest); ok {
			t.Fatal("historical shipped revision accepted")
		}
	}
	for _, origin := range []string{OriginUI, OriginGit} {
		candidate := current
		candidate.Metadata.Origin = origin
		candidate.Spec.HealthCheck.TimeoutSeconds--
		raw, _ := json.Marshal(candidate)
		candidate, err = Parse(raw)
		if err != nil || ValidateExecutableRevision(candidate, current) != nil {
			t.Fatal("current managed narrowing rejected")
		}
		candidate.Spec.Credential = &Credential{SecretKey: "token", Kind: "TOKEN"}
		raw, _ = json.Marshal(candidate)
		candidate, err = Parse(raw)
		if err != nil || ValidateExecutableRevision(candidate, current) == nil {
			t.Fatal("historical credential descriptor accepted")
		}
	}
	if !definitions["github"].RequiresConnectionCredential() {
		t.Fatal("provider credential boundary changed")
	}
}
