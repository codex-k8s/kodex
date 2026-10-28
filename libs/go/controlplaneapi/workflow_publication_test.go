package controlplaneapi

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// publicationFixture builds a minimal valid published workflow with full
// instruction text and an input default for canonical-wire tests.
// RU: publicationFixture создаёт минимальный опубликованный процесс с полным
// текстом инструкции и значением поля по умолчанию для проверки передачи.
func publicationFixture() WorkflowPublication {
	return WorkflowPublication{Version: 1, WorkflowRef: "wfl_workflow01", ProjectRef: "prj_project001", PublishedRef: "wfv_workflow01", SpecDigest: strings.Repeat("a", 64), WorkflowVersion: 7, PublishedVersion: 3, Name: "Процесс", CoordinatorAgentRef: "agt_coordinator", Concurrency: 1, TimeoutSeconds: 3600, InputFields: []WorkflowPublicationInput{{Key: "field-001", ValueType: "LONG_TEXT", DefaultValue: "Полное значение"}}, Steps: []WorkflowPublicationStep{{Key: "step-001", Position: 1, AgentRef: "agt_specialist", Instructions: "Полная инструкция", ExpectedResult: "Результат", TimeoutSeconds: 900}}, ResultSchema: WorkflowPublicationSchema{}}
}

// TestWorkflowPublicationCanonicalWhitelist verifies exact digest roundtrip
// and rejects unknown fields, duplicate keys, cycles and invalid text.
// RU: TestWorkflowPublicationCanonicalWhitelist проверяет точный дайджест и
// отказ для чужих полей, повторных ключей, циклов и неверного текста.
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

// TestWorkflowPublicationResultSchemaClosed accepts local bounded result
// shapes and rejects refs, nulls, duplicate keys and unregistered keywords.
// RU: TestWorkflowPublicationResultSchemaClosed принимает локальные
// ограниченные схемы и отклоняет ссылки, null, повторы и чужие ключи.
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

// TestWorkflowPublicationGateExpiry keeps absent legacy wire unchanged and exact TTL digest-bound.
// TestWorkflowPublicationGateExpiry сохраняет прежнюю передачу без срока и связывает явный срок с дайджестом.
func TestWorkflowPublicationGateExpiry(t *testing.T) {
	legacy := publicationFixture()
	raw, digest, err := EncodeWorkflowPublication(legacy)
	if err != nil || strings.Contains(string(raw), "approval_timeout_seconds") {
		t.Fatal("legacy catalog acquired a gate timeout")
	}
	decoded, err := DecodeWorkflowPublication(raw, digest)
	if err != nil || decoded.Steps[0].ApprovalTimeoutSeconds != nil {
		t.Fatal("absent gate policy did not roundtrip")
	}
	for _, ttl := range []int32{0, 1, 604800, 604801} {
		v := publicationFixture()
		v.Steps[0].HumanGateAfter = true
		v.Steps[0].GateDecisions = []string{"APPROVE"}
		v.Steps[0].ApprovalTimeoutSeconds = &ttl
		wire, pin, err := EncodeWorkflowPublication(v)
		if ttl == 0 || ttl > 604800 {
			if err == nil {
				t.Fatal("invalid catalog TTL accepted")
			}
			continue
		}
		if err != nil || pin == digest {
			t.Fatal("TTL not committed in catalog digest")
		}
		actual, err := DecodeWorkflowPublication(wire, pin)
		if err != nil || actual.Steps[0].ApprovalTimeoutSeconds == nil || *actual.Steps[0].ApprovalTimeoutSeconds != ttl {
			t.Fatal("strict catalog lost pinned TTL")
		}
		v.Steps[0].HumanGateAfter = false
		if _, _, err := EncodeWorkflowPublication(v); err == nil {
			t.Fatal("ungated TTL accepted")
		}
	}
}
