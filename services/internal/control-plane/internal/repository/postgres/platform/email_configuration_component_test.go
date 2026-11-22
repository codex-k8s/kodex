package platform

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/credentialmaterializer"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/emailprojection"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// testEmailConfiguration checks email configuration revisions and immutable owner documents, then
// retires the fixture before the next mailbox lifecycle checks.
// testEmailConfiguration проверяет ревизии почтовой конфигурации и неизменяемые документы
// владельца, затем выводит оснастку из эксплуатации перед следующей проверкой жизненного цикла ящика.
func testEmailConfiguration(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	raw, err := os.ReadFile("../../../../../../../contracts/email-bridge/v1/examples/mailboxes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var config api.Configuration
	if err := api.Decode(raw, &config); err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.connections.create",
	}, "control-api-gateway")
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Namespace: "kodex-system", Name: emailprojection.SecretName, UID: "email-bootstrap-fixture", ResourceVersion: "1",
	}})
	materializer, err := credentialmaterializer.New(client, "kodex-system", emailprojection.SecretName)
	if err != nil {
		t.Fatal(err)
	}
	service, err := platformservice.New(repository, platformservice.WithEmailCredentialMaterializer(materializer))
	if err != nil {
		t.Fatal(err)
	}
	mailbox := configureGitMailboxFixture(t, ctx, repository, service, owner, config.Mailboxes[0], "bootstrap")
	config.ManagedBy, config.Source, config.Mailboxes = "git", "git/mailbox-bootstrap-fixture", []api.Mailbox{mailbox}
	raw, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureEmail(ctx, raw); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("raw bootstrap accepted a mailbox without a configuration binding: %v", err)
	}
	if _, err := repository.InitializeEmailConfiguration(ctx, raw); err != nil {
		t.Fatalf("import authoritative mailbox fixture: %v", err)
	}
	work, found, err := repository.ClaimEmailMailboxPublication(ctx, "mailbox-bootstrap-fixture")
	if err != nil || !found || len(work.Configuration.Mailboxes) != 1 {
		t.Fatalf("claim exact bootstrap mailbox publication: %v", err)
	}
	completeMailboxLifecycleFixture(t, ctx, repository, service, work)
	config = work.Configuration
	mailbox = config.Mailboxes[0]
	// После проверки повышаем watermark пустым документом, чтобы следующий
	// mailbox lifecycle не унаследовал этот ящик.
	t.Cleanup(func() {
		current, err := repository.EmailConfiguration(ctx)
		if err != nil {
			t.Errorf("read email fixture cleanup: %v", err)
			return
		}
		current.Revision++
		current.Mailboxes = []api.Mailbox{}
		raw, err := json.Marshal(current)
		if err != nil {
			t.Errorf("encode email fixture cleanup: %v", err)
			return
		}
		if err := repository.ConfigureEmail(ctx, raw); err != nil {
			t.Errorf("retire email fixture forward-only: %v", err)
		}
	})
	accept := func(wantError bool) {
		t.Helper()
		encoded, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		err = repository.ConfigureEmail(ctx, encoded)
		if wantError && err == nil || !wantError && err != nil {
			t.Fatalf("accept configuration revision %d: %v", config.Revision, err)
		}
	}
	read := func(reader *Repository, revision int64, denied bool) {
		t.Helper()
		tx, err := repository.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		projection, err := reader.readEmailMailbox(ctx, tx, scope, mailbox.Id, revision)
		if denied && !errors.Is(err, errs.ErrForbidden) || !denied && (err != nil || projection.ConnectionRef != mailbox.ConnectionId) {
			t.Fatalf("read mailbox revision %d: %v", revision, err)
		}
	}
	initialRevision, initialGeneration := mailbox.Revision, mailbox.CredentialGeneration
	read(repository, initialRevision, false)
	accept(false)
	oldReader := *repository
	config.Mailboxes[0].Folder = "Archive"
	accept(true)
	config.Revision++
	accept(true)
	read(repository, initialRevision, false)
	config.Mailboxes[0].Revision++
	accept(false)
	read(&oldReader, initialRevision, true)
	read(repository, initialRevision, true)
	read(repository, initialRevision+1, false)
	config.Revision--
	accept(true)
	config.Revision += 2
	config.Mailboxes[0].Revision++
	config.Mailboxes[0].CredentialGeneration = initialGeneration - 1
	accept(true)
	config.Mailboxes[0].CredentialGeneration = initialGeneration
	secret := config.Mailboxes[0].Smtp.Secret
	config.Mailboxes[0].Smtp.Secret.Generation++
	accept(true)
	config.Mailboxes[0].Smtp.Secret = secret
	accept(false)
	updated := config.Mailboxes[0]
	config.Revision++
	config.Mailboxes = []api.Mailbox{}
	accept(false)
	read(repository, initialRevision+2, true)
	config.Revision++
	config.Mailboxes = []api.Mailbox{updated}
	accept(true)
	config.Mailboxes[0].Revision++
	accept(false)
	read(repository, initialRevision+3, false)
	config.Revision++
	config.Mailboxes[0].Revision++
	config.Mailboxes[0].ConnectionId = "unknown-connection"
	accept(true)
	read(repository, initialRevision+3, false)
	config.Revision--
	config.Mailboxes[0].Revision--
	config.Mailboxes[0].ConnectionId = mailbox.ConnectionId
	stored, err := repository.EmailConfiguration(ctx)
	if err != nil || api.Digest(stored) != api.Digest(config) {
		t.Fatalf("immutable runtime document readback: %v", err)
	}
	seed, err := json.Marshal(api.Configuration{Version: "email-bridge/v1", Revision: 1, ManagedBy: "git", Source: "release-bootstrap", Mailboxes: []api.Mailbox{}})
	if err != nil {
		t.Fatal(err)
	}
	restoredReader := *repository
	restored, err := restoredReader.InitializeEmailConfiguration(ctx, seed)
	if err != nil || restored.Revision != config.Revision || api.Digest(restored) != api.Digest(config) {
		t.Fatalf("release seed replaced immutable owner document: %v", err)
	}
	read(&restoredReader, initialRevision+3, false)
	t.Run("authorization and report owner lifecycle", func(t *testing.T) {
		connection, err := service.GetIntegrationConnection(ctx, owner, mailbox.ConnectionId)
		if err != nil {
			t.Fatal(err)
		}
		testEmailProducer(t, ctx, repository, service, owner, connection, config)
	})
}
