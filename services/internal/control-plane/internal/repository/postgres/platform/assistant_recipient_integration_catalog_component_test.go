package platform

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testAssistantRecipientIntegrationCatalog(t *testing.T, ctx context.Context, repository *Repository, service *serviceplatform.Service, owner, worker value.Principal, project *entity.Project, connectionRef, foreignHelperRef string) {
	t.Helper()
	prepareObservedWarmFixture(t, ctx, repository)
	warm := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.report"}, "runtime-controller")
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "recipient-catalog-agent", "Catalog Developer")
	unassigned := createLifecycleAgent(t, ctx, service, owner, project.Ref, "recipient-catalog-unassigned", "Unassigned Developer")
	unbound, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "recipient-catalog-unbound-registry"}, Payload: command.ConnectionInput{DefinitionKey: "github", Name: "Recipient registry github.repository fixture", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "registry"}}})
	if err != nil || unbound.Connection == nil {
		t.Fatal("create isolated unbound registry fixture")
	}
	draft := entity.WorkflowVersion{Name: "Catalog workflow", Purpose: "Closed catalog fixture", CoordinatorAgentRef: agent.Ref, VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600, CompletionCriteria: "Bounded result", ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "step", Position: 1, Name: "Step", AgentRef: agent.Ref, Instructions: "Complete fixture.", ExpectedResult: "Fixture result", TimeoutSeconds: 900}}}
	workflow, err := service.Execute(ctx, command.Command{Kind: command.CreateWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "recipient-catalog-workflow"}, Payload: command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agent.Ref, Draft: &draft}})
	if err != nil || workflow.Workflow == nil {
		t.Fatal("create recipient workflow")
	}
	for _, recipient := range []struct{ agent, workflow string }{{agent.Ref, ""}, {"", workflow.Workflow.Ref}} {
		for _, enabled := range []bool{true, false} {
			connection, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
			if err != nil {
				t.Fatal("read synthetic grant connection")
			}
			key := "recipient-disabled-" + recipient.agent + recipient.workflow
			if enabled {
				key += "-enable"
			} else {
				key += "-disable"
			}
			if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &connection.Version}, Payload: command.IntegrationGrantInput{ConnectionRef: connectionRef, AgentRef: recipient.agent, WorkflowRef: recipient.workflow, CapabilityKey: "github.actions.job.list", Enabled: enabled, ApprovalPolicy: "NONE"}}); err != nil {
				t.Fatalf("prepare disabled grant: %v", err)
			}
		}
	}
	reader := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.assistant.resources.search"}, "runtime-controller")
	for _, scope := range []string{"PROJECT", "SYSTEM"} {
		for _, recipient := range []struct {
			kind, ref string
			version   int64
		}{{"AGENT", agent.Ref, agent.Version}, {"WORKFLOW", workflow.Workflow.Ref, workflow.Workflow.Version}} {
			t.Run("recipient catalog "+scope+recipient.kind, func(t *testing.T) {
				prefix := "recipient-catalog-" + scope + recipient.kind
				execute := func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
					t.Helper()
					result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: prefix + key, ExpectedVersion: version}, Payload: payload})
					if err != nil {
						t.Fatalf("recipient fixture %s: %v", key, err)
					}
					return result
				}
				conversation := execute(command.CreateAssistantConversation, owner, "conversation", nil, command.AssistantConversationInput{AssistantScope: scope, ProjectRef: project.Ref, Context: entity.AssistantContextDescriptor{EntityKind: recipient.kind, EntityRef: recipient.ref}}).Conversation
				if conversation == nil || !contains(conversation.Context.AllowedOperations, "CHANGE_INTEGRATION_GRANT") {
					t.Fatal("recipient context omitted GRANT authority")
				}
				turn := execute(command.AddAssistantTurn, owner, "turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Read recipient grant candidates", DeliveryMode: "QUEUE"}).Conversation
				leases := execute(command.ClaimExecution, worker, "claim", nil, command.LeaseInput{WorkloadInstance: prefix, Limit: 1}).RuntimeItems
				if len(leases) != 1 {
					t.Fatal("missing exact recipient lease")
				}
				lease := leases[0]
				if recipient.kind == "AGENT" {
					testAssistantAgentConfigurationUnderLease(t, ctx, repository, service, owner, reader, lease, agent.Ref, unassigned.Ref, foreignHelperRef)
				} else {
					_, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), entity.AssistantConfigurationCatalogRequest{Kind: "AGENT_CONFIGURATION", AssistantRef: stringMap(lease, "agentRef"), EntityKind: "AGENT", EntityRef: agent.Ref})
					if !errors.Is(err, errs.ErrNotFound) {
						t.Fatal("Workflow context read agent instructions")
					}
				}
				runRef := turn.Turns[0].RunRef
				defer func() {
					run, err := service.GetRun(ctx, owner, runRef)
					if err != nil {
						t.Error("read synthetic run cleanup")
						return
					}
					if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "cancel", ExpectedVersion: &run.Version}, Payload: command.RunCommandInput{RunRef: run.Ref, Reason: "Synthetic catalog complete"}}); err != nil {
						t.Errorf("cancel synthetic catalog run: %v", err)
					}
				}()
				input := entity.AssistantConfigurationCatalogRequest{Kind: "RECIPIENT_INTEGRATION_GRANTS", AssistantRef: stringMap(lease, "agentRef"), Query: "Own repository"}
				read := func(actor value.Principal, fence string, generation int64) (entity.AssistantConfigurationCatalogResponse, error) {
					return service.ListAssistantConfigurationCatalog(ctx, actor, stringMap(lease, "leaseRef"), fence, generation, input)
				}
				result, err := read(reader, stringMap(lease, "fence"), lease["generation"].(int64))
				if err != nil {
					t.Fatalf("recipient read: %v", err)
				}
				catalog := result.RecipientIntegrationGrants
				if catalog == nil || catalog.RecipientRef != recipient.ref || catalog.RecipientKind != recipient.kind || catalog.RecipientVersion != recipient.version || result.ProjectRef != project.Ref || result.AssistantProfileRef != "" || len(catalog.Entries) == 0 {
					t.Fatal("recipient catalog missing exact pins")
				}
				if scope == "PROJECT" && recipient.kind == "AGENT" {
					t.Run("source registry filters unresolved revision before pagination", func(t *testing.T) {
						selected := input
						selected.Query = unbound.Connection.Name
						original, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), selected)
						if err != nil || original.RecipientIntegrationGrants == nil || len(original.RecipientIntegrationGrants.Entries) == 0 {
							t.Fatal("DB admission did not enumerate exact unbound fixture", err)
						}
						// Отдельный producer воспроизводит hot reload раньше обновления DB
						// registry, не меняя реальные fixtures или общий repository map.
						updated := *repository
						updated.integrationDefinitions = make(map[string]integrationpackage.Package, len(repository.integrationDefinitions))
						for key, definition := range repository.integrationDefinitions {
							updated.integrationDefinitions[key] = definition
						}
						previous := updated.integrationDefinitions["github"]
						currentDefinition := previous
						currentDefinition.Metadata.Version = "9000.0.0"
						currentDefinition, err = integrationpackage.Parse(asJSON(currentDefinition))
						if err != nil {
							t.Fatal("canonical prospective registry fixture", err)
						}
						// Новый adapter output contract делает существующий published
						// managed package пригодным только для metadata read, не execution.
						for index := range currentDefinition.Spec.Capabilities {
							if currentDefinition.Spec.Capabilities[index].Key == "github.pull_request.review.create" {
								for field := range currentDefinition.Spec.Capabilities[index].OutputFields {
									if currentDefinition.Spec.Capabilities[index].OutputFields[field].Key == "id" {
										currentDefinition.Spec.Capabilities[index].OutputFields[field].Maximum--
									}
								}
							}
						}
						currentDefinition, err = integrationpackage.Parse(asJSON(currentDefinition))
						if err != nil {
							t.Fatal("canonical prospective adapter contract", err)
						}
						updated.integrationDefinitions["github"] = currentDefinition
						updatedService, err := serviceplatform.New(&updated)
						if err != nil {
							t.Fatal(err)
						}
						for _, search := range []string{unbound.Connection.Name, "registry github.repository"} {
							for _, offset := range []int32{0, 10, 20, 30, 40, 50} {
								selected := input
								selected.Query, selected.Offset = search, offset
								filtered, err := updatedService.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), selected)
								if err != nil || filtered.RecipientIntegrationGrants == nil || len(filtered.RecipientIntegrationGrants.Entries) != 0 || filtered.NextOffset != 0 {
									t.Fatalf("unresolved source revision poisoned page%d: %v", offset, err)
								}
							}
						}
						selected.Query = "Own obsolete repository2.4"
						retired, err := updatedService.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), selected)
						if err != nil || retired.RecipientIntegrationGrants == nil || len(retired.RecipientIntegrationGrants.Entries) == 0 {
							t.Fatal("exact bound incompatible package metadata vanished", err)
						}
						for _, entry := range retired.RecipientIntegrationGrants.Entries {
							if entry.Grant.Candidate.Grantable || entry.Grant.Candidate.Reason != "PACKAGE_UNAVAILABLE" {
								t.Fatal("bound incompatible package gained grant eligibility")
							}
						}
						// Исключённый tuple не становится исполняемым: exact loader по-прежнему
						// закрыто отказывает, а исходный producer сохраняет доступные rows.
						resolved, err := repository.ResolvePrincipal(ctx, owner)
						if err != nil {
							t.Fatal(err)
						}
						ownerScope, err := repository.resolveScope(ctx, resolved)
						if err != nil {
							t.Fatal(err)
						}
						if _, err := updated.integrationPackage(ctx, repository.pool, ownerScope.organizationID, unbound.Connection.Ref, previous.Metadata.Key, previous.Metadata.Version, previous.Digest); !errors.Is(err, errIntegrationPackageUnavailable) || !errors.Is(err, errs.ErrForbidden) {
							t.Fatalf("excluded revision gained execution eligibility: %v", err)
						}
						seen := map[string]bool{}
						for offset := int32(0); ; {
							selected := input
							selected.Offset = offset
							page, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), selected)
							if err != nil || page.RecipientIntegrationGrants == nil {
								t.Fatalf("current revision page%d: %v", offset, err)
							}
							for _, entry := range page.RecipientIntegrationGrants.Entries {
								key := entry.Grant.ConnectionRef + "\x00" + entry.Grant.Candidate.Capability.Key
								if seen[key] {
									t.Fatal("pagination duplicated eligible candidate")
								}
								seen[key] = true
							}
							if page.NextOffset == 0 {
								break
							}
							if page.NextOffset != offset+10 || len(page.RecipientIntegrationGrants.Entries) != 10 || page.NextOffset > 1000 {
								t.Fatal("pagination lost bounded monotonic progress")
							}
							offset = page.NextOffset
						}
						if len(seen) == 0 {
							t.Fatal("current eligible candidates disappeared")
						}
					})
				}
				found := false
				disabled := false
				for _, entry := range catalog.Entries {
					if entry.Grant.ConnectionRef == connectionRef {
						found = true
						if entry.Grant.Candidate.Capability.Key == "github.actions.job.list" && entry.Grant.Candidate.CurrentGrantRef != "" && entry.Grant.Candidate.CurrentGrantVersion > 0 && !entry.Grant.Candidate.CurrentGrantEnabled {
							disabled = true
						}
					}
					if entry.Grant.Candidate.CurrentGrantEnabled || entry.Pins.ProjectVersion != project.Version || entry.Pins.RecipientVersion != recipient.version || entry.Pins.ContextDigest == "" {
						t.Fatal("recipient catalog mixed current grants/pins")
					}
				}
				if !found || !disabled {
					t.Fatal("missing disabled/unconfigured recipient candidate")
				}
				if recipient.kind == "WORKFLOW" {
					selected := entity.AssistantConfigurationCatalogRequest{Kind: "WORKFLOW_CONFIGURATION", AssistantRef: input.AssistantRef, EntityKind: "WORKFLOW", EntityRef: recipient.ref}
					snapshot, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), selected)
					if err != nil || snapshot.WorkflowConfiguration == nil || snapshot.WorkflowConfiguration.Version != recipient.version || snapshot.WorkflowConfiguration.ProjectRef != project.Ref {
						t.Fatalf("full Workflow snapshot missing: %v", err)
					}
					var before map[string]any
					if json.Unmarshal(snapshot.WorkflowConfiguration.ConfigurationJSON, &before) != nil || assistantString(before, "workflowRef") != recipient.ref || !assistantWorkflowContainsAgent(before, agent.Ref) || len(before["steps"].([]any)) != 1 {
						t.Fatal("Workflow graph snapshot lost exact assigned role")
					}
					fresh, err := service.GetWorkflow(ctx, owner, recipient.ref)
					if err != nil {
						t.Fatal(err)
					}
					exactDraft, _ := json.Marshal(fresh.Draft)
					snapshotDraft, _ := json.Marshal(before["draft"])
					var original map[string]any
					if json.Unmarshal(exactDraft, &original) != nil || !reflect.DeepEqual(original, before["draft"]) || len(snapshotDraft) == 0 {
						t.Fatal("Workflow read changed authoritative draft")
					}
					selected.EntityRef = "wfl_foreign123"
					if _, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), selected); !errors.Is(err, errs.ErrNotFound) {
						t.Fatal("foreign Workflow selector accepted")
					}
					selected.Kind, selected.EntityKind, selected.EntityRef = "RECIPIENT_INTEGRATION_GRANTS", "AGENT", agent.Ref
					if scope == "PROJECT" {
						grants, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), selected)
						if err != nil || grants.RecipientIntegrationGrants == nil || grants.RecipientIntegrationGrants.RecipientRef != agent.Ref || grants.RecipientIntegrationGrants.ContextEntityRef != recipient.ref || grants.RecipientIntegrationGrants.ContextEntityVersion != recipient.version || len(grants.RecipientIntegrationGrants.Entries) == 0 {
							t.Fatalf("assigned AGENT catalog missing: %v", err)
						}
					}
					for _, ref := range []string{unassigned.Ref, foreignHelperRef} {
						selected.EntityRef = ref
						if _, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), selected); !errors.Is(err, errs.ErrNotFound) {
							t.Fatal("unassigned or foreign AGENT selector accepted")
						}
					}
				}
				if recipient.kind == "AGENT" {
					selected := input
					selected.EntityKind, selected.EntityRef = "AGENT", unassigned.Ref
					if _, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), selected); !errors.Is(err, errs.ErrNotFound) {
						t.Fatal("AGENT context read another AGENT")
					}
				}
				if _, err := read(owner, stringMap(lease, "fence"), lease["generation"].(int64)); !errors.Is(err, errs.ErrForbidden) {
					t.Fatal("ordinary actor bypassed runtime read")
				}
				if _, err := read(reader, "wrong-fence", lease["generation"].(int64)); !errors.Is(err, errs.ErrNotFound) {
					t.Fatal("wrong fence accepted")
				}
				if _, err := read(reader, stringMap(lease, "fence"), lease["generation"].(int64)+1); !errors.Is(err, errs.ErrNotFound) {
					t.Fatal("wrong generation accepted")
				}
				input.AssistantRef = agent.Ref
				if _, err := read(reader, stringMap(lease, "fence"), lease["generation"].(int64)); !errors.Is(err, errs.ErrForbidden) {
					t.Fatal("payload recipient replaced source helper")
				}
				input.AssistantRef = stringMap(lease, "agentRef")
				input.AssistantRef = foreignHelperRef
				if _, err := read(reader, stringMap(lease, "fence"), lease["generation"].(int64)); !errors.Is(err, errs.ErrForbidden) {
					t.Fatal("foreign project helper replayed recipient lease")
				}
				input.AssistantRef = stringMap(lease, "agentRef")
				var expiry time.Time
				if err := repository.pool.QueryRow(ctx, queryAssistantCurrentConfigurationExpire, stringMap(lease, "leaseRef")).Scan(&expiry); err != nil {
					t.Fatal(err)
				}
				if _, err := read(reader, stringMap(lease, "fence"), lease["generation"].(int64)); !errors.Is(err, errs.ErrNotFound) {
					t.Fatal("expired lease accepted")
				}
				if _, err := repository.pool.Exec(ctx, queryAssistantCurrentConfigurationRestoreExpiry, stringMap(lease, "leaseRef"), expiry); err != nil {
					t.Fatal(err)
				}
				if recipient.kind == "AGENT" {
					agent = testAssistantInstructionRealtime(t, ctx, repository, service, owner, worker, lease, agent.Ref, prefix+"-instruction-realtime")
				}
				if scope == "SYSTEM" && recipient.kind == "AGENT" {
					changed := execute(command.SetAgentEnabled, owner, "stale-recipient", &agent.Version, command.AgentInput{Ref: agent.Ref, Enabled: false})
					if changed.Agent == nil {
						t.Fatal("missing changed recipient")
					}
					_, agentReadErr := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), entity.AssistantConfigurationCatalogRequest{Kind: "AGENT_CONFIGURATION", AssistantRef: stringMap(lease, "agentRef"), EntityKind: "AGENT", EntityRef: agent.Ref})
					if !errors.Is(agentReadErr, errs.ErrNotFound) {
						t.Fatal("agent instructions accepted stale context version")
					}
					if _, err := read(reader, stringMap(lease, "fence"), lease["generation"].(int64)); !errors.Is(err, errs.ErrNotFound) {
						t.Fatal("accepted stale immutable recipient context")
					}
				}
				if scope == "SYSTEM" && recipient.kind == "WORKFLOW" {
					resolved, err := repository.ResolvePrincipal(ctx, owner)
					if err != nil {
						t.Fatal(err)
					}
					current, err := repository.resolveScope(ctx, resolved)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := repository.pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "MEMBER"); err != nil {
						t.Fatal("revoke synthetic catalog authority")
					}
					_, readErr := read(reader, stringMap(lease, "fence"), lease["generation"].(int64))
					if _, err := repository.pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "OWNER"); err != nil {
						t.Fatal("restore synthetic catalog authority")
					}
					if !errors.Is(readErr, errs.ErrNotFound) && !errors.Is(readErr, errs.ErrForbidden) {
						t.Fatal("revoked root authority retained recipient catalog")
					}
				}
			})
		}
	}
}
