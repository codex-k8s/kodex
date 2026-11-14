package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// testTelegramManagedHealthOnlyComponent publishes and binds a real narrowed owner revision, completes its exact health ledger, and proves a healthy service credential cannot restore its removed notification capability.
// testTelegramManagedHealthOnlyComponent публикует и привязывает настоящее owner сужение, завершает точный health ledger и доказывает, что исправный service credential не восстанавливает удалённую notification capability.
func testTelegramManagedHealthOnlyComponent(t *testing.T, ctx context.Context, r *Repository, service *serviceplatform.Service, projectRef string) {
	t.Helper()
	connection := telegramCreateFixtureConnection(t, ctx, r, service, map[string]any{"base_url": integrationpackage.TelegramAPIOrigin, "bot_id": float64(123456), "chat_id": "-1001234", "topic": "77", "quiet_enabled": false}, "telegram-managed-health")
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["telegram"]
	health, exists := definition.Capability("telegram.connection.read")
	if !exists {
		t.Fatal("shipped health capability missing")
	}
	definition.Metadata.Version = "2.0.0"
	definition.Spec.Name = "Служебный бот только с проверкой"
	definition.Spec.Capabilities = []integrationpackage.Capability{health}
	input := command.ManagedConfigurationInput{Name: definition.Spec.Name, ContentFormat: "JSON", Content: string(asJSON(definition))}
	var published command.Result
	for _, step := range []struct {
		kind                  command.Kind
		operation, key, state string
	}{
		{kind: command.CreateIntegrationDefinition, operation: "platform.command.integration-definitions.create-draft", key: "create", state: "DRAFT"},
		{kind: command.ValidateIntegrationDefinition, operation: "platform.command.integration-definitions.validate", key: "validate", state: "VALID"},
		{kind: command.PublishIntegrationDefinition, operation: "platform.command.integration-definitions.publish", key: "publish", state: "PUBLISHED"},
	} {
		p := telegramFixturePrincipal(t, ctx, r, step.operation, "", false)
		mutation := value.Mutation{IdempotencyKey: "telegram-managed-health-" + step.key}
		if published.ManagedConfiguration != nil {
			mutation.ExpectedVersion = &published.ManagedConfiguration.Version
			input = command.ManagedConfigurationInput{ConfigurationRef: published.ManagedConfiguration.Ref, RevisionRef: published.ManagedRevision.Ref}
		}
		published, err = service.Execute(ctx, command.Command{Kind: step.kind, Principal: p, Mutation: mutation, Payload: input})
		if err != nil || published.ManagedConfiguration == nil || published.ManagedRevision == nil || published.ManagedRevision.State != step.state {
			t.Fatal("publish narrowed Telegram definition", step.key, err)
		}
	}
	p := telegramFixturePrincipal(t, ctx, r, "platform.query.managed-configurations.impact.get", "", false)
	impact, err := service.GetManagedConfigurationImpact(ctx, p, published.ManagedConfiguration.Ref, published.ManagedRevision.Ref, query.Filter{})
	if err != nil || impact.Digest == "" {
		t.Fatal("read exact narrowed definition impact", err)
	}
	p = telegramFixturePrincipal(t, ctx, r, "platform.command.integration-definitions.rebind", "", false)
	if _, err := service.Execute(ctx, command.Command{Kind: command.RebindIntegrationDefinition, Principal: p, Mutation: value.Mutation{IdempotencyKey: "telegram-managed-health-rebind", ExpectedVersion: &published.ManagedConfiguration.Version}, Payload: command.ManagedConfigurationInput{
		ConfigurationRef: published.ManagedConfiguration.Ref, RevisionRef: published.ManagedRevision.Ref, ImpactDigest: impact.Digest, Consumers: []entity.ManagedConfigurationConsumer{{Kind: "INTEGRATION_CONNECTION", Ref: connection.Ref, ExpectedAbsent: true}}}}); err != nil {
		t.Fatal("bind exact health-only connection revision", err)
	}
	p = telegramFixturePrincipal(t, ctx, r, "platform.query.integration-connections.get", "", false)
	rebound, err := service.GetIntegrationConnection(ctx, p, connection.Ref)
	if err != nil || rebound.DefinitionDigest != published.ManagedRevision.Digest || rebound.State != "NOT_CONNECTED" || rebound.CredentialRevision != nil {
		t.Fatal("rebind retained obsolete health or credential pins", err)
	}
	p = telegramFixturePrincipal(t, ctx, r, "platform.command.integrations.configure-credential", "", false)
	configured, err := service.Execute(ctx, command.Command{Kind: command.ConfigureConnectionCredential, Principal: p, Mutation: value.Mutation{IdempotencyKey: "telegram-managed-health-rebound-credential", ExpectedVersion: &rebound.Version}, Payload: command.ConnectionInput{
		Ref: connection.Ref, MaterializationRef: "telegram-managed-health-rebound", CredentialRevision: &entity.IntegrationCredentialRevision{SecretRef: "kodex-system/kodex-integration-credentials#telegram-managed-health-rebound", SecretUID: "11111111-1111-4111-8111-111111111111", SecretResourceVersion: "2", ContentSHA256: strings.Repeat("e", 64)}}})
	if err != nil || configured.Connection == nil {
		t.Fatal("configure exact health-only service credential", err)
	}
	checked := telegramVerifyFixtureConnection(t, ctx, r, service, connection.Ref, configured.Connection.Version, "telegram-managed-health-rebound")
	if checked.State != "CONNECTED" || checked.DefinitionDigest != published.ManagedRevision.Digest {
		t.Fatal("health-only revision did not complete its real health path")
	}
	p = telegramFixturePrincipal(t, ctx, r, "platform.query.telegram-subscription.get", projectRef, false)
	current, canManage, err := service.GetTelegramSubscription(ctx, p, projectRef)
	if err != nil || current == nil || !canManage {
		t.Fatal("read exact current subscription before narrowed package refusal", err)
	}
	p = telegramFixturePrincipal(t, ctx, r, "platform.command.telegram-subscription.configure", projectRef, false)
	if _, err := service.ConfigureTelegramSubscription(ctx, p, value.Mutation{IdempotencyKey: "telegram-managed-health-cannot-notify", ExpectedVersion: &current.Version}, port.TelegramSubscriptionConfigure{ProjectRef: projectRef, ConnectionRef: connection.Ref, Enabled: true, EventTypes: []string{"FINISHED"}, DurationSeconds: 3600, MaxEffects: 1}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("healthy narrowed package restored a removed effect capability", err)
	}
}
