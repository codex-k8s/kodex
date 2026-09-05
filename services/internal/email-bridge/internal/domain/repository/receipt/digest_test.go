package receipt

import (
	"strings"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

func TestExternalReceiptDigestCanonical(t *testing.T) {
	r := Record{ID: strings.Repeat("a", 32), Key: "effect:1", Digest: strings.Repeat("b", 64), Status: "unknown", Audit: Audit{Actor: "actor", Agent: "agent", Grant: "grant", Operation: api.OperationSend, ConfigurationRevision: 1, CredentialGeneration: 2, GateApproved: true}}
	scope := Scope{Tenant: "tenant", Mailbox: "mailbox"}
	const golden = "6dfdb1521d14b99bec6fac759edeb2a11ce30120cbeb1489ab7baa0d5150e41e"
	if r.ExternalDigest(scope) != golden {
		t.Fatal("canonical receipt digest changed")
	}
	r.Status, r.UID, r.UIDValidity, r.ContentDigest, r.Folder = "accepted", "42", 7, strings.Repeat("c", 64), "INBOX"
	if r.ExternalDigest(scope) != golden {
		t.Fatal("mutable outcome changed receipt identity")
	}
	for _, mutate := range []func(*Record){
		func(r *Record) { r.ID = strings.Repeat("d", 32) },
		func(r *Record) { r.Key += "x" },
		func(r *Record) { r.Digest = strings.Repeat("d", 64) },
		func(r *Record) { r.Resource = "resource" },
		func(r *Record) { r.Audit.Actor += "x" },
		func(r *Record) { r.Audit.Agent += "x" },
		func(r *Record) { r.Audit.Grant += "x" },
		func(r *Record) { r.Audit.Operation = api.OperationReply },
		func(r *Record) { r.Audit.ConfigurationRevision++ },
		func(r *Record) { r.Audit.CredentialGeneration++ },
		func(r *Record) { r.Audit.GateApproved = false },
	} {
		copy := r
		mutate(&copy)
		if copy.ExternalDigest(scope) == golden {
			t.Fatal("immutable field omitted from digest")
		}
	}
	if r.ExternalDigest(Scope{Tenant: "other", Mailbox: scope.Mailbox}) == golden || r.ExternalDigest(Scope{Tenant: scope.Tenant, Mailbox: "other"}) == golden {
		t.Fatal("receipt tenant scope omitted")
	}
}
