package platform

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestWorkflowEvidencePageUTF8 checks exact multibyte reassembly, explicit EOF,
// middle-rune rejection and the full-source ceiling independently of task size.
// TestWorkflowEvidencePageUTF8 проверяет точную сборку многобайтного текста, явный EOF,
// отказ посередине rune и предел полного источника независимо от размера task.
func TestWorkflowEvidencePageUTF8(t *testing.T) {
	source := []byte(strings.Repeat("а😀<&>\n", 9))
	var assembled bytes.Buffer
	var offset int64
	for pages := 0; pages < 200; pages++ {
		sink := newWorkflowEvidencePageSink(offset, 7)
		// The page may cross the source writer's unrelated chunk boundaries.
		// Страница может пересекать независимые границы chunks source writer.
		for start := 0; start < len(source); start += 3 {
			if _, err := sink.Write(source[start:min(start+3, len(source))]); err != nil {
				t.Fatal(err)
			}
		}
		text, next, eof, err := sink.result()
		if err != nil || !utf8.ValidString(text) || sink.digest() != workflowResultSHA(source) {
			t.Fatal("valid Unicode source page rejected")
		}
		assembled.WriteString(text)
		if eof {
			break
		}
		if next <= offset {
			t.Fatal("page did not advance")
		}
		offset = next
	}
	if !bytes.Equal(assembled.Bytes(), source) {
		t.Fatal("Unicode pages changed full source")
	}
	for _, invalid := range []int64{1, 3, int64(len(source) + 1)} {
		sink := newWorkflowEvidencePageSink(invalid, 7)
		_, _ = sink.Write(source)
		if _, _, _, err := sink.result(); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("invalid offset accepted", invalid)
		}
	}
	eof := newWorkflowEvidencePageSink(int64(len(source)), 7)
	_, _ = eof.Write(source)
	if text, next, done, err := eof.result(); err != nil || text != "" || !done || next != int64(len(source)) {
		t.Fatal("explicit EOF rejected")
	}
	limit := newWorkflowEvidencePageSink(0, 4)
	limit.size = maximumWorkflowEvidenceSourceBytes
	if _, err := limit.Write([]byte("x")); !errors.Is(err, errWorkflowEvidenceDeliveryLimit) {
		t.Fatal("full source overflow was truncated")
	}
}

// TestWorkflowEvidenceManifestClosedSource rejects ambiguous or incomplete owner
// snapshots instead of silently canonically accepting attacker-selected fields.
// TestWorkflowEvidenceManifestClosedSource отвергает ambiguous либо incomplete owner
// snapshots вместо молчаливой канонизации caller-selected fields.
func TestWorkflowEvidenceManifestClosedSource(t *testing.T) {
	task := "Preserve every settled result."
	row := workflowEvidenceOutcome{InvocationRef: "inv_source001", Operation: "github.repository.content.read", State: "FAILED", InputDigest: strings.Repeat("a", 64), SafeErrorCode: "INTEGRATION_REQUEST_REJECTED"}
	raw := asJSON(map[string]any{"schema": workflowEvidenceSnapshotSchema, "task": task, "nativeOutcomes": []any{row.wire()}})
	if _, err := workflowEvidenceManifestTask(task, raw); err != nil {
		t.Fatal("closed negative result rejected", err)
	}
	for _, bad := range [][]byte{
		append(append([]byte{}, raw...), []byte(` {}`)...),
		bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"ambiguous","schema":`), 1),
		bytes.Replace(raw, []byte(`"invocationRef":`), []byte(`"invocationRef":"ambiguous","invocationRef":`), 1),
		bytes.Replace(raw, []byte(`"ownerDecision":null`), []byte(`"ownerDecision":{"gateRef":"gat_source001","version":1,"decision":"REJECTED","comment":"a","comment":"b"}`), 1),
	} {
		if _, err := workflowEvidenceManifestTask(task, bad); err == nil {
			t.Fatal("ambiguous source accepted")
		}
	}
}
