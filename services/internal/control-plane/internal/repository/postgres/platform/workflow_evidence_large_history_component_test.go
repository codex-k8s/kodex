package platform

import "strings"

// workflowPDLCSeedLargeReadHistory completes twelve ordinary immutable file reads
// through real owner admission, worker claims, and receipts before the author's
// WRITE. These trusted gateway fixtures reproduce the continuation-size failure;
// they do not claim external GitHub delivery or prepare business rows with SQL.
// workflowPDLCSeedLargeReadHistory завершает двенадцать обычных immutable-чтений
// через настоящие owner admission, worker claims и receipts до WRITE автора.
// Trusted gateway fixtures воспроизводят отказ размера продолжения, не заявляя
// внешний GitHub delivery и не подготавливая business rows через SQL.
func workflowPDLCSeedLargeReadHistory(fixture *workflowEvidenceNativeFixture, lease map[string]any, head, blob string) {
	fixture.f.t.Helper()
	text := strings.Repeat("Documentation read result. ", 256)
	for index := 0; index < 12; index++ {
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
