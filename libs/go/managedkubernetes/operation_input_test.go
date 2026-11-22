package managedkubernetes

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestOperationInputVariants exercises actual Kubernetes payload families without conflating them.
// TestOperationInputVariants проверяет настоящие Kubernetes payload families без их смешения.
func TestOperationInputVariants(t *testing.T) {
	object := `"group":"","version":"v1","resource":"configmaps","namespace":"example","name":"settings","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17"`
	for _, testCase := range []struct {
		name, operation, raw string
	}{
		{"get", "kubernetes.get", `{ "resource":"pods", "version":"v1", "namespace":"example", "group":"", "name":"web" }`},
		{"core list", "kubernetes.list", `{"group":"","version":"v1","resource":"pods","namespace":"example","options":{"limit":100}}`},
		{"all namespaces", "kubernetes.list.all-namespaces", `{"group":"","version":"v1","resource":"pods","namespace":""}`},
		{"create configmap", "kubernetes.create", `{"group":"","version":"v1","resource":"configmaps","namespace":"example","body":{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"example"},"data":{"mode":"safe"}}}`},
		{"create generateName", "kubernetes.create", `{"group":"batch","version":"v1","resource":"jobs","namespace":"example","body":{"apiVersion":"batch/v1","kind":"Job","metadata":{"generateName":"check-","namespace":"example"},"spec":{}}}`},
		{"replace configmap root", "kubernetes.replace", `{` + object + `,"body":{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"example","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17"},"data":{"mode":"safe"}}}`},
		{"apply new object", "kubernetes.apply", `{"group":"","version":"v1","resource":"configmaps","namespace":"example","name":"settings","body":{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"example"},"data":{"mode":"safe"}},"options":{"fieldManager":"kodex"}}`},
		{"merge partial", "kubernetes.patch", `{` + object + `,"body":{"data":{"removed":null}},"options":{"patchType":"MERGE"}}`},
		{"strategic partial", "kubernetes.patch", `{` + object + `,"body":{"metadata":{"labels":{"mode":"safe"}}},"options":{"patchType":"STRATEGIC"}}`},
		{"JSON patch", "kubernetes.patch", `{` + object + `,"body":[{"op":"test","path":"/data/mode","value":"old"},{"op":"add","path":"/data/a~1b","value":null}],"options":{"patchType":"JSON"}}`},
		{"delete options different GVK", "kubernetes.delete", `{"group":"apps","version":"v1","resource":"deployments","namespace":"example","name":"web","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17","body":{"apiVersion":"v1","kind":"DeleteOptions","propagationPolicy":"Foreground","preconditions":{"uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17"}}}`},
		{"identity", "kubernetes.identity", `{}`},
		{"exec typed argv", "kubernetes.exec", `{` + object + `,"options":{"command":["echo","example"],"timeoutSeconds":10}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			input, canonical, err := DecodeOperationInput(testCase.operation, []byte(testCase.raw))
			if err != nil {
				t.Fatalf("accepted input rejected: %v", err)
			}
			_, repeated, err := DecodeOperationInput(testCase.operation, canonical)
			if err != nil || !bytes.Equal(canonical, repeated) {
				t.Fatal("canonical input did not round trip")
			}
			if input.Resource != "" && testCase.operation != "kubernetes.list.all-namespaces" && input.ValidateGrantScope(testCase.operation, "example") != nil {
				t.Fatal("exact namespace grant rejected")
			}
		})
	}
}

// TestOperationInputRejectsAmbiguity verifies closed refusals before any normalized value escapes.
// TestOperationInputRejectsAmbiguity проверяет closed refusals до выдачи normalized value.
func TestOperationInputRejectsAmbiguity(t *testing.T) {
	valid := `{"group":"","version":"v1","resource":"pods","namespace":"example","name":"web"}`
	for _, raw := range []string{
		`null`, `[]`, valid + `{}`, strings.Replace(valid, `"name":"web"`, `"name":"web","name":"other"`, 1),
		strings.Replace(valid, `"name":"web"`, `"Name":"web"`, 1),
		strings.Replace(valid, `"name":"web"`, `"name":null`, 1),
		strings.Replace(valid, `"name":"web"`, `"name":"../other"`, 1),
		strings.Replace(valid, `"version":"v1"`, `"version":""`, 1),
		strings.Replace(valid, `"name":"web"`, `"name":"web","target_ref":"other"`, 1),
		strings.Replace(valid, `"name":"web"`, `"name":"web","options":{"rawFlags":["--token=private"]}`, 1),
		strings.Replace(valid, `"name":"web"`, `"name":"web","resourceVersion":"9"`, 1),
		string(append([]byte(valid[:len(valid)-1]), 0xff, '}')),
		strings.Repeat(" ", MaximumOperationInputBytes) + valid,
	} {
		input, canonical, err := DecodeOperationInput("kubernetes.get", []byte(raw))
		if err != ErrInvalidOperationInput || canonical != nil || input.Name != "" || input.Resource != "" {
			t.Fatal("invalid input escaped the closed boundary")
		}
	}
	if _, _, err := DecodeOperationInput("kubernetes.unregistered", []byte(`{}`)); err == nil {
		t.Fatal("unknown operation acquired an input profile")
	}
}

// TestOperationInputBodyRefusals protects UID/RV witnesses, nullable patch values and union selection.
// TestOperationInputBodyRefusals защищает UID/RV witnesses, nullable patch values и union selection.
func TestOperationInputBodyRefusals(t *testing.T) {
	base := `"group":"","version":"v1","resource":"configmaps","namespace":"example","name":"settings","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17"`
	for _, testCase := range []struct{ operation, raw string }{
		{"kubernetes.patch", `{` + base + `,"body":{"data":{"x":1}},"options":{"PatchType":"MERGE"}}`},
		{"kubernetes.patch", `{` + base + `,"body":{"data":{"x":1}},"options":{"patchType":"MERGE","patchType":"JSON"}}`},
		{"kubernetes.patch", `{` + base + `,"body":{"data":{"x":1}},"options":{"patchType":"JSON"}}`},
		{"kubernetes.patch", `{` + base + `,"body":[{"op":"remove","path":"/data/x"}],"options":{"patchType":"MERGE"}}`},
		{"kubernetes.patch", `{` + base + `,"body":[{"op":"add","path":"/data/x"}],"options":{"patchType":"JSON"}}`},
		{"kubernetes.patch", `{` + base + `,"body":[{"op":"remove","path":"/data/~9x"}],"options":{"patchType":"JSON"}}`},
		{"kubernetes.patch", `{` + base + `,"body":[{"op":"remove","path":"/data/x","value":1}],"options":{"patchType":"JSON"}}`},
		{"kubernetes.patch", `{` + base + `,"body":{"data":{"x":1,"x":2}},"options":{"patchType":"MERGE"}}`},
		{"kubernetes.patch", `{` + base + `,"body":null,"options":{"patchType":"MERGE"}}`},
		{"kubernetes.delete", `{` + base + `,"body":{"apiVersion":"apps/v1","kind":"Deployment"}}`},
		{"kubernetes.delete", `{` + base + `,"body":{"preconditions":{"resourceVersion":"18"}}}`},
		{"kubernetes.replace", `{` + base + `,"body":{"apiVersion":"v2","kind":"ConfigMap","metadata":{"name":"settings","namespace":"example","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17"}}}`},
		{"kubernetes.replace", `{` + base + `,"body":{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"other","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17"}}}`},
		{"kubernetes.create", `{"group":"","version":"v1","resource":"pods","namespace":"example","body":{"apiVersion":"v1","kind":"Pod","metadata":{"namespace":"example","name":"web","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8"}}}`},
	} {
		if _, _, err := DecodeOperationInput(testCase.operation, []byte(testCase.raw)); err != ErrInvalidOperationInput {
			t.Fatalf("unsafe body accepted for %s", testCase.operation)
		}
	}
	deep := `{"x":` + strings.Repeat(`{"x":`, 20) + `null` + strings.Repeat(`}`, 20) + `}`
	if _, _, err := DecodeOperationInput("kubernetes.patch", []byte(`{`+base+`,"body":`+deep+`,"options":{"patchType":"MERGE"}}`)); err == nil {
		t.Fatal("excessive body nesting was accepted")
	}
}

// TestOperationInputScopeSeparation prevents ordinary list and node grants from widening namespaces.
// TestOperationInputScopeSeparation запрещает ordinary list и node grants расширять namespaces.
func TestOperationInputScopeSeparation(t *testing.T) {
	for _, testCase := range []struct {
		operation, scope, inputNamespace string
		allowed                          bool
	}{
		{"kubernetes.list", "example", "example", true},
		{"kubernetes.list", "example", "other", false},
		{"kubernetes.list", "@all-namespaces", "", false},
		{"kubernetes.list", "@cluster", "", true},
		{"kubernetes.list.all-namespaces", "@all-namespaces", "", true},
		{"kubernetes.watch.all-namespaces", "@all-namespaces", "example", false},
		{"kubernetes.list.all-namespaces", "example", "example", false},
		{"kubernetes.identity", "@cluster", "", true},
		{"kubernetes.identity", "example", "", false},
		{"kubernetes.drain", "@cluster", "", true},
		{"kubernetes.drain", "example", "example", false},
		{"kubernetes.delete", "*", "*", false},
		{"kubernetes.get", "", "", false},
	} {
		input := OperationInput{Namespace: testCase.inputNamespace}
		if (input.ValidateGrantScope(testCase.operation, testCase.scope) == nil) != testCase.allowed {
			t.Fatalf("unexpected grant admission for %s/%s", testCase.operation, testCase.scope)
		}
	}
}

// TestOperationInputOwnedSchemas verifies all known schemas compile and returned bytes cannot mutate cache.
// TestOperationInputOwnedSchemas проверяет compile всех known schemas и неизменность cache через returned bytes.
func TestOperationInputOwnedSchemas(t *testing.T) {
	for _, operation := range OperationInputOperations() {
		raw, err := OperationInputSchema(operation)
		if err != nil || !json.Valid(raw) {
			t.Fatalf("invalid owned schema for %s: %v", operation, err)
		}
		raw[0] = '['
		repeated, err := OperationInputSchema(operation)
		if err != nil || !json.Valid(repeated) || bytes.Equal(raw, repeated) {
			t.Fatal("caller mutated the cached schema")
		}
	}
}
