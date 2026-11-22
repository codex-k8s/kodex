package integrationpackage

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// TestSSHInputPackageMatchesActualTypedBounds passes canonical owner input through the real shipped capability and shared broker codec, including the decoded base64 boundary and required null rejection.
// TestSSHInputPackageMatchesActualTypedBounds передаёт canonical owner input через actual shipped capability и shared broker codec, включая decoded base64 boundary и required null rejection.
func TestSSHInputPackageMatchesActualTypedBounds(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	execute, ok := definitions[SSHExecuteDefinitionKey].Capability("ssh.execute")
	if !ok {
		t.Fatal("SSH execute definition is absent")
	}
	for _, length := range []int{managedssh.MaximumCommand, managedssh.MaximumCommand + 1} {
		raw, _ := json.Marshal(map[string]any{"command": strings.Repeat("x", length), "maximum_output": 1})
		_, err := execute.ValidateInput(raw)
		if (err == nil) != (length == managedssh.MaximumCommand) {
			t.Fatal("package and transport command bounds differ")
		}
	}
	create, ok := definitions[SSHManagedDefinitionKey].Capability("ssh.file.create")
	if !ok {
		t.Fatal("managed file definition is absent")
	}
	for _, length := range []int{managedssh.MaximumContent, managedssh.MaximumContent + 1, managedssh.MaximumContent + 2} {
		raw, _ := json.Marshal(map[string]any{"content_base64": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", length))), "path": "data", "resource": "work"})
		canonical, err := create.ValidateInput(raw)
		if (err == nil) != (length == managedssh.MaximumContent) {
			t.Fatal("encoded size replaced decoded content bound")
		}
		if err == nil {
			input, decodeErr := managedssh.DecodeOperationInput(create.Operation, canonical)
			if decodeErr != nil {
				t.Fatal("owner input disagrees with broker codec")
			}
			input.Clear()
		}
	}
	for _, raw := range []string{`{"command":"x","maximum_output":1,"stdin_base64":"YQ==\n"}`} {
		if _, err := execute.ValidateInput([]byte(raw)); err == nil {
			t.Fatal("invalid optional stdin became empty or normalized bytes")
		}
	}
}

// TestSSHNullContentCannotBecomeEmptyFile preserves explicit scalar presence at the actual owner input boundary; generic empty-string permission must not normalize null into an approved empty file or stdin.
// TestSSHNullContentCannotBecomeEmptyFile сохраняет explicit scalar presence на actual owner input boundary; generic empty-string permission не должна превращать null в согласованный empty file либо stdin.
func TestSSHNullContentCannotBecomeEmptyFile(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ definition, capability, input string }{
		{SSHManagedDefinitionKey, "ssh.file.create", `{"content_base64":null,"path":"data","resource":"work"}`},
		{SSHExecuteDefinitionKey, "ssh.execute", `{"command":"x","maximum_output":1,"stdin_base64":null}`},
	} {
		capability, ok := definitions[test.definition].Capability(test.capability)
		if !ok {
			t.Fatal("SSH capability is absent")
		}
		if _, err := capability.ValidateInput([]byte(test.input)); err == nil {
			t.Errorf("null scalar became empty content for %s", test.capability)
		}
	}
}

// TestSSHDefinitionCannotAdvertiseUnsupportedExecuteFields rejects a tenant revision that expands the public maximum or changes exact scalar representation while the actual broker decoder remains bounded.
// TestSSHDefinitionCannotAdvertiseUnsupportedExecuteFields отклоняет tenant revision, расширяющую public maximum либо меняющую exact scalar representation при неизменном bounded broker decoder.
func TestSSHDefinitionCannotAdvertiseUnsupportedExecuteFields(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Field){func(field *Field) { field.MaximumLength = 32768 }, func(field *Field) { field.Type = "BOOLEAN" }, func(field *Field) { field.Required = false }} {
		definition := definitions[SSHExecuteDefinitionKey]
		definition.Spec.Capabilities = append([]Capability(nil), definition.Spec.Capabilities...)
		for index, capability := range definition.Spec.Capabilities {
			if capability.Operation != "ssh.execute" {
				continue
			}
			definition.Spec.Capabilities[index].InputFields = append([]Field(nil), capability.InputFields...)
			change(&definition.Spec.Capabilities[index].InputFields[0])
		}
		if validateSSHDefinition(definition) == nil {
			t.Fatal("package advertised unsupported execute input")
		}
	}
}
