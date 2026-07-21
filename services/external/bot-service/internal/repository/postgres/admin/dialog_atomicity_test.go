//go:build postgres

package admin_test

import (
	"context"
	"errors"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDialogValidationRollbackKeepsCapabilityRetryableAcrossRace(t *testing.T) {
	dsn := isolatedPostgresTestDSN(t, "dialog_atomic")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("migrate dialog atomicity schema: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open dialog atomicity pool: %v", err)
	}
	defer pool.Close()
	repository := postgresrepo.NewRepository(pool)
	now := time.Now().UTC()
	tokenHash := make([]byte, 32)
	contextHash := make([]byte, 32)
	for index := range tokenHash {
		tokenHash[index] = byte(index + 1)
		contextHash[index] = byte(32 - index)
	}
	if err := repository.IssueInteractionCapability(ctx, securityrepo.IssueCapabilityInput{
		TokenHash: tokenHash, Kind: "dialog", Operation: "repository.upsert",
		ResourceType: "repository", ResourceID: "new", ChannelID: "channel",
		PostBinding: "post", ActorUserID: "owner-id", ActorUserName: "owner",
		InstallationScope: "single-installation", WorkspaceScope: "installation-root",
		ContextHash: contextHash, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		State: securityrepo.CapabilityStateUnused,
	}); err != nil {
		t.Fatalf("issue dialog capability: %v", err)
	}
	consume := securityrepo.ConsumeCapabilityInput{
		TokenHash: tokenHash, Kind: "dialog", Operation: "repository.upsert",
		ResourceType: "repository", ResourceID: "new", ChannelID: "channel",
		PostBinding: "post", ActorUserID: "owner-id", ContextHash: contextHash, Now: now.Add(time.Minute),
	}
	validationFailure := errors.New("synthetic correctable validation failure")
	invalidStarted := make(chan struct{})
	releaseInvalid := make(chan struct{})
	invalidResult := make(chan error, 1)
	go func() {
		_, atomicErr := repository.ConsumeInteractionCapabilityWithMutation(ctx, consume, func(adminrepo.Repository) error {
			close(invalidStarted)
			<-releaseInvalid
			return validationFailure
		})
		invalidResult <- atomicErr
	}()
	<-invalidStarted
	correctedResult := make(chan error, 1)
	go func() {
		_, atomicErr := repository.ConsumeInteractionCapabilityWithMutation(ctx, consume, func(store adminrepo.Repository) error {
			_, _, mutationErr := store.UpsertProject(ctx, adminrepo.UpsertProjectInput{
				Name: "Corrected dialog", Slug: "corrected-dialog", AdvancedSettings: "{}",
			})
			return mutationErr
		})
		correctedResult <- atomicErr
	}()
	select {
	case err := <-correctedResult:
		t.Fatalf("corrected retry bypassed in-flight capability lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseInvalid)
	if err := <-invalidResult; !errors.Is(err, validationFailure) {
		t.Fatalf("invalid dialog error = %v", err)
	}
	if err := <-correctedResult; err != nil {
		t.Fatalf("corrected retry error = %v", err)
	}
	if _, err := repository.GetProjectBySlug(ctx, "corrected-dialog"); err != nil {
		t.Fatalf("corrected business mutation missing: %v", err)
	}
	if _, err := repository.ConsumeInteractionCapabilityWithMutation(ctx, consume, func(adminrepo.Repository) error {
		return nil
	}); !errors.Is(err, securityrepo.ErrCapabilityConsumed) {
		t.Fatalf("successful dialog replay error = %v", err)
	}
}

func TestConsumedCapabilityReplayRequiresExactBindingPostgres(t *testing.T) {
	dsn := isolatedPostgresTestDSN(t, "consumed_capability_replay")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("migrate consumed replay schema: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open consumed replay pool: %v", err)
	}
	defer pool.Close()
	repository := postgresrepo.NewRepository(pool)
	now := time.Now().UTC()
	tokenHash := make([]byte, 32)
	contextHash := make([]byte, 32)
	for index := range tokenHash {
		tokenHash[index] = byte(index + 33)
		contextHash[index] = byte(index + 65)
	}
	if err := repository.IssueInteractionCapability(ctx, securityrepo.IssueCapabilityInput{
		TokenHash: tokenHash, Kind: "action", Operation: "action;kind=agent_turn;action=stop_turn",
		ResourceType: "agent_session_turn", ResourceID: "42", ChannelID: "channel",
		PostBinding: "post", ActorUserID: "owner-id", ActorUserName: "owner",
		InstallationScope: "single-installation", WorkspaceScope: "7", SessionScope: "session-7",
		ContextHash: contextHash, IssuedAt: now, ExpiresAt: now.Add(time.Hour), State: securityrepo.CapabilityStateUnused,
	}); err != nil {
		t.Fatalf("issue stop capability: %v", err)
	}
	input := securityrepo.ConsumeCapabilityInput{
		TokenHash: tokenHash, Kind: "action", Operation: "action;kind=agent_turn;action=stop_turn",
		ResourceType: "agent_session_turn", ResourceID: "42", ChannelID: "channel",
		PostBinding: "post", ActorUserID: "owner-id", ContextHash: contextHash, Now: now.Add(time.Minute),
	}
	if _, err := repository.ConsumeInteractionCapabilityWithMutation(ctx, input, func(adminrepo.Repository) error { return nil }); err != nil {
		t.Fatalf("consume stop capability: %v", err)
	}
	capability, err := repository.ReplayConsumedInteractionCapabilityWithMutation(ctx, input, func(capability securityrepo.Capability, _ adminrepo.Repository) error {
		if capability.WorkspaceScope != "7" || capability.SessionScope != "session-7" || capability.ActorUserID != "owner-id" {
			t.Fatalf("replay capability=%#v", capability)
		}
		return nil
	})
	if err != nil || capability.State != securityrepo.CapabilityStateConsumed {
		t.Fatalf("exact consumed replay capability=%#v error=%v", capability, err)
	}
	for _, changed := range []securityrepo.ConsumeCapabilityInput{
		func() securityrepo.ConsumeCapabilityInput {
			value := input
			value.ActorUserID = "other-user"
			return value
		}(),
		func() securityrepo.ConsumeCapabilityInput { value := input; value.ResourceID = "43"; return value }(),
		func() securityrepo.ConsumeCapabilityInput {
			value := input
			value.ContextHash = append([]byte(nil), contextHash...)
			value.ContextHash[0]++
			return value
		}(),
	} {
		called := false
		if _, err := repository.ReplayConsumedInteractionCapabilityWithMutation(ctx, changed, func(securityrepo.Capability, adminrepo.Repository) error {
			called = true
			return nil
		}); !errors.Is(err, securityrepo.ErrCapabilityBinding) || called {
			t.Fatalf("changed replay error=%v called=%t", err, called)
		}
	}
}
