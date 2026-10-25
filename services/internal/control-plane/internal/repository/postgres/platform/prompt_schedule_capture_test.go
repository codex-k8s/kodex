package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

// TestSchedulePromptCaptureNeedsOwnerFormatMarker prevents nested template-shaped legacy values
// from gaining template authority, binds the owner format to the digest and rejects unknown
// formats or tampered captures.
// TestSchedulePromptCaptureNeedsOwnerFormatMarker не даёт вложенным legacy-значениям в форме
// шаблона назначать полномочия шаблона, связывает формат владельца с дайджестом и отклоняет
// неизвестные форматы и подменённые снимки.
func TestSchedulePromptCaptureNeedsOwnerFormatMarker(t *testing.T) {
	digest := sha256.Sum256([]byte("Owner template"))
	raw := []byte(fmt.Sprintf(`{"revision":1,"values":{},"template":{"ref":"mrev_abcdefgh","digest":%q,"content":"Owner template"}}`, hex.EncodeToString(digest[:])))
	legacy, err := decodeSchedulePromptCapture(0, raw)
	if err != nil || legacy.Template != nil || legacy.Values["template"] == nil {
		t.Fatal("legacy input assigned template authority")
	}
	current, err := decodeSchedulePromptCapture(1, raw)
	if err != nil || current.Template == nil || current.Template.Ref != "mrev_abcdefgh" {
		t.Fatalf("owner capture rejected: %v", err)
	}
	if schedulePromptDigest(0, raw) == schedulePromptDigest(1, raw) {
		t.Fatal("format marker is not digest bound")
	}
	if _, err := decodeSchedulePromptCapture(2, raw); err == nil {
		t.Fatal("unknown format accepted")
	}
	if _, err := decodeSchedulePromptCapture(1, []byte(`{"revision":1,"values":{},"template":{"ref":"mrev_abcdefgh","digest":"bad","content":"Owner template"}}`)); err == nil {
		t.Fatal("tampered capture accepted")
	}
}
