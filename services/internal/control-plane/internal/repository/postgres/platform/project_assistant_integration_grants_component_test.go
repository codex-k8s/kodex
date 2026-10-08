package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProjectAssistantIntegrationGrantsComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated connection PostgreSQL")
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.assistant.turns.add"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "project-self-grant-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s %s: %v", kind, key, err)
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Connection purpose", Language: "en"}).Project
	profile := execute(command.CreateProjectAssistant, owner, "profile", nil, command.ProjectAssistantInput{ProjectRef: project.Ref, Name: "Own helper", Purpose: "Synthetic integration setup", Instructions: "Configure approved project resources after confirmation."}).ProjectAssistant
	foreignProject := execute(command.CreateProject, owner, "foreign-project", nil, command.ProjectInput{Name: "Other purpose", Language: "en"}).Project
	foreign := execute(command.CreateProjectAssistant, owner, "foreign-profile", nil, command.ProjectAssistantInput{ProjectRef: foreignProject.Ref, Name: "Other helper", Purpose: "Synthetic other setup", Instructions: "Configure only this project after confirmation."}).ProjectAssistant

	currentOwner, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.resolveScope(ctx, currentOwner)
	if err != nil {
		t.Fatal(err)
	}
	setupConversation := execute(command.CreateAssistantConversation, owner, "setup-conversation", nil, command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: project.Ref}).Conversation
	setupTurn := execute(command.AddAssistantTurn, owner, "setup-turn", nil, command.AssistantTurnInput{ConversationRef: setupConversation.Ref, Content: "Prepare own connection", DeliveryMode: "QUEUE"}).Conversation
	setupLeases := execute(command.ClaimExecution, worker, "setup-claim", nil, command.LeaseInput{WorkloadInstance: "project-grant-setup", Limit: 1}).RuntimeItems
	if len(setupLeases) != 1 {
		t.Fatal("missing setup lease")
	}
	setupLease := setupLeases[0]
	setupPlan := execute(command.ProposeAssistantPlan, worker, "setup-plan", nil, command.ProposeAssistantPlanInput{LeaseRef: stringMap(setupLease, "leaseRef"), Fence: stringMap(setupLease, "fence"), Generation: setupLease["generation"].(int64), Summary: "Prepare repository", Operations: []entity.AssistantPlanOperation{{Key: "connection", Type: prepareProjectAssistantConnection, Title: "Own connection", Summary: "Confirm own connection", Parameters: map[string]any{"projectAssistantRef": profile.AgentRef, "definitionKey": "github", "name": "Own repository", "publicConfiguration": map[string]any{"owner": "fixture", "repository": "repository"}}}}}).Plan
	setupPlan = execute(command.ValidateAssistantPlan, owner, "setup-validate", &setupPlan.Version, command.AssistantPlanInput{PlanRef: setupPlan.Ref, Revision: setupPlan.Revision}).Plan
	setupApplied := execute(command.ApplyAssistantPlan, owner, "setup-apply", &setupPlan.Version, command.AssistantPlanInput{PlanRef: setupPlan.Ref, Revision: setupPlan.Revision})
	if setupApplied.Plan == nil || setupApplied.Plan.State != "APPLIED" || len(setupApplied.PlanReceipt.Operations) != 1 {
		t.Fatal("setup plan not applied")
	}
	connectionValue, err := service.GetIntegrationConnection(ctx, owner, setupApplied.PlanReceipt.Operations[0].ResourceRef)
	if err != nil {
		t.Fatal(err)
	}
	connection := &connectionValue
	setupRun, err := service.GetRun(ctx, owner, setupTurn.Turns[0].RunRef)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "setup-stop", &setupRun.Version, command.RunCommandInput{RunRef: setupRun.Ref, Reason: "Synthetic setup complete"})
	if connection == nil {
		t.Fatal("missing own connection")
	}
	if _, err = pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, connection.Ref); err != nil {
		t.Fatal("prepare synthetic credential metadata")
	}
	foreignConnection := execute(command.CreateConnection, owner, "foreign-connection", nil, command.ConnectionInput{DefinitionKey: "github", Name: "Unowned repository", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "other"}}).Connection
	if _, err = service.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, project.Ref, foreignConnection.Ref, "", query.Page{Size: 100}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign origin candidate: %v", err)
	}
	if _, err = service.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, foreignProject.Ref, connection.Ref, "", query.Page{Size: 100}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign helper candidate: %v", err)
	}
	candidates, err := service.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, project.Ref, connection.Ref, "", query.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	if candidates.ProjectRef != project.Ref || candidates.ProfileRef != profile.Ref || candidates.AssistantRef != profile.AgentRef || candidates.ScopeKind != "ORGANIZATION" {
		t.Fatal("candidate owner tuple mismatch")
	}
	conversation := execute(command.CreateAssistantConversation, owner, "grant-conversation", nil, command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: project.Ref}).Conversation
	if !contains(conversation.Context.AllowedOperations, changeProjectAssistantIntegrationGrant) {
		t.Fatal("context omitted own grant")
	}
	execute(command.AddAssistantTurn, owner, "turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Prepare own grants", DeliveryMode: "QUEUE"})
	leases := execute(command.ClaimExecution, worker, "claim", nil, command.LeaseInput{WorkloadInstance: "project-grant-fixture", Limit: 1}).RuntimeItems
	if len(leases) != 1 {
		t.Fatal("exact synthetic lease missing")
	}
	lease := leases[0]
	reader := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.assistant.resources.search"}, "runtime-controller")
	if _, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), entity.AssistantConfigurationCatalogRequest{Kind: "RECIPIENT_INTEGRATION_GRANTS", AssistantRef: profile.AgentRef}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("recipient catalog accepted context without selected recipient authority")
	}
	catalog, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), entity.AssistantConfigurationCatalogRequest{Kind: "PROJECT_INTEGRATION_GRANTS", AssistantRef: profile.AgentRef})
	if err != nil {
		t.Fatal(err)
	}
	if catalog.ScopeKind != "PROJECT" || catalog.ProjectRef != project.Ref || catalog.AssistantProfileRef != profile.Ref || len(catalog.Entries) != 0 || len(catalog.ProjectIntegrationGrants) == 0 || len(catalog.ProjectIntegrationGrants) > 10 {
		t.Fatal("typed own catalog mismatch")
	}
	for _, item := range catalog.ProjectIntegrationGrants {
		if item.ConnectionRef != connection.Ref {
			t.Fatal("foreign connection leaked in own catalog")
		}
	}
	t.Run("aggregate ignores unresolved exact package but preserves current corruption failure", func(t *testing.T) {
		old := execute(command.CreateConnection, owner, "obsolete-connection", nil, command.ConnectionInput{DefinitionKey: "github", Name: "Own obsolete repository2.4", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "obsolete"}}).Connection
		old = execute(command.SetConnectionEnabled, owner, "obsolete-disable", &old.Version, command.ConnectionInput{Ref: old.Ref, Enabled: false}).Connection
		// Disposable fixture неизвестной прошлой exact ревизии: не legacy decoder
		// и не реальный historical digest. Purpose назначается только серверным tuple.
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET definition_version='2.4.0',definition_digest=repeat('9',64),version=version+1 WHERE ref=$1`, old.Ref); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO control_plane.project_assistant_connection_purposes
		(organization_id,connection_id,project_ref,profile_ref,assistant_ref,profile_version,agent_version,created_by)
		SELECT c.organization_id,c.id,p.ref,profile.ref,a.ref,profile.version,a.version,c.created_by
		FROM control_plane.integration_connections c JOIN control_plane.project_assistant_profiles profile ON profile.organization_id=c.organization_id
		JOIN control_plane.projects p ON p.id=profile.project_id JOIN control_plane.agents a ON a.id=profile.agent_id
		WHERE c.ref=$1 AND profile.ref=$2`, old.Ref, profile.Ref); err != nil {
			t.Fatal(err)
		}
		if _, err := service.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, project.Ref, old.Ref, "", query.Page{Size: 100}); !errors.Is(err, errs.ErrForbidden) || !errors.Is(err, errIntegrationPackageUnavailable) {
			// Domain service сохраняет исходную typed ошибку; transport не получает
			// права на недоступный package даже при наличии purpose.
			t.Fatalf("single unavailable package did not reject: %v", err)
		}
		// Sibling recipient index уже исключает unbound exact revision в
		// authoritative admission до пагинации; никаких новых skip в нём нет.
		shippedRevisions, err := r.assistantRecipientCatalogShippedRevisions()
		if err != nil {
			t.Fatal(err)
		}
		rows, err := pool.Query(ctx, queryAssistantRecipientIntegrationCatalogEntries, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project_id": "",
			"project_ref": project.Ref, "recipient_kind": "AGENT", "recipient_ref": profile.AgentRef, "query": old.Name, "offset": int32(0), "shipped_revisions": shippedRevisions})
		if err != nil {
			t.Fatal(err)
		}
		found := rows.Next()
		readErr := rows.Err()
		rows.Close()
		if found || readErr != nil {
			t.Fatalf("recipient admission enumerated unresolved revision: %v", readErr)
		}
		read := func(search string, offset int32) (entity.AssistantConfigurationCatalogResponse, error) {
			return service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64),
				entity.AssistantConfigurationCatalogRequest{Kind: "PROJECT_INTEGRATION_GRANTS", AssistantRef: profile.AgentRef, Query: search, Offset: offset})
		}
		for _, search := range []string{"", connection.Name} {
			for _, offset := range []int32{0, 10, 20, 30, 40} {
				result, err := read(search, offset)
				if err != nil {
					t.Fatalf("unavailable old package poisoned catalog offset%d: %v", offset, err)
				}
				if offset == 0 && len(result.ProjectIntegrationGrants) == 0 {
					t.Fatal("eligible current package disappeared")
				}
				for _, item := range result.ProjectIntegrationGrants {
					if item.ConnectionRef != connection.Ref || item.DefinitionVersion != connection.DefinitionVersion || item.DefinitionDigest != connection.DefinitionDigest {
						t.Fatal("unavailable or foreign connection leaked")
					}
				}
			}
		}
		// Найденный published package с несовпавшими current pins — не marker.
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET definition_version=$2,definition_digest=$3,version=version+1 WHERE ref=$1`, old.Ref, connection.DefinitionVersion, connection.DefinitionDigest); err != nil {
			t.Fatal(err)
		}
		definition := r.integrationDefinitions["github"]
		definition.Spec.HealthCheck.TimeoutSeconds--
		definition.Spec.Name = "Synthetic published current package"
		bound := publishAndRebindManagedConfiguration(t, ctx, service, owner, "catalog-corrupt-pins", command.CreateIntegrationDefinition,
			command.ValidateIntegrationDefinition, command.PublishIntegrationDefinition, command.RebindIntegrationDefinition,
			command.ManagedConfigurationInput{Name: definition.Spec.Name, ContentFormat: "JSON", Content: string(asJSON(definition))},
			entity.ManagedConfigurationConsumer{Kind: "INTEGRATION_CONNECTION", Ref: old.Ref})
		if bound.ManagedRevision == nil {
			t.Fatal("published fixture missing")
		}
		t.Run("exact bound unsupported package is diagnostic and does not poison current sibling", func(t *testing.T) {
			// Отдельный producer воспроизводит смену executable registry. Published
			// content остаётся immutable; меняются только prospective source pins
			// и disposable sibling fixture, без legacy decoder или grant authority.
			updated := *r
			updated.integrationDefinitions = make(map[string]integrationpackage.Package, len(r.integrationDefinitions))
			for key, definition := range r.integrationDefinitions {
				updated.integrationDefinitions[key] = definition
			}
			future, err := integrationpackage.Parse(asJSON(r.integrationDefinitions["github"]))
			if err != nil {
				t.Fatal(err)
			}
			future.Metadata.Version = "9000.0.0"
			for index := range future.Spec.Capabilities {
				if future.Spec.Capabilities[index].Key == "github.pull_request.review.create" {
					for field := range future.Spec.Capabilities[index].OutputFields {
						if future.Spec.Capabilities[index].OutputFields[field].Key == "id" {
							future.Spec.Capabilities[index].OutputFields[field].Maximum--
						}
					}
				}
			}
			future, err = integrationpackage.Parse(asJSON(future))
			if err != nil {
				t.Fatal(err)
			}
			updated.integrationDefinitions["github"] = future
			updatedService, err := serviceplatform.New(&updated)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET definition_version=$2,definition_digest=$3 WHERE ref=$1`, connection.Ref, future.Metadata.Version, future.Digest); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET definition_version=$2,definition_digest=$3 WHERE ref=$1`, connection.Ref, connection.DefinitionVersion, connection.DefinitionDigest); err != nil {
					t.Fatal(err)
				}
			}()
			retired, err := updatedService.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, project.Ref, old.Ref, "", query.Page{Size: 100})
			if err != nil || len(retired.Items) == 0 || retired.DefinitionDigest != bound.ManagedRevision.Digest {
				t.Fatalf("exact bound metadata disappeared: %v", err)
			}
			for _, item := range retired.Items {
				if item.Grantable || item.Reason != "PACKAGE_UNAVAILABLE" || item.CurrentGrantEnabled {
					t.Fatal("incompatible package gained enable eligibility")
				}
			}
			first, err := updatedService.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, project.Ref, old.Ref, "", query.Page{Size: 1})
			if err != nil || first.NextPageToken == "" {
				t.Fatal("diagnostic package cursor missing", err)
			}
			if _, err := updatedService.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, project.Ref, old.Ref, "changed query", query.Page{Size: 1, Token: first.NextPageToken}); !errors.Is(err, errs.ErrInvalid) {
				t.Fatalf("diagnostic cursor escaped query pins: %v", err)
			}
			if _, err := updatedService.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, project.Ref, connection.Ref, "", query.Page{Size: 1, Token: first.NextPageToken}); !errors.Is(err, errs.ErrInvalid) {
				t.Fatalf("diagnostic cursor escaped connection pins: %v", err)
			}
			readUpdated := func(search string, offset int32, fence string, generation int64) (entity.AssistantConfigurationCatalogResponse, error) {
				return updatedService.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), fence, generation,
					entity.AssistantConfigurationCatalogRequest{Kind: "PROJECT_INTEGRATION_GRANTS", AssistantRef: profile.AgentRef, Query: search, Offset: offset})
			}
			seen := map[string]bool{}
			for offset := int32(0); ; {
				page, err := readUpdated("", offset, stringMap(lease, "fence"), lease["generation"].(int64))
				if err != nil {
					t.Fatalf("bound unsupported package poisoned aggregate: %v", err)
				}
				for _, item := range page.ProjectIntegrationGrants {
					seen[item.ConnectionRef] = true
					if item.ConnectionRef == old.Ref {
						if item.Candidate.Grantable || item.Candidate.Reason != "PACKAGE_UNAVAILABLE" {
							t.Fatal("aggregate lost unsupported classification")
						}
					} else if item.ConnectionRef != connection.Ref || item.DefinitionDigest != future.Digest || item.Candidate.Reason == "PACKAGE_UNAVAILABLE" {
						t.Fatal("current or foreign sibling admission changed")
					}
				}
				if page.NextOffset == 0 {
					break
				}
				if page.NextOffset <= offset || page.NextOffset > 100 {
					t.Fatal("aggregate pagination did not progress")
				}
				offset = page.NextOffset
			}
			if !seen[old.Ref] || !seen[connection.Ref] || len(seen) != 2 {
				t.Fatal("diagnostic old package or eligible current sibling disappeared")
			}
			filtered, err := readUpdated(connection.Name, 0, stringMap(lease, "fence"), lease["generation"].(int64))
			if err != nil || len(filtered.ProjectIntegrationGrants) == 0 {
				t.Fatal("current sibling query failed", err)
			}
			for _, item := range filtered.ProjectIntegrationGrants {
				if item.ConnectionRef != connection.Ref {
					t.Fatal("query isolation changed")
				}
			}
			if _, err := readUpdated("", 0, "wrong-fence", lease["generation"].(int64)); err == nil {
				t.Fatal("unsupported package read bypassed lease fence")
			}
			if _, err := readUpdated("", 0, stringMap(lease, "fence"), lease["generation"].(int64)+1); err == nil {
				t.Fatal("unsupported package read bypassed lease generation")
			}
			if _, err := updatedService.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, foreignProject.Ref, old.Ref, "", query.Page{Size: 100}); !errors.Is(err, errs.ErrNotFound) {
				t.Fatalf("metadata reader widened project/profile authority: %v", err)
			}
			if _, err := updated.integrationPackage(ctx, pool, current.organizationID, old.Ref, "github", retired.DefinitionVersion, retired.DefinitionDigest); !errors.Is(err, errs.ErrForbidden) || errors.Is(err, errIntegrationPackageUnavailable) {
				t.Fatalf("bound incompatible package became executable or skippable: %v", err)
			}
			// Поднимаем только disposable readiness, чтобы enable отказ был именно
			// executable package admission, а не прежнее disabled состояние.
			if _, err := pool.Exec(ctx, strings.ReplaceAll(queryIntegrationGrantPolicyCredentialFixture, "icred_policy_fixture", "icred_retired_fixture"), old.Ref); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET enabled=true,version=version+1 WHERE ref=$1`, old.Ref); err != nil {
				t.Fatal(err)
			}
			retired, err = updatedService.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, project.Ref, old.Ref, "", query.Page{Size: 100})
			if err != nil || len(retired.Items) == 0 || retired.Items[0].Grantable || retired.Items[0].Reason != "PACKAGE_UNAVAILABLE" {
				t.Fatal("readiness accidentally enabled retired package", err)
			}
			if _, err := updatedService.Execute(ctx, command.Command{Kind: command.ChangeProjectAssistantIntegrationGrant, Principal: owner,
				Mutation: value.Mutation{IdempotencyKey: "project-retired-enable-denied", ExpectedVersion: &retired.ConnectionVersion},
				Payload: command.ProjectAssistantIntegrationGrantInput{AssistantRef: profile.AgentRef, Grant: command.SystemAssistantIntegrationGrantInput{
					ConnectionRef: old.Ref, CapabilityKey: "github.repository.metadata.read", Enabled: true, ApprovalPolicy: "NONE"}}}); !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("metadata reader enabled unsupported package: %v", err)
			}
		})
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET definition_digest=repeat('8',64),version=version+1 WHERE ref=$1`, old.Ref); err != nil {
			t.Fatal(err)
		}
		if _, err := read(connection.Name, 40); err == nil || errors.Is(err, errIntegrationPackageUnavailable) {
			t.Fatalf("corrupt current pins were silently omitted: %v", err)
		}
		// Отказ query/SQL чтения также не превращается в успешный пустой catalog.
		cancelled, cancelRead := context.WithCancel(ctx)
		cancelRead()
		if _, err := service.ListAssistantConfigurationCatalog(cancelled, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), entity.AssistantConfigurationCatalogRequest{Kind: "PROJECT_INTEGRATION_GRANTS", AssistantRef: profile.AgentRef}); err == nil {
			t.Fatal("unavailable read became a successful aggregate")
		}
		// Восстанавливаем только disposable fixture для остальных lifecycle cases.
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET definition_digest=$2,version=version+1 WHERE ref=$1`, old.Ref, bound.ManagedRevision.Digest); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM control_plane.project_assistant_connection_purposes WHERE connection_id=(SELECT id FROM control_plane.integration_connections WHERE ref=$1)`, old.Ref); err != nil {
			t.Fatal(err)
		}
	})
	readKeys := strings.Fields("github.repository.metadata.read github.repository.content.list github.repository.content.read github.branch.list github.branch.read github.commit.list github.commit.read github.issue.list github.issue.read github.pull_request.list github.pull_request.read github.pull_request.file.list github.pull_request.review.list github.pull_request.review.read github.check_run.list github.check_run.read github.actions.run.list github.actions.run.read github.actions.job.list github.actions.job.read")
	operations := []entity.AssistantPlanOperation{}
	for _, item := range candidates.Items {
		if contains(readKeys, item.Capability.Key) && item.Capability.Risk == "READ" && item.Capability.ApprovalPolicy == "NONE" {
			operations = append(operations, entity.AssistantPlanOperation{Key: fmt.Sprintf("read%d", len(operations)), Type: changeProjectAssistantIntegrationGrant, Title: "Own repository read", Summary: "Confirm project helper repository read", Parameters: map[string]any{"projectAssistantRef": profile.AgentRef, "connectionRef": connection.Ref, "capabilityKey": item.Capability.Key, "enabled": true, "approvalPolicy": "NONE"}})
		}
	}
	if len(operations) != 20 {
		t.Fatalf("expected current GitHub20 READ capabilities, got %d", len(operations))
	}
	propose := func(key string, ops []entity.AssistantPlanOperation) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "project-self-grant-" + key}, Payload: command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64), Summary: "Confirm20 own repository reads", Operations: ops}})
	}
	bad := operations[0]
	bad.Parameters = cloneAssistantFields(bad.Parameters)
	bad.Parameters["projectAssistantRef"] = foreign.AgentRef
	if _, err = propose("foreign", []entity.AssistantPlanOperation{bad}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("foreign self proposal: %v", err)
	}
	bad = operations[0]
	bad.Parameters = cloneAssistantFields(bad.Parameters)
	bad.Parameters["connectionRef"] = foreignConnection.Ref
	if _, err = propose("foreign-origin", []entity.AssistantPlanOperation{bad}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign origin proposal: %v", err)
	}
	bad = operations[0]
	bad.Type = "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT"
	bad.Parameters = cloneAssistantFields(bad.Parameters)
	delete(bad.Parameters, "projectAssistantRef")
	if _, err = propose("system", []entity.AssistantPlanOperation{bad}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("PROJECT system grant: %v", err)
	}
	unsupported := operations[0]
	unsupported.Parameters = cloneAssistantFields(unsupported.Parameters)
	unsupported.Parameters["capabilityKey"] = "github.repository.content.delete"
	if _, err = propose("package-policy-reject", []entity.AssistantPlanOperation{unsupported}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("unsupported destructive/NONE policy: %v", err)
	}
	allowedWrite := operations[0]
	allowedWrite.Parameters = cloneAssistantFields(allowedWrite.Parameters)
	allowedWrite.Parameters["capabilityKey"] = "github.issue.comment.create"
	if proposal, err := propose("package-none-write", []entity.AssistantPlanOperation{allowedWrite}); err != nil || proposal.Plan == nil {
		t.Fatalf("package-allowed WRITE/NONE proposal: %v", err)
	}
	prepared, err := propose("prepare20", operations)
	if err != nil {
		t.Fatal(err)
	}
	plan := prepared.Plan
	if plan == nil || len(plan.Operations) != 20 {
		t.Fatal("missing typed plan")
	}
	for _, op := range plan.Operations {
		if op.Parameters["projectAssistantRef"] != profile.AgentRef || op.Parameters["assistantProfileRef"] != profile.Ref || op.Parameters["projectRef"] != project.Ref || op.Parameters["assistantScope"] != "PROJECT" || op.Parameters["scopeKind"] != "ORGANIZATION" || op.Target.Ref != connection.Ref {
			t.Fatal("server owner pins mismatch")
		}
	}
	forged := plan.Operations[0]
	forged.Parameters = cloneAssistantFields(forged.Parameters)
	forged.Parameters["assistantProfileRef"] = foreign.Ref
	if _, err = service.Execute(ctx, command.Command{Kind: command.UpdateAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "project-self-forge", ExpectedVersion: &plan.Version}, Payload: command.AssistantPlanDraftInput{PlanRef: plan.Ref, Summary: "Invalid foreign profile", Operations: []entity.AssistantPlanOperation{forged}}}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("readonly pin edit: %v", err)
	}
	validated := execute(command.ValidateAssistantPlan, owner, "validate", &plan.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision}).Plan
	if validated == nil || validated.State != "VALID" {
		t.Fatalf("validate20: %+v", validated)
	}
	immutable, _ := json.Marshal(validated.Operations)
	applied := execute(command.ApplyAssistantPlan, owner, "apply", &validated.Version, command.AssistantPlanInput{PlanRef: validated.Ref, Revision: validated.Revision})
	if applied.Plan == nil || applied.Plan.State != "APPLIED" || applied.PlanReceipt == nil || len(applied.PlanReceipt.Operations) != 20 || !assistantJSONEqual(json.RawMessage(immutable), applied.Plan.Operations) {
		t.Fatal("atomic20 grants changed snapshot or self-conflicted")
	}
	fresh, err := service.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, project.Ref, connection.Ref, "", query.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	enabled := 0
	for _, item := range fresh.Items {
		if item.CurrentGrantEnabled {
			enabled++
			if item.CurrentApprovalPolicy != "NONE" || item.CurrentGrantVersion != 1 {
				t.Fatal("grant policy mismatch")
			}
		}
	}
	if enabled != 20 || fresh.ConnectionVersion != candidates.ConnectionVersion+20 {
		t.Fatalf("20grant readback: count=%d version=%d", enabled, fresh.ConnectionVersion)
	}
	replay := execute(command.ApplyAssistantPlan, owner, "apply", &validated.Version, command.AssistantPlanInput{PlanRef: validated.Ref, Revision: validated.Revision})
	if replay.PlanReceipt == nil || replay.PlanReceipt.Ref != applied.PlanReceipt.Ref {
		t.Fatal("replay duplicated owner receipt")
	}
	disable := operations[0]
	disable.Parameters = cloneAssistantFields(disable.Parameters)
	disable.Parameters["enabled"] = false
	disableResult, err := propose("disable-prepare", []entity.AssistantPlanOperation{disable})
	if err != nil {
		t.Fatal(err)
	}
	disablePlan := execute(command.ValidateAssistantPlan, owner, "disable-validate", &disableResult.Plan.Version, command.AssistantPlanInput{PlanRef: disableResult.Plan.Ref, Revision: disableResult.Plan.Revision}).Plan
	if disablePlan.State != "VALID" {
		t.Fatal("disable not VALID")
	}
	agent, err := service.GetAgent(ctx, owner, profile.AgentRef)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.UpdateAgent, owner, "rename-helper", &agent.Version, command.AgentInput{Ref: agent.Ref, ProjectRef: agent.ProjectRef, Name: "Updated own helper", Purpose: agent.Purpose, RoleDescription: agent.RoleDescription, AvatarURL: agent.AvatarURL, RoleDefinitionRef: agent.RoleDefinitionRef})
	stale := execute(command.ApplyAssistantPlan, owner, "disable-stale", &disablePlan.Version, command.AssistantPlanInput{PlanRef: disablePlan.Ref, Revision: disablePlan.Revision}).Plan
	if stale == nil || stale.State != "STALE" {
		t.Fatal("agent-version drift did not close STALE")
	}
	staleEdited := stale.Operations[0]
	rebased := execute(command.UpdateAssistantPlan, owner, "disable-rebase", &stale.Version, command.AssistantPlanDraftInput{PlanRef: stale.Ref, Summary: "Explicit new owner revision", Operations: []entity.AssistantPlanOperation{staleEdited}}).Plan
	if rebased == nil || rebased.State != "DRAFT" || rebased.Revision != stale.Revision+1 || mustAssistantInt64(rebased.Operations[0].Parameters, "agentVersion") != agent.Version+1 {
		t.Fatal("STALE explicit revision did not refresh immutable server pins")
	}
	rebased = execute(command.ValidateAssistantPlan, owner, "disable-revalidate", &rebased.Version, command.AssistantPlanInput{PlanRef: rebased.Ref, Revision: rebased.Revision}).Plan
	disabled := execute(command.ApplyAssistantPlan, owner, "disable-apply", &rebased.Version, command.AssistantPlanInput{PlanRef: rebased.Ref, Revision: rebased.Revision}).Plan
	if disabled == nil || disabled.State != "APPLIED" {
		t.Fatal("fresh explicit revision was not applied")
	}
	afterDisable, err := service.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, project.Ref, connection.Ref, disable.Parameters["capabilityKey"].(string), query.Page{Size: 100})
	if err != nil || len(afterDisable.Items) != 1 || afterDisable.Items[0].CurrentGrantEnabled {
		t.Fatal("disable did not close exact grant")
	}
	grantRun, err := service.GetRun(ctx, owner, stringMap(lease, "runRef"))
	if err != nil {
		t.Fatal("read synthetic own-grant run cleanup")
	}
	execute(command.CancelRun, owner, "own-grant-stop", &grantRun.Version, command.RunCommandInput{RunRef: grantRun.Ref, Reason: "Synthetic own-grant scenario complete"})
	testAssistantRecipientIntegrationCatalog(t, ctx, r, service, owner, worker, project, connection.Ref, foreign.AgentRef)
	if _, err = pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "MEMBER"); err != nil {
		t.Fatal("revoke synthetic owner")
	}
	for _, version := range []int64{validated.Version, 1} {
		if _, err = service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "project-self-grant-apply", ExpectedVersion: &version}, Payload: command.AssistantPlanInput{PlanRef: validated.Ref, Revision: validated.Revision}}); !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("revoked replay beforeOCC: %v", err)
		}
	}
	if _, err = service.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, project.Ref, connection.Ref, "", query.Page{Size: 100}); !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("revoked candidates: %v", err)
	}
	if _, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), entity.AssistantConfigurationCatalogRequest{Kind: "PROJECT_INTEGRATION_GRANTS", AssistantRef: profile.AgentRef}); err == nil {
		t.Fatal("revoked/closed lease catalog returned entries")
	}
}
