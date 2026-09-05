package emailbridgeapi

import (
	"strings"
	"testing"
	"time"
)

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

func TestExecutionBindingDoesNotChangeSemanticDigest(t *testing.T) {
	command := Command{Operation: OperationSend, MailboxId: "mailbox", EffectKey: "effect"}
	before := Digest(command)
	ref := "inv_fixture01"
	ctx := WithExecutionBinding(t.Context(), &ExecutionBinding{InvocationRef: &ref})
	if ExecutionFromContext(ctx) == nil || Digest(command) != before {
		t.Fatal("transport binding changed semantic input")
	}
}
