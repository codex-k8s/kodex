package app

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// policyConfigurerFixture records only validated bootstrap intent, allowing tests to detect premature or repeated authority configuration.
// policyConfigurerFixture сохраняет лишь validated bootstrap intent, позволяя обнаружить преждевременную и повторную authority configuration.
type policyConfigurerFixture struct {
	policy value.OIDCIdentityPolicy
	calls  int
}

// ConfigureStandardOIDCIdentity captures the value without installing an identity provider, claiming an owner or writing persistent session state.
// ConfigureStandardOIDCIdentity сохраняет value без установки identity provider, назначения owner и записи persistent session state.
func (fixture *policyConfigurerFixture) ConfigureStandardOIDCIdentity(policy value.OIDCIdentityPolicy) error {
	fixture.policy = policy
	fixture.calls++
	return nil
}

// TestOIDCIdentityPolicyLoader proves explicit optional selection, bounded closed documents and exact issuer matching before any owner configuration.
// TestOIDCIdentityPolicyLoader проверяет явный optional selection, bounded closed documents и exact issuer до любой owner configuration.
func TestOIDCIdentityPolicyLoader(t *testing.T) {
	policy := value.OIDCIdentityPolicy{Schema: "kodex.oidc-identity-policy/v1", Revision: 1,
		Issuer: "https://identity.example.invalid/realm/synthetic", SessionLifetimeSeconds: 3600,
		BootstrapSubjects: []string{"20000000-0000-4000-8000-000000000001"}}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	file := t.TempDir() + "/identity-policy.json"
	if os.WriteFile(file, raw, 0600) != nil {
		t.Fatal("write synthetic identity policy")
	}
	owner := &policyConfigurerFixture{}
	if configureOIDCIdentityPolicy(owner, Config{}) != nil || owner.calls != 0 {
		t.Fatal("default claims mode unexpectedly required or applied platform policy")
	}
	config := Config{OIDCAuthorityMode: "platform-managed", OIDCIssuer: policy.Issuer, OIDCIdentityPolicyFile: file}
	if err := configureOIDCIdentityPolicy(owner, config); err != nil || owner.calls != 1 || owner.policy.Revision != 1 {
		t.Fatal("valid explicit policy failed to reach the bootstrap owner")
	}
	for _, invalid := range []string{
		string(raw[:len(raw)-1]) + `,"organization_id":"caller-selected"}`,
		string(raw) + ` {}`,
		strings.Replace(string(raw), `"revision":1`, `"revision":0`, 1),
		strings.Replace(string(raw), policy.Issuer, "https://other.example.invalid", 1),
		strings.Repeat(" ", 16385),
		`{"schema":"kodex.oidc-identity-policy/v1"}`,
	} {
		if os.WriteFile(file, []byte(invalid), 0600) != nil {
			t.Fatal("write invalid synthetic policy")
		}
		before := owner.calls
		if configureOIDCIdentityPolicy(owner, config) == nil || owner.calls != before {
			t.Fatal("invalid policy reached owner bootstrap configuration")
		}
	}
	for _, mode := range []string{"claims", "automatic"} {
		config.OIDCAuthorityMode = mode
		before := owner.calls
		if configureOIDCIdentityPolicy(owner, config) == nil || owner.calls != before {
			t.Fatal("ambiguous mode/file selection silently chose a contract")
		}
	}
}
