package platform

import (
	"context"
	_ "embed"
	"io"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

//go:embed testdata/sql/assistant_file_body_absent.sql
var queryAssistantFileBodyAbsent string

// Сценарий использует только штатные owner/worker маршруты и disposable БД.
func testAssistantFileRevisionLifecycle(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner, worker value.Principal, oldLease map[string]any, project, agent, ref, foreignProject string) {
	t.Helper()
	execute := func(kind command.Kind, principal value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: principal,
			Mutation: value.Mutation{IdempotencyKey: "file-revision-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("file revision fixture %s/%s: %v", kind, key, err)
		}
		return result
	}
	original, err := service.GetArtifact(ctx, owner, ref)
	if err != nil || original.CurrentRevisionRef == "" {
		t.Fatalf("original immutable head: %v", err)
	}
	read := func(revision string) string {
		t.Helper()
		download, err := service.DownloadArtifactRevision(ctx, owner, ref, revision, "DOWNLOAD")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(download.Reader)
		closeErr := download.Reader.Close()
		if err != nil || closeErr != nil {
			t.Fatal("exact revision download failed")
		}
		return string(body)
	}
	oldBody := read(original.CurrentRevisionRef)
	propose := func(lease map[string]any, key string, operation entity.AssistantPlanOperation) error {
		_, err := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: "file-revision-" + key}, Payload: command.ProposeAssistantPlanInput{
				LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Summary: "Закрытая проверка", Operations: []entity.AssistantPlanOperation{operation}}})
		return err
	}
	operation := entity.AssistantPlanOperation{Key: "update-note", Type: createProjectFileRevision, Title: "Обновить заметку", Summary: "Новая неизменяемая версия",
		Parameters: map[string]any{"artifactRef": ref, "mediaType": "text/markdown", "content": "revision fixture initial body"}}
	if err := propose(oldLease, "deny-old-capability-pin", operation); err == nil {
		t.Fatal("fresh capability self-granted authority to an older immutable runtime revision")
	}
	conversation := execute(command.CreateAssistantConversation, owner, "conversation", nil, command.AssistantConversationInput{
		AssistantScope: "PROJECT", ProjectRef: project, Context: entity.AssistantContextDescriptor{EntityKind: "AGENT", EntityRef: agent},
	}).Conversation
	execute(command.AddAssistantTurn, owner, "turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Обнови существующую заметку.", DeliveryMode: "QUEUE"})
	var lease map[string]any
	for _, claim := range execute(command.ClaimExecution, worker, "claim", nil, command.LeaseInput{WorkloadInstance: "file-revision-fixture", Limit: 10}).RuntimeItems {
		if stringMap(claim, "sessionRef") == conversation.SessionRef {
			lease = claim
		}
	}
	if lease == nil {
		t.Fatal("new revision turn did not receive an exact lease")
	}
	foreign, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "file-revision-foreign"}, port.ArtifactUpload{
		ProjectRef: foreignProject, FileName: original.FileName, MediaType: "text/markdown", SizeBytes: 7, Reader: strings.NewReader("foreign")})
	if err != nil {
		t.Fatal(err)
	}
	badForeign := operation
	badForeign.Parameters = map[string]any{"artifactRef": foreign.Ref, "mediaType": "text/markdown", "content": "forbidden"}
	if propose(lease, "deny-foreign-project", badForeign) == nil {
		t.Fatal("project-global source escaped its exact target project")
	}
	plan := executeWorkerAssistantPlan(t, ctx, service, worker, lease, "file-revision-propose", operation).Plan
	if plan == nil || assistantString(plan.Operations[0].Parameters, "contentRef") == "" || plan.ConversationRef != conversation.Ref ||
		assistantString(plan.Operations[0].Before, "currentRevisionRef") != original.CurrentRevisionRef {
		t.Fatal("revision proposal lost source conversation, staging receipt or immutable before")
	}
	edited := plan.Operations[0]
	edited.Parameters = map[string]any{"artifactRef": ref, "mediaType": "text/markdown", "content": "revision fixture owner edited body"}
	draft := execute(command.UpdateAssistantPlan, owner, "draft", &plan.Version, command.AssistantPlanDraftInput{
		PlanRef: plan.Ref, Summary: plan.Summary, Operations: []entity.AssistantPlanOperation{edited}}).Plan
	if draft == nil || draft.Revision != plan.Revision+1 || assistantString(draft.Operations[0].After, "digest") == assistantString(plan.Operations[0].After, "digest") {
		t.Fatal("owner body edit did not create a newly staged canonical plan revision")
	}
	validated := execute(command.ValidateAssistantPlan, owner, "validate", &draft.Version, command.AssistantPlanInput{PlanRef: draft.Ref, Revision: draft.Revision}).Plan
	if validated == nil || validated.State != "VALID" {
		t.Fatalf("prepared revision did not validate: %#v", validated)
	}
	applyInput := command.AssistantPlanInput{PlanRef: validated.Ref, Revision: validated.Revision}
	applied := execute(command.ApplyAssistantPlan, owner, "apply", &validated.Version, applyInput)
	if applied.PlanReceipt == nil || applied.PlanReceipt.Outcome != "APPLIED" || len(applied.CreatedRefs) != 0 || len(applied.PlanReceipt.CreatedResourceRefs) != 0 ||
		len(applied.PlanReceipt.Operations) != 1 || applied.PlanReceipt.Operations[0].ArtifactRevision == nil {
		t.Fatal("stable artifact revision did not return immutable receipt or claimed a duplicate resource")
	}
	revision := applied.PlanReceipt.Operations[0].ArtifactRevision
	head, err := service.GetArtifact(ctx, owner, ref)
	if err != nil || head.Ref != original.Ref || head.CurrentRevisionRef != revision.Ref || head.Version != original.Version+1 || head.Revision != original.Revision+1 {
		t.Fatal("forward-only head did not advance exactly once")
	}
	if read(original.CurrentRevisionRef) != oldBody || read(revision.Ref) != "revision fixture owner edited body" {
		t.Fatal("head update changed historical bytes or new revision read")
	}
	replayed := execute(command.ApplyAssistantPlan, owner, "apply", &validated.Version, applyInput)
	if replayed.PlanReceipt == nil || replayed.PlanReceipt.Ref != applied.PlanReceipt.Ref || replayed.PlanReceipt.Operations[0].ArtifactRevision.Ref != revision.Ref {
		t.Fatal("idempotency replay created a second revision")
	}
	items, total, token, err := service.ListArtifactRevisions(ctx, owner, ref, query.Page{Size: 1})
	if err != nil || total != 2 || len(items) != 1 || items[0].Ref != revision.Ref || token == "" {
		t.Fatal("immutable history first page is not exact descending")
	}
	items, total, token, err = service.ListArtifactRevisions(ctx, owner, ref, query.Page{Size: 1, Token: token})
	if err != nil || total != 2 || len(items) != 1 || items[0].Ref != original.CurrentRevisionRef || token != "" {
		t.Fatal("immutable history continuation lost exact original")
	}
	if _, err := service.GetArtifactRevision(ctx, owner, ref, "arv_foreign_revision"); err == nil {
		t.Fatal("foreign revision lookup was not closed")
	}
	if _, err := service.DownloadArtifactRevision(ctx, owner, ref, "", "DOWNLOAD"); err == nil {
		t.Fatal("missing revision fell back to latest")
	}
	var bodyMatches int64
	if err := r.pool.QueryRow(ctx, queryAssistantFileBodyAbsent, "revision fixture").Scan(&bodyMatches); err != nil || bodyMatches != 0 {
		t.Fatalf("raw file body persisted in plan/receipt/audit/outbox: matches=%d err=%v", bodyMatches, err)
	}
	configuration, err := service.GetAgent(ctx, owner, agent)
	if err != nil || !contains(configuration.Capabilities, runtimecontract.ArtifactCapability) {
		t.Fatal("native revision changed helper capability")
	}
	// Открытый SOURCE экран не расширяет прежнюю CREATE_PROJECT_FILE границу.
	bad := operation
	bad.Type, bad.Parameters = "CREATE_PROJECT_FILE", map[string]any{"projectRef": project, "fileName": "forbidden.md", "mediaType": "text/markdown", "content": "forbidden"}
	if propose(lease, "deny-old-create", bad) == nil {
		t.Fatal("new project-global revision boundary expanded old create screen gate")
	}
}
