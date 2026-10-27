package emailprojection

import (
	"errors"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"k8s.io/client-go/kubernetes/fake"
)

// TestMailboxPreflightClosed checks both paths reject mismatched pins and missing admission before writes.
// TestMailboxPreflightClosed проверяет отказ обоих путей до записи при несовпадении pins или отсутствии admission.
func TestMailboxPreflightClosed(t *testing.T) {
	_, _, configuration, document := publicationFixture(t)
	for _, which := range []string{"publish", "readback"} {
		t.Run(which, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			publisher, err := New(client, "kodex-system")
			if err != nil {
				t.Fatal(err)
			}
			run := func(config api.Configuration, doc mailpolicy.MailDocument) error {
				if which == "publish" {
					_, e := publisher.PublishMailbox(t.Context(), config, nil, doc)
					return e
				}
				_, e := publisher.CheckMailbox(t.Context(), config, nil, doc)
				return e
			}
			bad := document
			bad.ConfigurationRevision++
			err = run(configuration, bad)
			if !errors.Is(err, ErrInvalid) || len(client.Actions()) != 0 {
				t.Fatal("invalid pins reached client")
			}
			err = run(configuration, document)
			if !errors.Is(err, ErrUnavailable) {
				t.Fatal("missing admission did not fail closed")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatal("preflight performed a mutation")
				}
			}
		})
	}
}
