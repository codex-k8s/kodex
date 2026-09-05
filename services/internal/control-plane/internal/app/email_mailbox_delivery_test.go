package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/emailprojection"
)

type mailboxDeliveryFixture struct {
	work               entity.EmailMailboxPublicationWork
	fail               string
	steps              []string
	claimed, recovered bool
	configuredRevision int64
}

func (f *mailboxDeliveryFixture) ReconcileConfiguredEmail(_ context.Context, configuration api.Configuration) error {
	f.configuredRevision = configuration.Revision
	return f.step("configured")
}

func TestMailboxDeliveryReloadsGitSourceAndKeepsRecoveryOnInvalidCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mailboxes.json")
	f := &mailboxDeliveryFixture{}
	d := &mailboxDelivery{store: f, publisher: f, sourceFile: path}
	for _, revision := range []int64{1, 2} {
		raw, err := json.Marshal(api.Configuration{Version: "email-bridge/v1", Revision: revision, ManagedBy: "git", Source: "git-fixture", Mailboxes: []api.Mailbox{}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := d.reconcile(t.Context()); err != nil || f.configuredRevision != revision {
			t.Fatalf("source was not reloaded: %v", err)
		}
	}
	if err := os.WriteFile(path, []byte(`{"unrecognized":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	f.steps = nil
	if _, err := d.reconcile(t.Context()); err == nil || strings.Join(f.steps, ",") != "admission,claim" {
		t.Fatalf("invalid candidate cancelled existing owner recovery or passed: %v %v", f.steps, err)
	}
}

func (f *mailboxDeliveryFixture) step(name string) error {
	f.steps = append(f.steps, name)
	if f.fail == name {
		return errors.New("fixture rejection")
	}
	return nil
}
func (f *mailboxDeliveryFixture) CheckPublicationAdmission(context.Context) error {
	return f.step("admission")
}
func (f *mailboxDeliveryFixture) ClaimEmailMailboxPublication(context.Context, string) (entity.EmailMailboxPublicationWork, bool, error) {
	return f.work, f.claimed, f.step("claim")
}
func (f *mailboxDeliveryFixture) RecoverEmailMailboxPublication(context.Context, entity.EmailMailboxPublicationWork) (bool, error) {
	return f.recovered, f.step("recovery")
}
func (f *mailboxDeliveryFixture) ReleaseEmailMailboxPublication(context.Context, entity.EmailMailboxPublicationWork) error {
	return f.step("release")
}
func (f *mailboxDeliveryFixture) StageEmailMailboxPolicy(_ context.Context, _ entity.EmailMailboxPublicationWork, doc mailpolicy.MailDocument) error {
	f.work.PolicyDocument, _ = json.Marshal(doc)
	return f.step("stage")
}
func (f *mailboxDeliveryFixture) EmailCredentialDigests(context.Context, api.Configuration) (map[string]string, error) {
	return map[string]string{}, f.step("credentials")
}
func (f *mailboxDeliveryFixture) PublishMailbox(context.Context, api.Configuration, map[string]string, mailpolicy.MailDocument) (emailprojection.Receipt, error) {
	return emailprojection.Receipt{}, f.step("publish")
}
func (f *mailboxDeliveryFixture) MarkEmailMailboxApplied(context.Context, entity.EmailMailboxPublicationWork) error {
	return f.step("applied")
}
func (f *mailboxDeliveryFixture) CheckMailbox(context.Context, api.Configuration, map[string]string, mailpolicy.MailDocument) (emailprojection.Receipt, error) {
	return emailprojection.Receipt{}, f.step("readback")
}
func (f *mailboxDeliveryFixture) CompleteEmailMailboxPublication(context.Context, entity.EmailMailboxPublicationWork) error {
	return f.step("complete")
}
func (f *mailboxDeliveryFixture) Resolve(context.Context, string) (mailpolicy.Snapshot, error) {
	return mailpolicy.Snapshot{}, errors.New("unexpected DNS query")
}

func TestMailboxDeliveryStopsBeforeOwnerActivationOnPartialEffect(t *testing.T) {
	for _, failure := range []string{"admission", "claim", "recovery", "stage", "credentials", "publish", "applied", "readback", "complete", ""} {
		t.Run(failure, func(t *testing.T) {
			f := &mailboxDeliveryFixture{fail: failure, claimed: true, work: entity.EmailMailboxPublicationWork{Ref: "publication-fixture", ClaimGeneration: 1, Configuration: api.Configuration{Version: "email-bridge/v1", Revision: 2, ManagedBy: "ui", Source: "control-plane", Mailboxes: []api.Mailbox{}}}}
			d := &mailboxDelivery{store: f, publisher: f, resolver: f, claimant: "worker", gatewayDigest: strings.Repeat("a", 64)}
			_, err := d.reconcile(t.Context())
			if (err != nil) != (failure != "") {
				t.Fatalf("unexpected outcome: %v", err)
			}
			steps := strings.Join(f.steps, ",")
			if failure != "" && failure != "complete" && strings.Contains(steps, "complete") {
				t.Fatalf("partial delivery activated: %s", steps)
			}
			if failure == "" && steps != "admission,claim,recovery,stage,credentials,publish,applied,readback,complete,release" {
				t.Fatalf("delivery order: %s", steps)
			}
		})
	}
}

func TestMailboxDeliveryCompetingClaimNeverReplaysAcceptedProjection(t *testing.T) {
	f := &mailboxDeliveryFixture{}
	d := &mailboxDelivery{store: f, publisher: f}
	handled, err := d.reconcile(t.Context())
	if !handled || err != nil || strings.Join(f.steps, ",") != "admission,claim" {
		t.Fatalf("competing claim allowed fallback: %v %v", handled, err)
	}
}
