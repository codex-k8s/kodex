package platform

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// testEmailMailboxOwner checks mailbox draft, publication, binding, removal and Git ownership with
// delivery acknowledgements and durable expiry outcomes.
// testEmailMailboxOwner проверяет черновик, публикацию, привязку, удаление и владение Git для
// ящика с подтверждениями доставки и сохранёнными исходами истечения срока.
func testEmailMailboxOwner(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner, reader value.Principal, connectionRef string, mailbox api.Mailbox) {
	t.Helper()
	create := command.Command{Kind: command.CreateEmailMailboxDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "mailbox-owner-create"},
		Payload: command.EmailMailboxInput{ConnectionRef: connectionRef, Managed: command.ManagedConfigurationInput{Name: "Mailbox owner fixture", ContentFormat: "YAML", Content: "{}"}}}
	created, err := service.Execute(ctx, create)
	if err != nil || created.EmailMailbox == nil || created.EmailMailbox.Revision.State != "DRAFT" {
		t.Fatalf("create mailbox draft: %v", err)
	}
	view := created.EmailMailbox
	replayed, err := service.Execute(ctx, create)
	if err != nil || replayed.EmailMailbox == nil || replayed.EmailMailbox.Revision.Ref != view.Revision.Ref {
		t.Fatalf("mailbox create replay: %v", err)
	}
	change := func(kind command.Kind, key, content string) (command.Result, error) {
		version := view.Configuration.Version
		return service.Execute(ctx, command.Command{Kind: kind, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &version}, Payload: command.EmailMailboxInput{
				Managed: command.ManagedConfigurationInput{ConfigurationRef: view.Configuration.Ref, RevisionRef: view.Revision.Ref, ContentFormat: "JSON", Content: content}}})
	}
	invalid, err := change(command.ValidateEmailMailboxDraft, "mailbox-owner-invalid", "")
	if err != nil || invalid.EmailMailbox == nil || invalid.EmailMailbox.Revision.State != "INVALID" || len(invalid.EmailMailbox.Diagnostics) != 1 {
		t.Fatalf("validate incomplete mailbox: %v", err)
	}
	view = invalid.EmailMailbox
	spec := entity.EmailMailboxSpecification{Enabled: mailbox.Enabled, ReceiveProtocol: mailbox.ReceiveProtocol,
		AllowedFolders: mailbox.AllowedFolders, ArchiveFolder: mailbox.ArchiveFolder, DraftsFolder: mailbox.DraftsFolder,
		Folder: mailbox.Folder, Sender: mailbox.Sender, ReplyTo: mailbox.ReplyTo, Recipients: mailbox.Recipients, HelloName: mailbox.HelloName,
		SMTP: mailbox.Smtp, IMAP: mailbox.Imap, POP: mailbox.Pop, Limits: mailbox.Limits, Policies: mailbox.Policies}
	// Публичный typed профиль не может содержать legacy MARK. Старый helper
	// также пропускал replyTo: сохраняем его INVALID, затем новую forward revision.
	spec.Policies = slices.DeleteFunc(slices.Clone(spec.Policies), func(policy api.OperationPolicy) bool { return policy.Operation == api.OperationMark })
	if len(spec.Policies) != 21 {
		t.Fatal("public mailbox policy count mismatch")
	}
	incomplete := spec
	incomplete.ReplyTo = ""
	incompleteRaw, err := json.Marshal(incomplete)
	if err != nil {
		t.Fatal(err)
	}
	incompleteSaved, err := change(command.SaveEmailMailboxDraft, "mailbox-helper-incomplete-save", string(incompleteRaw))
	if err != nil {
		t.Fatal(err)
	}
	view = incompleteSaved.EmailMailbox
	incompleteValidated, err := change(command.ValidateEmailMailboxDraft, "mailbox-helper-incomplete-validate", "")
	if err != nil || incompleteValidated.EmailMailbox == nil || incompleteValidated.EmailMailbox.Revision.State != "INVALID" {
		t.Fatalf("helper without replyTo accepted: %v", err)
	}
	view = incompleteValidated.EmailMailbox
	disabled := spec
	disabled.Enabled = false
	disabled.SMTP.Secret.Name = "email-" + strings.Repeat("0", 32)
	disabledRaw, err := json.Marshal(disabled)
	if err != nil {
		t.Fatal(err)
	}
	disabledPreview, err := service.PreviewEmailMailboxConfiguration(ctx, owner, connectionRef, "JSON", string(disabledRaw))
	if err != nil || disabledPreview.Valid || len(disabledPreview.Diagnostics) != 1 || disabledPreview.Diagnostics[0].Code != emailpolicy.DiagnosticCredential {
		t.Fatalf("disabled mailbox preview must verify credential owner: %v", err)
	}
	disabledSaved, err := change(command.SaveEmailMailboxDraft, "mailbox-owner-disabled-save", string(disabledRaw))
	if err != nil || disabledSaved.EmailMailbox == nil {
		t.Fatalf("save disabled mailbox draft: %v", err)
	}
	view = disabledSaved.EmailMailbox
	disabledValidated, err := change(command.ValidateEmailMailboxDraft, "mailbox-owner-disabled-validate", "")
	if err != nil || disabledValidated.EmailMailbox == nil || disabledValidated.EmailMailbox.Revision.State != "INVALID" || len(disabledValidated.EmailMailbox.Diagnostics) != 1 || disabledValidated.EmailMailbox.Diagnostics[0].Code != emailpolicy.DiagnosticCredential {
		t.Fatalf("disabled mailbox validation must verify credential owner: %v", err)
	}
	view = disabledValidated.EmailMailbox
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := change(command.SaveEmailMailboxDraft, "mailbox-owner-save", string(raw))
	if err != nil || saved.EmailMailbox == nil || saved.EmailMailbox.Revision.Ref == view.Revision.Ref || saved.EmailMailbox.Revision.ParentRevisionRef != view.Revision.Ref || len(saved.EmailMailbox.Specification.Policies) != 21 {
		t.Fatalf("save immutable mailbox revision: %v", err)
	}
	view = saved.EmailMailbox
	valid, err := change(command.ValidateEmailMailboxDraft, "mailbox-owner-valid", "")
	if err != nil || valid.EmailMailbox == nil || valid.EmailMailbox.Revision.State != "VALID" {
		if valid.EmailMailbox != nil {
			t.Logf("validation diagnostics: %v", valid.EmailMailbox.Diagnostics)
		}
		t.Fatalf("validate mailbox with exact credential owner: %v", err)
	}
	view = valid.EmailMailbox
	published, err := change(command.PublishEmailMailboxDraft, "mailbox-owner-publish", "")
	if err != nil || published.EmailMailbox == nil || published.EmailMailbox.Revision.State != "PUBLISHED" || published.EmailMailbox.Publication != nil {
		t.Fatalf("publish revision without delivery claim: %v", err)
	}
	view = published.EmailMailbox
	read, err := service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, view.Configuration.Ref, view.Revision.Ref)
	if err != nil || read.Revision.Digest != view.Revision.Digest || read.Specification.SMTP.Secret.Name != spec.SMTP.Secret.Name {
		t.Fatalf("exact mailbox readback: %v", err)
	}
	list, err := service.ListEmailMailboxConfigurations(ctx, owner, connectionRef, "owner fixture", query.Page{Size: 1})
	if err != nil || list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("bounded mailbox list: %v", err)
	}
	if _, err := service.GetEmailMailboxConfiguration(ctx, reader, connectionRef, view.Configuration.Ref, ""); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("mailbox reader without integration permission: %v", err)
	}
	receipt, err := service.GetEmailMailboxCredentialReceipt(ctx, owner, connectionRef, "email-password-first")
	if err != nil || receipt.Name == "" || receipt.ContentSHA256 != "" || receipt.SecretRef != "" || receipt.SecretUID != "" {
		t.Fatalf("safe credential receipt: %v", err)
	}
	if _, err := service.GetEmailMailboxCredentialReceipt(ctx, reader, connectionRef, "email-password-first"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("credential receipt leaked to reader: %v", err)
	}
	credentials, total, next, err := service.ListEmailMailboxCredentials(ctx, owner, connectionRef, "AUTH_SECRET", query.Page{Size: 1})
	if err != nil || total != 2 || len(credentials) != 1 || next == "" {
		t.Fatalf("credential page: %v", err)
	}
	second, _, _, err := service.ListEmailMailboxCredentials(ctx, owner, connectionRef, "AUTH_SECRET", query.Page{Size: 1, Token: next})
	if err != nil || len(second) != 1 || second[0].Name == credentials[0].Name {
		t.Fatalf("credential continuation: %v", err)
	}
	if _, _, _, err := service.ListEmailMailboxCredentials(ctx, owner, connectionRef, "USERNAME", query.Page{Size: 1, Token: next}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("credential cursor scope: %v", err)
	}
	preview, err := service.PreviewEmailMailboxConfiguration(ctx, owner, connectionRef, "YAML", "credentialValue: hidden")
	if err != nil || preview.Specification != nil || preview.Valid || len(preview.Diagnostics) != 1 {
		t.Fatalf("safe parser failure preview: %v", err)
	}
	version := view.Configuration.Version
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.managed_configuration_sets SET managed_by='GIT',source='https://git.example.test/mailboxes',source_revision=repeat('a',40) WHERE ref=$1`, view.Configuration.Ref); err != nil {
		t.Fatal(err)
	}
	copied, err := service.Execute(ctx, command.Command{Kind: command.CopyGitManagedConfiguration, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "mailbox-owner-git-copy", ExpectedVersion: &version},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: view.Configuration.Ref, Name: "Mailbox Git copy"}})
	if err != nil || copied.ManagedConfiguration == nil {
		t.Fatalf("mailbox copy: %v", err)
	}
	copyView, err := service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, copied.ManagedConfiguration.Ref, "")
	if err != nil || copyView.Revision.State != "DRAFT" || copyView.MailboxRef == view.MailboxRef || copyView.Revision.ParentRevisionRef != view.Revision.Ref || copyView.BoundRevisionRef != "" {
		t.Fatalf("mailbox copy owner/readback/lineage: %v", err)
	}
	detached, err := service.Execute(ctx, command.Command{Kind: command.DetachGitManagedConfiguration, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "mailbox-owner-git-detach", ExpectedVersion: &version},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: view.Configuration.Ref}})
	if err != nil || detached.ManagedConfiguration == nil {
		t.Fatalf("mailbox detach: %v", err)
	}
	view, err = func() (*entity.EmailMailboxConfigurationView, error) {
		read, err := service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, view.Configuration.Ref, view.Revision.Ref)
		return &read, err
	}()
	if err != nil || view.Configuration.ManagedBy != "UI" {
		t.Fatalf("mailbox detached owner read: %v", err)
	}
	version = view.Configuration.Version
	bound, err := service.Execute(ctx, command.Command{Kind: command.BindEmailMailboxConfiguration, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "mailbox-owner-bind", ExpectedVersion: &version},
		Payload: command.EmailMailboxInput{ConnectionRef: connectionRef, ExpectedConnectionVersion: view.ConnectionVersion,
			Managed: command.ManagedConfigurationInput{ConfigurationRef: view.Configuration.Ref, RevisionRef: view.Revision.Ref}}})
	if err != nil || bound.EmailPublication == nil || bound.EmailPublication.State != "PENDING" {
		t.Fatalf("bind must await delivery: %v; actions=%+v", err, view.NextActions)
	}
	work, found, err := repository.ClaimEmailMailboxPublication(ctx, "mailbox-pg-worker")
	if err != nil || !found || work.Ref != bound.EmailPublication.Ref {
		t.Fatalf("claim exact mailbox publication: %v", err)
	}
	if _, found, err := repository.ClaimEmailMailboxPublication(ctx, "competing-worker"); err != nil || found {
		t.Fatalf("concurrent mailbox claim: %v", err)
	}
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "email-bridge", Operation: "platform.email.configuration.report",
	}, "email-bridge")
	if err := service.ReportEmailConfigurationReadback(ctx, worker, work.Configuration.Revision, api.Digest(work.Configuration)); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("callback before apply: %v", err)
	}
	document, err := mailpolicy.Produce(ctx, work.Configuration, strings.Repeat("a", 64), mailboxOwnerResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.StageEmailMailboxPolicy(ctx, work, document); err != nil {
		t.Fatalf("stage exact network policy: %v", err)
	}
	if err := repository.MarkEmailMailboxApplied(ctx, work); err != nil {
		t.Fatalf("record exact apply: %v", err)
	}
	if err := repository.CompleteEmailMailboxPublication(ctx, work); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("READY before callback: %v", err)
	}
	if err := service.ReportEmailConfigurationReadback(ctx, worker, work.Configuration.Revision, api.Digest(work.Configuration)); err != nil {
		t.Fatalf("accept exact applied callback before READY: %v", err)
	}
	if err := repository.CompleteEmailMailboxPublication(ctx, work); err != nil {
		t.Fatalf("complete mailbox publication: %v", err)
	}
	ready, err := service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, "", "")
	if err != nil || ready.Publication == nil || ready.Publication.State != "READY" || ready.BoundRevisionRef != view.Revision.Ref {
		t.Fatalf("authoritative bound READY readback: %v", err)
	}
	testMailboxObservationLifecycle(t, ctx, repository, service, owner, ready)
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.integration_connections SET enabled=false,version=version+1 WHERE ref=$1`, connectionRef); err != nil {
		t.Fatal(err)
	}
	stale, found, err := repository.ClaimEmailMailboxPublication(ctx, "mailbox-recovery-worker")
	if err != nil || !found {
		t.Fatalf("claim revoked publication: %v", err)
	}
	if recovered, err := repository.RecoverEmailMailboxPublication(ctx, stale); err != nil || !recovered {
		t.Fatalf("forward recovery after connection revocation: %v", err)
	}
	if err := service.ReportEmailConfigurationReadback(ctx, worker, stale.Configuration.Revision, api.Digest(stale.Configuration)); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("superseded callback accepted: %v", err)
	}
	recovery, found, err := repository.ClaimEmailMailboxPublication(ctx, "mailbox-recovery-worker")
	if err != nil || !found || recovery.Configuration.Revision <= stale.Configuration.Revision || len(recovery.Configuration.Mailboxes) != 0 {
		t.Fatalf("recovery must advance revision and remove revoked mailbox: %v", err)
	}
	// Общая тестовая подготовка applied readback; завершение каждого перехода проверяется отдельно.
	prepareAppliedReadback := func(item entity.EmailMailboxPublicationWork) {
		t.Helper()
		document, err := mailpolicy.Produce(ctx, item.Configuration, strings.Repeat("a", 64), mailboxOwnerResolver{})
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.StageEmailMailboxPolicy(ctx, item, document); err != nil {
			t.Fatal(err)
		}
		if err := repository.MarkEmailMailboxApplied(ctx, item); err != nil {
			t.Fatal(err)
		}
		if err := service.ReportEmailConfigurationReadback(ctx, worker, item.Configuration.Revision, api.Digest(item.Configuration)); err != nil {
			t.Fatal(err)
		}
	}
	prepareAppliedReadback(recovery)
	if err := repository.CompleteEmailMailboxPublication(ctx, recovery); err != nil {
		t.Fatalf("complete removal recovery: %v", err)
	}
	if _, err := service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, "", ""); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("revoked recovery retained binding: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.integration_connections SET enabled=true,version=version+1 WHERE ref=$1`, connectionRef); err != nil {
		t.Fatal(err)
	}
	mailbox.Revision = 1
	git := api.Configuration{Version: "email-bridge/v1", Revision: 1, ManagedBy: "git", Source: "git/mailbox-owner-fixture", Mailboxes: []api.Mailbox{mailbox}}
	raw, err = json.Marshal(git)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.InitializeEmailConfiguration(ctx, raw); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("Git adopted a personal mailbox: %v", err)
	}
	personalPassword := mailbox.Smtp.Secret
	mailbox = configureGitMailboxFixture(t, ctx, repository, service, owner, mailbox, "lifecycle")
	connectionRef = mailbox.ConnectionId
	git.Mailboxes = []api.Mailbox{mailbox}
	staleOwner := git
	staleOwner.Mailboxes = slices.Clone(git.Mailboxes)
	staleOwner.Mailboxes[0].CredentialOwnerGeneration--
	raw, err = json.Marshal(staleOwner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.InitializeEmailConfiguration(ctx, raw); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("Git accepted a stale mailbox owner generation: %v", err)
	}
	foreignCredential := git
	foreignCredential.Mailboxes = slices.Clone(git.Mailboxes)
	foreignCredential.Mailboxes[0].Smtp.Secret = personalPassword
	raw, err = json.Marshal(foreignCredential)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.InitializeEmailConfiguration(ctx, raw); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("Git accepted credentials from a personal mailbox: %v", err)
	}
	shared, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
	if err != nil {
		t.Fatal(err)
	}
	disabledResult, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "git-mailbox-lifecycle-disable-before-import", ExpectedVersion: &shared.Version},
		Payload:  command.ConnectionInput{Ref: connectionRef, Enabled: false}})
	if err != nil || disabledResult.Connection == nil || disabledResult.Connection.Enabled {
		t.Fatalf("disable shared mailbox before import: %v", err)
	}
	raw, err = json.Marshal(git)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.InitializeEmailConfiguration(ctx, raw); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("Git queued an enabled mailbox on a disabled connection: %v", err)
	}
	after, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
	if err != nil || after.Enabled || after.Version != disabledResult.Connection.Version {
		t.Fatalf("rejected Git import changed the disabled connection: %v", err)
	}
	enabled, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "git-mailbox-lifecycle-enable-after-reject", ExpectedVersion: &after.Version},
		Payload:  command.ConnectionInput{Ref: connectionRef, Enabled: true}})
	if err != nil || enabled.Connection == nil || !enabled.Connection.Enabled {
		t.Fatalf("enable shared mailbox for import: %v", err)
	}
	importGit := func(configuration api.Configuration) {
		t.Helper()
		raw, err := json.Marshal(configuration)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repository.InitializeEmailConfiguration(ctx, raw); err != nil {
			t.Fatalf("configured Git import: %v", err)
		}
	}
	completeGit := func() {
		t.Helper()
		work, found, err := repository.ClaimEmailMailboxPublication(ctx, "git-owner-worker")
		if err != nil || !found {
			t.Fatalf("claim Git delivery: %v", err)
		}
		if recovered, err := repository.RecoverEmailMailboxPublication(ctx, work); err != nil || recovered {
			t.Fatalf("fresh Git snapshot rejected: %v", err)
		}
		prepareAppliedReadback(work)
		if err := repository.CompleteEmailMailboxPublication(ctx, work); err != nil {
			t.Fatalf("Git binding activation: %v", err)
		}
	}
	importGit(git)
	importGit(git)
	if _, err := service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, "", ""); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("Git binding before delivery: %v", err)
	}
	completeGit()
	accepted, err := repository.EmailConfiguration(ctx)
	if err != nil || len(accepted.Mailboxes) != 1 ||
		accepted.Mailboxes[0].ConnectionId != connectionRef ||
		accepted.Mailboxes[0].CredentialOwnerGeneration != mailbox.CredentialOwnerGeneration {
		t.Fatalf("Git publication lost its verified owner generation: %v", err)
	}
	ownershipRead, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ownershipRead.Rollback(ctx)
	eligibility, err := readEmailConfigurationOwnersTx(ctx, ownershipRead, accepted)
	if err != nil || !eligibility[[2]string{mailbox.TenantId, connectionRef}] {
		t.Fatalf("published Git mailbox is not eligible for projection: %v", err)
	}
	if err := ownershipRead.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	foreignSource := git
	foreignSource.Source = "git/other-mailbox-owner-fixture"
	raw, err = json.Marshal(foreignSource)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.InitializeEmailConfiguration(ctx, raw); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("another Git source claimed the same connection: %v", err)
	}
	gitView, err := service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, "", "")
	if err != nil || gitView.Configuration.ManagedBy != "GIT" || gitView.Revision.State != "PUBLISHED" || gitView.Publication == nil || gitView.Publication.State != "READY" {
		t.Fatalf("Git safe lineage readback: %v", err)
	}
	git.Revision = 2
	git.Mailboxes = []api.Mailbox{}
	importGit(git)
	completeGit()
	if _, err := service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, "", ""); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("Git removal kept binding: %v", err)
	}
	git.Revision = 3
	git.Mailboxes = []api.Mailbox{mailbox}
	importGit(git)
	completeGit()
	gitView, err = service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, "", "")
	if err != nil {
		t.Fatal(err)
	}
	gitVersion := gitView.Configuration.Version
	if _, err := service.Execute(ctx, command.Command{Kind: command.DetachGitManagedConfiguration, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "mailbox-real-git-detach", ExpectedVersion: &gitVersion}, Payload: command.ManagedConfigurationInput{ConfigurationRef: gitView.Configuration.Ref}}); err != nil {
		t.Fatalf("detach configured Git: %v", err)
	}
	git.Revision = 4
	git.Mailboxes[0].Sender = "changed@example.test"
	git.Mailboxes[0].EnvelopeFrom = "changed@example.test"
	importGit(git)
	detachedView, err := service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, gitView.Configuration.Ref, "")
	if err != nil || detachedView.Configuration.ManagedBy != "UI" || detachedView.Revision.State != "DRAFT" || detachedView.Specification.Sender != mailbox.Sender {
		t.Fatalf("Git import overwrote detached draft: %v", err)
	}
	changedGit := git
	changedGit.Mailboxes = []api.Mailbox{}
	raw, _ = json.Marshal(changedGit)
	if _, err := repository.InitializeEmailConfiguration(ctx, raw); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("same Git revision changed digest: %v", err)
	}
	emptyGit := api.Configuration{Version: "email-bridge/v1", Revision: 1, ManagedBy: "git", Source: "git/empty-owner-fixture", Mailboxes: []api.Mailbox{}}
	importGit(emptyGit)
	importGit(emptyGit)
	expired, err := repository.EmailConfiguration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	expired.Revision++
	raw, _ = json.Marshal(expired)
	if _, err := repository.pool.Exec(ctx, `INSERT INTO control_plane.email_mailbox_publications(ref,revision,digest,document,organization_id,connection_id,connection_version,created_by,kind,expires_at)
SELECT 'mailpub_expiredfixture1046',$1,$2,$3::jsonb,p.organization_id,p.connection_id,c.version,p.created_by,'RECOVERY',clock_timestamp()-interval '1 minute'
FROM control_plane.email_mailbox_publications p JOIN control_plane.integration_connections c ON c.id=p.connection_id ORDER BY p.revision DESC LIMIT 1`, expired.Revision, api.Digest(expired), raw); err != nil {
		t.Fatal(err)
	}
	expiredWork, found, err := repository.ClaimEmailMailboxPublication(ctx, "expired-owner-worker")
	if err != nil || !found {
		t.Fatalf("claim expired publication: %v", err)
	}
	if recovered, err := repository.RecoverEmailMailboxPublication(ctx, expiredWork); err != nil || !recovered {
		t.Fatalf("expired delivery recovery: %v", err)
	}
	var failure string
	if err := repository.pool.QueryRow(ctx, `SELECT failure_code FROM control_plane.email_mailbox_publications WHERE ref='mailpub_expiredfixture1046' AND state='FAILED'`).Scan(&failure); err != nil || failure != "EMAIL_MAILBOX_DELIVERY_EXPIRED" {
		t.Fatalf("durable expired outcome: %v", err)
	}
}

// configureGitMailboxFixture uses the same disable, select-service, configure-credentials and enable
// commands as an owner. Its dedicated connection shares neither a personal binding nor descriptors.
// configureGitMailboxFixture вызывает штатные команды отключения, выбора service, настройки credentials
// и включения. Выделенное подключение не наследует личную привязку или дескрипторы.
func configureGitMailboxFixture(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, mailbox api.Mailbox, fixtureKey string) api.Mailbox {
	t.Helper()
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	serviceRef := "svc_email_git_" + fixtureKey
	fixtureID := "git-mailbox-" + fixtureKey
	if _, err := repository.pool.Exec(ctx, `INSERT INTO control_plane.subjects(ref,organization_id,issuer,external_subject_digest,display_name,kind)
SELECT $1,id,'example.invalid',$3,'Git mailbox fixture','SERVICE'
FROM control_plane.organizations WHERE ref=$2`, serviceRef, resolved.AuthorityTenant, fmt.Sprintf("%x", sha256.Sum256([]byte(serviceRef)))); err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: fixtureID + "-connection"},
		Payload: command.ConnectionInput{DefinitionKey: "email", Name: "Git mailbox fixture " + fixtureKey, PublicConfiguration: map[string]any{
			"base_url": "https://email-bridge.kodex-system.svc.cluster.local", "mailbox_id": fixtureID, "from_address": mailbox.Sender,
		}}})
	if err != nil || created.Connection == nil {
		t.Fatalf("create Git mailbox connection: %v", err)
	}
	connection := *created.Connection
	setEnabled := func(key string, enabled bool) {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: fixtureID + "-" + key, ExpectedVersion: &connection.Version},
			Payload:  command.ConnectionInput{Ref: connection.Ref, Enabled: enabled}})
		if err != nil || result.Connection == nil || result.Connection.Enabled != enabled {
			t.Fatalf("set Git mailbox enabled=%t: %v", enabled, err)
		}
		connection = *result.Connection
	}
	setEnabled("disable-git-mailbox", false)
	ownershipOperator := owner
	ownershipOperator.Permission = "platform.email-mailboxes.ownership.configure"
	selected, err := service.Execute(ctx, command.Command{Kind: command.ConfigureEmailMailboxOwnership, Principal: ownershipOperator,
		Mutation: value.Mutation{IdempotencyKey: fixtureID + "-select-owner", ExpectedVersion: &connection.Version},
		Payload:  command.EmailMailboxOwnershipInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialProjectShared, ServiceSubjectRef: serviceRef}})
	if err != nil || selected.EmailMailboxOwnership == nil || !selected.EmailMailboxOwnership.OwnerEligible {
		t.Fatalf("select explicit Git mailbox service owner: %v", err)
	}
	connection.Version = selected.EmailMailboxOwnership.ConnectionVersion
	setEnabled("enable-git-mailbox", true)
	credentialOperator := owner
	credentialOperator.Permission = "platform.command.email-mailbox.configure-credential"
	credential := func(kind string, valueBytes []byte) api.Descriptor {
		t.Helper()
		stored, err := service.ConfigureEmailMailboxCredential(ctx, credentialOperator,
			value.Mutation{IdempotencyKey: fixtureID + "-" + kind, ExpectedVersion: &connection.Version}, connection.Ref, kind, valueBytes)
		if err != nil {
			t.Fatalf("configure Git mailbox %s descriptor: %v", kind, err)
		}
		connection.Version = stored.ConnectionVersion
		return api.Descriptor{Name: stored.Name, Generation: stored.Generation}
	}
	ca := credential("CA_CERTIFICATE", personalEmailFixtureCA(t))
	user := credential("USERNAME", []byte("git-mailbox@example.invalid"))
	password := credential("AUTH_SECRET", []byte("synthetic-git-mailbox-credential"))
	mailbox.Id, mailbox.TenantId, mailbox.ConnectionId = fixtureID, resolved.AuthorityTenant, connection.Ref
	mailbox.CredentialOwnerGeneration = selected.EmailMailboxOwnership.Generation
	mailbox.CredentialGeneration = connection.Version
	mailbox.Smtp.Ca, mailbox.Smtp.Username, mailbox.Smtp.Secret = ca, user, password
	if mailbox.Imap != nil {
		endpoint := *mailbox.Imap
		endpoint.Ca, endpoint.Username, endpoint.Secret = ca, user, password
		mailbox.Imap = &endpoint
	}
	if mailbox.Pop != nil {
		endpoint := *mailbox.Pop
		endpoint.Ca, endpoint.Username, endpoint.Secret = ca, user, password
		mailbox.Pop = &endpoint
	}
	return mailbox
}

// mailboxOwnerResolver provides a fixed public-address resolver for mailbox policy fixtures
// without performing DNS lookup.
// mailboxOwnerResolver предоставляет резолвер с фиксированным публичным адресом для почтовой
// оснастки без обращения к DNS.
type mailboxOwnerResolver struct{}

// Resolve returns the fixture address with a one-minute expiry independently of the requested
// hostname and context.
// Resolve возвращает адрес оснастки со сроком действия в одну минуту независимо от запрошенного
// имени и контекста.
func (mailboxOwnerResolver) Resolve(context.Context, string) (mailpolicy.Snapshot, error) {
	return mailpolicy.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}, ExpiresAt: time.Now().Add(time.Minute)}, nil
}
