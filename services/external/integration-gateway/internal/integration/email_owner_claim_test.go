package integration

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"google.golang.org/protobuf/proto"
)

func TestCPProducedEmailClaim(t *testing.T) {
	kind := os.Getenv("KODEX_EMAIL_CLAIM_KIND")
	if kind == "" {
		t.Skip("disposable CP producer entrypoint required")
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 256<<10))
	if err != nil {
		t.Fatal(err)
	}
	var request Request
	if kind == "health" {
		var claim cp.IntegrationConnectionTestClaim
		if proto.Unmarshal(raw, &claim) != nil || claim.CredentialRevision != nil {
			t.Fatal("invalid owner health claim")
		}
		request = RequestFromTest(&claim)
	} else if kind == "invocation" {
		var claim cp.IntegrationInvocationClaim
		if proto.Unmarshal(raw, &claim) != nil || claim.CredentialRevision != nil {
			t.Fatal("invalid owner invocation claim")
		}
		request = RequestFromInvocation(&claim)
	} else {
		t.Fatal("unexpected fixture kind")
	}
	adapter := testAdapter(t)
	calls := 0
	emailFixture(t, adapter, func(w http.ResponseWriter, r *http.Request) {
		calls++
		binding, err := api.ParseExecutionHeader(r.Header.Get(api.ExecutionHeader))
		if err != nil || binding.Lease.Fence != request.EmailExecution.Lease.Fence || r.Header.Get("Authorization") != "Bearer "+binding.Lease.Fence {
			t.Fatal("owner fence binding lost")
		}
		body := `{"status":"ready"}`
		if kind == "invocation" {
			body = `{"status":"accepted","message_id":"fixture-effect"}`
		}
		_, _ = io.WriteString(w, body)
	})
	run := func(r Request) error {
		if kind == "health" {
			_, err := adapter.Test(t.Context(), r)
			return err
		}
		_, err := adapter.Execute(t.Context(), r)
		return err
	}
	if err := run(request); err != nil || calls != 1 {
		t.Fatalf("owner claim failed: %v calls=%d", err, calls)
	}
	for _, variant := range []string{"missing-binding", "expired", "generic-credential", "foreign-scope"} {
		bad := request
		binding := *request.EmailExecution
		bad.EmailExecution = &binding
		switch variant {
		case "missing-binding":
			bad.EmailExecution = nil
		case "expired":
			bad.EmailExecution.Lease.ExpiresAt = time.Now().Add(-time.Second)
		case "generic-credential":
			bad.Credential = &CredentialRevision{}
		case "foreign-scope":
			if kind == "health" {
				bad.Configuration = map[string]any{"base_url": "https://foreign.invalid", "mailbox_id": "foreign", "from_address": "foreign@example.invalid"}
			} else {
				bad.ResourceScopeDigest = strings.Repeat("0", 64)
			}
		}
		if run(bad) == nil || calls != 1 {
			t.Fatalf("invalid %s reached bridge", variant)
		}
	}
}
