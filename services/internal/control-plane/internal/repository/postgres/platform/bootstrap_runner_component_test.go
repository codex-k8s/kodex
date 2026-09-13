package platform

import (
	"context"
	"strings"
	"testing"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testBootstrapRunnerAdvancesForNewAgent(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Bootstrap runner owner", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct bootstrap runner service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "bootstrap-runner-project"},
		Payload:  command.ProjectInput{Name: "Bootstrap runner project", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create bootstrap runner project: project=%#v err=%v", project.Project, err)
	}
	first := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "bootstrap-runner-first", "First runner agent")
	firstConfiguration, err := service.GetAgentRuntimeConfiguration(ctx, owner, first.Ref)
	if err != nil {
		t.Fatalf("read first bootstrap runner: %v", err)
	}
	original := repository.roleImages
	next := original
	next.DefaultImageReference = "registry.invalid/kodex/agent-runner@sha256:" + strings.Repeat("f", 64)
	if err := repository.ConfigureRoleImages(next); err != nil {
		t.Fatalf("configure next bootstrap runner: %v", err)
	}
	next = repository.roleImages
	defer func() {
		if restoreErr := repository.ConfigureRoleImages(original); restoreErr != nil {
			t.Errorf("restore bootstrap runner: %v", restoreErr)
		}
	}()
	second := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "bootstrap-runner-second", "Second runner agent")
	secondConfiguration, err := service.GetAgentRuntimeConfiguration(ctx, owner, second.Ref)
	if err != nil {
		t.Fatalf("read second bootstrap runner: %v", err)
	}
	firstReadback, err := service.GetAgentRuntimeConfiguration(ctx, owner, first.Ref)
	if err != nil {
		t.Fatalf("read preserved bootstrap runner pin: %v", err)
	}
	if firstConfiguration.Environment.CurrentVersion.Image.Digest != original.DefaultImageDigest ||
		firstReadback.EnvironmentBinding.VersionRef != firstConfiguration.EnvironmentBinding.VersionRef ||
		firstReadback.Environment.CurrentVersion.Image.Digest != original.DefaultImageDigest {
		t.Fatalf("existing agent bootstrap pin changed: before=%#v after=%#v", firstConfiguration.EnvironmentBinding, firstReadback.EnvironmentBinding)
	}
	if secondConfiguration.Environment.CurrentVersion.Image.Reference != next.DefaultImageReference ||
		secondConfiguration.Environment.CurrentVersion.Image.Digest != next.DefaultImageDigest ||
		secondConfiguration.EnvironmentBinding.VersionRef == firstConfiguration.EnvironmentBinding.VersionRef {
		t.Fatalf("new agent did not select next bootstrap runner: binding=%#v image=%#v", secondConfiguration.EnvironmentBinding, secondConfiguration.Environment.CurrentVersion.Image)
	}
}
