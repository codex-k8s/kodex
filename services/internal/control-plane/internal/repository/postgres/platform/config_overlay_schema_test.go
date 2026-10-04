package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

func TestRuntimeOverlayReasoningValuesUseExactKeyNotPosition(t *testing.T) {
	schema := runtimecontract.OverlaySchema([]string{"medium"}, "medium")
	schema.Fields[0], schema.Fields[1] = schema.Fields[1], schema.Fields[0]
	values, err := runtimeOverlayReasoningValues(schema)
	if err != nil || len(values) != 1 || values[0] != "medium" {
		t.Fatal("reordered search modes were used as model reasoning capabilities")
	}
	if diagnostics := runtimecontract.DiagnoseConfigOverlay(`model_reasoning_effort = "live"`, values); len(diagnostics) != 1 || diagnostics[0].Code != runtimecontract.OverlayEffortUnsupported {
		t.Fatal("search value expanded reasoning capability")
	}
	for _, fields := range [][]runtimecontract.ConfigOverlayField{
		nil,
		{{Key: "web_search", ValueType: "string", AllowedValues: []string{"live"}}},
		{{Key: "model_reasoning_effort", ValueType: "boolean"}},
		{{Key: "model_reasoning_effort", ValueType: "string"}, {Key: "model_reasoning_effort", ValueType: "string"}},
	} {
		if _, err := runtimeOverlayReasoningValues(runtimecontract.ConfigOverlaySchema{Fields: fields}); !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal("missing, duplicate or wrong-typed capability field was defaulted")
		}
	}
	values, err = runtimeOverlayReasoningValues(runtimecontract.OverlaySchema(nil, ""))
	if err != nil || values == nil || len(runtimecontract.DiagnoseConfigOverlay(`model_reasoning_effort = "medium"`, values)) != 1 {
		t.Fatal("empty model capability list disabled compatibility validation")
	}
}
