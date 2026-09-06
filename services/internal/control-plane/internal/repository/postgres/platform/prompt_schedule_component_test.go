package platform

import (
	"context"
	"strings"
	"testing"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func bindScheduleTemplateFixture(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, projectRef, scheduleRef, key, content string) string {
	t.Helper()
	execute := func(kind command.Kind, version *int64, payload command.ManagedConfigurationInput) command.Result {
		result, err := executePromptPublicationFixture(t, ctx, service, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-" + string(kind), ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("schedule template %s: %v", kind, err)
		}
		return result
	}
	created := execute(command.CreatePromptTemplateDraft, nil, command.ManagedConfigurationInput{ProjectRef: projectRef, Name: key, ContentFormat: "TEXT", Content: content})
	input := command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref, RevisionRef: created.ManagedRevision.Ref}
	validated := execute(command.ValidatePromptTemplateDraft, &created.ManagedConfiguration.Version, input)
	published := execute(command.PublishPromptTemplateDraft, &validated.ManagedConfiguration.Version, input)
	impact, err := service.GetManagedConfigurationImpact(ctx, owner, input.ConfigurationRef, input.RevisionRef, query.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	input.ImpactDigest = impact.Digest
	input.Consumers = []entity.ManagedConfigurationConsumer{{Kind: "SCHEDULE", Ref: scheduleRef}}
	execute(command.RebindPromptTemplate, &published.ManagedConfiguration.Version, input)
	return created.ManagedRevision.Ref
}

func checkCapturedScheduleRuntime(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, runRef, templateRef string) {
	t.Helper()
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "schedule-captured-runtime"}, Payload: command.LeaseInput{WorkloadInstance: "schedule-template-runtime", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim captured schedule prompt: %v", err)
	}
	lease := claimed.RuntimeItems[0]
	if stringMap(lease, "runRef") != runRef || !strings.HasPrefix(stringMap(lease, "task"), "Captured ") || !strings.Contains(stringMap(lease, "task"), runRef) {
		t.Fatalf("captured root task lost immutable template or fresh run: %q", stringMap(lease, "task"))
	}
	snapshot, ok := lease["promptSnapshot"].(entity.PromptMaterializationSnapshot)
	if !ok || len(snapshot.ExtraTemplates) != 1 || snapshot.ExtraTemplates[0].Ref != templateRef || snapshot.ExtraTemplates[0].Rendered == nil || snapshot.ExtraTemplates[0].Rendered.Content != stringMap(lease, "task") {
		t.Fatal("captured automation provenance lost")
	}
	var task string
	if err := repository.pool.QueryRow(ctx, `SELECT turn.content FROM control_plane.session_turns turn JOIN control_plane.runs run ON run.id=turn.run_id WHERE run.ref=$1`, runRef).Scan(&task); err != nil || task != stringMap(lease, "task") {
		t.Fatalf("root turn task was not committed with revision: %v", err)
	}
}
