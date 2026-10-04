package platform

import (
	"context"
	_ "embed"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed testdata/sql/environment_initial_system_binding_fixture.sql
var queryEnvironmentInitialSystemBindingFixture string

//go:embed testdata/sql/environment_initial_system_binding_readback.sql
var queryEnvironmentInitialSystemBindingReadback string

func testInitialSystemEnvironmentPublication(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner value.Principal, scope scope, assistantRef string) {
	for _, scenario := range []string{"selected", "stale-binding", "not-selected"} {
		t.Run(scenario, func(t *testing.T) {
			prefix := "initial-system-" + scenario
			var changed int64
			if err := r.pool.QueryRow(ctx, queryEnvironmentInitialSystemBindingFixture, assistantRef, false).Scan(&changed); err != nil || changed != 1 {
				t.Fatalf("initial binding fixture: %v", err)
			}
			before, err := service.GetAgentRuntimeConfiguration(ctx, owner, assistantRef)
			if err != nil {
				t.Fatal(err)
			}
			operation := entity.AssistantPlanOperation{Type: "PREPARE_RUNTIME_ENVIRONMENT_REVISION", Key: prefix, Title: "Настроить собственное окружение", Summary: "Подготовить owner draft", Parameters: map[string]any{
				"environmentRef": before.Environment.Ref, "systemAssistantRef": assistantRef, "description": prefix,
			}}
			tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			operation, err = r.hydrateAssistantEnvironmentOperation(ctx, tx, scope, "", operation)
			_ = tx.Rollback(ctx)
			if err != nil {
				t.Fatal(err)
			}
			operation, err = normalizeAssistantOperation(operation)
			if err != nil {
				t.Fatal(err)
			}
			create, err := assistantOperationCommand(operation)
			if err != nil || create.Kind != command.CreateOrganizationRuntimeEnvironmentDraft {
				t.Fatalf("typed SYSTEM draft command: %v", err)
			}
			create.Principal, create.Mutation.IdempotencyKey = owner, prefix+"-create"
			created, err := service.Execute(ctx, create)
			if err != nil {
				t.Fatal(err)
			}
			invoke := func(kind command.Kind, key string, version *int64, input command.RuntimeEnvironmentDraftInput) (command.Result, error) {
				return service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + key, ExpectedVersion: version}, Payload: input})
			}
			draft := created.RuntimeEnvironmentDraft
			validated, err := invoke(command.ValidateRuntimeEnvironmentDraft, "-validate", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
			if err != nil || validated.RuntimeEnvironmentDraft.State != "VALID" {
				t.Fatalf("validate SYSTEM draft: %v", err)
			}
			draft = validated.RuntimeEnvironmentDraft
			prepared, err := invoke(command.PrepareEnvironmentDraftImpact, "-prepare", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
			if err != nil || prepared.RevisionImpactPlan.Total != 1 {
				t.Fatalf("initial SYSTEM consumer absent from impact: %v", err)
			}
			plan := prepared.RevisionImpactPlan
			page, err := service.GetRevisionImpactPlan(ctx, owner, plan.Ref, "", query.Page{Size: 100})
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("initial SYSTEM impact readback: %v", err)
			}
			item := page.Items[0]
			if item.ConsumerRef != assistantRef || item.SourceRevisionRef != before.Environment.CurrentVersion.Ref || item.BindingRef != before.EnvironmentBinding.Ref || item.BindingVersion != before.EnvironmentBinding.Version || item.ConsumerVersion != before.AgentVersion || item.ScopeKind != "ORGANIZATION" || item.ProjectRef != "" {
				t.Fatal("initial SYSTEM owner snapshot mismatch")
			}
			selected := []string{item.Ref}
			if scenario == "not-selected" {
				selected = nil
			}
			if scenario == "stale-binding" {
				if err := r.pool.QueryRow(ctx, queryEnvironmentInitialSystemBindingFixture, assistantRef, true).Scan(&changed); err != nil || changed != 1 {
					t.Fatalf("stale binding fixture: %v", err)
				}
			}
			payload := command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, PlanRef: plan.Ref, SelectedItemRefs: selected}
			if scenario == "selected" {
				func() {
					original := queryCommandsExecuteInsertAuditEventsRefProjectIdAction
					queryCommandsExecuteInsertAuditEventsRefProjectIdAction = queryRuntimeClaimAuditUnavailable
					defer func() { queryCommandsExecuteInsertAuditEventsRefProjectIdAction = original }()
					if _, err := invoke(command.PublishRuntimeEnvironmentDraft, "-publish", &draft.Version, payload); !errors.Is(err, errs.ErrUnavailable) {
						t.Fatalf("publication audit failure suppressed: %v", err)
					}
				}()
				rolledBack, err := service.GetAgentRuntimeConfiguration(ctx, owner, assistantRef)
				if err != nil || rolledBack.Environment.Version != before.Environment.Version || rolledBack.EnvironmentBinding != before.EnvironmentBinding || rolledBack.AgentVersion != before.AgentVersion {
					t.Fatalf("partial environment/binding publication committed: %v", err)
				}
			}
			published, err := invoke(command.PublishRuntimeEnvironmentDraft, "-publish", &draft.Version, payload)
			if err != nil {
				t.Fatalf("owner publication: %v", err)
			}
			after, err := service.GetAgentRuntimeConfiguration(ctx, owner, assistantRef)
			if err != nil || after.Environment.CurrentVersion.Ref != published.RuntimeEnvironment.CurrentVersion.Ref {
				t.Fatalf("fresh SYSTEM runtime configuration: %v", err)
			}
			if !reflect.DeepEqual(before.Configuration, after.Configuration) || !reflect.DeepEqual(before.Environment.CurrentVersion.Values, after.Environment.CurrentVersion.Values) || !reflect.DeepEqual(before.Environment.CurrentVersion.Tools, after.Environment.CurrentVersion.Tools) || !reflect.DeepEqual(before.Environment.CurrentVersion.SecretDescriptors, after.Environment.CurrentVersion.SecretDescriptors) || !reflect.DeepEqual(before.Environment.CurrentVersion.Policy, after.Environment.CurrentVersion.Policy) || before.Environment.CurrentVersion.Image != after.Environment.CurrentVersion.Image {
				t.Fatal("publication changed unrelated SYSTEM configuration")
			}
			page, err = service.GetRevisionImpactPlan(ctx, owner, plan.Ref, "", query.Page{Size: 100})
			if err != nil || len(page.Items) != 1 || page.Plan.State != "APPLIED" {
				t.Fatalf("publication impact receipt: %v", err)
			}
			var isNull bool
			if err := r.pool.QueryRow(ctx, queryEnvironmentInitialSystemBindingReadback, assistantRef).Scan(&isNull); err != nil {
				t.Fatal(err)
			}
			expected := "APPLIED"
			if scenario == "stale-binding" {
				expected = "CONFLICT"
			} else if scenario == "not-selected" {
				expected = "NOT_SELECTED"
			}
			result := page.Items[0]
			if result.Outcome != expected || isNull != (scenario != "selected") {
				t.Fatalf("initial SYSTEM outcome/pin: %s null=%t", result.Outcome, isNull)
			}
			if scenario == "selected" && (after.EnvironmentBinding.Version != before.EnvironmentBinding.Version+1 || after.AgentVersion != before.AgentVersion+1 || result.ResultRevisionRef != after.EnvironmentBinding.VersionRef || result.ResultBindingVersion != after.EnvironmentBinding.Version || result.ResultConsumerVersion != after.AgentVersion) {
				t.Fatal("selected SYSTEM explicit version pins mismatch")
			}
			replay, err := invoke(command.PublishRuntimeEnvironmentDraft, "-publish", &draft.Version, payload)
			if err != nil || replay.RevisionImpactPlan.Ref != plan.Ref || replay.RuntimeEnvironment.CurrentVersion.Ref != after.Environment.CurrentVersion.Ref {
				t.Fatalf("owner publication replay: %v", err)
			}
			replayed, err := service.GetAgentRuntimeConfiguration(ctx, owner, assistantRef)
			if err != nil || replayed.EnvironmentBinding != after.EnvironmentBinding || replayed.AgentVersion != after.AgentVersion {
				t.Fatalf("publication replay repeated binding effect: %v", err)
			}
			if scenario == "not-selected" {
				consumer := entity.RuntimeEnvironmentConsumer{AgentRef: assistantRef, AgentVersion: after.AgentVersion, BindingRef: item.BindingRef, BindingVersion: item.BindingVersion, VersionRef: item.SourceRevisionRef, ScopeKind: item.ScopeKind, OrganizationRef: item.OrganizationRef}
				_, err := service.Execute(ctx, command.Command{Kind: command.RebindRuntimeEnvironment, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "-generic-old-snapshot", ExpectedVersion: &after.Environment.Version}, Payload: command.RuntimeEnvironmentRebindInput{EnvironmentRef: after.Environment.Ref, VersionRef: after.Environment.CurrentVersion.Ref, Consumers: []entity.RuntimeEnvironmentConsumer{consumer}}})
				if !errors.Is(err, errs.ErrVersionMismatch) {
					t.Fatalf("generic rebind accepted initial SYSTEM fallback: %v", err)
				}
			}
		})
	}
}
