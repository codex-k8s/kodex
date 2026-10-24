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

// mailboxDeliveryFixture tracks synthetic publication steps, claimant/recovery outcomes and
// configured revision with one injected step failure and no real external publisher.
// mailboxDeliveryFixture отслеживает синтетические шаги публикации, исходы claim и recovery и
// configured revision с одним задаваемым отказом шага без реального внешнего publisher.
type mailboxDeliveryFixture struct {
	work               entity.EmailMailboxPublicationWork
	fail               string
	steps              []string
	claimed, recovered bool
	configuredRevision int64
}

// ReconcileConfiguredEmail records the candidate source revision and applies the configured
// synthetic failure at the source-reconciliation step.
// ReconcileConfiguredEmail записывает candidate source revision и применяет заданный синтетический
// отказ на шаге согласования источника.
func (f *mailboxDeliveryFixture) ReconcileConfiguredEmail(_ context.Context, configuration api.Configuration) error {
	f.configuredRevision = configuration.Revision
	return f.step("configured")
}

// TestMailboxDeliveryReloadsGitSourceAndKeepsRecoveryOnInvalidCandidate checks changed Git revisions
// reload each time and an invalid candidate still reaches owner claim handling while returning
// the import failure; the fixture has no claim, so it does not execute a recovery effect.
// TestMailboxDeliveryReloadsGitSourceAndKeepsRecoveryOnInvalidCandidate проверяет перечитывание Git
// ревизий и поиск owner claim при неверном candidate с возвратом ошибки импорта; claim в оснастке
// отсутствует, поэтому эффект recovery не исполняется.
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

// step appends a named synthetic delivery step and returns a fixed fixture rejection only for the
// configured failing step.
// step добавляет именованный синтетический шаг доставки и возвращает фиксированный отказ оснастки
// только для заданного ошибочного шага.
func (f *mailboxDeliveryFixture) step(name string) error {
	f.steps = append(f.steps, name)
	if f.fail == name {
		return errors.New("fixture rejection")
	}
	return nil
}

// CheckPublicationAdmission records synthetic publication admission without querying cluster or
// publisher state outside the fixture.
// CheckPublicationAdmission записывает синтетический admission публикации без запроса состояния
// кластера либо publisher вне оснастки.
func (f *mailboxDeliveryFixture) CheckPublicationAdmission(context.Context) error {
	return f.step("admission")
}

// ClaimEmailMailboxPublication returns the configured synthetic work and claim-presence flag while
// recording the exact claim step.
// ClaimEmailMailboxPublication возвращает заданные синтетическую работу и флаг наличия claim с
// записью точного шага claim.
func (f *mailboxDeliveryFixture) ClaimEmailMailboxPublication(context.Context, string) (entity.EmailMailboxPublicationWork, bool, error) {
	return f.work, f.claimed, f.step("claim")
}

// RecoverEmailMailboxPublication returns the configured recovered flag and records owner recovery
// before any staged or published effect.
// RecoverEmailMailboxPublication возвращает заданный флаг восстановления и записывает owner
// recovery до любого staged либо published эффекта.
func (f *mailboxDeliveryFixture) RecoverEmailMailboxPublication(context.Context, entity.EmailMailboxPublicationWork) (bool, error) {
	return f.recovered, f.step("recovery")
}

// ReleaseEmailMailboxPublication records release of the synthetic claim so tests can verify
// cleanup ordering after success or partial failure.
// ReleaseEmailMailboxPublication записывает освобождение синтетического claim для проверки порядка
// cleanup после успеха либо частичного отказа.
func (f *mailboxDeliveryFixture) ReleaseEmailMailboxPublication(context.Context, entity.EmailMailboxPublicationWork) error {
	return f.step("release")
}

// StageEmailMailboxPolicy stores the synthetic policy document in fixture work and records staging
// without persisting external owner state.
// StageEmailMailboxPolicy сохраняет синтетический policy document в работе оснастки и записывает
// staging без сохранения внешнего owner state.
func (f *mailboxDeliveryFixture) StageEmailMailboxPolicy(_ context.Context, _ entity.EmailMailboxPublicationWork, doc mailpolicy.MailDocument) error {
	f.work.PolicyDocument, _ = json.Marshal(doc)
	return f.step("stage")
}

// EmailCredentialDigests returns an empty synthetic commitment map and records credential lookup
// without loading any actual credential values.
// EmailCredentialDigests возвращает пустую синтетическую карту commitments и записывает поиск
// credentials без загрузки реальных значений credentials.
func (f *mailboxDeliveryFixture) EmailCredentialDigests(context.Context, api.Configuration) (map[string]string, error) {
	return map[string]string{}, f.step("credentials")
}

// PublishMailbox records a synthetic publish attempt and returns an empty receipt or the injected
// step failure without external writes.
// PublishMailbox записывает синтетическую попытку publish и возвращает пустую квитанцию либо
// заданный отказ шага без внешней записи.
func (f *mailboxDeliveryFixture) PublishMailbox(context.Context, api.Configuration, map[string]string, mailpolicy.MailDocument) (emailprojection.Receipt, error) {
	return emailprojection.Receipt{}, f.step("publish")
}

// MarkEmailMailboxApplied records the synthetic applied step separately from external readback and
// owner completion.
// MarkEmailMailboxApplied записывает синтетический шаг applied отдельно от внешнего readback и
// завершения владельца.
func (f *mailboxDeliveryFixture) MarkEmailMailboxApplied(context.Context, entity.EmailMailboxPublicationWork) error {
	return f.step("applied")
}

// CheckMailbox records synthetic readback independently of publish and applied so partial effects
// cannot imply completion.
// CheckMailbox записывает синтетический readback независимо от publish и applied, чтобы частичные
// эффекты не означали завершение.
func (f *mailboxDeliveryFixture) CheckMailbox(context.Context, api.Configuration, map[string]string, mailpolicy.MailDocument) (emailprojection.Receipt, error) {
	return emailprojection.Receipt{}, f.step("readback")
}

// CompleteEmailMailboxPublication records the synthetic owner completion step after all required
// earlier delivery effects and checks.
// CompleteEmailMailboxPublication записывает синтетический шаг завершения владельца после всех
// обязательных предыдущих эффектов и проверок доставки.
func (f *mailboxDeliveryFixture) CompleteEmailMailboxPublication(context.Context, entity.EmailMailboxPublicationWork) error {
	return f.step("complete")
}

// Resolve rejects unexpected DNS use so empty-mailbox delivery fixtures cannot silently depend on
// real network resolution.
// Resolve отклоняет неожиданный DNS вызов, чтобы оснастка доставки пустого mailbox не зависела
// молча от реального сетевого разрешения.
func (f *mailboxDeliveryFixture) Resolve(context.Context, string) (mailpolicy.Snapshot, error) {
	return mailpolicy.Snapshot{}, errors.New("unexpected DNS query")
}

// TestMailboxDeliveryStopsBeforeOwnerActivationOnPartialEffect injects failure at each
// admission/claim/recovery/stage/credential/publish/applied/readback/complete step and checks
// incomplete paths never reach owner completion.
// TestMailboxDeliveryStopsBeforeOwnerActivationOnPartialEffect задаёт отказ каждого шага
// admission, claim, recovery, stage, credentials, publish, applied, readback и complete и
// проверяет отсутствие завершения владельца на неполном пути.
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

// TestMailboxDeliveryCompetingClaimNeverReplaysAcceptedProjection checks absence of a claim still
// returns handled after admission/claim, preventing fallback publication of an older accepted
// projection.
// TestMailboxDeliveryCompetingClaimNeverReplaysAcceptedProjection проверяет возврат handled после
// admission и claim при отсутствии claim с запретом fallback публикации старой принятой проекции.
func TestMailboxDeliveryCompetingClaimNeverReplaysAcceptedProjection(t *testing.T) {
	f := &mailboxDeliveryFixture{}
	d := &mailboxDelivery{store: f, publisher: f}
	handled, err := d.reconcile(t.Context())
	if !handled || err != nil || strings.Join(f.steps, ",") != "admission,claim" {
		t.Fatalf("competing claim allowed fallback: %v %v", handled, err)
	}
}
