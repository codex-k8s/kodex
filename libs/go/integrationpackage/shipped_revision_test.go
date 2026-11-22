package integrationpackage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// TestShippedRevisionRequiresExactCurrentPins accepts only the current email version/digest and
// managed narrowing, without reintroducing obsolete mailbox credentials.
// TestShippedRevisionRequiresExactCurrentPins допускает лишь текущие версию/дайджест email и
// сужение управляемой версии без возврата устаревших реквизитов ящика.
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
	for _, version := range []string{"1.4.0", "1.4.1", "1.4.2"} {
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

// TestEmailPreviousReleaseDoesNotInheritPersonalAuthority freezes the exact public1.4.2 package
// from main5582a9d5b5e63175bf325e9d171bd505a1fe4947. Its real old digest and NONE send policy
// must not become executable under the personal-mailbox successor through shipped or managed pins.
// TestEmailPreviousReleaseDoesNotInheritPersonalAuthority закрепляет точный public package1.4.2
// из main5582a9d5b5e63175bf325e9d171bd505a1fe4947. Настоящий старый digest и NONE send policy
// не становятся исполняемыми под personal-mailbox successor через shipped либо managed pins.
func TestEmailPreviousReleaseDoesNotInheritPersonalAuthority(t *testing.T) {
	raw, err := os.ReadFile("testdata/email-1.4.2.yaml")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "29ad9dd139b2f15b7e14a23566b479eae5513778bd74d9832eab6c85c5ca2689" {
		t.Fatal("frozen previous email package source changed")
	}
	previous, err := Parse(raw)
	if err != nil || previous.Metadata.Version != "1.4.2" || previous.Spec.Adapter != "EMAIL_HTTPS" || previous.Spec.Credential != nil {
		t.Fatal("invalid frozen previous email package", err)
	}
	send, ok := previous.Capability("email.message.send")
	if !ok || send.ApprovalPolicy != string(ApprovalNone) || !send.AllowsApprovalPolicy(string(ApprovalNone)) {
		t.Fatal("previous release fixture no longer represents its actual NONE send policy")
	}
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	current := definitions["email"]
	if _, ok := ResolveShippedRevision(current, previous.Metadata.Version, previous.Digest); ok {
		t.Fatal("real previous email pins inherited personal mailbox authority")
	}
	for _, origin := range []string{Origin, OriginUI, OriginGit} {
		candidate := previous
		candidate.Metadata.Origin = origin
		encoded, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		candidate, err = Parse(encoded)
		if err != nil || ValidateExecutableRevision(candidate, current) == nil {
			t.Fatal("previous email release bypassed current executable profile", origin, err)
		}
	}
}
