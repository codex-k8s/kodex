package platform

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/credentialmaterializer"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/emailprojection"
	"github.com/jackc/pgx/v5/pgxpool"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestPersonalEmailMailboxOwnerDevelopmentComponent checks actual owner/ACL/credential/revoke
// transactions for two ordinary project readers without network or live Secrets. Domain
// principals use the existing fixture protocol, not pending policy110 transport registration.
// Its first health command rejects unfinished publication before TESTING. The test then publishes
// the mailbox, qualifies NONE/SCOPED/EACH with a durable mutation Begin and fences an actual READ
// lease after revoke. Registered RPC/API, remote SMTP/IMAP and schema049 catalog qualification
// remain separate required tests.
// TestPersonalEmailMailboxOwnerDevelopmentComponent проверяет реальные owner/ACL/credential/revoke
// транзакции двух обычных project readers без сети и live Secrets. Domain principals используют
// существующий fixture protocol, а не pending transport registration110. Первая health command
// отклоняет незавершённую публикацию до TESTING. Затем тест публикует mailbox и ограждает настоящую
// READ lease после revoke, включая NONE/SCOPED/EACH с durable mutation Begin. Registered RPC/API,
// remote SMTP/IMAP и catalog qualification049 остаются отдельными обязательными проверками.
func TestPersonalEmailMailboxOwnerDevelopmentComponent(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "claimed-health-revoke"
		if pending {
			name = "open-gates-and-read-revoke"
		}
		t.Run(name, func(t *testing.T) { personalEmailOwnerDevelopmentCase(t, pending) })
	}
}

// personalEmailOwnerDevelopmentCase uses a fresh isolated database for each real owner lifecycle.
// Pending calls are revoked before root cancellation; the other case preserves a claimed health
// lease. These distinct triggers must not hide one another through earlier generic cleanup.
// personalEmailOwnerDevelopmentCase использует новую изолированную БД каждого owner lifecycle.
// Pending calls отзываются до root cancel; другой сценарий сохраняет claimed health lease.
// Разные triggers не должны скрывать друг друга предварительной общей очисткой.
func personalEmailOwnerDevelopmentCase(t *testing.T, pending bool) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	configuration, err := pgxpool.ParseConfig(isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("parse isolated personal email database")
	}
	if os.Getenv("KODEX_EMAIL_GATEWAY_TEST_BINARY") == "" {
		t.Fatal("personal email development requires the current gateway test consumer")
	}
	deadlineTrace := &emailAuthorizationDeadlineTrace{base: componentQueryRejectionTracer{t: t}}
	configuration.ConnConfig.Tracer = deadlineTrace
	configuration.AfterConnect = deadlineTrace.configureCodec
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal("open isolated personal email database")
	}
	defer pool.Close()
	r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	personalEmailMailboxBindingACLFixture(t, ctx, r)
	seedObservedCatalogFixture(t, ctx, r)
	admin := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	client := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "kodex-system", Name: emailprojection.SecretName, UID: "personal-email-development", ResourceVersion: "1"}, Data: map[string][]byte{}})
	materializer, err := credentialmaterializer.New(client, "kodex-system", emailprojection.SecretName)
	if err != nil {
		t.Fatal(err)
	}
	service, err := platformservice.New(r, platformservice.WithEmailCredentialMaterializer(materializer))
	if err != nil {
		t.Fatal(err)
	}
	projects := make([]string, 0, 2)
	for _, key := range []string{"email-project-a", "email-project-b"} {
		created, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: admin,
			Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.ProjectInput{Name: key}})
		if err != nil || created.Project == nil {
			t.Fatal("create existing project membership fixture", err)
		}
		projects = append(projects, created.Project.Ref)
	}
	a := contextProjectReader(t, ctx, r, service, admin, projects[0], "MEMORY")
	b := contextProjectReader(t, ctx, r, service, admin, projects[1], "SKILL")
	a.Permission, b.Permission = "platform.email-mailboxes.mine.create", "platform.email-mailboxes.mine.create"
	createA := command.Command{Kind: command.CreatePersonalEmailMailboxConnection, Principal: a,
		Mutation: value.Mutation{IdempotencyKey: "personal-email-create-a"}, Payload: command.PersonalEmailMailboxConnectionInput{Name: strings.Repeat("я", 160), Sender: "a@example.test"}}
	first, err := service.Execute(ctx, createA)
	if err != nil || first.Connection == nil || first.EmailMailboxOwnership == nil {
		t.Fatal("create ordinary user's personal mailbox", err)
	}
	connection := *first.Connection
	replayedCreate, err := service.Execute(ctx, createA)
	if err != nil || replayedCreate.Connection == nil || replayedCreate.Connection.Ref != connection.Ref || replayedCreate.Connection.Version != connection.Version || replayedCreate.EmailMailboxOwnership == nil || replayedCreate.EmailMailboxOwnership.Generation != 1 {
		t.Fatal("personal mailbox create replay duplicated or changed the assigned connection", err)
	}
	a.Permission = "platform.email-mailboxes.receipts.read"
	createReceipt, err := service.GetPersonalEmailMailboxMutationReceipt(ctx, a, "CREATE", "personal-email-create-a")
	if err != nil || createReceipt.Mailbox == nil || createReceipt.Mailbox.Ownership.ConnectionRef != connection.Ref || createReceipt.Mailbox.Name != strings.Repeat("я", 160) {
		t.Fatal("recover actual committed personal mailbox create receipt", err)
	}
	if !connection.Enabled || connection.State != "NOT_CONNECTED" || connection.CredentialPolicy != entity.IntegrationCredentialInitiatorRequired || first.EmailMailboxOwnership.Generation != 1 || !first.EmailMailboxOwnership.OwnerEligible {
		t.Fatal("personal creation ownership or enablement mismatch")
	}
	second, err := service.Execute(ctx, command.Command{Kind: command.CreatePersonalEmailMailboxConnection, Principal: b,
		Mutation: value.Mutation{IdempotencyKey: "personal-email-create-b"}, Payload: command.PersonalEmailMailboxConnectionInput{Name: "Второй личный ящик", Sender: "b@example.test"}})
	if err != nil || second.Connection == nil || second.Connection.Ref == connection.Ref {
		t.Fatal("create second ordinary user's personal mailbox", err)
	}
	b.Permission = "platform.email-mailboxes.receipts.read"
	if _, err := service.GetPersonalEmailMailboxMutationReceipt(ctx, b, "CREATE", "personal-email-create-a"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("another user recovered the first mailbox mutation receipt", err)
	}
	a.Permission = "platform.email-mailboxes.mine.list"
	items, _, err := service.ListMyEmailMailboxes(ctx, a, "", query.Page{Size: 20})
	if err != nil || len(items) != 1 || items[0].Ownership.ConnectionRef != connection.Ref {
		t.Fatal("personal catalog crossed owners", err)
	}
	b.Permission = "platform.email-mailboxes.ownership.read"
	if _, err := service.GetEmailMailboxOwnership(ctx, b, connection.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("foreign user read mailbox ownership", err)
	}
	a.Permission = "platform.command.projects.create"
	if _, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: a,
		Mutation: value.Mutation{IdempotencyKey: "personal-email-no-project-rights"}, Payload: command.ProjectInput{Name: "Denied project"}}); err == nil {
		t.Fatal("personal metadata ACL granted project creation")
	}
	a.Permission = "platform.command.integrations.test"
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: a,
		Mutation: value.Mutation{IdempotencyKey: "personal-email-unpublished-health", ExpectedVersion: &connection.Version}, Payload: command.ConnectionInput{Ref: connection.Ref}}); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("unpublished personal mailbox queued health", err)
	}
	a.Permission = "platform.query.integration-connections.get"
	unchanged, err := service.GetIntegrationConnection(ctx, a, connection.Ref)
	if err != nil || unchanged.State != "NOT_CONNECTED" || unchanged.Version != connection.Version {
		t.Fatal("failed health changed connection", err)
	}
	a.Permission = "platform.command.email-mailbox.configure-credential"
	credential, err := service.ConfigureEmailMailboxCredential(ctx, a, value.Mutation{IdempotencyKey: "personal-email-password-a", ExpectedVersion: &connection.Version}, connection.Ref, "AUTH_SECRET", []byte("synthetic-app-password-a"))
	if err != nil || credential.ConnectionVersion != connection.Version+1 {
		t.Fatal("configure current personal credential", err)
	}
	connection.Version = credential.ConnectionVersion
	client.ClearActions()
	b.Permission = "platform.command.email-mailbox.configure-credential"
	if _, err := service.ConfigureEmailMailboxCredential(ctx, b, value.Mutation{IdempotencyKey: "personal-email-foreign-password", ExpectedVersion: &connection.Version}, connection.Ref, "AUTH_SECRET", []byte("foreign-synthetic-value")); err == nil || len(client.Actions()) != 0 {
		t.Fatal("foreign owner reached credential materializer", err)
	}
	var finishRuntime func()
	connection, health := personalEmailPublishedHealthFixture(t, ctx, r, service, admin, a, connection, credential, deadlineTrace, !pending, func(connected entity.IntegrationConnection) {
		finishRuntime = personalEmailReadRuntimeFixture(t, ctx, r, service, admin, a, b, projects, connected, second.Connection.Ref, pending)
		if !pending {
			finishRuntime()
		}
	})
	client.ClearActions()
	a.Permission = "platform.email-mailboxes.ownership.revoke"
	revoked, err := service.Execute(ctx, command.Command{Kind: command.RevokeEmailMailboxOwnership, Principal: a,
		Mutation: value.Mutation{IdempotencyKey: "personal-email-revoke-a", ExpectedVersion: &connection.Version}, Payload: command.EmailMailboxOwnershipInput{ConnectionRef: connection.Ref}})
	if err != nil || revoked.EmailMailboxOwnership == nil || revoked.EmailMailboxOwnership.Generation != 2 || revoked.EmailMailboxOwnership.State != "REVOKED" {
		t.Fatal("revoke personal mailbox ownership", err)
	}
	connection.Version = revoked.EmailMailboxOwnership.ConnectionVersion
	a.Permission = "platform.email-mailboxes.receipts.read"
	revokeReceipt, err := service.GetPersonalEmailMailboxMutationReceipt(ctx, a, "REVOKE", "personal-email-revoke-a")
	if err != nil || revokeReceipt.Ownership == nil || revokeReceipt.Ownership.Generation != 2 || revokeReceipt.Ownership.State != "REVOKED" || revokeReceipt.Ownership.OwnerEligible {
		t.Fatal("recover committed revoke receipt without restoring eligibility", err)
	}
	historical, err := service.GetPersonalEmailMailboxMutationReceipt(ctx, a, "CREATE", "personal-email-create-a")
	if err != nil || historical.Mailbox == nil || historical.Mailbox.Ownership.Generation != 1 {
		t.Fatal("historical create receipt lost its immutable generation after revoke", err)
	}
	a.Permission = "platform.email-mailboxes.ownership.read"
	currentOwner, err := service.GetEmailMailboxOwnership(ctx, a, connection.Ref)
	if err != nil || currentOwner.Generation != 2 || currentOwner.State != "REVOKED" || currentOwner.OwnerEligible {
		t.Fatal("historical receipt became current mailbox authority", err)
	}
	if pending {
		finishRuntime()
	}
	if !pending {
		if _, err := service.ResolveEmailAuthorization(ctx, health.email, health.authorization); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("revoked mailbox retained read authorization", err)
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: health.gateway,
			Mutation: value.Mutation{IdempotencyKey: "personal-email-stale-health-completion"}, Payload: health.completion}); err == nil {
			t.Fatal("revoked health lease completed successfully")
		}
	}
	a.Permission = "platform.command.email-mailbox.configure-credential"
	if _, err := service.ConfigureEmailMailboxCredential(ctx, a, value.Mutation{IdempotencyKey: "personal-email-revoked-password", ExpectedVersion: &connection.Version}, connection.Ref, "AUTH_SECRET", []byte("revoked-synthetic-value")); err == nil || len(client.Actions()) != 0 {
		t.Fatal("revoked owner reached credential materializer", err)
	}
	if _, err := service.GetEmailMailboxCredentialReceipt(ctx, a, connection.Ref, "personal-email-password-a"); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("old credential receipt confirmed revoked owner", err)
	}
	a.Permission = "platform.command.integrations.enable"
	disabled, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: a,
		Mutation: value.Mutation{IdempotencyKey: "personal-email-disable-a", ExpectedVersion: &connection.Version}, Payload: command.ConnectionInput{Ref: connection.Ref, Enabled: false}})
	if err != nil || disabled.Connection == nil || disabled.Connection.Enabled {
		t.Fatal("disable personal mailbox before owner change", err)
	}
	connection = *disabled.Connection
	a.Permission = "platform.email-mailboxes.ownership.configure"
	if _, err := service.Execute(ctx, command.Command{Kind: command.ConfigureEmailMailboxOwnership, Principal: a,
		Mutation: value.Mutation{IdempotencyKey: "personal-email-no-service-switch", ExpectedVersion: &connection.Version}, Payload: command.EmailMailboxOwnershipInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialProjectShared, ServiceSubjectRef: "svc_fixture"}}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("self mailbox ACL selected service identity", err)
	}
	restored, err := service.Execute(ctx, command.Command{Kind: command.ConfigureEmailMailboxOwnership, Principal: a,
		Mutation: value.Mutation{IdempotencyKey: "personal-email-restore-a", ExpectedVersion: &connection.Version}, Payload: command.EmailMailboxOwnershipInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialInitiatorRequired}})
	if !errors.Is(err, errs.ErrConflict) || restored.EmailMailboxOwnership != nil {
		t.Fatal("owner configuration bypassed a started UNKNOWN mailbox effect", err)
	}
	b.Permission = "platform.email-mailboxes.ownership.revoke"
	other := *second.Connection
	closed, err := service.Execute(ctx, command.Command{Kind: command.RevokeEmailMailboxOwnership, Principal: b,
		Mutation: value.Mutation{IdempotencyKey: "personal-email-revoke-b", ExpectedVersion: &other.Version}, Payload: command.EmailMailboxOwnershipInput{ConnectionRef: other.Ref}})
	if err != nil || closed.EmailMailboxOwnership == nil {
		t.Fatal("revoke idle second user's mailbox", err)
	}
	other.Version = closed.EmailMailboxOwnership.ConnectionVersion
	b.Permission = "platform.command.integrations.enable"
	idle, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: b,
		Mutation: value.Mutation{IdempotencyKey: "personal-email-disable-b", ExpectedVersion: &other.Version}, Payload: command.ConnectionInput{Ref: other.Ref, Enabled: false}})
	if err != nil || idle.Connection == nil {
		t.Fatal("disable idle second user's mailbox", err)
	}
	other = *idle.Connection
	b.Permission = "platform.email-mailboxes.ownership.configure"
	restored, err = service.Execute(ctx, command.Command{Kind: command.ConfigureEmailMailboxOwnership, Principal: b,
		Mutation: value.Mutation{IdempotencyKey: "personal-email-restore-b", ExpectedVersion: &other.Version}, Payload: command.EmailMailboxOwnershipInput{ConnectionRef: other.Ref, Policy: entity.IntegrationCredentialInitiatorRequired}})
	if err != nil || restored.EmailMailboxOwnership == nil || restored.EmailMailboxOwnership.Generation != 3 || !restored.EmailMailboxOwnership.OwnerEligible {
		t.Fatal("restore idle own mailbox with fresh generation", err)
	}
}
