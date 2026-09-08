package platform

import (
	"context"
	_ "embed"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

//go:embed testdata/sql/effective_capabilities_missing_image.sql
var queryEffectiveCapabilitiesMissingImage string

//go:embed testdata/sql/effective_capabilities_restore_image.sql
var queryEffectiveCapabilitiesRestoreImage string

func testEffectiveCapabilityReadinessRedaction(t *testing.T, ctx context.Context, repository *Repository) {
	seedObservedCatalogFixture(t, ctx, repository)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "readiness-redaction-project"}, Payload: command.ProjectInput{Name: "Readiness redaction", Purpose: "Owner readiness survives public redaction", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "readiness-redaction-agent", "Readiness agent")
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if repository.roleImages.RoleRuntimeContractRevision == 0 || repository.roleImages.RoleRuntimeContractSHA256 == "" || !view.Environment.Ready || view.Environment.CurrentVersion.Image.RoleRuntimeContractRevision != 0 || view.Environment.CurrentVersion.Image.RoleRuntimeContractSHA256 != "" {
		t.Fatal("fixture must cross the real scanner with a current, then redacted image contract")
	}
	candidate := view.Configuration.ProviderPolicy.AccountCandidates[0]
	catalog, err := service.ListModelCatalog(ctx, owner, view.Configuration.Provider, candidate.AccountRef, query.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	candidate.CatalogRevision, candidate.CatalogDigest, candidate.ProviderDefinitionKey, candidate.DefaultReasoningEffort = catalog.Revision, catalog.Digest, view.Configuration.Provider, ""
	configured, err := service.Execute(ctx, command.Command{Kind: command.PublishAgentRuntimeConfig, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "readiness-redaction-runtime", ExpectedVersion: &view.AgentVersion}, Payload: command.AgentRuntimeConfigurationInput{AgentRef: agent.Ref, RuntimeProfileRef: view.Configuration.RuntimeProfileRef, Model: view.Configuration.Model, ProviderPolicyMode: "FIXED", ProviderAccounts: []entity.ProviderAccountCandidate{candidate}}})
	if err != nil {
		t.Fatal(err)
	}
	version := configured.RuntimeConfiguration.AgentVersion
	if _, err = service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "readiness-redaction-capability", ExpectedVersion: &version}, Payload: command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: "platform.artifact.manage", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	read := func(t *testing.T, want bool) entity.AgentEffectiveCapabilities {
		t.Helper()
		actual, err := service.GetAgentEffectiveCapabilities(ctx, owner, agent.Ref, "", "", query.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		index := slices.IndexFunc(actual.Items, func(item entity.EffectiveCapability) bool { return item.Key == "platform.artifact.manage" })
		if index < 0 || actual.RuntimeReady != want || actual.Items[index].Effective != want {
			t.Fatalf("owner readiness after scanner redaction: ready=%t want=%t", actual.RuntimeReady, want)
		}
		return actual
	}
	t.Run("current image and valid catalog remain effective after redaction", func(t *testing.T) { read(t, true) })
	for _, variant := range []string{"stale contract revision", "stale contract digest"} {
		t.Run(variant, func(t *testing.T) {
			previous := repository.roleImages
			defer func() { repository.roleImages = previous }()
			if variant == "stale contract revision" {
				repository.roleImages.RoleRuntimeContractRevision++
			} else {
				repository.roleImages.RoleRuntimeContractSHA256 = strings.Repeat("f", 64)
			}
			current, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
			if err != nil || current.Environment.Ready || !slices.Contains(current.Environment.ReadinessBlockers, "ROLE_RUNTIME_CONTRACT_STALE") {
				t.Fatalf("stale owner image readiness: %v", err)
			}
			read(t, false)
		})
	}
	t.Run("disabled agent remains blocked", func(t *testing.T) {
		before := read(t, true)
		disabled, err := service.Execute(ctx, command.Command{Kind: command.SetAgentEnabled, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "readiness-redaction-disable", ExpectedVersion: &before.AgentVersion}, Payload: command.AgentInput{Ref: agent.Ref, Enabled: false}})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := service.Execute(ctx, command.Command{Kind: command.SetAgentEnabled, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "readiness-redaction-enable", ExpectedVersion: &disabled.Agent.Version}, Payload: command.AgentInput{Ref: agent.Ref, Enabled: true}}); err != nil {
				t.Fatal(err)
			}
		}()
		read(t, false)
	})
	for _, variant := range []string{"missing catalog pin", "stale catalog pin"} {
		t.Run(variant, func(t *testing.T) {
			before := read(t, true)
			invalid := candidate
			if variant == "missing catalog pin" {
				invalid.CatalogRevision = ""
				invalid.CatalogDigest = ""
			} else {
				invalid.CatalogDigest = strings.Repeat("f", 64)
				invalid.CatalogRevision = "mcat_" + invalid.CatalogDigest
			}
			_, err := service.Execute(ctx, command.Command{Kind: command.PublishAgentRuntimeConfig, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "readiness-redaction-" + strings.ReplaceAll(variant, " ", "-"), ExpectedVersion: &before.AgentVersion}, Payload: command.AgentRuntimeConfigurationInput{AgentRef: agent.Ref, RuntimeProfileRef: view.Configuration.RuntimeProfileRef, Model: view.Configuration.Model, ProviderPolicyMode: "FIXED", ProviderAccounts: []entity.ProviderAccountCandidate{invalid}}})
			if variant == "missing catalog pin" && !errors.Is(err, errs.ErrInvalid) || variant == "stale catalog pin" && !errors.Is(err, errs.ErrVersionMismatch) {
				t.Fatalf("invalid catalog publication was accepted: %v", err)
			}
			read(t, true)
		})
	}
	t.Run("missing stored image fails closed through scanner", func(t *testing.T) {
		const missingRef = "renvv_readiness_missing_image"
		if _, err := repository.pool.Exec(ctx, queryEffectiveCapabilitiesMissingImage, owner.AuthorityTenant, agent.Ref, missingRef); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := repository.pool.Exec(ctx, queryEffectiveCapabilitiesRestoreImage, owner.AuthorityTenant, agent.Ref, missingRef); err != nil {
				t.Fatal(err)
			}
		}()
		if result, err := service.GetAgentEffectiveCapabilities(ctx, owner, agent.Ref, "", "", query.Filter{}); !errors.Is(err, errs.ErrUnavailable) || result.RuntimeReady {
			t.Fatalf("missing image did not fail closed: %v", err)
		}
	})
	t.Run("catalog drift closes previously valid readiness", func(t *testing.T) {
		read(t, true)
		t.Cleanup(func() {
			if _, err := repository.pool.Exec(ctx, queryCatalogFixtureAdvanceAccount, owner.AuthorityTenant, candidate.AccountRef); err != nil {
				t.Fatal(err)
			}
			seedObservedCatalogFixture(t, ctx, repository)
		})
		if _, err := repository.pool.Exec(ctx, queryCatalogFixtureAdvanceAccount, owner.AuthorityTenant, candidate.AccountRef); err != nil {
			t.Fatal(err)
		}
		seedObservedCatalogFixture(t, ctx, repository, func(observation *platformrepo.ProviderModelCatalogObservation) {
			observation.Models[0].DefaultReasoningEffort = "low"
		})
		changed, err := service.ListModelCatalog(ctx, owner, view.Configuration.Provider, candidate.AccountRef, query.Filter{})
		if err != nil || changed.Digest == candidate.CatalogDigest {
			t.Fatalf("catalog fixture content did not change: %v", err)
		}
		read(t, false)
	})
}
