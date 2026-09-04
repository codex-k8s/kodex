package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testInteractionIdentity(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.interaction-identities.bind",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	const connectionRef = "interaction_identity_connection"
	_, err = pool.Exec(ctx, `INSERT INTO control_plane.integration_connections
(ref,organization_id,definition_key,name,state,masked_credentials_state,created_by,definition_version,definition_digest,public_configuration)
SELECT $1,$2::uuid,'mattermost','Identity component connection','CONNECTED','CONFIGURED',$3::uuid,definition_version,digest,
'{"base_url":"https://mattermost.example.test","team_name":"test-team","channel_name":"test-channel"}'::jsonb
FROM control_plane.integration_definitions WHERE stable_key='mattermost'`, connectionRef, current.organizationID, current.actorID)
	if err != nil {
		t.Fatal(err)
	}
	version := int64(1)
	bind := command.Command{Kind: command.BindInteractionIdentity, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "identity-bind", ExpectedVersion: &version},
		Payload: command.InteractionIdentityInput{ConnectionRef: connectionRef, SubjectRef: current.actorRef, ExternalTeamRef: "team-ref", ExternalChannelRef: "channel-ref", ExternalUserDigest: strings.Repeat("b", 64)}}
	created, err := service.Execute(ctx, bind)
	if err != nil || created.InteractionIdentity == nil {
		t.Fatalf("bind identity: %v", err)
	}
	replayed, err := service.Execute(ctx, bind)
	if err != nil || replayed.InteractionIdentity.Ref != created.InteractionIdentity.Ref {
		t.Fatalf("identity replay: %v", err)
	}
	items, next, err := service.ListInteractionIdentities(ctx, owner, connectionRef, query.Page{Size: 1})
	if err != nil || len(items) != 1 || next != "" {
		t.Fatalf("identity list: %v", err)
	}
	message := command.InteractionMessageInput{ConnectionRef: connectionRef, ExternalTeamRef: "team-ref", ExternalChannelRef: "channel-ref", ExternalUserDigest: strings.Repeat("b", 64)}
	resolve := func(input command.InteractionMessageInput) (scope, error) {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		return repository.resolveInteractionIdentity(ctx, tx, current, input)
	}
	human, err := resolve(message)
	if err != nil || human.actorID != current.actorID || human.interactionIdentityID == "" || !human.credentialAuthenticatedAt.IsZero() {
		t.Fatalf("mapped identity: %v", err)
	}
	testInteractionACK(t, ctx, repository, pool, owner, connectionRef)
	for _, field := range []string{"team", "channel", "user", "connection"} {
		other := message
		switch field {
		case "team":
			other.ExternalTeamRef = "other"
		case "channel":
			other.ExternalChannelRef = "other"
		case "user":
			other.ExternalUserDigest = strings.Repeat("c", 64)
		case "connection":
			other.ConnectionRef = "other"
		}
		if _, err := resolve(other); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("cross-%s identity: %v", field, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET version=version+1 WHERE ref=$1`, connectionRef); err != nil {
		t.Fatal(err)
	}
	sources, err := service.ListInteractionSources(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	foundSource := false
	for _, source := range sources {
		if stringMap(source, "connectionRef") == connectionRef {
			foundSource = true
			if source["connectionVersion"] != int64(2) || stringMap(source, "credentialRef") != "ack_fixture_credential" {
				t.Fatal("source version did not change with a stable credential reference")
			}
		}
	}
	if !foundSource {
		t.Fatal("interaction source not projected")
	}
	if _, err := resolve(message); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("changed connection retained identity authority: %v", err)
	}
	stale := created.InteractionIdentity.Version + 1
	revoke := command.Command{Kind: command.RevokeInteractionIdentity, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "identity-revoke-stale", ExpectedVersion: &stale}, Payload: command.InteractionIdentityInput{IdentityRef: created.InteractionIdentity.Ref}}
	if _, err := service.Execute(ctx, revoke); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale revoke: %v", err)
	}
	revoke.Mutation.IdempotencyKey = "identity-revoke"
	revoke.Mutation.ExpectedVersion = &created.InteractionIdentity.Version
	revoked, err := service.Execute(ctx, revoke)
	if err != nil || revoked.InteractionIdentity.State != "REVOKED" {
		t.Fatalf("revoke identity: %v", err)
	}
	var memberRef, gateRef string
	err = pool.QueryRow(ctx, `INSERT INTO control_plane.subjects
(organization_id,ref,issuer,external_subject_digest,display_name)
VALUES ($1::uuid,'usr_interaction_member','verified-oidc-subject',repeat('e',64),'Interaction member') RETURNING ref`, current.organizationID).Scan(&memberRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.AddPlatformMembership, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "interaction-member-add"}, Payload: command.PlatformMembershipInput{UserRef: memberRef, Role: "MEMBER", Active: true}}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT ref FROM control_plane.owner_gates WHERE organization_id=$1::uuid LIMIT 1`, current.organizationID).Scan(&gateRef); err != nil {
		t.Fatal(err)
	}
	version = 2
	bind.Mutation.IdempotencyKey = "identity-bind-member"
	payload := bind.Payload.(command.InteractionIdentityInput)
	payload.SubjectRef = memberRef
	bind.Payload = payload
	if _, err := service.Execute(ctx, bind); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	human, err = repository.resolveInteractionIdentity(ctx, tx, current, message)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.requireAccess(ctx, tx, human, "gate.resolve", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "OWNER_GATE", ResourceRef: gateRef}); err == nil {
		t.Fatal("mapped member acquired gateway gate authority")
	}
}
