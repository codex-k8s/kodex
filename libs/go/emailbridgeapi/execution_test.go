package emailbridgeapi

import (
	"strings"
	"testing"
	"time"
)

// TestExecutionHeaderExactBinding verifies roundtrip, mutually exclusive
// source refs, bounded lease generation and rejection of expired or malformed headers.
// RU: TestExecutionHeaderExactBinding проверяет передачу привязки, единственность
// источника, предел поколения и отказ для истёкших либо ошибочных заголовков.
func TestExecutionHeaderExactBinding(t *testing.T) {
	for _, source := range []string{"invocation", "connection-test"} {
		t.Run(source, func(t *testing.T) {
			ref := "source_fixture01"
			b := &ExecutionBinding{Lease: ExecutionLease{Ref: "lease_fixture01", Fence: "fixture-fence", Generation: 7, ExpiresAt: time.Now().Add(time.Minute).UTC()}}
			if source == "invocation" {
				b.InvocationRef = &ref
			} else {
				b.ConnectionTestRef = &ref
			}
			raw, err := ExecutionHeaderValue(b)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseExecutionHeader(raw)
			if err != nil || Digest(got) != Digest(b) {
				t.Fatal("execution binding roundtrip failed")
			}
			for _, malformed := range []string{raw + `{}`, strings.Replace(raw, `"generation":7`, `"generation":7,"generation":8`, 1), strings.Replace(raw, `"generation":7`, `"generation":9007199254740992`, 1), `{"unexpected":true}`, strings.Repeat("a", 4097)} {
				if _, err := ParseExecutionHeader(malformed); err == nil {
					t.Fatal("malformed execution binding accepted")
				}
			}
			b.Lease.ExpiresAt = time.Now().Add(-time.Second)
			raw, _ = ExecutionHeaderValue(b)
			if _, err := ParseExecutionHeader(raw); err == nil {
				t.Fatal("expired lease accepted")
			}
			b.Lease.Ref = strings.Repeat("a", 129)
			if ValidExecutionBinding(b) {
				t.Fatal("oversize lease ref accepted")
			}
		})
	}
}

// TestExecutionBindingDoesNotChangeSemanticDigest checks that attaching a
// transport binding leaves the command's semantic input hash unchanged.
// RU: TestExecutionBindingDoesNotChangeSemanticDigest проверяет, что
// транспортная привязка не меняет смысловой дайджест входной команды.
func TestExecutionBindingDoesNotChangeSemanticDigest(t *testing.T) {
	command := Command{Operation: OperationSend, MailboxId: "mailbox", EffectKey: "effect"}
	before := Digest(command)
	ref := "inv_fixture01"
	ctx := WithExecutionBinding(t.Context(), &ExecutionBinding{InvocationRef: &ref})
	if ExecutionFromContext(ctx) == nil || Digest(command) != before {
		t.Fatal("transport binding changed semantic input")
	}
}
