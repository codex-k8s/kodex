package platform

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestIntegrationContinuationReceipt preserves the owner receipt through the separate Human Gate
// continuation JSON and rejects malformed/non-success locators without changing owner feedback.
// TestIntegrationContinuationReceipt сохраняет owner receipt в отдельном continuation JSON после
// Human Gate; повреждённые/non-success locators отвергаются без изменения owner feedback.
func TestIntegrationContinuationReceipt(t *testing.T) {
	for _, ref := range []string{"erc_fixture123", "", "erc_short", "inv_fixture123", "erc_" + strings.Repeat("x", 93), "erc_fixture\nPRIVATE_SECRET"} {
		t.Run("receipt", func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"invocation_ref": "inv_fixture123", "state": "SUCCEEDED", "result_summary": "provider data", "receipt_ref": ref, "owner_decision": map[string]any{"gate_ref": "gat_fixture123", "gate_version": 2, "decision": "APPROVE", "comment": "Keep exact owner feedback"}})
			var snapshot integrationContinuationSnapshot
			if json.Unmarshal(raw, &snapshot) != nil {
				t.Fatal("continuation fixture invalid")
			}
			task, err := integrationContinuationTask(snapshot)
			if ref != "" && ref != "erc_fixture123" {
				if !errors.Is(err, errs.ErrConflict) || task != "" {
					t.Fatal("malformed continuation receipt accepted")
				}
				return
			}
			if err != nil {
				t.Fatal("valid continuation receipt rejected")
			}
			_, body, ok := strings.Cut(task, "\n\n")
			var wire map[string]any
			if !ok || json.Unmarshal([]byte(body), &wire) != nil {
				t.Fatal("typed continuation JSON missing")
			}
			if ref != "" {
				if wire["receipt_ref"] != ref || !strings.Contains(task, "receiptRef") {
					t.Fatal("owner receipt lost before workflow declaration")
				}
			} else if _, exists := wire["receipt_ref"]; exists {
				t.Fatal("legacy continuation invented receipt")
			}
		})
	}
}

// TestIntegrationContinuationReceiptNonSuccess prevents failed or rejected invocations from
// presenting a successful evidence locator merely because stale metadata was populated.
// TestIntegrationContinuationReceiptNonSuccess не позволяет failed/rejected invocation выдавать
// successful evidence locator лишь из-за заполненных stale metadata.
func TestIntegrationContinuationReceiptNonSuccess(t *testing.T) {
	for _, tc := range []struct{ state, decision string }{{"FAILED", "APPROVE"}, {"REJECTED", "REJECT"}, {"CANCELLED", "CANCEL"}} {
		snapshot := integrationContinuationSnapshot{InvocationRef: "inv_fixture123", ReceiptRef: "erc_fixture123", State: tc.state, OwnerDecision: integrationContinuationDecision{GateRef: "gat_fixture123", GateVersion: 2, Decision: tc.decision}}
		if task, err := integrationContinuationTask(snapshot); task != "" || !errors.Is(err, errs.ErrConflict) {
			t.Fatal("non-success continuation exposed receipt")
		}
	}
}
