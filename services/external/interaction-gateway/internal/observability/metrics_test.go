package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestAgentBotRepairBacklogUsesDurableBoundedReason(t *testing.T) {
	t.Parallel()
	registry := prometheus.NewRegistry()
	metrics, err := New(func(collectors ...prometheus.Collector) error {
		for _, collector := range collectors {
			if err := registry.Register(collector); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	metrics.SetBotIdentityRepairBacklog("recovery_timeout", 2)
	metrics.SetBotIdentityRepairBacklog("private-provider-reason", 1)
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]float64)
	for _, family := range families {
		if family.GetName() != "mattercodex_interaction_gateway_agent_bot_identity_repairs" {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "reason" {
					values[label.GetValue()] = metric.GetGauge().GetValue()
				}
			}
		}
	}
	if values["recovery_timeout"] != 2 || values["other"] != 1 || len(values) != 2 {
		t.Fatalf("repair gauge labels are not closed or durable values were lost: %v", values)
	}
}
