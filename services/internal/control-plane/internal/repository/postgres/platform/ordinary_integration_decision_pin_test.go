package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestOrdinaryIntegrationDecisionPin covers both exact writer formats and rejects ambiguous private task payloads.
// A returned locator is not authority; the owner source query still validates lease, lineage, visibility and task bytes.
// TestOrdinaryIntegrationDecisionPin проверяет оба формата writer и запрещает неоднозначный payload приватной задачи.
// Возвращённая ссылка не является полномочием: source query проверяет lease, lineage, видимость и байты задачи.
func TestOrdinaryIntegrationDecisionPin(t *testing.T) {
	base := integrationContinuationSnapshot{InvocationRef: "inv_fixture123", State: "CANCELLED", OwnerDecision: integrationContinuationDecision{GateRef: "gat_fixture123", GateVersion: 2, Decision: "APPROVE", Comment: "Historical approval"}}
	for _, large := range []bool{false, true} {
		snapshot := base
		if large {
			snapshot.ResultSummary = strings.Repeat("Synthetic result. ", 3000)
		}
		task, err := integrationContinuationTask(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		pin, err := ordinaryIntegrationDecisionPin(task)
		if err != nil || pin != snapshot.OwnerDecision.GateRef {
			t.Fatal("exact writer decision pin missing", err)
		}
		if !strings.Contains(task, "subsequently cancelled") {
			t.Fatal("historical approval masked cancellation")
		}
	}
	task, err := integrationContinuationTask(base)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"missing payload":  "Continue without an owner payload",
		"null":             "Continue\n\nnull",
		"unknown field":    strings.Replace(task, `"invocation_ref":`, `"unexpected":true,"invocation_ref":`, 1),
		"duplicate field":  strings.Replace(task, `"gate_ref":"gat_fixture123"`, `"gate_ref":"gat_fixture123","gate_ref":"gat_other12345"`, 1),
		"trailing payload": task + `{}`,
		"wrong decision":   strings.Replace(task, `"decision":"APPROVE"`, `"decision":"REJECT"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if pin, err := ordinaryIntegrationDecisionPin(value); pin != "" || !errors.Is(err, errs.ErrConflict) {
				t.Fatal("ambiguous decision pin accepted", err)
			}
		})
	}
}
