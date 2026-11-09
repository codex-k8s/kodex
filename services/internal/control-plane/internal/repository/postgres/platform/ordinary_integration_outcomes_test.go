package platform

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// TestOrdinaryIntegrationManifestPreservesFullResult checks bounded tasks for every terminal decision without truncating provider bytes, Unicode or exact owner feedback.
// TestOrdinaryIntegrationManifestPreservesFullResult проверяет ограниченные задачи для всех terminal decisions без усечения provider bytes, Unicode либо точного owner feedback.
func TestOrdinaryIntegrationManifestPreservesFullResult(t *testing.T) {
	for _, pair := range [][2]string{{"APPROVE", "SUCCEEDED"}, {"APPROVE", "FAILED"}, {"REJECT", "REJECTED"}, {"CANCEL", "CANCELLED"}} {
		snapshot := integrationContinuationSnapshot{InvocationRef: "inv_fixture123", State: pair[1],
			ResultSummary: strings.Repeat("Synthetic data. ", 3000) + "🧪 полный конец",
			OwnerDecision: integrationContinuationDecision{GateRef: "gat_fixture123", GateVersion: 2, Decision: pair[0], Comment: "Точное замечание\nбез нового разрешения"}}
		task, err := integrationContinuationTask(snapshot)
		if err != nil || !runtimecontract.ValidAssistantTurnContent(task) || strings.Contains(task, snapshot.ResultSummary) {
			t.Fatal("large result was inlined or lost its bounded manifest", err)
		}
		var manifest struct {
			Schema              string                          `json:"schema"`
			SourceInvocationRef string                          `json:"sourceInvocationRef"`
			SourceSHA256        string                          `json:"sourceSHA256"`
			SizeBytes           int                             `json:"sizeBytes"`
			OutcomesCount       int                             `json:"outcomesCount"`
			Outcome             integrationContinuationSnapshot `json:"outcome"`
		}
		_, raw, ok := strings.Cut(task, "\n\n")
		source, _ := json.Marshal(snapshot)
		if !ok || json.Unmarshal([]byte(raw), &manifest) != nil || manifest.Schema != "kodex.integration-continuation-manifest.v1" ||
			manifest.SourceInvocationRef != snapshot.InvocationRef || manifest.SourceSHA256 != workflowResultSHA(source) || manifest.SizeBytes != len(source) || manifest.OutcomesCount != 1 ||
			manifest.Outcome.OwnerDecision != snapshot.OwnerDecision || manifest.Outcome.State != snapshot.State || manifest.Outcome.ResultSummary != "" {
			t.Fatal("manifest changed the complete source or exact decision")
		}
	}
}

// readOrdinaryIntegrationContinuationFixture reassembles a real owner continuation through bounded reads, checks repeatable page replay and rejects foreign, stale or malformed requests without external effects.
// readOrdinaryIntegrationContinuationFixture собирает настоящее owner continuation ограниченными чтениями, проверяет повтор страницы и запрет чужих, stale и malformed запросов без внешних эффектов.
func readOrdinaryIntegrationContinuationFixture(t *testing.T, lease map[string]any, read func(query.ExecutionIntegrationOutcomes) (entity.ExecutionIntegrationOutcomes, error), foreignInvocation string) (integrationContinuationSnapshot, query.ExecutionIntegrationOutcomes) {
	t.Helper()
	var manifest struct {
		SourceInvocationRef string `json:"sourceInvocationRef"`
		SourceSHA256        string `json:"sourceSHA256"`
		SizeBytes           int64  `json:"sizeBytes"`
	}
	_, raw, ok := strings.Cut(stringMap(lease, "task"), "\n\n")
	if !ok || json.Unmarshal([]byte(raw), &manifest) != nil || manifest.SizeBytes < 32768 {
		t.Fatal("large ordinary continuation has no owner manifest")
	}
	request := query.ExecutionIntegrationOutcomes{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"),
		Generation: runtimeRevisionMapInt64(lease, "generation"), SourceInvocationRef: manifest.SourceInvocationRef,
		SourceSHA256: manifest.SourceSHA256, MaximumBytes: 4096}
	for _, change := range []func(*query.ExecutionIntegrationOutcomes){
		func(v *query.ExecutionIntegrationOutcomes) { v.SourceInvocationRef = foreignInvocation },
		func(v *query.ExecutionIntegrationOutcomes) { v.Fence = "wrong-fixture-fence" },
		func(v *query.ExecutionIntegrationOutcomes) { v.Generation++ },
	} {
		bad := request
		change(&bad)
		if _, err := read(bad); !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("foreign or stale ordinary source returned data", err)
		}
	}
	bad := request
	bad.SourceSHA256 = strings.Repeat("f", 64)
	if _, err := read(bad); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("changed source commitment returned data", err)
	}
	var full strings.Builder
	for pageIndex := 0; pageIndex < 64; pageIndex++ {
		page, err := read(request)
		if err != nil || !utf8.ValidString(page.Text) || page.SourceInvocationRef != manifest.SourceInvocationRef || page.SourceSHA256 != manifest.SourceSHA256 ||
			page.PageSHA256 != workflowResultSHA([]byte(page.Text)) || page.OutcomesCount != 1 || page.SizeBytes != manifest.SizeBytes ||
			page.OffsetBytes != request.OffsetBytes || page.NextOffsetBytes != page.OffsetBytes+int64(len(page.Text)) || page.NextOffsetBytes > page.SizeBytes || len(page.Text) > 4096 {
			t.Fatal("ordinary source page failed its owner commitments", err)
		}
		if pageIndex == 0 {
			replay, err := read(request)
			if err != nil || replay != page {
				t.Fatal("lost read response could not rejoin the identical page", err)
			}
		}
		full.WriteString(page.Text)
		if page.EOF {
			break
		}
		if page.NextOffsetBytes <= request.OffsetBytes {
			t.Fatal("ordinary source pagination made no progress")
		}
		request.OffsetBytes = page.NextOffsetBytes
	}
	if int64(full.Len()) != manifest.SizeBytes || workflowResultSHA([]byte(full.String())) != manifest.SourceSHA256 {
		t.Fatal("ordinary source lost bytes before EOF")
	}
	var snapshot integrationContinuationSnapshot
	if json.Unmarshal([]byte(full.String()), &snapshot) != nil || snapshot.InvocationRef != manifest.SourceInvocationRef {
		t.Fatal("ordinary source lost its complete typed snapshot")
	}
	request.OffsetBytes = 0
	bad = request
	bad.OffsetBytes = int64(strings.Index(full.String(), "🧪") + 1)
	if _, err := read(bad); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("mid-rune source page was accepted", err)
	}
	return snapshot, request
}
