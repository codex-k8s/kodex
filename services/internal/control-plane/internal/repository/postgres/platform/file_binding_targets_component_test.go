package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testFileBindingTargets(t *testing.T, ctx context.Context, repository *Repository) {
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(input command.Command) command.Result {
		t.Helper()
		if input.Principal.ActorID == "" {
			input.Principal = owner
		}
		result, err := service.Execute(ctx, input)
		if err != nil {
			t.Fatalf("%s: %v", input.Kind, err)
		}
		return result
	}
	project := execute(command.Command{Kind: command.CreateProject, Mutation: value.Mutation{IdempotencyKey: "file-target-project"}, Payload: command.ProjectInput{Name: "File target scope", Purpose: "Protected file binding targets", Language: "en"}}).Project
	a := createLifecycleAgent(t, ctx, service, owner, project.Ref, "file-target-a", "File target A")
	b := createLifecycleAgent(t, ctx, service, owner, project.Ref, "file-target-b", "File target B")
	changeCapability := func(ref, key string, enabled bool) {
		t.Helper()
		agent, err := service.GetAgent(ctx, owner, ref)
		if err != nil {
			t.Fatal(err)
		}
		execute(command.Command{Kind: command.ChangeAgentCapability, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &agent.Version}, Payload: command.AgentBindingInput{AgentRef: ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: enabled}})
	}
	changeAgent := func(ref, key string, kind command.Kind, enabled bool) {
		t.Helper()
		agent, err := service.GetAgent(ctx, owner, ref)
		if err != nil {
			t.Fatal(err)
		}
		execute(command.Command{Kind: kind, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &agent.Version}, Payload: command.AgentInput{Ref: ref, Enabled: enabled}})
	}
	changeCapability(a.Ref, "file-target-a-files", true)
	changeCapability(b.Ref, "file-target-b-files", true)
	testRunAttachmentTargets(t, ctx, service, owner, project.Ref, a.Ref, b.Ref)
	changeAgent(a.Ref, "file-target-a-disable", command.SetAgentEnabled, false)
	const content = "File target fixture"
	artifact, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "file-target-upload"}, platformrepo.ArtifactUpload{ProjectRef: project.Ref, FileName: "file-target.txt", MediaType: "text/plain", SizeBytes: int64(len(content)), Reader: strings.NewReader(content)})
	if err != nil {
		t.Fatal(err)
	}
	read := func(p value.Principal, filter query.Filter) entity.ArtifactBindingTargets {
		t.Helper()
		result, err := service.ListArtifactBindingTargets(ctx, p, artifact.Ref, filter)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	find := func(page entity.ArtifactBindingTargets, ref string) entity.ArtifactBindingTarget {
		t.Helper()
		for _, item := range page.Items {
			if item.AgentRef == ref {
				return item
			}
		}
		t.Fatalf("missing target %s", ref)
		return entity.ArtifactBindingTarget{}
	}
	page := read(owner, query.Filter{})
	if page.Total != 2 || !find(page, a.Ref).CanBind || find(page, a.Ref).State != "DISABLED" {
		t.Fatalf("configured target without ready runtime was denied: %+v", page)
	}
	first := read(owner, query.Filter{Page: query.Page{Size: 1}})
	if first.Total != 2 || len(first.Items) != 1 || first.NextPageToken == "" {
		t.Fatalf("first page: %+v", first)
	}
	second := read(owner, query.Filter{Page: query.Page{Size: 1, Token: first.NextPageToken}})
	if second.Total != 2 || len(second.Items) != 1 || second.Items[0].AgentRef == first.Items[0].AgentRef || second.NextPageToken != "" {
		t.Fatalf("second page: %+v", second)
	}
	if read(owner, query.Filter{Query: "%"}).Total != 0 {
		t.Fatal("literal search became wildcard")
	}
	foreignProject := execute(command.Command{Kind: command.CreateProject, Mutation: value.Mutation{IdempotencyKey: "file-target-foreign-project"}, Payload: command.ProjectInput{Name: "Foreign file targets", Purpose: "Project boundary", Language: "en"}}).Project
	foreignAgent := createLifecycleAgent(t, ctx, service, owner, foreignProject.Ref, "file-target-foreign-agent", "Foreign file target")
	stale := int64(999)
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeArtifactBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "file-target-foreign-before-occ", ExpectedVersion: &stale}, Payload: command.ArtifactBindingInput{ArtifactRef: artifact.Ref, AgentRef: foreignAgent.Ref, Enabled: false}}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign target passed before OCC: %v", err)
	}
	if _, err := service.ListArtifactBindingTargets(ctx, owner, artifact.Ref, query.Filter{Query: "A", Page: query.Page{Token: first.NextPageToken}}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("query replay: %v", err)
	}
	bindCommand := func(p value.Principal, ref, key string, enabled bool) command.Command {
		t.Helper()
		latest, err := service.GetArtifact(ctx, owner, artifact.Ref)
		if err != nil {
			t.Fatal(err)
		}
		return command.Command{Kind: command.ChangeArtifactBinding, Principal: p, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &latest.Version}, Payload: command.ArtifactBindingInput{ArtifactRef: artifact.Ref, AgentRef: ref, Enabled: enabled}}
	}
	execute(bindCommand(owner, a.Ref, "file-target-bind-disabled", true))
	if _, err := service.ListArtifactBindingTargets(ctx, owner, artifact.Ref, query.Filter{Page: query.Page{Token: first.NextPageToken}}); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale artifact cursor: %v", err)
	}
	first = read(owner, query.Filter{Page: query.Page{Size: 1}})
	changeCapability(a.Ref, "file-target-a-revoke", false)
	if _, err := service.ListArtifactBindingTargets(ctx, owner, artifact.Ref, query.Filter{Page: query.Page{Token: first.NextPageToken}}); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale capability cursor: %v", err)
	}
	item := find(read(owner, query.Filter{}), a.Ref)
	if item.CanBind || !item.CanUnbind || item.BindReason != fileTargetCapabilityRequired {
		t.Fatalf("revoked capability cleanup: %+v", item)
	}
	changeAgent(a.Ref, "file-target-a-archive", command.ArchiveAgent, false)
	item = find(read(owner, query.Filter{}), a.Ref)
	if item.State != "ARCHIVED" || !item.Bound || !item.CanUnbind || item.CanBind {
		t.Fatalf("archive tombstone: %+v", item)
	}
	unbind := bindCommand(owner, a.Ref, "file-target-unbind-archive", false)
	execute(unbind)
	execute(unbind)
	if read(owner, query.Filter{}).Total != 1 {
		t.Fatal("unbound archive remained in catalog")
	}
	if _, err := service.Execute(ctx, bindCommand(owner, a.Ref, "file-target-rebind-archive", true)); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("archived target accepted new binding: %v", err)
	}

	input := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000004067", ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: "File target reader", CallerWorkload: "control-api-gateway", Operation: "platform.query.artifacts.list"}
	if _, err := repository.ResolveProofAuthority(ctx, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unbound reader: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: input.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("reader registration: %v", err)
	}
	accessBinding := func(key string, permissions []string, kind, ref string) entity.AccessBinding {
		t.Helper()
		role := execute(command.Command{Kind: command.CreateAccessRole, Mutation: value.Mutation{IdempotencyKey: key + "-role"}, Payload: command.AccessRoleInput{Name: key, PermissionKeys: permissions, AllowedScopes: []string{"RESOURCE_INSTANCE"}, ChangeComment: "File target fixture"}}).AccessRole
		return *execute(command.Command{Kind: command.CreateAccessBinding, Mutation: value.Mutation{IdempotencyKey: key + "-binding"}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: project.Ref, ResourceKind: kind, ResourceRef: ref}}}).AccessBinding
	}
	accessBinding("file-target-reader", []string{"artifact.view"}, "ARTIFACT", artifact.Ref)
	reader := resolvedTestPrincipal(t, ctx, repository, input, "control-api-gateway")
	if _, err := service.ListArtifactBindingTargets(ctx, reader, artifact.Ref, query.Filter{}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("readonly actor could manage bindings: %v", err)
	}
	bindingPermission := accessBinding("file-target-manager", []string{"artifact.bind"}, "ARTIFACT", artifact.Ref)
	if read(reader, query.Filter{}).Total != 0 {
		t.Fatal("hidden agent leaked into count")
	}
	visibility := accessBinding("file-target-agent-view", []string{"agent.view"}, "AGENT", b.Ref)
	if page := read(reader, query.Filter{}); page.Total != 1 || !find(page, b.Ref).CanBind {
		t.Fatalf("exact target visibility: %+v", page)
	}
	mutation := bindCommand(reader, b.Ref, "file-target-reader-bind", true)
	execute(mutation)
	execute(command.Command{Kind: command.RevokeAccessBinding, Mutation: value.Mutation{IdempotencyKey: "file-target-revoke-view", ExpectedVersion: &visibility.Version}, Payload: command.AccessBindingInput{BindingRef: visibility.Ref}})
	if _, err := service.Execute(ctx, mutation); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("revoked visibility replayed receipt: %v", err)
	}
	if read(reader, query.Filter{}).Total != 0 {
		t.Fatal("revoked visibility remained in catalog")
	}
	hiddenBindings, err := service.GetArtifact(ctx, reader, artifact.Ref)
	if err != nil || len(hiddenBindings.Bindings) != 0 {
		t.Fatalf("single artifact leaked hidden binding: %v %+v", err, hiddenBindings.Bindings)
	}
	listed, _, _, err := service.ListArtifacts(ctx, reader, query.Filter{ProjectRef: project.Ref})
	if err != nil || len(listed) != 1 || len(listed[0].Bindings) != 0 {
		t.Fatalf("artifact list leaked hidden binding: %v", err)
	}
	resolvedReader, err := repository.ResolvePrincipal(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, resolvedReader)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	historical := hiddenBindings
	historical.Bindings = []string{b.Ref}
	eventArtifact := historical
	event := entity.RunEvent{}
	event.Delta.Artifact = &eventArtifact
	if err := projectArtifactResults(ctx, tx, current, &command.Result{Artifact: &historical, Event: &event}); err != nil {
		t.Fatal(err)
	}
	if len(historical.Bindings) != 0 || len(event.Delta.Artifact.Bindings) != 0 {
		t.Fatal("historical result/event leaked hidden binding")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetRunAttachmentEligibility(ctx, reader, project.Ref, entity.RunTarget{Type: "AGENT", Ref: b.Ref}, ""); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("readonly actor got launch eligibility: %v", err)
	}
	execute(command.Command{Kind: command.RevokeAccessBinding, Mutation: value.Mutation{IdempotencyKey: "file-target-revoke-bind", ExpectedVersion: &bindingPermission.Version}, Payload: command.AccessBindingInput{BindingRef: bindingPermission.Ref}})
	accessBinding("file-target-readonly-launch", []string{"agent.view", "agent.launch"}, "AGENT", b.Ref)
	accessBinding("file-target-readonly-download", []string{"artifact.download"}, "ARTIFACT", artifact.Ref)
	readonlyEligibility, err := service.GetRunAttachmentEligibility(ctx, reader, project.Ref, entity.RunTarget{Type: "AGENT", Ref: b.Ref}, "")
	if err != nil || !readonlyEligibility.Eligible {
		t.Fatalf("readonly input actor required full write bundle: %v %+v", err, readonlyEligibility)
	}
}

func testRunAttachmentTargets(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, projectRef, stepAgent, coordinator string) {
	t.Helper()
	read := func(target entity.RunTarget) entity.RunAttachmentEligibility {
		t.Helper()
		result, err := service.GetRunAttachmentEligibility(ctx, owner, projectRef, target, "")
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	agentTarget := entity.RunTarget{Type: "AGENT", Ref: coordinator}
	before := read(agentTarget)
	if !before.Eligible || before.Reason != fileTargetAvailable || before.Digest == "" {
		t.Fatalf("ready agent attachment eligibility: %+v", before)
	}
	coordinatorAgent, err := service.GetAgent(ctx, owner, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "file-target-coordinator-delegate", ExpectedVersion: &coordinatorAgent.Version}, Payload: command.AgentBindingInput{AgentRef: coordinator, BindingRef: "platform.run.delegate", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	draft := entity.WorkflowVersion{Ref: "draft", Name: "File attachment workflow", Purpose: "Aggregate file target eligibility", CoordinatorAgentRef: coordinator, VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600, CompletionCriteria: "One result", ResultSchema: map[string]any{},
		Steps: []entity.WorkflowStep{{Key: "execute", Position: 1, Name: "Execute", AgentRef: stepAgent, Instructions: "Read the assigned immutable input", ExpectedResult: "One result", TimeoutSeconds: 60, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}}}}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "file-target-workflow"}, Payload: command.WorkflowInput{ProjectRef: projectRef, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: coordinator, Draft: &draft}})
	if err != nil {
		t.Fatal(err)
	}
	validated, err := service.Execute(ctx, command.Command{Kind: command.ValidateWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "file-target-workflow-validate", ExpectedVersion: &created.Workflow.Version}, Payload: command.WorkflowInput{Ref: created.Workflow.Ref}})
	if err != nil || validated.Workflow == nil || validated.Workflow.State != "VALID" {
		t.Fatalf("validate attachment workflow: %v", err)
	}
	published, err := service.Execute(ctx, command.Command{Kind: command.PublishWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "file-target-workflow-publish", ExpectedVersion: &validated.Workflow.Version}, Payload: command.WorkflowInput{Ref: created.Workflow.Ref}})
	if err != nil {
		t.Fatal(err)
	}
	target := entity.RunTarget{Type: "WORKFLOW", Ref: published.Workflow.Ref}
	page := read(target)
	if !page.Eligible || page.WorkflowVersionRef == "" {
		t.Fatalf("ready aggregate: %+v", page)
	}
	agent, err := service.GetAgent(ctx, owner, stepAgent)
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := service.Execute(ctx, command.Command{Kind: command.SetAgentEnabled, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "file-target-step-disable", ExpectedVersion: &agent.Version}, Payload: command.AgentInput{Ref: stepAgent, Enabled: false}})
	if err != nil {
		t.Fatal(err)
	}
	blocked := read(target)
	if blocked.Eligible || blocked.Reason != runAttachmentRuntimeNotReady || blocked.Digest == page.Digest {
		t.Fatalf("partial aggregate remained ready: %+v", blocked)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.SetAgentEnabled, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "file-target-step-enable", ExpectedVersion: &disabled.Agent.Version}, Payload: command.AgentInput{Ref: stepAgent, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if !read(target).Eligible {
		t.Fatal("restored aggregate did not recover")
	}
	agent, err = service.GetAgent(ctx, owner, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "file-target-coordinator-revoke", ExpectedVersion: &agent.Version}, Payload: command.AgentBindingInput{AgentRef: coordinator, BindingRef: runtimecontract.ArtifactCapability, Enabled: false}})
	if err != nil {
		t.Fatal(err)
	}
	if item := read(target); item.Eligible || item.Reason != fileTargetCapabilityRequired {
		t.Fatalf("aggregate ignored coordinator opt-in: %+v", item)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "file-target-coordinator-restore", ExpectedVersion: &revoked.Agent.Version}, Payload: command.AgentBindingInput{AgentRef: coordinator, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
}
