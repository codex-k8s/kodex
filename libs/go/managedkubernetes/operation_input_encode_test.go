package managedkubernetes

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestEncodeOperationInputPreservesExplicitEmptyCoordinates proves the typed producer can
// round-trip core, cluster/all-namespace and empty identity inputs through the real schema.
// TestEncodeOperationInputPreservesExplicitEmptyCoordinates доказывает typed producer round-trip
// core, cluster/all-namespace и empty identity input через настоящую schema.
func TestEncodeOperationInputPreservesExplicitEmptyCoordinates(t *testing.T) {
	for _, test := range []struct {
		operation, raw string
	}{
		{"kubernetes.get", `{"group":"","version":"v1","resource":"configmaps","namespace":"example","name":"selected"}`},
		{"kubernetes.get", `{"group":"","version":"v1","resource":"nodes","namespace":"","name":"selected"}`},
		{"kubernetes.list.all-namespaces", `{"group":"","version":"v1","resource":"pods","namespace":""}`},
		{"kubernetes.watch.all-namespaces", `{"group":"","version":"v1","resource":"pods","namespace":"","options":{"timeoutSeconds":10}}`},
		{"kubernetes.identity", `{}`},
		{"kubernetes.help", `{}`},
		{"kubernetes.create", `{"group":"","version":"v1","resource":"configmaps","namespace":"example","body":{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"selected","namespace":"example"}}}`},
	} {
		input, original, err := DecodeOperationInput(test.operation, []byte(test.raw))
		if err != nil {
			t.Fatal("invalid owned encoder fixture")
		}
		encoded, err := EncodeOperationInput(test.operation, input)
		if err != nil || !bytes.Equal(encoded, original) {
			t.Fatalf("typed round-trip changed required coordinates for %s", test.operation)
		}
	}
}

// TestEncodeOperationInputRejectsLossyOrIncompleteData closes invalid UTF-8 before replacement,
// retains duplicate/alias rejection and never fabricates required object identity or fields.
// TestEncodeOperationInputRejectsLossyOrIncompleteData закрывает invalid UTF-8 до replacement,
// сохраняет duplicate/alias rejection и не выдумывает required object identity либо fields.
func TestEncodeOperationInputRejectsLossyOrIncompleteData(t *testing.T) {
	valid := OperationInput{Version: "v1", Resource: "configmaps", Namespace: "example", Name: "selected"}
	for _, mutation := range []string{"utf8 coordinate", "utf8 body", "utf8 options", "duplicate body", "alias", "missing name", "unexpected field"} {
		t.Run(mutation, func(t *testing.T) {
			input, operation := valid, "kubernetes.get"
			switch mutation {
			case "utf8 coordinate":
				input.Name = string([]byte{0xff})
			case "utf8 body":
				input.Body = json.RawMessage("{\"name\":\"\xff\"}")
			case "utf8 options":
				input.Options = json.RawMessage("{\"name\":\"\xff\"}")
			case "duplicate body":
				operation = "kubernetes.create"
				input.Name = ""
				input.Body = json.RawMessage(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"first","name":"second","namespace":"example"}}`)
			case "alias":
				input.Options = json.RawMessage(`{"Limit":1}`)
				input.Name, operation = "", "kubernetes.list"
			case "missing name":
				input.Name = ""
			case "unexpected field":
				input.Body = json.RawMessage(`{}`)
			}
			if encoded, err := EncodeOperationInput(operation, input); err == nil || len(encoded) != 0 {
				t.Fatal("lossy/ambiguous/incomplete input acquired canonical execution bytes")
			}
		})
	}
	if _, err := EncodeOperationInput("kubernetes.unknown", valid); err == nil {
		t.Fatal("unknown operation acquired an encoder fallback")
	}
}
