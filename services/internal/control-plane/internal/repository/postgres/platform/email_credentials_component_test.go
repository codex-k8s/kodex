package platform

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"strconv"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/credentialmaterializer"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/emailprojection"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func testEmailCredentials(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.email-mailbox.configure-credential",
	}, "control-api-gateway")
	client := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "kodex-system", Name: emailprojection.SecretName, UID: "email-credential-fixture", ResourceVersion: "1"}, Data: map[string][]byte{}})
	materializer, err := credentialmaterializer.New(client, "kodex-system", emailprojection.SecretName)
	if err != nil {
		t.Fatal(err)
	}
	service, err := platformservice.New(repository, platformservice.WithEmailCredentialMaterializer(materializer))
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "email-credential-connection"}, Payload: command.ConnectionInput{
		DefinitionKey: "email", Name: "Email credential fixture", PublicConfiguration: map[string]any{"base_url": "https://email-bridge.kodex-system.svc.cluster.local", "mailbox_id": "credential-fixture", "from_address": "sender@example.test"},
	}})
	if err != nil || created.Connection == nil {
		t.Fatalf("create credential owner: %v", err)
	}
	connection := created.Connection
	version := connection.Version
	original := value.Mutation{IdempotencyKey: "email-password-first", ExpectedVersion: &version}
	first, err := service.ConfigureEmailMailboxCredential(ctx, owner, original, connection.Ref, "AUTH_SECRET", []byte("  synthetic secret  "))
	if err != nil || first.ConnectionVersion != version+1 || first.Generation != version+1 {
		t.Fatalf("configure email credential: %v", err)
	}
	client.ClearActions()
	replayed, err := service.ConfigureEmailMailboxCredential(ctx, owner, original, connection.Ref, "AUTH_SECRET", []byte("  synthetic secret  "))
	if err != nil || replayed.Name != first.Name || replayed.Generation != first.Generation || len(client.Actions()) != 0 {
		t.Fatalf("exact replay rematerialized: %v", err)
	}
	if _, err := service.ConfigureEmailMailboxCredential(ctx, owner, original, connection.Ref, "AUTH_SECRET", []byte("changed secret")); !errors.Is(err, errs.ErrIdempotencyReuse) {
		t.Fatalf("different secret replay: %v", err)
	}
	stale := value.Mutation{IdempotencyKey: "email-stale-new-command", ExpectedVersion: &version}
	if _, err := service.ConfigureEmailMailboxCredential(ctx, owner, stale, connection.Ref, "AUTH_SECRET", []byte("unused secret")); !errors.Is(err, errs.ErrVersionMismatch) || len(client.Actions()) != 0 {
		t.Fatalf("stale command wrote credential: %v", err)
	}
	version = first.ConnectionVersion
	second, err := service.ConfigureEmailMailboxCredential(ctx, owner, value.Mutation{IdempotencyKey: "email-password-next", ExpectedVersion: &version}, connection.Ref, "AUTH_SECRET", []byte("next synthetic secret"))
	if err != nil || second.Name == first.Name || second.Generation <= first.Generation {
		t.Fatalf("immutable replacement: %v", err)
	}
	secret, err := client.CoreV1().Secrets("kodex-system").Get(ctx, emailprojection.SecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	firstKey := first.Name + "." + strconv.FormatInt(first.Generation, 10)
	if string(secret.Data[firstKey]) != "  synthetic secret  " {
		t.Fatal("old generation changed or password trimmed")
	}
	version = second.ConnectionVersion
	username, err := service.ConfigureEmailMailboxCredential(ctx, owner, value.Mutation{IdempotencyKey: "email-username-first", ExpectedVersion: &version}, connection.Ref, "USERNAME", []byte("user@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	version = username.ConnectionVersion
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := service.ConfigureEmailMailboxCredential(ctx, owner, value.Mutation{IdempotencyKey: "email-ca-first", ExpectedVersion: &version}, connection.Ref, "CA_CERTIFICATE", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../../../../../contracts/email-bridge/v1/examples/mailboxes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var configuration api.Configuration
	if api.Decode(raw, &configuration) != nil {
		t.Fatal("decode mailbox fixture")
	}
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	mailbox := &configuration.Mailboxes[0]
	mailbox.TenantId, mailbox.ConnectionId = resolved.AuthorityTenant, connection.Ref
	descriptor := func(credential entity.EmailMailboxCredential) api.Descriptor {
		return api.Descriptor{Name: credential.Name, Generation: credential.Generation}
	}
	for _, endpoint := range []*api.Endpoint{&mailbox.Smtp, mailbox.Imap, mailbox.Pop} {
		if endpoint != nil {
			endpoint.Ca, endpoint.Username, endpoint.Secret = descriptor(ca), descriptor(username), descriptor(second)
		}
	}
	pins, err := repository.EmailCredentialDigests(ctx, configuration)
	if err != nil || len(pins) != 3 {
		t.Fatalf("credential owner pins: %v", err)
	}
	mailbox.Smtp.Secret = descriptor(username)
	if _, err := repository.EmailCredentialDigests(ctx, configuration); err == nil {
		t.Fatal("credential kind substitution accepted")
	}
	mailbox.Smtp.Secret = descriptor(second)
	mailbox.ConnectionId = "intconn_foreign"
	if _, err := repository.EmailCredentialDigests(ctx, configuration); err == nil {
		t.Fatal("cross connection credential accepted")
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "email-credential-reader-project"}, Payload: command.ProjectInput{Name: "Email credential reader"}})
	if err != nil || project.Project == nil {
		t.Fatalf("reader project: %v", err)
	}
	reader := contextProjectReader(t, ctx, repository, service, owner, project.Project.Ref, "EMAILCREDS")
	version = ca.ConnectionVersion
	client.ClearActions()
	if _, err := service.ConfigureEmailMailboxCredential(ctx, reader, value.Mutation{IdempotencyKey: "email-denied-credential", ExpectedVersion: &version}, connection.Ref, "AUTH_SECRET", []byte("denied fixture")); err == nil || len(client.Actions()) != 0 {
		t.Fatalf("credential materialized without permission: %v", err)
	}
}
