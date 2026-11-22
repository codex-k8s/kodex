package kubernetestarget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// credentialResourcePolicyFixture adds canonical operator metadata without changing the synthetic
// credential's route or authentication. It is syntax data, not a live controller qualification.
// credentialResourcePolicyFixture добавляет canonical operator metadata без изменения synthetic
// credential route/authentication. Это syntax data, не live controller qualification.
func credentialResourcePolicyFixture(t *testing.T, target Target) Target {
	t.Helper()
	profile := &managedkubernetes.ResourcePolicy{Controllers: []managedkubernetes.ControllerQualification{{
		Group: "custom.example.test", Version: "v1", Resource: "workers", Kind: "Worker", SchemaSHA256: strings.Repeat("a", 64),
		ControllerKind: "Deployment", ControllerNamespace: "controllers", ControllerName: "worker-controller",
		ControllerUID: "11111111-1111-4111-8111-111111111111", ControllerContainer: "manager", ControllerImageDigest: strings.Repeat("b", 64),
		TemplateSHA256: strings.Repeat("c", 64), Consumer: managedkubernetes.ControllerConsumerDataOnlyV1, Extractor: "NONE"}}}
	raw, err := json.Marshal(profile)
	if err != nil || profile.Validate() != nil {
		t.Fatal("invalid owned policy fixture")
	}
	digest := sha256.Sum256(raw)
	target.ResourcePolicy, target.ResourcePolicySHA256 = profile, hex.EncodeToString(digest[:])
	return target
}

// TestCredentialResourcePolicyPinsAndClone holds the exact source checksum and detached metadata
// while awaiting upper admission. Caller mutation cannot retarget the parsed credential's policy.
// TestCredentialResourcePolicyPinsAndClone сохраняет exact source checksum и detached metadata
// при ожидании upper admission. Caller mutation не меняет policy parsed credential.
func TestCredentialResourcePolicyPinsAndClone(t *testing.T) {
	config, target, now := credentialFixture(t)
	target = credentialResourcePolicyFixture(t, target)
	credential, err := ParseCredential(encodeCredential(t, config), target, now)
	if err != nil {
		t.Fatal("pinned profile was rejected")
	}
	target.ResourcePolicy.Controllers[0].ControllerName = "substituted"
	if credential.target.ResourcePolicy.Controllers[0].ControllerName != "worker-controller" || validateTarget(credential.target) != nil {
		t.Fatal("caller mutation changed a retained private profile")
	}
	for _, mutation := range []string{"missing digest", "wrong digest", "missing profile", "unqualified type"} {
		candidate := credentialResourcePolicyFixture(t, target)
		switch mutation {
		case "missing digest":
			candidate.ResourcePolicySHA256 = ""
		case "wrong digest":
			candidate.ResourcePolicySHA256 = strings.Repeat("d", 64)
		case "missing profile":
			candidate.ResourcePolicy = nil
		case "unqualified type":
			candidate.ResourcePolicy.Controllers[0].Consumer = "ALLOW"
		}
		if _, err := ParseCredential(encodeCredential(t, config), candidate, now); err == nil {
			t.Fatal("unbound or unknown policy entered credential use")
		}
	}
}
