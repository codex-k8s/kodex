package integrationpackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestGitHubBundleRollingCompatibility keeps the one known old version usable without gaining the new capability, while unknown versions or wrong digests remain closed.
// TestGitHubBundleRollingCompatibility сохраняет работоспособность одной известной старой version без новой capability, закрыто отклоняя unknown versions и wrong digests.
func TestGitHubBundleRollingCompatibility(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	current := definitions["github"]
	if current.Metadata.Version != "6.2.0" {
		t.Fatal("new bundle release missing from generated shipped catalog")
	}
	if _, found := current.Capability("github.repository.bundle.publish"); !found {
		t.Fatal("new typed bundle capability is unavailable")
	}
	previous, err := Parse(githubSixCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	resolved, found := ResolveShippedRevision(current, "6.0.0", previous.Digest)
	if !found || resolved.Metadata.Version != "6.0.0" || resolved.Digest != previous.Digest {
		t.Fatal("known old connection cannot resolve its exact package pins")
	}
	if _, found := resolved.Capability("github.repository.bundle.publish"); found {
		t.Fatal("old grant inherited new bulk authority")
	}
	for _, invalid := range []struct{ version, digest string }{{"5.9.0", previous.Digest}, {"6.2.0", previous.Digest}, {"6.0.0", current.Digest}, {"6.0.0", "wrong"}} {
		if _, found := ResolveShippedRevision(current, invalid.version, invalid.digest); found {
			t.Fatal("unknown shipped revision or substituted digest accepted")
		}
	}
	if _, found := ResolveShippedRevision(definitions["email"], "6.0.0", previous.Digest); found {
		t.Fatal("GitHub compatibility widened another adapter")
	}
	if bytes.Equal(githubSixCompatibility, []byte(shippedYAML["github.yaml"])) {
		t.Fatal("compatibility snapshot was silently replaced with the new package")
	}
}

// TestGitHubSixCompatibilityBytes pins the exact pre-upgrade definition rather than deriving historical permissions by removing fields from a future package.
// TestGitHubSixCompatibilityBytes закрепляет exact pre-upgrade definition вместо вывода historical permissions удалением полей будущего package.
func TestGitHubSixCompatibilityBytes(t *testing.T) {
	actual := sha256.Sum256(githubSixCompatibility)
	if hex.EncodeToString(actual[:]) != "433db658d85c257b998cf0baf7135ba499ad3bfc41dcd749b5ca12a7d6f8be99" {
		t.Fatal("compatibility snapshot differs from the pre-upgrade release")
	}
}

// TestGitHubSixOneCompatibilityPreservesApprovalPolicies proves that old file and bundle grants do not inherit the new autonomous choices, including under wrong identity or substituted digest.
// TestGitHubSixOneCompatibilityPreservesApprovalPolicies доказывает отсутствие новых автономных политик у старых file и bundle grants, включая подмену идентичности или digest.
func TestGitHubSixOneCompatibilityPreservesApprovalPolicies(t *testing.T) {
	actual := sha256.Sum256(githubSixOneCompatibility)
	if hex.EncodeToString(actual[:]) != "75e4a742702ebf1600fb793691407c5db68cd7886d795c3e1ef60a71247afb9d" {
		t.Fatal("compatibility snapshot differs from the shipped bundle release")
	}
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	current := definitions["github"]
	previous, err := Parse(githubSixOneCompatibility)
	if err != nil || previous.Digest != "057959121df2b88db45d9b0dba438a7ff39824992fcf1c13120a44162071c79f" {
		t.Fatal("historical package identity changed")
	}
	resolved, found := ResolveShippedRevision(current, "6.1.0", previous.Digest)
	if !found || resolved.Digest != previous.Digest {
		t.Fatal("historical bundle package cannot resolve exact pins")
	}
	for _, key := range []string{"github.issue.create", "github.issue.update", "github.repository.content.create", "github.repository.content.update", "github.repository.bundle.publish"} {
		capability, found := resolved.Capability(key)
		if !found || capability.AllowsApprovalPolicy("NONE") || capability.ApprovalPolicy != "HUMAN_EACH_EFFECT" {
			t.Fatal("historical operation inherited autonomous authority")
		}
	}
	for _, origin := range []string{OriginUI, OriginGit} {
		candidate := current
		candidate.Metadata.Origin = origin
		if _, found := ResolveShippedRevision(candidate, "6.1.0", previous.Digest); found {
			t.Fatal("managed identity acquired shipped compatibility authority")
		}
	}
	if _, found := ResolveShippedRevision(current, "6.1.0", current.Digest); found {
		t.Fatal("historical version accepted a substituted digest")
	}
}
