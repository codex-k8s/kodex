package platform

import (
	"encoding/json"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"strings"
)

// workflowPDLCSeedLargeReadHistory completes twelve ordinary immutable file reads
// through real owner admission, worker claims, and receipts before the author's
// WRITE. These trusted gateway fixtures reproduce the continuation-size failure;
// they do not claim external GitHub delivery or prepare business rows with SQL.
// workflowPDLCSeedLargeReadHistory завершает двенадцать обычных immutable-чтений
// через настоящие owner admission, worker claims и receipts до WRITE автора.
// Trusted gateway fixtures воспроизводят отказ размера продолжения, не заявляя
// внешний GitHub delivery и не подготавливая business rows через SQL.
func workflowPDLCSeedLargeReadHistory(fixture *workflowEvidenceNativeFixture, lease map[string]any, head, blob string) {
	workflowPDLCSeedReadHistory(fixture, lease, head, blob, 12)
}

// workflowPDLCSeedReadHistory admits each bounded READ through the native owner path;
// the count changes test history size without inserting business rows directly.
// workflowPDLCSeedReadHistory проводит каждое bounded READ штатным owner путём;
// число меняет размер тестовой истории без прямой вставки бизнесовых строк.
func workflowPDLCSeedReadHistory(fixture *workflowEvidenceNativeFixture, lease map[string]any, head, blob string, count int) {
	fixture.f.t.Helper()
	if count < 1 || count > 66 {
		fixture.f.t.Fatal("invalid bounded read history fixture")
	}
	text := strings.Repeat("Documentation read result. ", 256)
	for index := 0; index < count; index++ {
		invocation, err := fixture.resolve(lease, fixture.key("pdlc-large-read"), "github.repository.content.read", map[string]any{
			"path": "docs/source.md", "ref": head,
		})
		if err != nil {
			fixture.f.t.Fatal("admit ordinary documentation READ", completionOutcomeClass(err))
		}
		fixture.finishEffect(invocation, "github-content:"+blob, string(asJSON(map[string]any{
			"path": "docs/source.md", "type": "file", "sha": blob, "size": len(text),
			"text": text, "commit_sha": head, "offset_bytes": 0,
			"next_offset_bytes": len(text), "eof": true,
			"source_digest": "sha256:" + workflowResultSHA([]byte(text)),
			"chunk_digest":  "sha256:" + workflowResultSHA([]byte(text)),
		})))
	}
}

// workflowPDLCReadLargeHistory exercises the real leased owner reader, reassembles
// every byte and checks source/page pins, stale leases, foreign refs and bad digests.
// It returns a terminal-replay assertion; no business state is inserted or patched.
// workflowPDLCReadLargeHistory проверяет настоящий leased owner reader, собирает все
// байты и сверяет source/page pins, stale leases, foreign refs и bad digests.
// Возвращается проверка terminal replay; business state не вставляется и не меняется.
func workflowPDLCReadLargeHistory(fixture *workflowEvidenceNativeFixture, previous, lease map[string]any) func() {
	return workflowPDLCReadHistory(fixture, previous, lease, 15)
}

// workflowPDLCReadHistory checks the complete native snapshot for the selected
// fixture cardinality, including page integrity and terminal authority revocation.
// workflowPDLCReadHistory сверяет полный native snapshot заданной cardinality,
// включая целостность страниц и отзыв полномочий после завершения.
func workflowPDLCReadHistory(fixture *workflowEvidenceNativeFixture, previous, lease map[string]any, expectedCount int) func() {
	t := fixture.f.t
	t.Helper()
	task := stringMap(lease, "task")
	if !runtimecontract.ValidAssistantTurnContent(task) {
		t.Fatal("large history exceeded unchanged task budget")
	}
	var manifest struct {
		Schema              string `json:"schema"`
		SourceInvocationRef string `json:"sourceInvocationRef"`
		SourceSHA256        string `json:"sourceSHA256"`
		SizeBytes           int64  `json:"sizeBytes"`
		OutcomesCount       int    `json:"outcomesCount"`
	}
	if json.Unmarshal([]byte(task[strings.LastIndex(task, "\n")+1:]), &manifest) != nil || manifest.Schema != "kodex.workflow-evidence-manifest.v1" || manifest.OutcomesCount != expectedCount {
		t.Fatal("large history has no exact complete manifest", manifest.OutcomesCount)
	}
	request := query.ExecutionIntegrationOutcomes{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), SourceInvocationRef: manifest.SourceInvocationRef, SourceSHA256: manifest.SourceSHA256, MaximumBytes: 4096}
	reader := fixture.f.worker
	reader.Permission = "platform.runtime.execution.integration.outcomes.read"
	var assembled strings.Builder
	for pages := 0; pages < 128; pages++ {
		page, err := fixture.f.service.GetExecutionIntegrationOutcomes(fixture.f.ctx, reader, request)
		if err != nil {
			t.Fatal("exact leased history page unavailable", completionOutcomeClass(err))
		}
		if page.SourceSHA256 != manifest.SourceSHA256 || page.SourceInvocationRef != manifest.SourceInvocationRef || page.SizeBytes != manifest.SizeBytes || int(page.OutcomesCount) != manifest.OutcomesCount || page.OffsetBytes != request.OffsetBytes || page.NextOffsetBytes != request.OffsetBytes+int64(len(page.Text)) || len(page.Text) > 4096 || page.PageSHA256 != workflowResultSHA([]byte(page.Text)) {
			t.Fatal("owner page lost exact source coordinates or digest")
		}
		assembled.WriteString(page.Text)
		if page.EOF {
			break
		}
		if page.NextOffsetBytes <= request.OffsetBytes {
			t.Fatal("paged source made no progress")
		}
		request.OffsetBytes = page.NextOffsetBytes
	}
	if int64(assembled.Len()) != manifest.SizeBytes || workflowResultSHA([]byte(assembled.String())) != manifest.SourceSHA256 {
		t.Fatal("full history bytes did not reassemble to pinned digest")
	}
	var decoded struct {
		NativeOutcomes []map[string]any `json:"nativeOutcomes"`
	}
	if json.Unmarshal([]byte(assembled.String()), &decoded) != nil || len(decoded.NativeOutcomes) != expectedCount {
		t.Fatal("full read omitted settled facts")
	}
	for name, mutate := range map[string]func(*query.ExecutionIntegrationOutcomes){
		"prior-lease": func(q *query.ExecutionIntegrationOutcomes) {
			q.LeaseRef = stringMap(previous, "leaseRef")
			q.Fence = stringMap(previous, "fence")
			q.Generation = runtimeRevisionMapInt64(previous, "generation")
		},
		"fence":      func(q *query.ExecutionIntegrationOutcomes) { q.Fence = "foreign-fence" },
		"generation": func(q *query.ExecutionIntegrationOutcomes) { q.Generation++ },
		"source":     func(q *query.ExecutionIntegrationOutcomes) { q.SourceInvocationRef = "inv_foreign001" },
		"digest":     func(q *query.ExecutionIntegrationOutcomes) { q.SourceSHA256 = strings.Repeat("f", 64) },
		"past-eof":   func(q *query.ExecutionIntegrationOutcomes) { q.OffsetBytes = manifest.SizeBytes + 1 },
	} {
		changed := request
		changed.OffsetBytes = 0
		mutate(&changed)
		page, err := fixture.f.service.GetExecutionIntegrationOutcomes(fixture.f.ctx, reader, changed)
		if err == nil || page.Text != "" || !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrConflict) && !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("invalid history selector was not closed", name, completionOutcomeClass(err))
		}
	}
	request.OffsetBytes = 0
	return func() {
		t.Helper()
		page, err := fixture.f.service.GetExecutionIntegrationOutcomes(fixture.f.ctx, reader, request)
		if !errors.Is(err, errs.ErrNotFound) || page.Text != "" {
			t.Fatal("completed continuation retained runtime history read authority", completionOutcomeClass(err))
		}
	}
}
