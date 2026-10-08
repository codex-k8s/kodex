package platform

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestWorkflowDelegateInput(t *testing.T) {
	version := entity.WorkflowVersion{Name: "Workflow", CoordinatorAgentRef: "coordinator", Concurrency: 1, TimeoutSeconds: 3600,
		Inputs: []entity.WorkflowInputField{{Key: "field-001", Label: "Issue", Type: "TEXT", Required: true}},
		Steps:  []entity.WorkflowStep{{Key: "step", Name: "Step", Position: 1, AgentRef: "developer", Instructions: "Read immutable input", TimeoutSeconds: 900}}}
	spec, err := json.Marshal(version)
	if err != nil {
		t.Fatal(err)
	}
	root := []byte(`{"field-001":"Issue1796"}`)
	tooMany := map[string]any{}
	for index := 0; index < 100; index++ {
		tooMany[string(rune('A'+index))] = true
	}
	cases := []struct {
		name       string
		root, spec []byte
		additional map[string]any
		want       error
	}{
		{name: "empty"},
		{name: "additional", additional: map[string]any{"handoff": map[string]any{"revision": 1}}},
		{name: "identical", additional: map[string]any{"field-001": "Issue1796"}},
		{name: "collision", additional: map[string]any{"field-001": "Issue1797"}, want: errs.ErrInvalid},
		{name: "type-collision", additional: map[string]any{"field-001": false}, want: errs.ErrInvalid},
		{name: "null-collision", additional: map[string]any{"field-001": nil}, want: errs.ErrInvalid},
		{name: "combined-keys", additional: tooMany, want: errs.ErrInvalid},
		{name: "combined-bytes", additional: map[string]any{"handoff": strings.Repeat("x", 65510)}, want: errs.ErrInvalid},
		{name: "caller-budget", additional: map[string]any{"handoff": strings.Repeat("x", 65536)}, want: errs.ErrInvalid},
		{name: "caller-nonfinite", additional: map[string]any{"handoff": math.NaN()}, want: errs.ErrInvalid},
		{name: "caller-nonjson", additional: map[string]any{"handoff": make(chan bool)}, want: errs.ErrInvalid},
		{name: "malformed-root", root: []byte(`{`), want: errs.ErrUnavailable},
		{name: "array-root", root: []byte(`[]`), want: errs.ErrUnavailable},
		{name: "null-required-root", root: []byte(`null`), want: errs.ErrUnavailable},
		{name: "missing-required-root", root: []byte(`{}`), want: errs.ErrUnavailable},
		{name: "wrong-root-type", root: []byte(`{"field-001":1}`), want: errs.ErrUnavailable},
		{name: "unknown-root-field", root: []byte(`{"field-001":"Issue1796","other":true}`), want: errs.ErrUnavailable},
		{name: "malformed-spec", spec: []byte(`{`), want: errs.ErrUnavailable},
		{name: "null-spec", spec: []byte(`null`), want: errs.ErrUnavailable},
		{name: "missing-spec", spec: []byte(`{}`), want: errs.ErrUnavailable},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			rawRoot, rawSpec := test.root, test.spec
			if rawRoot == nil {
				rawRoot = root
			}
			if rawSpec == nil {
				rawSpec = spec
			}
			before := map[string]any{}
			for key, value := range test.additional {
				before[key] = value
			}
			result, err := workflowDelegateInput(rawRoot, rawSpec, test.additional)
			if !errors.Is(err, test.want) {
				t.Fatalf("unexpected error: %v", err)
			}
			if err != nil {
				if result != nil {
					t.Fatal("invalid input returned a partial merge")
				}
				return
			}
			wantLength := 1 + len(test.additional)
			if _, sameKey := test.additional["field-001"]; sameKey {
				wantLength--
			}
			if result["field-001"] != "Issue1796" || len(result) != wantLength {
				t.Fatal("immutable root fields lost")
			}
			if len(test.additional) != 0 && !reflect.DeepEqual(test.additional, before) {
				t.Fatal("caller input mutated")
			}
			result["field-001"] = "Changed child copy"
			if string(rawRoot) != string(root) {
				t.Fatal("root snapshot mutated")
			}
		})
	}
	t.Run("canonical-empty-input", func(t *testing.T) {
		version.Inputs = nil
		spec, _ := json.Marshal(version)
		// LaunchRun допускает nil input, только если нет обязательных полей.
		result, err := workflowDelegateInput([]byte(`null`), spec, nil)
		if err != nil || len(result) != 0 {
			t.Fatal("canonical empty Workflow input rejected")
		}
	})
	t.Run("numeric-json-equality", func(t *testing.T) {
		version.Inputs = []entity.WorkflowInputField{{Key: "field-001", Label: "Number", Type: "NUMBER", Required: true}}
		spec, _ := json.Marshal(version)
		result, err := workflowDelegateInput([]byte(`{"field-001":1}`), spec, map[string]any{"field-001": 1})
		if err != nil || result["field-001"] != float64(1) {
			t.Fatal("equal JSON value rejected or replaced original type")
		}
	})
}
