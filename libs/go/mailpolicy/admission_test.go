package mailpolicy

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

func TestPublicationAdmissionGeneratedReadbackAndClosedBoundary(t *testing.T) {
	policy, binding := PublicationAdmissionResources()
	raw, err := os.ReadFile("../../../deploy/k8s/base/egress-gateway/mail/publication-admission.json")
	if err != nil {
		t.Fatal(err)
	}
	var list map[string]any
	if json.Unmarshal(raw, &list) != nil || !reflect.DeepEqual(list["items"], []any{policy, binding}) {
		t.Fatal("admission artifact does not match shared owner contract")
	}
	environment, err := cel.NewEnv(cel.Variable("object", cel.DynType), cel.Variable("request", cel.DynType))
	if err != nil {
		t.Fatal(err)
	}
	spec := policy["spec"].(map[string]any)
	compile := func(expression string) cel.Program {
		t.Helper()
		ast, issues := environment.Compile(expression)
		if issues.Err() != nil {
			t.Fatal(issues.Err())
		}
		program, err := environment.Program(ast)
		if err != nil {
			t.Fatal(err)
		}
		return program
	}
	condition := compile(spec["matchConditions"].([]any)[0].(map[string]any)["expression"].(string))
	programs := []cel.Program{}
	for _, validation := range spec["validations"].([]any) {
		programs = append(programs, compile(validation.(map[string]any)["expression"].(string)))
	}
	document := MailDocument{Schema: MailSchema, ConfigurationRevision: 2, ConfigurationDigest: strings.Repeat("a", 64), GatewayPolicyDigest: strings.Repeat("b", 64), Destinations: []MailDestination{}}
	files, err := RenderFiles(document)
	if err != nil {
		t.Fatal(err)
	}
	newObject := func() map[string]any {
		var object map[string]any
		if json.Unmarshal(files["mail-configmap.json"], &object) != nil {
			t.Fatal("invalid fixture")
		}
		return object
	}
	request := map[string]any{"operation": "CREATE", "userInfo": map[string]any{"username": "system:serviceaccount:kodex-system:control-plane"}}
	evaluate := func(object map[string]any) bool {
		for _, program := range programs {
			result, _, err := program.Eval(map[string]any{"object": object, "request": request})
			if err != nil || result != types.True {
				return false
			}
		}
		return true
	}
	if result, _, err := condition.Eval(map[string]any{"object": newObject(), "request": request}); err != nil || result != types.True {
		t.Fatal("CP publication bypassed admission")
	}
	if !evaluate(newObject()) {
		t.Fatal("exact producer ConfigMap rejected")
	}
	for name, mutate := range map[string]func(map[string]any){
		"foreign namespace": func(o map[string]any) { o["metadata"].(map[string]any)["namespace"] = "kodex-runtime" },
		"foreign name":      func(o map[string]any) { o["metadata"].(map[string]any)["name"] = "control-plane-runtime" },
		"bad name digest":   func(o map[string]any) { o["metadata"].(map[string]any)["name"] = "egress-gateway-mail-bad" },
		"mutable":           func(o map[string]any) { o["immutable"] = false },
		"missing immutable": func(o map[string]any) { delete(o, "immutable") },
		"binary data":       func(o map[string]any) { o["binaryData"] = map[string]any{"payload": "YQ=="} },
		"missing labels":    func(o map[string]any) { delete(o["metadata"].(map[string]any), "labels") },
		"foreign app": func(o map[string]any) {
			o["metadata"].(map[string]any)["labels"].(map[string]any)["app.kubernetes.io/name"] = "control-plane"
		},
		"foreign component": func(o map[string]any) {
			o["metadata"].(map[string]any)["labels"].(map[string]any)["app.kubernetes.io/component"] = "other"
		},
		"owner reference": func(o map[string]any) {
			o["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{"uid": "foreign"}}
		},
		"missing data": func(o map[string]any) { delete(o, "data") },
		"extra data":   func(o map[string]any) { o["data"].(map[string]any)["other"] = "value" },
		"foreign data": func(o map[string]any) { o["data"] = map[string]any{"mailboxes.json": "value"} },
		"empty":        func(o map[string]any) { o["data"].(map[string]any)["mail-policy.json"] = "" },
		"wrong schema": func(o map[string]any) { o["data"].(map[string]any)["mail-policy.json"] = `{"schema":"unregistered",}` },
		"oversize": func(o map[string]any) {
			o["data"].(map[string]any)["mail-policy.json"] = `{"schema":"egress-mail/v1",` + strings.Repeat("x", 65536)
		},
	} {
		t.Run(name, func(t *testing.T) {
			object := newObject()
			mutate(object)
			if evaluate(object) {
				t.Fatal("CP create escaped exact admission boundary")
			}
		})
	}
	request["userInfo"] = map[string]any{"username": "installation-fixture"}
	if result, _, err := condition.Eval(map[string]any{"object": newObject(), "request": request}); err != nil || result != types.False {
		t.Fatal("CP role boundary unexpectedly changed another installer identity")
	}
}
