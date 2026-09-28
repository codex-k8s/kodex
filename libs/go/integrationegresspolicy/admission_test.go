package integrationegresspolicy

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

func TestPublicationAdmissionAndCreationBoundary(t *testing.T) {
	policy, binding := PublicationAdmissionResources()
	boundary, boundaryBinding := CreationBoundaryResources()
	raw, err := os.ReadFile("../../../deploy/k8s/base/egress-gateway/integration/publication-admission.json")
	if err != nil {
		t.Fatal(err)
	}
	var artifact map[string]any
	if json.Unmarshal(raw, &artifact) != nil || !reflect.DeepEqual(artifact["items"], []any{policy, binding, boundary, boundaryBinding}) {
		t.Fatal("integration admission artifact differs from shared source")
	}
	environment, err := cel.NewEnv(cel.Variable("object", cel.DynType), cel.Variable("request", cel.DynType))
	if err != nil {
		t.Fatal(err)
	}
	program := func(expression string) cel.Program {
		t.Helper()
		ast, issues := environment.Compile(expression)
		if issues.Err() != nil {
			t.Fatal(issues.Err())
		}
		compiled, err := environment.Program(ast)
		if err != nil {
			t.Fatal(err)
		}
		return compiled
	}
	request := map[string]any{"userInfo": map[string]any{"username": "system:serviceaccount:kodex-system:control-plane"}}
	produced, err := RenderFiles(Document{Schema: Schema, Generation: 1,
		SourceDigest: SourceDigest(nil), GatewayPolicyDigest: strings.Repeat("a", 64), Destinations: []Destination{}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if json.Unmarshal(produced["integration-configmap.json"], &object) != nil {
		t.Fatal("invalid rendered fixture")
	}
	evaluate := func(spec map[string]any, object map[string]any) bool {
		condition := spec["matchConditions"].([]any)[0].(map[string]any)["expression"].(string)
		matched, _, err := program(condition).Eval(map[string]any{"object": object, "request": request})
		if err != nil || matched != types.True {
			return false
		}
		for _, item := range spec["validations"].([]any) {
			valid, _, err := program(item.(map[string]any)["expression"].(string)).Eval(map[string]any{"object": object, "request": request})
			if err != nil || valid != types.True {
				return false
			}
		}
		return true
	}
	if !evaluate(policy["spec"].(map[string]any), object) || !evaluate(boundary["spec"].(map[string]any), object) {
		t.Fatal("exact producer ConfigMap rejected")
	}
	for name, mutate := range map[string]func(map[string]any){
		"foreign name": func(value map[string]any) { value["metadata"].(map[string]any)["name"] = "other-configmap" },
		"mutable":      func(value map[string]any) { value["immutable"] = false },
		"extra data":   func(value map[string]any) { value["data"].(map[string]any)["secret"] = "value" },
		"wrong schema": func(value map[string]any) {
			value["data"].(map[string]any)["integration-policy.json"] = `{"schema":"other"}`
		},
	} {
		t.Run(name, func(t *testing.T) {
			var changed map[string]any
			encoded, _ := json.Marshal(object)
			_ = json.Unmarshal(encoded, &changed)
			mutate(changed)
			if name == "foreign name" {
				if evaluate(boundary["spec"].(map[string]any), changed) {
					t.Fatal("unregistered ConfigMap name escaped creation boundary")
				}
				return
			}
			if evaluate(policy["spec"].(map[string]any), changed) && evaluate(boundary["spec"].(map[string]any), changed) {
				t.Fatal("invalid ConfigMap escaped all admission policies")
			}
		})
	}
}
