package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestLegacyReworkReadTask preserves full successful and failed READ outcomes without a synthetic approval, and distinguishes oversized delivery from malformed input.
// TestLegacyReworkReadTask сохраняет полные successful и failed READ outcomes без выдуманного approval и отличает oversized delivery от malformed input.
func TestLegacyReworkReadTask(t *testing.T) {
	snapshot := `{"schema":"kodex.legacy-owner-rework-read.v1","completedReads":[{"invocationRef":"inv_first_read","state":"SUCCEEDED","receiptRef":"erc_first_read","resultSummary":"exact successful data"},{"invocationRef":"inv_second_read","state":"FAILED","safeErrorCode":"INTEGRATION_REQUEST_REJECTED"}]}`
	task, err := legacyReworkReadTask([]byte(snapshot))
	if err != nil || !strings.HasSuffix(task, snapshot) || strings.Contains(task, "owner_decision") || strings.Contains(task, "The owner approved") {
		t.Fatal("READ join omitted outcomes or manufactured owner approval")
	}
	for _, invalid := range []string{`{`, `{"schema":"unknown","completedReads":[{}]}`, `{"schema":"kodex.legacy-owner-rework-read.v1","completedReads":[]}`} {
		if _, err := legacyReworkReadTask([]byte(invalid)); !errors.Is(err, errs.ErrConflict) || errors.Is(err, errs.ErrLegacyReworkReadLimit) {
			t.Fatal("malformed READ context selected overflow recovery")
		}
	}
	oversized := `{"schema":"kodex.legacy-owner-rework-read.v1","completedReads":[{"state":"SUCCEEDED","resultSummary":"` + strings.Repeat("x", 34000) + `"}]}`
	if _, err := legacyReworkReadTask([]byte(oversized)); !errors.Is(err, errs.ErrLegacyReworkReadLimit) {
		t.Fatal("READ delivery escaped canonical task budget")
	}
}
