package platform

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

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
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-mailbox-connection"}, Payload: command.ConnectionInput{
			DefinitionKey: "email", Name: "Email configuration fixture", PublicConfiguration: map[string]any{
				"base_url": "https://email-bridge.kodex-system.svc.cluster.local", "mailbox_id": config.Mailboxes[0].Id,
				"from_address": config.Mailboxes[0].Sender,
			},
		}})
	if err != nil || connection.Connection == nil {
		t.Fatalf("create email connection: %v", err)
	}
	config.Mailboxes[0].TenantId, config.Mailboxes[0].ConnectionId = resolved.AuthorityTenant, connection.Connection.Ref
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
		mailbox, err := reader.readEmailMailbox(ctx, tx, scope{organizationID: owner.AuthorityTenant}, "example-mailbox", revision)
		if denied && !errors.Is(err, errs.ErrForbidden) || !denied && (err != nil || mailbox.ConnectionRef != connection.Connection.Ref) {
			t.Fatalf("read mailbox revision %d: %v", revision, err)
		}
	}
	read(repository, 1, true)
	accept(false)
	accept(false)
	read(repository, 1, false)
	oldReader := *repository
	config.Mailboxes[0].Folder = "Archive"
	accept(true)
	config.Revision = 2
	accept(true)
	read(repository, 1, false)
	config.Mailboxes[0].Revision = 2
	config.Mailboxes[0].CredentialGeneration = 2
	accept(false)
	read(&oldReader, 1, true)
	read(repository, 1, true)
	read(repository, 2, false)
	config.Revision = 1
	accept(true)
	config.Revision = 3
	config.Mailboxes[0].Revision = 3
	config.Mailboxes[0].CredentialGeneration = 1
	accept(true)
	config.Mailboxes[0].CredentialGeneration = 2
	config.Mailboxes[0].Smtp.Secret.Generation++
	config.Mailboxes[0].Revision = 2
	accept(true)
	config.Mailboxes[0].Revision = 3
	accept(false)
	mailbox := config.Mailboxes[0]
	config.Revision = 4
	config.Mailboxes = []api.Mailbox{}
	accept(false)
	read(repository, 3, true)
	config.Revision = 5
	config.Mailboxes = []api.Mailbox{mailbox}
	accept(true)
	config.Mailboxes[0].Revision = 4
	accept(false)
	read(repository, 4, false)
	config.Revision = 6
	config.Mailboxes[0].Revision = 5
	config.Mailboxes[0].ConnectionId = "unknown-connection"
	accept(true)
	read(repository, 4, false)
	config.Revision = 5
	config.Mailboxes[0].Revision = 4
	config.Mailboxes[0].ConnectionId = connection.Connection.Ref
	t.Run("authorization and report owner lifecycle", func(t *testing.T) {
		testEmailProducer(t, ctx, repository, service, owner, *connection.Connection, config)
	})
}
