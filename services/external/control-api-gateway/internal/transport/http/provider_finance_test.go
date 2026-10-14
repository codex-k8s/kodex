package httptransport

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestFinancialProjectionPreservesKnownZeroAndUnknown(t *testing.T) {
	ordinary := false
	value, err := ProtoMap(&cp.ProviderFinancialObservation{Sequence: 1, State: "AVAILABLE", Windows: []*cp.ProviderFinancialWindow{{UsedPercent: new(int32(0))}}, Credits: &cp.ProviderFinancialCredits{HasCredits: new(false), Unlimited: new(false)}, OrdinaryUsageAllowed: &ordinary})
	if err != nil {
		t.Fatal(err)
	}
	windows := value["windows"].([]any)
	window := windows[0].(map[string]any)
	if window["usedPercent"] != float64(0) {
		t.Fatal("known zero lost in proto JSON")
	}
	if _, exists := window["resetsAt"]; exists {
		t.Fatal("unknown reset became zero")
	}
	credits := value["credits"].(map[string]any)
	if credits["hasCredits"] != false || credits["unlimited"] != false {
		t.Fatal("known credit flags lost")
	}
	if _, exists := credits["balance"]; exists {
		t.Fatal("unknown credit balance invented")
	}
	if value["ordinaryUsageAllowed"] != false {
		t.Fatal("explicit false lost")
	}
	if _, exists := value["spendControlReached"]; exists {
		t.Fatal("unknown spending flag invented")
	}
	absent, err := ProtoMap(&cp.ProviderFinancialWindow{})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := absent["usedPercent"]; exists {
		t.Fatal("missing window became known zero")
	}
	unknown, err := ProtoMap(&cp.ProviderFinancialCredits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"hasCredits", "unlimited", "balance"} {
		if _, exists := unknown[field]; exists {
			t.Fatal("absent credit field became a measurement")
		}
	}
}
