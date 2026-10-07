package callback

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestEnvironmentWorkspaceLimitsSchemaIsClosedAndBelowPlatformMaximum(t *testing.T) {
	resources := environmentPolicySchema()["properties"].(map[string]any)["resources"].(map[string]any)
	limits := resources["properties"].(map[string]any)["workspaceLimits"].(map[string]any)
	if limits["additionalProperties"] != false || !reflect.DeepEqual(limits["required"], []string{"maxBytes", "maxFiles"}) {
		t.Fatal("workspace budget schema is not closed")
	}
	fields := limits["properties"].(map[string]any)
	for name, maximum := range map[string]int64{"maxBytes": runtimecontract.RuntimeWorkspaceWritableBytes, "maxFiles": runtimecontract.RuntimeWorkspaceMaximumFiles} {
		field := fields[name].(map[string]any)
		if field["type"] != "integer" || field["minimum"] != 1 || field["maximum"] != maximum {
			t.Fatal("quota schema does not match platform bounds")
		}
	}
}
