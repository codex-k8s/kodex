package platform

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// personalEmailHealthFixture retains exact owner-issued READ test authority for a later revoke
// assertion. No SMTP/IMAP request, bridge process or successful remote health probe is performed.
// personalEmailHealthFixture сохраняет точные READ test authority владельца для последующей
// проверки отзыва. SMTP/IMAP request, bridge process или успешный remote health probe не выполняются.
type personalEmailHealthFixture struct {
	email, gateway value.Principal
	authorization  query.EmailAuthorization
	completion     command.IntegrationConnectionTestInput
}

// personalEmailFixtureCA creates an ephemeral synthetic CA accepted by the real credential
// validator; its private key remains local and is never stored in mailbox configuration.
// personalEmailFixtureCA создаёт ephemeral synthetic CA для настоящего credential validator;
// private key остаётся локальным и не сохраняется в mailbox configuration.
func personalEmailFixtureCA(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(2168), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// personalEmailPublishedHealthFixture follows the real draft/validate/publish/bind and durable
// publication owner path, including pre-apply/readback denials. Existing synthetic DNS/policy and
// callback fixtures replace remote Kubernetes/bridge effects. A first READ test finishes; the
// second remains claimed so its actual owner lease can be rejected after revoke.
// personalEmailPublishedHealthFixture проходит реальные draft/validate/publish/bind и durable
// publication owner path, включая отказы до apply/readback. Существующие synthetic DNS/policy и
// callback fixtures заменяют remote Kubernetes/bridge effects. Первый READ test завершается;
// второй остаётся claimed для отказа его настоящей owner lease после revoke.
func personalEmailPublishedHealthFixture(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, admin, owner value.Principal, connection entity.IntegrationConnection, secret entity.EmailMailboxCredential, deadlineTrace *emailAuthorizationDeadlineTrace, keepClaimedHealth bool, afterConnected func(entity.IntegrationConnection)) (entity.IntegrationConnection, personalEmailHealthFixture) {
	t.Helper()
	owner.Permission = "platform.command.email-mailbox.configure-credential"
	username, err := service.ConfigureEmailMailboxCredential(ctx, owner, value.Mutation{IdempotencyKey: "personal-email-username-a", ExpectedVersion: &connection.Version}, connection.Ref, "USERNAME", []byte("a@example.test"))
	if err != nil {
		t.Fatal("configure personal mailbox username", err)
	}
	connection.Version = username.ConnectionVersion
	ca, err := service.ConfigureEmailMailboxCredential(ctx, owner, value.Mutation{IdempotencyKey: "personal-email-ca-a", ExpectedVersion: &connection.Version}, connection.Ref, "CA_CERTIFICATE", personalEmailFixtureCA(t))
	if err != nil {
		t.Fatal("configure personal mailbox CA", err)
	}
	connection.Version = ca.ConnectionVersion
	raw, err := os.ReadFile("../../../../../../../contracts/email-bridge/v1/examples/mailboxes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var fixture api.Configuration
	if err := api.Decode(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	mailbox := fixture.Mailboxes[0]
	mailbox.Smtp.Host, mailbox.Smtp.ServerName, mailbox.Smtp.Port, mailbox.Smtp.TlsMode = "smtp.gmail.com", "smtp.gmail.com", 465, "implicit"
	mailbox.Imap.Host, mailbox.Imap.ServerName = "imap.gmail.com", "imap.gmail.com"
	descriptor := func(credential entity.EmailMailboxCredential) api.Descriptor {
		return api.Descriptor{Name: credential.Name, Generation: credential.Generation}
	}
	for _, endpoint := range []*api.Endpoint{&mailbox.Smtp, mailbox.Imap} {
		endpoint.Ca, endpoint.Username, endpoint.Secret = descriptor(ca), descriptor(username), descriptor(secret)
	}
	spec := entity.EmailMailboxSpecification{ProviderProfile: "GMAIL_APP_PASSWORD", Enabled: true, ReceiveProtocol: "imap",
		AllowedFolders: mailbox.AllowedFolders, ArchiveFolder: mailbox.ArchiveFolder, DraftsFolder: mailbox.DraftsFolder,
		Folder: mailbox.Folder, Sender: "a@example.test", ReplyTo: "a@example.test", Recipients: mailbox.Recipients,
		HelloName: mailbox.HelloName, SMTP: mailbox.Smtp, IMAP: mailbox.Imap, Limits: mailbox.Limits,
		Policies: slices.DeleteFunc(slices.Clone(mailbox.Policies), func(policy api.OperationPolicy) bool { return policy.Operation == api.OperationMark })}
	for index := range spec.Policies {
		switch spec.Policies[index].Operation {
		case "mark_read", "mark_unread", "move", "archive", "draft_create", "draft_update", "draft_delete":
			spec.Policies[index].Policy = api.Allow
		}
	}
	content, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateEmailMailboxDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-email-draft-a"}, Payload: command.EmailMailboxInput{ConnectionRef: connection.Ref,
			Managed: command.ManagedConfigurationInput{Name: "Личный Gmail app password", ContentFormat: "JSON", Content: string(content)}}})
	if err != nil || created.EmailMailbox == nil {
		t.Fatal("create personal mailbox draft", err)
	}
	view := created.EmailMailbox
	for _, phase := range []struct {
		kind command.Kind
		key  string
	}{{command.ValidateEmailMailboxDraft, "personal-email-validate-a"}, {command.PublishEmailMailboxDraft, "personal-email-publish-a"}} {
		result, err := service.Execute(ctx, command.Command{Kind: phase.kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: phase.key, ExpectedVersion: &view.Configuration.Version},
			Payload: command.EmailMailboxInput{Managed: command.ManagedConfigurationInput{ConfigurationRef: view.Configuration.Ref, RevisionRef: view.Revision.Ref}}})
		if err != nil || result.EmailMailbox == nil || result.EmailMailbox.Revision.State == "INVALID" {
			t.Fatal("advance personal mailbox draft", err)
		}
		view = result.EmailMailbox
	}
	bound, err := service.Execute(ctx, command.Command{Kind: command.BindEmailMailboxConfiguration, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-email-bind-a", ExpectedVersion: &view.Configuration.Version}, Payload: command.EmailMailboxInput{
			ConnectionRef: connection.Ref, ExpectedConnectionVersion: view.ConnectionVersion, Managed: command.ManagedConfigurationInput{ConfigurationRef: view.Configuration.Ref, RevisionRef: view.Revision.Ref}}})
	if err != nil || bound.EmailPublication == nil || bound.EmailPublication.State != "PENDING" {
		t.Fatal("bind personal mailbox with pending publication", err)
	}
	work, found, err := r.ClaimEmailMailboxPublication(ctx, "personal-email-development-publisher")
	if err != nil || !found || work.Ref != bound.EmailPublication.Ref || len(work.Configuration.Mailboxes) != 1 || work.Configuration.Mailboxes[0].CredentialOwnerGeneration != 1 {
		t.Fatal("claim exact owner-pinned personal mailbox publication", err)
	}
	email := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "email-bridge", Operation: "platform.email.configuration.report"}, "email-bridge")
	if err := service.ReportEmailConfigurationReadback(ctx, email, work.Configuration.Revision, api.Digest(work.Configuration)); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("personal publication accepted readback before apply", err)
	}
	policy, err := mailpolicy.Produce(ctx, work.Configuration, strings.Repeat("a", 64), mailboxOwnerResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.StageEmailMailboxPolicy(ctx, work, policy); err != nil {
		t.Fatal("stage personal mailbox policy", err)
	}
	if err := r.MarkEmailMailboxApplied(ctx, work); err != nil {
		t.Fatal("record personal mailbox synthetic apply", err)
	}
	if err := r.CompleteEmailMailboxPublication(ctx, work); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("personal publication READY before readback", err)
	}
	if err := service.ReportEmailConfigurationReadback(ctx, email, work.Configuration.Revision, api.Digest(work.Configuration)); err != nil {
		t.Fatal("record exact personal mailbox readback", err)
	}
	if err := r.CompleteEmailMailboxPublication(ctx, work); err != nil {
		t.Fatal("finish personal mailbox publication", err)
	}
	gateway := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integration-tests.claim"}, "integration-gateway")
	email.Permission = "platform.email.authorization.resolve"
	owner.Permission = "platform.command.integrations.test"
	var result personalEmailHealthFixture
	for _, key := range []string{"personal-email-health-a", "personal-email-health-revoke"} {
		connection, err = service.GetIntegrationConnection(ctx, owner, connection.Ref)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &connection.Version}, Payload: command.ConnectionInput{Ref: connection.Ref}}); err != nil {
			t.Fatal("queue published personal health", err)
		}
		claims, err := service.ClaimIntegrationConnectionTests(ctx, gateway, key, 32)
		if err != nil {
			t.Fatal("claim personal health", err)
		}
		var claim map[string]any
		for _, candidate := range claims {
			if stringMap(candidate, "connectionRef") == connection.Ref {
				claim = candidate
			}
		}
		if claim == nil || claim["credential"] != nil {
			t.Fatal("personal health missing or contains generic token")
		}
		testEmailGatewayClaim(t, claim, "health")
		binding := entity.EmailExecutionBinding{ConnectionTestRef: stringMap(claim, "testRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64), ExpiresAt: claim["expiresAt"].(time.Time)}
		mailbox = work.Configuration.Mailboxes[0]
		semantic, err := api.CommandForIntegration("email.delivery.health.read", mailbox.Id, mailbox.Sender, "", []byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		input := query.EmailAuthorization{Binding: binding, MailboxRef: mailbox.Id, ConfigurationRevision: mailbox.Revision, Operation: "health", Sender: mailbox.Sender, Folder: mailbox.Folder, SemanticInputDigest: api.Digest(semantic)}
		decision, err := service.ResolveEmailAuthorization(ctx, email, input)
		if err != nil || !decision.Allowed || decision.CredentialGeneration != mailbox.CredentialGeneration {
			t.Fatal("authorize current personal mailbox health", err)
		}
		completion := command.IntegrationConnectionTestInput{TestRef: binding.ConnectionTestRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, Success: true}
		result = personalEmailHealthFixture{email: email, gateway: gateway, authorization: input, completion: completion}
		if key == "personal-email-health-a" {
			if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: key + "-complete"}, Payload: completion}); err != nil {
				t.Fatal("complete synthetic personal health observation", err)
			}
			connected, err := service.GetIntegrationConnection(ctx, owner, connection.Ref)
			if err != nil || connected.State != "CONNECTED" {
				t.Fatal("personal health did not establish connected owner state", err)
			}
			connected = personalEmailFiniteAuthorizationFixture(t, ctx, r, service, admin, owner, gateway, email, connected, mailbox, deadlineTrace)
			afterConnected(connected)
			if !keepClaimedHealth {
				break
			}
		}
	}
	connection, err = service.GetIntegrationConnection(ctx, owner, connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	return connection, result
}
