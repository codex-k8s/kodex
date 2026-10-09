package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Оснастка вызывается штатной suite PROJECT-профиля; provider и живые данные не используются.
func testAssistantArtifactSearch(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner, worker, reader value.Principal, lease map[string]any, foreignProject string) {
	t.Helper()
	project, agent := stringMap(lease, "projectRef"), stringMap(lease, "agentRef")
	name := "Заметка проверки поиска.md"
	execute := func(kind command.Kind, principal value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: principal,
			Mutation: value.Mutation{IdempotencyKey: "artifact-search-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("artifact search fixture %s: %v", kind, err)
		}
		return result
	}
	// CREATE_PROJECT_FILE разрешён на обзоре проекта, не на исходном экране агента.
	creator := execute(command.CreateAssistantConversation, owner, "creator-conversation", nil, command.AssistantConversationInput{
		AssistantScope: "PROJECT", ProjectRef: project, Context: entity.AssistantContextDescriptor{EntityKind: "PROJECT", EntityRef: project},
	}).Conversation
	execute(command.AddAssistantTurn, owner, "creator-turn", nil, command.AssistantTurnInput{ConversationRef: creator.Ref, Content: "Подготовь заметку проекта.", DeliveryMode: "QUEUE"})
	for _, claim := range execute(command.ClaimExecution, worker, "creator-claim", nil, command.LeaseInput{WorkloadInstance: "artifact-search-fixture", Limit: 10}).RuntimeItems {
		if stringMap(claim, "sessionRef") == creator.SessionRef {
			lease = claim
		}
	}
	if stringMap(lease, "sessionRef") != creator.SessionRef {
		t.Fatal("project overview creator did not receive an exact lease")
	}
	plan := executeWorkerAssistantPlan(t, ctx, service, worker, lease, "artifact-search-plan", entity.AssistantPlanOperation{
		Key: "project-file", Type: "CREATE_PROJECT_FILE", Title: "Создать заметку", Summary: "Создать файл после подтверждения",
		Parameters: map[string]any{"projectRef": project, "fileName": name, "mediaType": "text/markdown", "content": "# Безопасная синтетическая заметка\n"},
	}).Plan
	validated := execute(command.ValidateAssistantPlan, owner, "validate", &plan.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision})
	if validated.Plan == nil || len(validated.Plan.ValidationProblems) != 0 {
		t.Fatal("project file plan did not validate")
	}
	applied := execute(command.ApplyAssistantPlan, owner, "apply", &validated.Plan.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: validated.Plan.Revision})
	if applied.PlanReceipt == nil || applied.PlanReceipt.Outcome != "APPLIED" || len(applied.PlanReceipt.CreatedResourceRefs) != 1 {
		t.Fatal("approved project file did not return an exact receipt")
	}
	ref := applied.PlanReceipt.CreatedResourceRefs[0]
	// Такой же display name в другом проекте не расширяет область PROJECT lease.
	if _, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "artifact-search-foreign"}, port.ArtifactUpload{
		ProjectRef: foreignProject, FileName: name, MediaType: "text/markdown", SizeBytes: 4, Reader: strings.NewReader("safe"),
	}); err != nil {
		t.Fatal(err)
	}
	search := func(execution map[string]any) ([]entity.SearchResult, error) {
		items, truncated, err := service.SearchAssistantResources(ctx, reader, stringMap(execution, "leaseRef"), stringMap(execution, "fence"), runtimeRevisionMapInt64(execution, "generation"), name)
		if truncated {
			t.Fatal("small metadata fixture unexpectedly truncated")
		}
		return items, err
	}
	items, err := search(lease)
	if err != nil || len(items) != 1 || items[0].Kind != "ARTIFACT" || items[0].Ref != ref || items[0].ProjectRef != project || items[0].Title != name || items[0].Subtitle != "text/markdown" {
		t.Fatalf("approved artifact metadata unavailable or foreign scope escaped: count=%d err=%v", len(items), err)
	}
	// Создание и поиск файла не дают ни capability, ни runtime catalog.
	profile, err := service.GetAgent(ctx, owner, agent)
	if err != nil || contains(profile.Capabilities, runtimecontract.ArtifactCapability) || lease["fileCatalog"] != nil {
		t.Fatal("metadata discovery granted file execution authority")
	}
	identity := port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000001797", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Artifact metadata exact reader", CallerWorkload: "control-api-gateway", Operation: "platform.query.projects.get", ProjectRef: project}
	if _, err := r.ResolveProofAuthority(ctx, identity); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unbound synthetic reader unexpectedly authorized")
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: identity.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatal("synthetic reader registration failed")
	}
	bind := func(key string, permissions []string, scope entity.AccessScope) *entity.AccessBinding {
		t.Helper()
		role := execute(command.CreateAccessRole, owner, key+"-role", nil, command.AccessRoleInput{Name: key, PermissionKeys: permissions, AllowedScopes: []string{"RESOURCE_INSTANCE"}, ChangeComment: "Синтетическая проверка точного чтения"}).AccessRole
		return execute(command.CreateAccessBinding, owner, key+"-binding", nil, command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.CurrentVersion.Ref, Scope: scope}).AccessBinding
	}
	bind("artifact-search-project", []string{"project.view"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: project, ResourceKind: "PROJECT", ResourceRef: project})
	bind("artifact-search-agent", []string{"agent.view", "agent.launch"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: project, ResourceKind: "AGENT", ResourceRef: agent})
	visibility := bind("artifact-search-file", []string{"artifact.view"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: project, ResourceKind: "ARTIFACT", ResourceRef: ref})
	actor := resolvedTestPrincipal(t, ctx, r, identity, "control-api-gateway")
	conversation := execute(command.CreateAssistantConversation, actor, "reader-conversation", nil, command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: project}).Conversation
	execute(command.AddAssistantTurn, actor, "reader-turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Найди созданную заметку и покажи ссылку.", DeliveryMode: "QUEUE"})
	claims := execute(command.ClaimExecution, worker, "reader-claim", nil, command.LeaseInput{WorkloadInstance: "artifact-search-fixture", Limit: 10}).RuntimeItems
	var readerLease map[string]any
	for _, claim := range claims {
		if stringMap(claim, "sessionRef") == conversation.SessionRef {
			readerLease = claim
		}
	}
	if readerLease == nil {
		t.Fatal("reader assistant did not receive an exact lease")
	}
	items, err = search(readerLease)
	if err != nil || len(items) != 1 || items[0].Ref != ref || readerLease["fileCatalog"] != nil {
		t.Fatalf("exact metadata-only reader lost file or gained catalog: count=%d err=%v", len(items), err)
	}
	execute(command.RevokeAccessBinding, owner, "revoke-view", &visibility.Version, command.AccessBindingInput{BindingRef: visibility.Ref})
	items, err = search(readerLease)
	if err != nil || len(items) != 0 {
		t.Fatalf("revoked artifact view retained metadata: count=%d err=%v", len(items), err)
	}
	if _, _, err := service.SearchAssistantResources(ctx, reader, stringMap(readerLease, "leaseRef"), "wrong-fence", runtimeRevisionMapInt64(readerLease, "generation"), name); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("stale artifact search lease accepted")
	}
}
