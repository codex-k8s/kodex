// Package platform реализует authority, lifecycle, idempotency и OCC web-first control-plane.
package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/artifactpolicy"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type Service struct {
	repository                     repository.Repository
	credentialMaterializer         CredentialMaterializer
	providerCredentialMaterializer ProviderCredentialMaterializer
}

const promptFullMaterializationMaximumAuthenticationAge = 5 * time.Minute

type Option func(*Service)

func WithCredentialMaterializer(materializer CredentialMaterializer) Option {
	return func(service *Service) { service.credentialMaterializer = materializer }
}

func New(repo repository.Repository, options ...Option) (*Service, error) {
	if repo == nil {
		return nil, errors.New("platform repository is required")
	}
	service := &Service{repository: repo}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service, nil
}

func (service *Service) Bootstrap(ctx context.Context) error {
	return service.repository.Bootstrap(ctx)
}
func (service *Service) Ready(ctx context.Context) error {
	// Общая readiness подтверждает только owned state control-plane. Downstream
	// materializer имеет отдельную exact-path readiness: иначе control-plane
	// исчезает из Service endpoints до старта secret-broker, а secret-broker не
	// может проверить принадлежащий control-plane runtime-secret work path.
	return service.repository.Ready(ctx)
}

func (service *Service) ResolveProofAuthority(ctx context.Context, input repository.ProofPrincipalInput) (repository.ProofAuthority, error) {
	return service.repository.ResolveProofAuthority(ctx, input)
}

func (service *Service) AcceptWorkerGrant(ctx context.Context, input repository.WorkerGrantInput) error {
	return service.repository.AcceptWorkerGrant(ctx, input)
}

func (service *Service) NextAuthorityProofRevision(ctx context.Context) (uint64, error) {
	return service.repository.NextAuthorityProofRevision(ctx)
}

func (service *Service) principal(ctx context.Context, principal value.Principal) (value.Principal, error) {
	if err := principal.Validate(); err != nil {
		return value.Principal{}, errs.ErrUnauthorized
	}
	resolved, err := service.repository.ResolvePrincipal(ctx, principal)
	if err != nil {
		return value.Principal{}, err
	}
	return resolved, nil
}

func (service *Service) GetBootstrapState(ctx context.Context, p value.Principal) (repository.BootstrapState, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.BootstrapState{}, err
	}
	return service.repository.GetBootstrapState(ctx, p)
}
func (service *Service) GetPlatformEventCursor(ctx context.Context, p value.Principal) (string, int64, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return "", 0, err
	}
	return service.repository.GetPlatformEventCursor(ctx, p)
}
func (service *Service) GetOverview(ctx context.Context, p value.Principal, projectRef string) (repository.Overview, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.Overview{}, err
	}
	return service.repository.GetOverview(ctx, p, projectRef)
}
func (service *Service) ListCapabilities(ctx context.Context, p value.Principal) ([]entity.IntegrationCapability, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	return service.repository.ListCapabilities(ctx, p)
}
func (service *Service) ListRuntimes(ctx context.Context, p value.Principal) ([]entity.RuntimeSelection, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	return service.repository.ListRuntimes(ctx, p)
}
func (service *Service) Search(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.SearchResult, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if len([]rune(filter.Query)) < 2 || len([]rune(filter.Query)) > 200 {
		return nil, 0, "", errs.ErrInvalid
	}
	if filter.Page.Size == 0 {
		filter.Page.Size = filter.Limit
	}
	return service.repository.Search(ctx, p, filter)
}
func (service *Service) ListVFSNodes(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.VFSNode, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.ResourceRef == "" {
		filter.ResourceRef = "/projects"
	}
	if !strings.HasPrefix(filter.ResourceRef, "/") || strings.Contains(filter.ResourceRef, "..") || len(filter.ResourceRef) > 1000 {
		return nil, 0, "", errs.ErrInvalid
	}
	return service.repository.ListVFSNodes(ctx, p, filter)
}
func (service *Service) SearchVFS(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.VFSNode, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if len([]rune(filter.Query)) < 2 || len([]rune(filter.Query)) > 200 {
		return nil, 0, "", errs.ErrInvalid
	}
	return service.repository.SearchVFS(ctx, p, filter)
}
func (service *Service) ListProjects(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Project, string, []string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", nil, err
	}
	return service.repository.ListProjects(ctx, p, filter)
}
func (service *Service) GetProject(ctx context.Context, p value.Principal, ref string) (entity.Project, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Project{}, err
	}
	return service.repository.GetProject(ctx, p, ref)
}
func (service *Service) ListPlatformMemberships(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Membership, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListPlatformMemberships(ctx, p, filter)
}
func (service *Service) ListPlatformMembershipCandidates(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.User, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListPlatformMembershipCandidates(ctx, p, filter)
}
func (service *Service) ListMemberships(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Membership, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListMemberships(ctx, p, filter)
}
func (service *Service) ListMembershipCandidates(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.User, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListMembershipCandidates(ctx, p, filter)
}
func (service *Service) ListAgents(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Agent, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAgents(ctx, p, filter)
}
func (service *Service) GetAgent(ctx context.Context, p value.Principal, ref string) (entity.Agent, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Agent{}, err
	}
	return service.repository.GetAgent(ctx, p, ref)
}
func (service *Service) GetAgentRuntimeConfiguration(ctx context.Context, p value.Principal, ref string) (entity.AgentRuntimeConfigurationView, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.AgentRuntimeConfigurationView{}, err
	}
	return service.repository.GetAgentRuntimeConfiguration(ctx, p, ref)
}
func (service *Service) ListAgentRuntimeConfigurations(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.AgentRuntimeConfiguration, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAgentRuntimeConfigurations(ctx, p, filter)
}
func (service *Service) ListRuntimeEnvironments(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.RuntimeEnvironmentSet, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuntimeEnvironments(ctx, p, filter)
}
func (service *Service) GetRuntimeEnvironment(ctx context.Context, p value.Principal, ref string) (entity.RuntimeEnvironmentSet, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeEnvironmentSet{}, err
	}
	return service.repository.GetRuntimeEnvironment(ctx, p, ref)
}
func (service *Service) GetRuntimeEnvironmentReadiness(ctx context.Context, p value.Principal, ref string) (entity.RuntimeEnvironmentReadiness, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeEnvironmentReadiness{}, err
	}
	return service.repository.GetRuntimeEnvironmentReadiness(ctx, p, ref)
}
func (service *Service) ListRuntimeEnvironmentAgents(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Agent, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuntimeEnvironmentAgents(ctx, p, filter)
}
func (service *Service) ListRuntimeEnvironmentVersions(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.RuntimeEnvironmentVersion, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuntimeEnvironmentVersions(ctx, p, filter)
}
func (service *Service) ListRuntimeSecrets(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.RuntimeSecret, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuntimeSecrets(ctx, p, filter)
}
func (service *Service) GetRuntimeSecret(ctx context.Context, p value.Principal, ref string) (entity.RuntimeSecret, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	return service.repository.GetRuntimeSecret(ctx, p, ref)
}
func (service *Service) PrepareRuntimeSecretOperation(ctx context.Context, p value.Principal, input repository.RuntimeSecretPrepareInput) (repository.RuntimeSecretPrepareResult, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.RuntimeSecretPrepareResult{}, err
	}
	input.Mutation.Operation = "runtime-secret." + strings.ToLower(input.Kind)
	input.Mutation.IntentDigest = digest(struct {
		Kind, ProjectRef, SecretRef, Name, Description, ValueType, ExpectedContentSHA256 string
		ExpectedVersion                                                                  *int64
	}{input.Kind, input.ProjectRef, input.SecretRef, input.Name, input.Description, input.ValueType, input.ExpectedContentSHA256, input.Mutation.ExpectedVersion})
	return service.repository.PrepareRuntimeSecretOperation(ctx, p, input)
}
func (service *Service) ListRuntimeSecretRecoveryWork(ctx context.Context, p value.Principal, page repository.RuntimeSecretRecoveryPage) ([]entity.RuntimeSecretRecoveryWork, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuntimeSecretRecoveryWork(ctx, p, page)
}
func (service *Service) ConsumeRuntimeSecretOperation(ctx context.Context, p value.Principal, input repository.RuntimeSecretConsumeInput) (entity.RuntimeSecretOperation, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecretOperation{}, err
	}
	return service.repository.ConsumeRuntimeSecretOperation(ctx, p, input)
}
func (service *Service) CompleteRuntimeSecretOperation(ctx context.Context, p value.Principal, input repository.RuntimeSecretCompleteInput) (entity.RuntimeSecret, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	return service.repository.CompleteRuntimeSecretOperation(ctx, p, input)
}
func (service *Service) FailRuntimeSecretOperation(ctx context.Context, p value.Principal, input repository.RuntimeSecretFailInput) (repository.RuntimeSecretFailureResult, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.RuntimeSecretFailureResult{}, err
	}
	return service.repository.FailRuntimeSecretOperation(ctx, p, input)
}
func (service *Service) RecoverRuntimeSecretMaterialization(ctx context.Context, p value.Principal, input repository.RuntimeSecretRecoveryInput) (repository.RuntimeSecretRecoveryResult, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.RuntimeSecretRecoveryResult{}, err
	}
	return service.repository.RecoverRuntimeSecretMaterialization(ctx, p, input)
}
func (service *Service) ListTemplateVariables(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.TemplateVariable, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	return service.repository.ListTemplateVariables(ctx, p, filter)
}
func (service *Service) ListProviderDefinitions(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.ProviderDefinition, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListProviderDefinitions(ctx, p, filter)
}

func (service *Service) ValidatePromptTemplate(ctx context.Context, p value.Principal, template string) ([]promptservice.Diagnostic, error) {
	if _, err := service.principal(ctx, p); err != nil {
		return nil, err
	}
	return promptservice.Validate(template, promptservice.Catalog()), nil
}

func (service *Service) PreviewPromptTemplate(ctx context.Context, p value.Principal, template, targetKind, targetRef string, includeFull bool) (promptservice.Materialization, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return promptservice.Materialization{}, err
	}
	if targetKind == "" {
		targetKind = "SYNTHETIC"
	}
	var snapshot entity.PromptMaterializationSnapshot
	authorizationTarget := organizationTargetForPreview(p.AuthorityTenant)
	switch targetKind {
	case "SYNTHETIC":
		if targetRef != "" || strings.TrimSpace(template) == "" {
			return promptservice.Materialization{}, errs.ErrInvalid
		}
		snapshot = syntheticPromptSnapshot(template)
	case "RUN", "SESSION":
		if strings.TrimSpace(targetRef) == "" {
			return promptservice.Materialization{}, errs.ErrInvalid
		}
		snapshot, err = service.repository.GetPromptMaterializationSnapshot(ctx, p, targetKind, targetRef)
		if err != nil {
			return promptservice.Materialization{}, err
		}
		authorizationTarget = entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: snapshot.ProjectRef,
			ResourceKind: targetKind, ResourceRef: targetRef}
	default:
		return promptservice.Materialization{}, errs.ErrInvalid
	}
	if strings.TrimSpace(template) == "" {
		template = snapshot.TemplateContent
	} else {
		digest := sha256.Sum256([]byte(template))
		snapshot.TemplateDigest = hex.EncodeToString(digest[:])
		snapshot.TemplateRef = "preview_" + snapshot.TemplateDigest[:24]
		snapshot.TemplateContent = template
	}
	materialized, err := promptservice.Materialize(template, promptservice.Snapshot{
		TargetKind: snapshot.TargetKind, TargetRef: snapshot.TargetRef, ProjectRef: snapshot.ProjectRef,
		RunRef: snapshot.RunRef, SessionRef: snapshot.SessionRef, TemplateRef: snapshot.TemplateRef,
		TemplateDigest: snapshot.TemplateDigest, Variables: snapshot.Variables, StructuredVariables: snapshot.StructuredVariables,
		UserCapabilities: snapshot.UserCapabilities, AgentCapabilities: snapshot.AgentCapabilities,
		WorkflowCapabilities: snapshot.WorkflowCapabilities, ConnectionCapabilities: snapshot.ConnectionCapabilities,
		HumanGateCapabilities: snapshot.HumanGateCapabilities, WorkflowStage: snapshot.WorkflowStage,
		Automation: snapshot.Automation, SessionContinuation: snapshot.SessionContinuation,
	})
	if err != nil {
		return materialized, errs.ErrInvalid
	}
	if includeFull {
		access, accessErr := service.repository.QueryEffectiveAccess(ctx, p, "", authorizationTarget,
			[]string{"prompt.full.view"}, time.Now().UTC())
		if accessErr != nil || len(access.Decisions) != 1 || !access.Decisions[0].Allowed {
			return promptservice.Materialization{}, errs.ErrForbidden
		}
		if !p.InteractiveAuthenticationIsFresh(time.Now().UTC(), promptFullMaterializationMaximumAuthenticationAge) {
			return promptservice.Materialization{}, errs.ErrFreshAuthenticationRequired
		}
	}
	return materialized, nil
}

func syntheticPromptSnapshot(template string) entity.PromptMaterializationSnapshot {
	digest := sha256.Sum256([]byte(template))
	digestHex := hex.EncodeToString(digest[:])
	variables := promptservice.Catalog()
	variables["user.ref"], variables["user.name"] = "usr_preview0001", "Пользователь"
	variables["organization.ref"], variables["organization.name"] = "org_preview0001", "Организация"
	variables["project.ref"], variables["project.name"] = "prj_preview0001", "Проект"
	variables["agent.ref"], variables["agent.name"] = "agt_preview0001", "Агент"
	return entity.PromptMaterializationSnapshot{
		TargetKind: promptservice.TargetAgent, TargetRef: "agt_preview0001", ProjectRef: "prj_preview0001",
		TemplateRef: "preview_" + digestHex[:24], TemplateDigest: digestHex, TemplateContent: template,
		Variables: variables, UserCapabilities: []string{}, AgentCapabilities: []string{},
	}
}

func organizationTargetForPreview(ref string) entity.AccessScope {
	return entity.AccessScope{Kind: "ORGANIZATION", ResourceKind: "ORGANIZATION", ResourceRef: ref}
}
func (service *Service) ListModelCapabilities(ctx context.Context, p value.Principal, definitionKey, accountRef string, filter query.Filter) ([]entity.ModelCapability, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	return service.repository.ListModelCapabilities(ctx, p, strings.TrimSpace(definitionKey), strings.TrimSpace(accountRef), filter)
}
func (service *Service) ListManagedConfigurationHistory(ctx context.Context, p value.Principal, ref string, page query.Page) (entity.ManagedConfigurationSet, []entity.ManagedConfigurationRevision, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ManagedConfigurationSet{}, nil, 0, "", err
	}
	if strings.TrimSpace(ref) == "" {
		return entity.ManagedConfigurationSet{}, nil, 0, "", errs.ErrInvalid
	}
	return service.repository.ListManagedConfigurationHistory(ctx, p, strings.TrimSpace(ref), page)
}
func (service *Service) GetManagedConfigurationImpact(ctx context.Context, p value.Principal, ref, revisionRef string) (entity.ManagedConfigurationImpact, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ManagedConfigurationImpact{}, err
	}
	if strings.TrimSpace(ref) == "" || strings.TrimSpace(revisionRef) == "" {
		return entity.ManagedConfigurationImpact{}, errs.ErrInvalid
	}
	return service.repository.GetManagedConfigurationImpact(ctx, p, strings.TrimSpace(ref), strings.TrimSpace(revisionRef))
}
func (service *Service) GetEffectiveManagedConfiguration(ctx context.Context, p value.Principal, kind, consumerKind, consumerRef string) (entity.ManagedConfigurationBindingSnapshot, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ManagedConfigurationBindingSnapshot{}, err
	}
	kind, consumerKind, consumerRef = strings.TrimSpace(kind), strings.TrimSpace(consumerKind), strings.TrimSpace(consumerRef)
	allowed := kind == "ROLE_IMAGE" && consumerKind == "RUNTIME_ENVIRONMENT" ||
		kind == "INTEGRATION_DEFINITION" && consumerKind == "INTEGRATION_CONNECTION"
	if !allowed || consumerRef == "" {
		return entity.ManagedConfigurationBindingSnapshot{}, errs.ErrInvalid
	}
	return service.repository.GetEffectiveManagedConfiguration(ctx, p, kind, consumerKind, consumerRef)
}
func (service *Service) GetSystemSTTConfiguration(ctx context.Context, p value.Principal) (entity.SystemSTTConfiguration, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.SystemSTTConfiguration{}, err
	}
	return service.repository.GetSystemSTTConfiguration(ctx, p)
}
func (service *Service) ListProviderAccounts(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.ProviderAccount, string, []string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", nil, err
	}
	return service.repository.ListProviderAccounts(ctx, p, filter)
}
func (service *Service) GetProviderAccount(ctx context.Context, p value.Principal, ref string) (entity.ProviderAccount, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	if strings.TrimSpace(ref) == "" {
		return entity.ProviderAccount{}, errs.ErrInvalid
	}
	return service.repository.GetProviderAccount(ctx, p, ref)
}
func (service *Service) ListRoleImageRecipeRevisions(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.RoleImageRecipeRevision, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRoleImageRecipeRevisions(ctx, p, filter)
}
func (service *Service) ListAgentInstructionVersions(ctx context.Context, p value.Principal, ref string, page query.Page) ([]entity.InstructionVersion, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	agent, err := service.repository.GetAgent(ctx, p, ref)
	if err != nil {
		return nil, "", err
	}
	return pagePublishedInstructionVersions(agent.PublishedInstructionVersions, page)
}

func pagePublishedInstructionVersions(versions []entity.InstructionVersion, page query.Page) ([]entity.InstructionVersion, string, error) {
	limit := int(page.Size)
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	cursor := int64(0)
	if page.Token != "" {
		parsed, err := strconv.ParseInt(page.Token, 10, 32)
		if err != nil || parsed < 1 {
			return nil, "", errs.ErrInvalid
		}
		cursor = parsed
	}
	start := 0
	if cursor > 0 {
		for start < len(versions) && int64(versions[start].VersionNumber) >= cursor {
			start++
		}
	}
	end := min(start+limit, len(versions))
	items := append([]entity.InstructionVersion(nil), versions[start:end]...)
	next := ""
	if end < len(versions) && len(items) > 0 {
		next = strconv.FormatInt(int64(items[len(items)-1].VersionNumber), 10)
	}
	return items, next, nil
}
func (service *Service) ListWorkflows(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Workflow, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListWorkflows(ctx, p, filter)
}
func (service *Service) GetWorkflow(ctx context.Context, p value.Principal, ref string) (entity.Workflow, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Workflow{}, err
	}
	return service.repository.GetWorkflow(ctx, p, ref)
}
func (service *Service) ListRuns(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Run, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuns(ctx, p, filter)
}
func (service *Service) GetRun(ctx context.Context, p value.Principal, ref string) (entity.Run, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Run{}, err
	}
	return service.repository.GetRun(ctx, p, ref)
}
func (service *Service) GetRunGraph(ctx context.Context, p value.Principal, ref string) (entity.Run, entity.RunGraph, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	return service.repository.GetRunGraph(ctx, p, ref)
}
func (service *Service) ListRunEvents(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.RunEvent, int64, bool, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, false, err
	}
	return service.repository.ListRunEvents(ctx, p, filter)
}
func (service *Service) ListOwnerGates(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.OwnerGate, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListOwnerGates(ctx, p, filter)
}
func (service *Service) GetOwnerGate(ctx context.Context, p value.Principal, ref string) (entity.OwnerGate, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.OwnerGate{}, err
	}
	return service.repository.GetOwnerGate(ctx, p, ref)
}
func (service *Service) ListArtifacts(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Artifact, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListArtifacts(ctx, p, filter)
}
func (service *Service) GetArtifact(ctx context.Context, p value.Principal, ref string) (entity.Artifact, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Artifact{}, err
	}
	return service.repository.GetArtifact(ctx, p, ref)
}
func (service *Service) GetArtifactImpact(ctx context.Context, p value.Principal, ref, action string) (entity.ArtifactImpact, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ArtifactImpact{}, err
	}
	ref = strings.TrimSpace(ref)
	action = strings.TrimSpace(action)
	if ref == "" || action != "DELETE" && action != "PURGE" {
		return entity.ArtifactImpact{}, errs.ErrInvalid
	}
	return service.repository.GetArtifactImpact(ctx, p, ref, action)
}
func (service *Service) GetAttachmentSet(ctx context.Context, p value.Principal, ref string, page query.Page) (entity.AttachmentSet, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.AttachmentSet{}, "", err
	}
	if strings.TrimSpace(ref) == "" || page.Size < 0 || page.Size > 100 || len(page.Token) > 256 {
		return entity.AttachmentSet{}, "", errs.ErrInvalid
	}
	return service.repository.GetAttachmentSet(ctx, p, ref, page)
}
func (service *Service) UploadArtifact(ctx context.Context, p value.Principal, mutation value.Mutation, input repository.ArtifactUpload) (entity.Artifact, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Artifact{}, err
	}
	if input.SizeBytes < 0 || input.SizeBytes > repository.MaximumArtifactBytes || input.Reader == nil ||
		(strings.TrimSpace(input.ProjectRef) == "" && strings.TrimSpace(input.RunRef) != "") {
		return entity.Artifact{}, errs.ErrInvalid
	}
	if _, err := input.Reader.Seek(0, io.SeekStart); err != nil {
		return entity.Artifact{}, errs.ErrInvalid
	}
	contentDigest := sha256.New()
	written, err := io.Copy(contentDigest, io.LimitReader(input.Reader, repository.MaximumArtifactBytes+1))
	if err != nil || written != input.SizeBytes {
		return entity.Artifact{}, errs.ErrInvalid
	}
	actualDigest := "sha256:" + hex.EncodeToString(contentDigest.Sum(nil))
	if input.Digest != "" && input.Digest != actualDigest {
		return entity.Artifact{}, errs.ErrInvalid
	}
	input.Digest = actualDigest
	verdict, err := artifactpolicy.InspectReader(input.FileName, input.MediaType, input.Reader, input.SizeBytes)
	if err != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	input.MediaType = verdict.MediaType
	input.ScanState = verdict.ScanState
	input.PreviewState = verdict.PreviewState
	if _, err := input.Reader.Seek(0, io.SeekStart); err != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	mutation.Operation = "artifact.upload"
	mutation.IntentDigest = digest(struct {
		ProjectRef, RunRef, FileName, MediaType, Digest string
		SizeBytes                                       int64
	}{input.ProjectRef, input.RunRef, input.FileName, input.MediaType, input.Digest, input.SizeBytes})
	if err := mutation.Validate(); err != nil {
		return entity.Artifact{}, errs.ErrInvalid
	}
	return service.repository.UploadArtifact(ctx, p, mutation, input)
}

func (service *Service) UploadAgentAvatar(ctx context.Context, p value.Principal, mutation value.Mutation, input repository.AgentAvatarUpload) (entity.Agent, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Agent{}, err
	}
	if strings.TrimSpace(input.ProjectRef) == "" || strings.TrimSpace(input.AgentRef) == "" ||
		input.ExpectedVersion < 1 || input.SizeBytes < 1 || input.SizeBytes > 5<<20 || input.Reader == nil {
		return entity.Agent{}, errs.ErrInvalid
	}
	if _, err := input.Reader.Seek(0, io.SeekStart); err != nil {
		return entity.Agent{}, errs.ErrInvalid
	}
	contentDigest := sha256.New()
	written, err := io.Copy(contentDigest, io.LimitReader(input.Reader, 5<<20+1))
	if err != nil || written != input.SizeBytes {
		return entity.Agent{}, errs.ErrInvalid
	}
	actualDigest := "sha256:" + hex.EncodeToString(contentDigest.Sum(nil))
	if input.Digest != "" && input.Digest != actualDigest {
		return entity.Agent{}, errs.ErrInvalid
	}
	input.Digest = actualDigest
	verdict, err := artifactpolicy.InspectReader(input.FileName, input.MediaType, input.Reader, input.SizeBytes)
	if err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	if verdict.ScanState != "CLEAN" || verdict.PreviewState != "AVAILABLE" ||
		!containsString([]string{"image/jpeg", "image/png", "image/webp"}, verdict.MediaType) {
		return entity.Agent{}, errs.ErrInvalid
	}
	input.MediaType, input.ScanState, input.PreviewState = verdict.MediaType, verdict.ScanState, verdict.PreviewState
	if _, err := input.Reader.Seek(0, io.SeekStart); err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	mutation.Operation = "agent.avatar.upload"
	mutation.IntentDigest = digest(struct {
		ProjectRef, AgentRef, FileName, MediaType, Digest string
		SizeBytes, ExpectedVersion                        int64
	}{input.ProjectRef, input.AgentRef, input.FileName, input.MediaType, input.Digest, input.SizeBytes, input.ExpectedVersion})
	if err := mutation.Validate(); err != nil {
		return entity.Agent{}, errs.ErrInvalid
	}
	return service.repository.UploadAgentAvatar(ctx, p, mutation, input)
}

func (service *Service) PurgeArtifact(ctx context.Context, p value.Principal, mutation value.Mutation, artifactRef, impactDigest string) (string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return "", err
	}
	artifactRef = strings.TrimSpace(artifactRef)
	impactDigest = strings.TrimSpace(impactDigest)
	if artifactRef == "" || impactDigest == "" || mutation.ExpectedVersion == nil || strings.TrimSpace(mutation.IdempotencyKey) == "" {
		return "", errs.ErrInvalid
	}
	mutation.Operation = "artifact.purge"
	mutation.IntentDigest = digest(struct {
		ArtifactRef, ImpactDigest string
		ExpectedVersion           int64
	}{ArtifactRef: artifactRef, ImpactDigest: impactDigest, ExpectedVersion: *mutation.ExpectedVersion})
	return service.repository.PurgeArtifact(ctx, p, mutation, artifactRef, impactDigest)
}
func (service *Service) DownloadArtifact(ctx context.Context, p value.Principal, ref, purpose string) (repository.ArtifactDownload, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.ArtifactDownload{}, err
	}
	if purpose != "DOWNLOAD" && purpose != "PREVIEW" {
		return repository.ArtifactDownload{}, errs.ErrInvalid
	}
	return service.repository.DownloadArtifact(ctx, p, ref, purpose)
}
func (service *Service) ReadExecutionArtifact(ctx context.Context, p value.Principal, leaseRef, fence string, generation int64, artifactRef string) (repository.ArtifactDownload, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.ArtifactDownload{}, err
	}
	if p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.execution.artifact.read" ||
		strings.TrimSpace(leaseRef) == "" || strings.TrimSpace(fence) == "" || generation < 1 || strings.TrimSpace(artifactRef) == "" {
		return repository.ArtifactDownload{}, errs.ErrForbidden
	}
	return service.repository.ReadExecutionArtifact(ctx, p, leaseRef, fence, generation, artifactRef)
}
func (service *Service) ListSchedules(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Schedule, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListSchedules(ctx, p, filter)
}
func (service *Service) GetSchedule(ctx context.Context, p value.Principal, ref string) (entity.Schedule, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Schedule{}, err
	}
	if strings.TrimSpace(ref) == "" {
		return entity.Schedule{}, errs.ErrInvalid
	}
	return service.repository.GetSchedule(ctx, p, ref)
}
func (service *Service) ListScheduleRevisions(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.ScheduleRevision, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListScheduleRevisions(ctx, p, filter)
}
func (service *Service) ListScheduleRuns(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.ScheduleRunOccurrence, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListScheduleRuns(ctx, p, filter)
}
func (service *Service) ListIntegrationDefinitions(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.IntegrationDefinition, string, []string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", nil, err
	}
	return service.repository.ListIntegrationDefinitions(ctx, p, filter)
}
func (service *Service) ListIntegrationConnections(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.IntegrationConnection, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListIntegrationConnections(ctx, p, filter)
}
func (service *Service) GetIntegrationConnection(ctx context.Context, p value.Principal, ref string) (entity.IntegrationConnection, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	return service.repository.GetIntegrationConnection(ctx, p, ref)
}
func (service *Service) GetSystemAssistant(ctx context.Context, p value.Principal) (entity.SystemAssistant, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.SystemAssistant{}, err
	}
	return service.repository.GetSystemAssistant(ctx, p)
}
func (service *Service) ListAssistantConversations(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.AssistantConversation, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAssistantConversations(ctx, p, filter)
}
func (service *Service) GetAdministration(ctx context.Context, p value.Principal) (repository.Administration, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.Administration{}, err
	}
	return service.repository.GetAdministration(ctx, p)
}
func (service *Service) ListAuditEvents(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.AuditEvent, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAuditEvents(ctx, p, filter)
}

func (service *Service) ListPermissionRegistry(ctx context.Context, p value.Principal) ([]entity.PermissionDefinition, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	return service.repository.ListPermissionRegistry(ctx, p)
}

func (service *Service) ListAccessSubjects(ctx context.Context, p value.Principal, filter query.Filter, kind string) ([]entity.AccessSubject, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAccessSubjects(ctx, p, filter, kind)
}

func (service *Service) ListOIDCGroups(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.OIDCGroup, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListOIDCGroups(ctx, p, filter)
}

func (service *Service) ListAccessRoles(ctx context.Context, p value.Principal, page query.Page, includeArchived bool) ([]entity.AccessRole, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAccessRoles(ctx, p, page, includeArchived)
}

func (service *Service) ListAccessRoleVersions(ctx context.Context, p value.Principal, roleRef string, page query.Page) (entity.AccessRole, []entity.AccessRoleVersion, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.AccessRole{}, nil, "", err
	}
	return service.repository.ListAccessRoleVersions(ctx, p, roleRef, page)
}

func (service *Service) ListAccessBindings(ctx context.Context, p value.Principal, filter query.AccessBindingFilter) ([]entity.AccessBinding, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAccessBindings(ctx, p, filter)
}

func (service *Service) QueryEffectiveAccess(ctx context.Context, p value.Principal, subjectRef string, target entity.AccessScope, permissionKeys []string, evaluatedAt time.Time) (entity.EffectiveAccess, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.EffectiveAccess{}, err
	}
	return service.repository.QueryEffectiveAccess(ctx, p, subjectRef, target, permissionKeys, evaluatedAt)
}

func (service *Service) SimulateAccess(ctx context.Context, p value.Principal, input command.AccessSimulationInput) (entity.AccessSimulation, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.AccessSimulation{}, err
	}
	return service.repository.SimulateAccess(ctx, p, input)
}

func (service *Service) Execute(ctx context.Context, input command.Command) (command.Result, error) {
	principal, err := service.principal(ctx, input.Principal)
	if err != nil {
		return command.Result{}, err
	}
	input.Principal = principal
	return service.executeResolved(ctx, input)
}

func (service *Service) executeResolved(ctx context.Context, input command.Command) (command.Result, error) {
	if !knownCommand(input.Kind) || input.Payload == nil {
		return command.Result{}, errs.ErrInvalid
	}
	input.Mutation.Operation = "controlplane." + strings.ToLower(string(input.Kind))
	intentPayload := input.Payload
	if input.Kind == command.ConfigureConnectionCredential {
		payload, ok := input.Payload.(command.ConnectionInput)
		if !ok || payload.CredentialRevision == nil {
			return command.Result{}, errs.ErrInvalid
		}
		intentPayload = struct {
			Ref, MaterializationRef, ContentSHA256 string
		}{payload.Ref, payload.MaterializationRef, payload.CredentialRevision.ContentSHA256}
	}
	input.Mutation.IntentDigest = digest(struct {
		Kind    command.Kind
		Payload any
	}{input.Kind, intentPayload})
	if err := input.Mutation.Validate(); err != nil {
		return command.Result{}, errs.ErrInvalid
	}
	if input.Kind == command.SetAgentEnabled {
		payload, ok := input.Payload.(command.AgentInput)
		if !ok || payload.Ref == "system-assistant" && !payload.Enabled {
			return command.Result{}, errs.ErrProtected
		}
	}
	if input.Kind == command.ArchiveAgent {
		payload, ok := input.Payload.(command.AgentInput)
		if !ok || payload.Ref == "system-assistant" {
			return command.Result{}, errs.ErrProtected
		}
	}
	return service.repository.Execute(ctx, input)
}

func (service *Service) ReconcileWarmRuntime(ctx context.Context, p value.Principal, instance string) (entity.SystemAssistant, map[string]any, bool, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	if strings.TrimSpace(instance) == "" {
		return entity.SystemAssistant{}, nil, false, errs.ErrInvalid
	}
	return service.repository.ReconcileWarmRuntime(ctx, p, instance)
}
func (service *Service) ReportWarmRuntime(ctx context.Context, p value.Principal, input command.WarmRuntimeInput) (entity.SystemAssistant, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.SystemAssistant{}, err
	}
	if p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.warm.report" {
		return entity.SystemAssistant{}, errs.ErrForbidden
	}
	return service.repository.ReportWarmRuntime(ctx, p, input)
}

func (service *Service) ClaimSessionArchiveTasks(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if p.CallerWorkload != "session-archive" || p.Permission != "platform.session-archive.tasks.claim" ||
		strings.TrimSpace(instance) == "" || limit < 1 || limit > 16 {
		return nil, errs.ErrForbidden
	}
	return service.repository.ClaimSessionArchiveTasks(ctx, p, instance, limit)
}

func (service *Service) RenewSessionArchiveTask(ctx context.Context, p value.Principal, input command.SessionArchiveTaskInput) (map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if p.CallerWorkload != "session-archive" || p.Permission != "platform.session-archive.tasks.renew" {
		return nil, errs.ErrForbidden
	}
	return service.repository.RenewSessionArchiveTask(ctx, p, input)
}
func (service *Service) ClaimDueSchedules(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(instance) == "" || limit < 1 || limit > 128 {
		return nil, errs.ErrInvalid
	}
	return service.repository.ClaimDueSchedules(ctx, p, instance, limit)
}
func (service *Service) ClaimIntegrationConnectionTests(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(instance) == "" || limit < 1 || limit > 32 {
		return nil, errs.ErrInvalid
	}
	return service.repository.ClaimIntegrationConnectionTests(ctx, p, instance, limit)
}
func (service *Service) ResolveIntegrationInvocation(ctx context.Context, p value.Principal, input map[string]string, boundedInput map[string]any) (map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"run_ref", "node_ref", "connection_ref", "capability_key", "idempotency_key"} {
		if strings.TrimSpace(input[key]) == "" {
			return nil, errs.ErrInvalid
		}
	}
	return service.repository.ResolveIntegrationInvocation(ctx, p, input, boundedInput)
}
func (service *Service) ClaimIntegrationInvocations(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(instance) == "" || limit < 1 || limit > 32 {
		return nil, errs.ErrInvalid
	}
	return service.repository.ClaimIntegrationInvocations(ctx, p, instance, limit)
}
func (service *Service) GetIntegrationInvocation(ctx context.Context, p value.Principal, ref string) (map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ref) == "" {
		return nil, errs.ErrInvalid
	}
	return service.repository.GetIntegrationInvocation(ctx, p, ref)
}
func (service *Service) ListInteractionSources(ctx context.Context, p value.Principal) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	return service.repository.ListInteractionSources(ctx, p)
}
func (service *Service) ClaimInteractionDeliveries(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(instance) == "" || limit < 1 || limit > 32 {
		return nil, errs.ErrInvalid
	}
	return service.repository.ClaimInteractionDeliveries(ctx, p, instance, limit)
}

func knownCommand(kind command.Kind) bool {
	switch kind {
	case command.CompleteOnboarding, command.CreateProject, command.UpdateProject,
		command.AddPlatformMembership, command.ChangePlatformMembership, command.RemovePlatformMembership,
		command.AddMembership, command.ChangeMembership, command.RemoveMembership,
		command.CreateAgent, command.UpdateAgent, command.SetAgentEnabled, command.ArchiveAgent,
		command.SetAgentAvatar, command.RemoveAgentAvatar,
		command.CreateInstructions, command.ValidateInstructions, command.PublishInstructions,
		command.RollbackInstructions, command.PublishAgentRuntimeConfig,
		command.CreateConfigOverlayDraft, command.ValidateConfigOverlayDraft, command.PublishConfigOverlayDraft,
		command.RollbackConfigOverlay, command.CreateRuntimeEnvironment, command.PublishRuntimeEnvironment,
		command.RollbackRuntimeEnvironment, command.SetRuntimeEnvironmentEnabled, command.DeleteRuntimeEnvironment,
		command.BindAgentRuntimeEnvironment,
		command.ChangeAgentCapability, command.ChangeAgentGrant,
		command.CreateWorkflow, command.UpdateWorkflow,
		command.ValidateWorkflow, command.PublishWorkflow, command.ArchiveWorkflow,
		command.LaunchRun, command.AddSessionTurn, command.CancelRun, command.RetryRun,
		command.ResolveOwnerGate, command.ChangeArtifactBinding, command.DeleteArtifact,
		command.RestoreArtifact, command.CreateAttachmentSetDraft, command.AddAttachmentSetItems,
		command.RemoveAttachmentSetItems, command.FinalizeAttachmentSet, command.CreateSchedule,
		command.UpdateSchedule, command.SetScheduleEnabled, command.ArchiveSchedule, command.DeleteSchedule,
		command.CreateProviderAccount, command.StartProviderDeviceAuth, command.AuthorizeProviderAPIKey,
		command.RefreshProviderAuthorization, command.RevokeProviderAccount, command.DeleteProviderAccount, command.SetProviderAccountEnabled,
		command.CreateConnection, command.UpdateConnection, command.DeleteConnection,
		command.ConfigureConnectionCredential,
		command.TestConnection, command.SetConnectionEnabled, command.ChangeIntegrationGrant,
		command.CreateAssistantConversation, command.UpdateAssistantConversation, command.AddAssistantTurn,
		command.UpdateAssistantPlan, command.ValidateAssistantPlan, command.ApplyAssistantPlan, command.RejectAssistantPlan,
		command.UpdateAssistantInstructions, command.RecoverAssistant, command.ClaimExecution,
		command.RenewExecution, command.ReportExecutionProgress, command.CommitProviderCredentialRefresh,
		command.CompleteExecution,
		command.DelegateExecution, command.ProposeAssistantPlan, command.ProposeAssistantMetadata,
		command.ProposeRunMetadata, command.RecordRunToolCall,
		command.CompleteSessionSnapshot, command.CompleteSessionRestore,
		command.CompleteSessionPVCDeletion, command.CompleteSessionObjectDeletion,
		command.FailSessionArchiveTask,
		command.MaterializeOccurrence, command.CompleteConnectionTest,
		command.CompleteIntegrationInvocation, command.CompleteInteractionDelivery,
		command.AcceptInteractionMessage, command.CreateAccessRole,
		command.CreateAccessRoleVersion, command.ArchiveAccessRole,
		command.CreateAccessBinding, command.ChangeAccessBinding, command.RevokeAccessBinding,
		command.CreatePromptTemplateDraft, command.ValidatePromptTemplateDraft,
		command.PublishPromptTemplateDraft, command.RebindPromptTemplate,
		command.CreateRoleImageRevisionDraft, command.ValidateRoleImageRevision,
		command.PublishRoleImageRevision, command.RebindRoleImage,
		command.CreateIntegrationDefinition, command.ValidateIntegrationDefinition,
		command.PublishIntegrationDefinition, command.RebindIntegrationDefinition,
		command.CreateSystemSTTDraft, command.ValidateSystemSTTDraft,
		command.PublishSystemSTTDraft, command.RebindSystemSTT,
		command.DetachGitManagedConfiguration, command.CopyGitManagedConfiguration:
		return true
	default:
		return false
	}
}

func digest(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		encoded = []byte(fmt.Sprintf("unsupported:%T", value))
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}
