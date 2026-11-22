package managedkubernetes

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestIdentityLocatorRequiresPurposeStoragePins keeps private identity metadata outside generic
// JSON and refuses partial, substituted or noncanonical purpose storage identities.
// TestIdentityLocatorRequiresPurposeStoragePins исключает приватные metadata из общего JSON
// и отклоняет неполные, подменённые либо неканонические purpose storage identities.
func TestIdentityLocatorRequiresPurposeStoragePins(t *testing.T) {
	name, err := IdentitySecretName("kbi_synthetic_identity", 3)
	if err != nil {
		t.Fatal("derive synthetic identity name")
	}
	valid := IdentityLocator{IdentityRef: "kbi_synthetic_identity", TargetRef: "target_synthetic", Revision: 3,
		Namespace: "broker-private", Name: name, UID: "76b0b43a-84c2-44d6-81a4-7c477a92ef2e", ResourceVersion: "42", ContentSHA256: strings.Repeat("a", 64)}
	if valid.Validate("broker-private") != nil {
		t.Fatal("complete purpose locator was rejected")
	}
	for _, mutate := range []func(*IdentityLocator){
		func(v *IdentityLocator) { v.IdentityRef = "" },
		func(v *IdentityLocator) { v.IdentityRef = "another_identity" },
		func(v *IdentityLocator) { v.TargetRef = "" },
		func(v *IdentityLocator) { v.Namespace = "another-private" },
		func(v *IdentityLocator) { v.Name = "runtime-secret" },
		func(v *IdentityLocator) { v.Revision++ },
		func(v *IdentityLocator) { v.Revision = 0 },
		func(v *IdentityLocator) { v.UID = "00000000-0000-0000-0000-000000000000" },
		func(v *IdentityLocator) { v.UID = strings.ToUpper(v.UID) },
		func(v *IdentityLocator) { v.ResourceVersion = "" },
		func(v *IdentityLocator) { v.ContentSHA256 = strings.Repeat("A", 64) },
	} {
		changed := valid
		mutate(&changed)
		if changed.Validate("broker-private") == nil {
			t.Fatal("partial or substituted private locator was accepted")
		}
	}
	raw, err := json.Marshal(valid)
	if err != nil || string(raw) != "{}" || fmt.Sprintf("%v", valid) != identityLocatorRedacted || fmt.Sprintf("%#v", valid) != identityLocatorRedacted {
		t.Fatal("private purpose storage pins escaped diagnostic boundary")
	}
}

// TestIdentityProvisionStaysSeparateFromExecutionLocator refuses incomplete pre-create pins
// and keeps private coordinates out of generic JSON and diagnostic formatting.
// TestIdentityProvisionStaysSeparateFromExecutionLocator отклоняет неполные pre-create pins
// и скрывает приватные coordinates из общего JSON и diagnostic formatting.
func TestIdentityProvisionStaysSeparateFromExecutionLocator(t *testing.T) {
	valid := IdentityProvision{IdentityRef: "kbi_synthetic_identity", TargetRef: "target_synthetic", Revision: 3,
		Namespace: "broker-private", ContentSHA256: strings.Repeat("a", 64)}
	if valid.Validate("broker-private") != nil {
		t.Fatal("valid pre-create pins were rejected")
	}
	for _, mutate := range []func(*IdentityProvision){
		func(v *IdentityProvision) { v.IdentityRef = "" },
		func(v *IdentityProvision) { v.TargetRef = "" },
		func(v *IdentityProvision) { v.Revision = 0 },
		func(v *IdentityProvision) { v.Namespace = "another-private" },
		func(v *IdentityProvision) { v.ContentSHA256 = "" },
	} {
		changed := valid
		mutate(&changed)
		if changed.Validate("broker-private") == nil {
			t.Fatal("incomplete pre-create pins became materializable")
		}
	}
	raw, err := json.Marshal(valid)
	if err != nil || string(raw) != "{}" || fmt.Sprintf("%v", valid) != identityLocatorRedacted || fmt.Sprintf("%#v", valid) != identityLocatorRedacted {
		t.Fatal("pre-create metadata escaped the private boundary")
	}
}
