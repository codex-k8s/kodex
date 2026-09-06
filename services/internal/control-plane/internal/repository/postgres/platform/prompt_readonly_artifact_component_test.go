package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
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

func testReadonlyArtifactClaim(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner, worker, reader value.Principal, projectRef, agentRef, setRef, artifactRef string) {
	t.Helper()
	for _, write := range []bool{false, true} {
		testArtifactReadWriteBoundary(t, ctx, repository, service, owner, worker, reader, projectRef, agentRef, setRef, artifactRef, write)
	}
}

func testArtifactReadWriteBoundary(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner, worker, reader value.Principal, projectRef, agentRef, setRef, artifactRef string, write bool) {
	t.Helper()
	prefix := "readonly-context-"
	actorID := "20000000-0000-4000-8000-000000006419"
	if write {
		prefix = "revoked-upload-context-"
		actorID = "20000000-0000-4000-8000-000000006420"
	}
	identity := platformrepo.ProofPrincipalInput{ExternalActorID: actorID, ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: prefix + "actor", CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch"}
	if _, err := repository.ResolveProofAuthority(ctx, identity); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unbound readonly actor: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: identity.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("readonly actor registration: %v", err)
	}
	permissions := []string{"project.view", "agent.view", "agent.launch", "run.view", "artifact.view", "artifact.download"}
	if write {
		permissions = append(permissions, "artifact.bind", "artifact.delete")
	}
	role, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: prefix + "role"}, Payload: command.AccessRoleInput{Name: prefix + "role", PermissionKeys: permissions, AllowedScopes: []string{"PROJECT"}, ChangeComment: "Readonly selected context fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: prefix + "binding"}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.AccessRole.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "PROJECT", ProjectRef: projectRef}}})
	if err != nil {
		t.Fatal(err)
	}
	var uploadBinding *entity.AccessBinding
	if write {
		role, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "upload-role"}, Payload: command.AccessRoleInput{Name: prefix + "upload", PermissionKeys: []string{"artifact.upload"}, AllowedScopes: []string{"PROJECT"}, ChangeComment: "Exact output upload fixture"}})
		if err != nil {
			t.Fatal(err)
		}
		bound, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "upload-bind"}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.AccessRole.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "PROJECT", ProjectRef: projectRef}}})
		if err != nil {
			t.Fatal(err)
		}
		uploadBinding = bound.AccessBinding
	}
	actor := resolvedTestPrincipal(t, ctx, repository, identity, "control-api-gateway")
	launch, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: actor,
		Mutation: value.Mutation{IdempotencyKey: prefix + "launch"}, Payload: command.LaunchRunInput{
			ProjectRef: projectRef, Title: "Read selected context", Task: "Read the selected immutable context.",
			Target: entity.RunTarget{Type: "AGENT", Ref: agentRef}, AttachmentSetRef: setRef}})
	if err != nil || launch.Run == nil {
		t.Fatalf("launch readonly context: %v", err)
	}
	setOptIn := func(enabled bool, key string) {
		t.Helper()
		agent, err := service.GetAgent(ctx, owner, agentRef)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &agent.Version}, Payload: command.AgentBindingInput{AgentRef: agentRef, BindingRef: runtimecontract.ArtifactCapability, Enabled: enabled}})
		if err != nil {
			t.Fatal(err)
		}
	}
	setOptIn(false, prefix+"optin-revoke")
	deniedClaim, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: prefix + "optin-denied"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-readonly", Limit: 1}})
	if err != nil || len(deniedClaim.RuntimeItems) != 0 {
		t.Fatalf("new claim ignored removed Agent opt-in: %v", err)
	}
	failedRun, err := service.GetRun(ctx, actor, launch.Run.Ref)
	if err != nil || failedRun.State != "FAILED" {
		t.Fatalf("revoked opt-in left a nonterminal run: %v", err)
	}
	setOptIn(true, prefix+"optin-restore")
	launch, err = service.Execute(ctx, command.Command{Kind: command.RetryRun, Principal: actor,
		Mutation: value.Mutation{IdempotencyKey: prefix + "retry-after-optin", ExpectedVersion: &failedRun.Version},
		Payload:  command.RunCommandInput{RunRef: failedRun.Ref}})
	if err != nil || launch.Run == nil {
		t.Fatalf("retry readonly context after restoring opt-in: %v", err)
	}
	claim, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: prefix + "claim"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-readonly", Limit: 1}})
	if err != nil || len(claim.RuntimeItems) != 1 {
		t.Fatalf("claim readonly context: %v", err)
	}
	lease := claim.RuntimeItems[0]
	if stringMap(lease, "runRef") != launch.Run.Ref {
		t.Fatal("claimed a different readonly context")
	}
	if values, ok := lease["capabilities"].([]string); !ok || contains(values, runtimecontract.ArtifactCapability) != write {
		t.Fatalf("readonly context gained write capability: %#v", lease["capabilities"])
	}
	files, ok := lease["artifacts"].([]map[string]any)
	if !ok || len(files) != 2 {
		t.Fatalf("readonly context lost selected files: %#v", lease["artifacts"])
	}
	read, err := service.ReadExecutionArtifact(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), artifactRef)
	if err != nil {
		t.Fatalf("read selected context without write capability: %v", err)
	}
	_, readErr := io.Copy(io.Discard, read.Reader)
	closeErr := read.Reader.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read context body: %v %v", readErr, closeErr)
	}
	if uploadBinding != nil {
		if _, err := service.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "upload-revoke", ExpectedVersion: &uploadBinding.Version}, Payload: command.AccessBindingInput{BindingRef: uploadBinding.Ref}}); err != nil {
			t.Fatal(err)
		}
	}
	content := []byte("Readonly context is not write authority.")
	digest := sha256.Sum256(content)
	_, err = service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: prefix + "output-denied"}, Payload: command.CompleteExecutionInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64), Success: true, ResultSummary: "Readonly output",
			Artifacts: []command.CompletedArtifact{{FileName: "result.txt", MediaType: "text/plain", SHA256: hex.EncodeToString(digest[:]), SizeBytes: int64(len(content)), Content: content}}}})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("readonly input expanded output authority: %v", err)
	}
	viewRole, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "view-role"}, Payload: command.AccessRoleInput{Name: prefix + "editor", PermissionKeys: []string{"project.view", "agent.view", "run.view"}, AllowedScopes: []string{"PROJECT"}, ChangeComment: "Keep editor access after file revocation"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "view-binding"}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: viewRole.AccessRole.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "PROJECT", ProjectRef: projectRef}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: prefix + "revoke", ExpectedVersion: &binding.AccessBinding.Version},
		Payload:  command.AccessBindingInput{BindingRef: binding.AccessBinding.Ref}}); err != nil {
		t.Fatal(err)
	}
	_, denied := service.ReadExecutionArtifact(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), artifactRef)
	if !errors.Is(denied, errs.ErrNotFound) {
		t.Fatalf("revoked input reader retained access: %v", denied)
	}
	catalog, err := service.ListPromptContextVariables(ctx, actor, query.Filter{Query: "files", Page: query.Page{Size: 100}, TemplateContext: &query.TemplateVariableContext{TargetKind: "AGENT", TargetRef: agentRef}})
	if err != nil || len(catalog.Variables) == 0 {
		t.Fatalf("file revoke hid authorized editor: %v", err)
	}
	for _, item := range catalog.Variables {
		if item.Available || item.Reason != "PERMISSION_REQUIRED" {
			t.Fatalf("file revoke lost disabled reason: %s", item.Name)
		}
	}
	preview, err := service.PreviewPromptTemplateWithContext(ctx, actor, "Agent {{.agent.name}}", "AGENT", agentRef, false, query.PromptPreviewContext{}, "")
	if err != nil || preview.Complete || preview.Prompt != "" {
		t.Fatalf("permission-incomplete preview claimed runtime readiness: %v", err)
	}
	completeClaimedExecution(t, ctx, service, worker, lease, prefix+"done", false)
}
