package controlplaneapi

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func publicationFixture() WorkflowPublication {
	return WorkflowPublication{Version: 1, WorkflowRef: "wfl_workflow01", ProjectRef: "prj_project001", PublishedRef: "wfv_workflow01", SpecDigest: strings.Repeat("a", 64), WorkflowVersion: 7, PublishedVersion: 3, Name: "Процесс", CoordinatorAgentRef: "agt_coordinator", Concurrency: 1, TimeoutSeconds: 3600, InputFields: []WorkflowPublicationInput{{Key: "field-001", ValueType: "LONG_TEXT", DefaultValue: "Полное значение"}}, Steps: []WorkflowPublicationStep{{Key: "step-001", Position: 1, AgentRef: "agt_specialist", Instructions: "Полная инструкция", ExpectedResult: "Результат", TimeoutSeconds: 900}}, ResultSchema: WorkflowPublicationSchema{}}
}
func TestWorkflowPublicationCanonicalWhitelist(t *testing.T) {
	value := publicationFixture()
	raw, digest, err := EncodeWorkflowPublication(value)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeWorkflowPublication(raw, digest)
	if err != nil || decoded.Steps[0].Instructions != value.Steps[0].Instructions || decoded.InputFields[0].DefaultValue != value.InputFields[0].DefaultValue {
		t.Fatal("full text or defaults lost")
	}
	for name, changed := range map[string][]byte{
		"unknown":    append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"secret":"CANARY"}`)...),
		"duplicate":  append([]byte(`{"version":1,`), raw[1:]...),
		"whitespace": append([]byte(" "), raw...),
		"missing":    []byte("{}"),
	} {
		t.Run(name, func(t *testing.T) {
			sum := sha256.Sum256(changed)
			if _, err := DecodeWorkflowPublication(changed, hex.EncodeToString(sum[:])); err == nil {
				t.Fatal("noncanonical wire accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*WorkflowPublication){
		"foreign shape": func(v *WorkflowPublication) { v.ProjectRef = "invalid" },
		"enum":          func(v *WorkflowPublication) { v.InputFields[0].ValueType = "SECRET" },
		"cycle":         func(v *WorkflowPublication) { v.Steps[0].DependsOn = []string{"step-001"} },
		"utf8":          func(v *WorkflowPublication) { v.Instructions = string([]byte{255}) },
		"nul":           func(v *WorkflowPublication) { v.Instructions = "x\x00" },
		"oversize":      func(v *WorkflowPublication) { v.Instructions = strings.Repeat("x", WorkflowPublicationMaximumBytes) },
	} {
		t.Run(name, func(t *testing.T) {
			v := publicationFixture()
			mutate(&v)
			if _, _, err := EncodeWorkflowPublication(v); err == nil {
				t.Fatal("invalid wire accepted")
			}
		})
	}
}
func TestWorkflowPublicationResultSchemaClosed(t *testing.T) {
	for _, raw := range []string{`{}`, `{"type":"object","properties":{"status":{"type":"string","enum":["ok","failed"]}},"required":["status"],"additionalProperties":false}`} {
		if _, err := DecodeWorkflowPublicationSchema([]byte(raw)); err != nil {
			t.Fatalf("safe schema rejected: %v", err)
		}
	}
	for _, raw := range []string{`null`, `{"type":null}`, `{"type":""}`, `{"items":null}`, `{"items":{"items":null}}`, `{"type":"string","type":"integer"}`, `{"$ref":"https://invalid/secret"}`, `{"secret":"CANARY"}`, `{"properties":{"x":null}}`, `{"enum":[{"payload":"CANARY"}]}`, `{"required":["missing"]}`, `{"items":{"items":{"unknown":true}}}`} {
		if _, err := DecodeWorkflowPublicationSchema([]byte(raw)); err == nil {
			t.Fatal("unsafe schema accepted")
		}
	}
}
