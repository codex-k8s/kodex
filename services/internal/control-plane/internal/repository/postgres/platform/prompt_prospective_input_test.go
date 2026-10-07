package platform

import (
	"math"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestPromptProspectiveWorkflowInput(t *testing.T) {
	fields := []entity.WorkflowInputField{
		{Key: "text", Type: "TEXT", Required: true},
		{Key: "long", Type: "LONG_TEXT", Required: true},
		{Key: "number", Type: "NUMBER", Required: true},
		{Key: "boolean", Type: "BOOLEAN", Required: true},
		{Key: "date", Type: "DATE"},
		{Key: "select", Type: "SELECT", Options: []string{"one"}},
	}
	for _, input := range []map[string]any{nil, {}, {"number": float64(1)}, {"boolean": false}, {"text": "Provided"}} {
		if !validPromptWorkflowInput(fields, input, true) {
			t.Fatal("prospective input rejected absent declared fields")
		}
		if validPromptWorkflowInput(fields, input, false) {
			t.Fatal("runtime input accepted absent required fields")
		}
	}
	for _, input := range []map[string]any{
		{"unknown": "value"}, {"text": float64(1)}, {"text": ""}, {"text": nil},
		{"text": strings.Repeat("x", 4001)}, {"long": strings.Repeat("x", 32769)},
		{"number": "1"}, {"number": math.Inf(1)}, {"number": math.NaN()},
		{"boolean": "false"}, {"date": "2026-02-30"}, {"select": "foreign"},
	} {
		if validPromptWorkflowInput(fields, input, true) {
			t.Fatal("prospective input accepted invalid supplied value")
		}
	}
	complete := map[string]any{"text": "Provided", "long": "Provided", "number": float64(1), "boolean": false, "date": "2026-10-06", "select": "one"}
	if !validPromptWorkflowInput(fields, complete, true) || !validPromptWorkflowInput(fields, complete, false) {
		t.Fatal("complete input rejected")
	}
	if !fields[0].Required {
		t.Fatal("prospective validation changed the runtime schema")
	}
}

func TestPromptProspectiveMissingInputsAreUnavailable(t *testing.T) {
	snapshot := entity.PromptMaterializationSnapshot{UnavailableVariables: map[string]string{"task": "ORIGIN_REQUIRED"}}
	fields := []entity.WorkflowInputField{{Key: "provided", Required: true}, {Key: "missing", Required: true}, {Key: "optional"}}
	markUnavailablePromptWorkflowInputs(&snapshot, fields, map[string]any{"provided": "value"})
	for _, name := range []string{"input.values", "input.values.missing", "input.values.optional"} {
		if snapshot.UnavailableVariables[name] != "RUNTIME_CONTEXT_REQUIRED" {
			t.Fatal("missing declared input is not explicitly unavailable")
		}
	}
	if snapshot.UnavailableVariables["input.values.provided"] != "" || snapshot.UnavailableVariables["task"] != "ORIGIN_REQUIRED" {
		t.Fatal("supplied input or task origin was changed")
	}
}
