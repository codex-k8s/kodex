package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"gopkg.in/yaml.v3"
)

const (
	defaultProviderSlug                 = "github"
	defaultProjectSlug                  = "kodex"
	defaultProjectDisplayName           = "kodex"
	defaultRepositoryDefaultBranch      = "main"
	defaultActorID                      = "onboarding-runner"
	defaultSource                       = "cmd/onboarding-runner"
	defaultCheckedSourcePath            = "services.yaml"
	defaultAccessManagerAuthTokenEnv    = "KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN"
	defaultProjectCatalogAuthTokenEnv   = "KODEX_PROJECT_CATALOG_GRPC_AUTH_TOKEN"
	defaultProviderHubAuthTokenEnv      = "KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN"
	defaultAccessManagerAuthEnvRefName  = "KODEX_ONBOARDING_RUNNER_ACCESS_MANAGER_GRPC_AUTH_TOKEN_ENV"
	defaultProjectCatalogAuthEnvRefName = "KODEX_ONBOARDING_RUNNER_PROJECT_CATALOG_GRPC_AUTH_TOKEN_ENV"
	defaultProviderHubAuthEnvRefName    = "KODEX_ONBOARDING_RUNNER_PROVIDER_HUB_GRPC_AUTH_TOKEN_ENV"
	maxCheckedPayloadBytes              = 256 * 1024
	maxServicesPolicyImportBytes        = 512 * 1024
	maxWatermarkJSONBytes               = 16 * 1024
	maxBootstrapSetupFiles              = 64
	maxBootstrapSetupBytes              = 4 * 1024 * 1024
	runnerRepositoryReadAccessPriority  = 100
	selfDeployServiceAccessPriority     = 100
	selfDeployOwnerAccessPriority       = 100
	servicesPolicyImportAccessPriority  = 100
	selfDeployOwnerReadProbeResourceID  = "self-deploy-plan-probe"
	selfDeployOwnerGateProbeResourceID  = "self-deploy-gate-probe"
)

type selfDeployServiceAccessGrant struct {
	subjectID    string
	actionKey    string
	resourceType string
}

type selfDeployOwnerAccessGrant struct {
	actionKey    string
	resourceType string
	scopeType    string
	probeID      string
}

var selfDeployServiceAccessGrants = []selfDeployServiceAccessGrant{
	{subjectID: "agent-manager", actionKey: accesscatalog.ActionProjectPolicyRead, resourceType: accesscatalog.ResourceServicesPolicy},
	{subjectID: "agent-manager", actionKey: accesscatalog.ActionGovernanceRiskEvaluate, resourceType: accesscatalog.ResourceGovernanceRiskAssessment},
	{subjectID: "agent-manager", actionKey: accesscatalog.ActionGovernanceRiskRead, resourceType: accesscatalog.ResourceGovernanceRiskAssessment},
	{subjectID: "agent-manager", actionKey: accesscatalog.ActionGovernanceGateRequest, resourceType: accesscatalog.ResourceGovernanceGate},
	{subjectID: "agent-manager", actionKey: accesscatalog.ActionGovernanceGateRead, resourceType: accesscatalog.ResourceGovernanceGate},
	{subjectID: "staff-gateway", actionKey: accesscatalog.ActionProjectPolicyRead, resourceType: accesscatalog.ResourceServicesPolicy},
	{subjectID: "staff-gateway", actionKey: accesscatalog.ActionGovernanceRiskRead, resourceType: accesscatalog.ResourceGovernanceRiskAssessment},
	{subjectID: "staff-gateway", actionKey: accesscatalog.ActionGovernanceGateRead, resourceType: accesscatalog.ResourceGovernanceGate},
}

var selfDeployOwnerAccessGrants = []selfDeployOwnerAccessGrant{
	{actionKey: accesscatalog.ActionGovernanceRiskRead, resourceType: accesscatalog.ResourceGovernanceRiskAssessment, scopeType: accesscatalog.ScopeProject, probeID: selfDeployOwnerReadProbeResourceID},
	{actionKey: accesscatalog.ActionGovernanceGateRead, resourceType: accesscatalog.ResourceGovernanceGate, scopeType: accesscatalog.ScopeProject, probeID: selfDeployOwnerReadProbeResourceID},
	{actionKey: accesscatalog.ActionGovernanceGateDecide, resourceType: accesscatalog.ResourceGovernanceGate, scopeType: accesscatalog.ScopeGlobal, probeID: selfDeployOwnerGateProbeResourceID},
}

func main() {
	options := runnerOptions{}
	flag.StringVar(&options.AccessManagerAddr, "access-manager-addr", os.Getenv("KODEX_ACCESS_MANAGER_GRPC_ADDR"), "access-manager gRPC address")
	flag.StringVar(&options.ProjectCatalogAddr, "project-catalog-addr", os.Getenv("KODEX_PROJECT_CATALOG_GRPC_ADDR"), "project-catalog gRPC address")
	flag.StringVar(&options.ProviderHubAddr, "provider-hub-addr", os.Getenv("KODEX_PROVIDER_HUB_GRPC_ADDR"), "provider-hub gRPC address")
	flag.StringVar(&options.AccessManagerAuthTokenEnv, "access-manager-auth-token-env", envOr(defaultAccessManagerAuthEnvRefName, defaultAccessManagerAuthTokenEnv), "env var name containing access-manager shared gRPC token; empty disables metadata")
	flag.StringVar(&options.ProjectCatalogAuthTokenEnv, "project-catalog-auth-token-env", envOr(defaultProjectCatalogAuthEnvRefName, defaultProjectCatalogAuthTokenEnv), "env var name containing project-catalog shared gRPC token; empty disables metadata")
	flag.StringVar(&options.ProviderHubAuthTokenEnv, "provider-hub-auth-token-env", envOr(defaultProviderHubAuthEnvRefName, defaultProviderHubAuthTokenEnv), "env var name containing provider-hub shared gRPC token; empty disables metadata")
	flag.StringVar(&options.ScenarioFilePath, "scenario-file", "", "checked onboarding scenario JSON; payload values are never printed")
	flag.StringVar(&options.OrganizationID, "organization-id", os.Getenv("KODEX_ONBOARDING_RUNNER_ORGANIZATION_ID"), "organization id used when project-id is not provided")
	flag.StringVar(&options.ProjectID, "project-id", "", "project-catalog project id")
	flag.StringVar(&options.ProjectSlug, "project-slug", envOr("KODEX_ONBOARDING_RUNNER_PROJECT_SLUG", defaultProjectSlug), "project slug used when project-id is not provided")
	flag.StringVar(&options.ProjectDisplayName, "project-display-name", envOr("KODEX_ONBOARDING_RUNNER_PROJECT_NAME", defaultProjectDisplayName), "project display name used when project is created")
	flag.StringVar(&options.ProjectDescription, "project-description", os.Getenv("KODEX_ONBOARDING_RUNNER_PROJECT_DESCRIPTION"), "optional project description")
	flag.StringVar(&options.RepositoryID, "repository-id", "", "project-catalog repository binding id")
	flag.StringVar(&options.ProviderSlug, "provider-slug", defaultProviderSlug, "provider slug")
	flag.StringVar(&options.ProviderOwner, "provider-owner", os.Getenv("KODEX_ONBOARDING_RUNNER_PROVIDER_OWNER"), "provider repository owner used to build repository binding")
	flag.StringVar(&options.ProviderName, "provider-name", os.Getenv("KODEX_ONBOARDING_RUNNER_PROVIDER_NAME"), "provider repository name used to build repository binding")
	flag.StringVar(&options.RepositoryFullName, "repository-full-name", "", "provider repository owner/name")
	flag.StringVar(&options.RepositoryDefaultBranch, "repository-default-branch", envOr("KODEX_ONBOARDING_RUNNER_REPOSITORY_DEFAULT_BRANCH", defaultRepositoryDefaultBranch), "default branch for generated repository binding scenario")
	flag.StringVar(&options.ProviderRepositoryID, "provider-repository-id", "", "provider-native repository id")
	flag.StringVar(&options.ExternalAccountID, "external-account-id", os.Getenv("KODEX_ONBOARDING_RUNNER_EXTERNAL_ACCOUNT_ID"), "external account id selected by caller policy")
	flag.StringVar(&options.AllowedProviderOwner, "allowed-provider-owner", os.Getenv("KODEX_ONBOARDING_RUNNER_ALLOWED_OWNER"), "required owner for apply mode")
	flag.StringVar(&options.RepositoryNamePrefix, "repository-name-prefix", os.Getenv("KODEX_ONBOARDING_RUNNER_REPOSITORY_PREFIX"), "required repository name prefix for apply mode")
	flag.StringVar(&options.AllowedProviderRepository, "allowed-provider-repository", os.Getenv("KODEX_ONBOARDING_RUNNER_ALLOWED_REPOSITORY"), "optional exact repository name allowed for apply mode")
	flag.StringVar(&options.OwnerUserRef, "owner-user-ref", envOr("KODEX_ONBOARDING_RUNNER_OWNER_USER_REF", os.Getenv("KODEX_BOOTSTRAP_OWNER_USER_REF")), "safe owner user ref for self-deploy governance access; do not pass email")
	flag.StringVar(&options.IdempotencyKey, "idempotency-key", "", "optional idempotency key prefix")
	flag.StringVar(&options.RequestID, "request-id", "", "optional request id")
	flag.StringVar(&options.ActorID, "actor-id", defaultActorID, "safe actor id")
	flag.StringVar(&options.Kind, "kind", "both", "bootstrap, adoption or both")
	flag.StringVar(&options.CheckedPayloadFilePath, "checked-payload-file", os.Getenv("KODEX_ONBOARDING_RUNNER_CHECKED_PAYLOAD_FILE"), "normalized checked validated_payload_json file; values are never printed")
	flag.StringVar(&options.WatermarkJSONFilePath, "watermark-json-file", os.Getenv("KODEX_ONBOARDING_RUNNER_WATERMARK_JSON_FILE"), "safe watermark JSON file for checked artifact producer")
	flag.StringVar(&options.CheckedSourcePath, "checked-source-path", os.Getenv("KODEX_ONBOARDING_RUNNER_CHECKED_SOURCE_PATH"), "source path for checked services policy artifact")
	flag.StringVar(&options.CheckedArtifactRef, "checked-artifact-ref", os.Getenv("KODEX_ONBOARDING_RUNNER_CHECKED_ARTIFACT_REF"), "optional immutable checked artifact ref")
	flag.StringVar(&options.CheckedArtifactVersion, "checked-artifact-version", os.Getenv("KODEX_ONBOARDING_RUNNER_CHECKED_ARTIFACT_VERSION"), "optional checked artifact version; defaults to provider merge commit")
	flag.StringVar(&options.ServicesPolicyFilePath, "services-policy-file", os.Getenv("KODEX_ONBOARDING_RUNNER_SERVICES_POLICY_FILE"), "local checked services.yaml file for ImportServicesPolicy; values are never printed")
	flag.StringVar(&options.ServicesPolicySourcePath, "services-policy-source-path", os.Getenv("KODEX_ONBOARDING_RUNNER_SERVICES_POLICY_SOURCE_PATH"), "repository path for imported services policy")
	flag.StringVar(&options.ServicesPolicySourceRef, "services-policy-source-ref", os.Getenv("KODEX_ONBOARDING_RUNNER_SERVICES_POLICY_SOURCE_REF"), "source ref for imported services policy")
	flag.StringVar(&options.ServicesPolicySourceCommitSHA, "services-policy-source-commit-sha", os.Getenv("KODEX_ONBOARDING_RUNNER_SERVICES_POLICY_SOURCE_COMMIT_SHA"), "source commit sha for imported services policy")
	flag.StringVar(&options.ServicesPolicySourceBlobSHA, "services-policy-source-blob-sha", os.Getenv("KODEX_ONBOARDING_RUNNER_SERVICES_POLICY_SOURCE_BLOB_SHA"), "optional source blob sha for imported services policy")
	timeout := flag.Duration("timeout", 30*time.Second, "runner timeout")
	applyFlag := flag.Bool("apply", false, "execute mutating product API calls")
	flag.Parse()

	options.Apply = *applyFlag || strings.EqualFold(os.Getenv("KODEX_ONBOARDING_RUNNER_APPLY"), "true")
	options.Timeout = *timeout

	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()

	clients, closeClients, err := newGRPCClients(options)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "onboarding-runner: %s\n", redact(err.Error()))
		os.Exit(1)
	}
	defer closeClients()

	if err := run(ctx, options, clients, os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "onboarding-runner: %s\n", redact(err.Error()))
		os.Exit(1)
	}
}

type projectCatalogAPI interface {
	CreateProject(context.Context, *projectsv1.CreateProjectRequest, ...grpc.CallOption) (*projectsv1.ProjectResponse, error)
	ListProjects(context.Context, *projectsv1.ListProjectsRequest, ...grpc.CallOption) (*projectsv1.ListProjectsResponse, error)
	AttachRepository(context.Context, *projectsv1.AttachRepositoryRequest, ...grpc.CallOption) (*projectsv1.RepositoryResponse, error)
	GetRepository(context.Context, *projectsv1.GetRepositoryRequest, ...grpc.CallOption) (*projectsv1.RepositoryResponse, error)
	ListRepositories(context.Context, *projectsv1.ListRepositoriesRequest, ...grpc.CallOption) (*projectsv1.ListRepositoriesResponse, error)
	CreateProviderRepository(context.Context, *projectsv1.CreateProviderRepositoryRequest, ...grpc.CallOption) (*projectsv1.RepositoryProviderCreateResponse, error)
	CreateRepositoryBootstrapPullRequest(context.Context, *projectsv1.CreateRepositoryBootstrapPullRequestRequest, ...grpc.CallOption) (*projectsv1.RepositoryBootstrapPullRequestResponse, error)
	ImportServicesPolicy(context.Context, *projectsv1.ImportServicesPolicyRequest, ...grpc.CallOption) (*projectsv1.ServicesPolicyResponse, error)
	ReconcileBootstrapMergeSignal(context.Context, *projectsv1.ReconcileBootstrapMergeSignalRequest, ...grpc.CallOption) (*projectsv1.BootstrapServicesPolicyImportResponse, error)
	ReconcileAdoptionMergeSignal(context.Context, *projectsv1.ReconcileAdoptionMergeSignalRequest, ...grpc.CallOption) (*projectsv1.BootstrapServicesPolicyImportResponse, error)
}

type providerHubAPI interface {
	GetRepositoryMergeSignal(context.Context, *providersv1.GetRepositoryMergeSignalRequest, ...grpc.CallOption) (*providersv1.RepositoryMergeSignalResponse, error)
	ListRepositoryMergeSignals(context.Context, *providersv1.ListRepositoryMergeSignalsRequest, ...grpc.CallOption) (*providersv1.ListRepositoryMergeSignalsResponse, error)
	GetRepositoryChangeSignal(context.Context, *providersv1.GetRepositoryChangeSignalRequest, ...grpc.CallOption) (*providersv1.RepositoryChangeSignalResponse, error)
	ListRepositoryChangeSignals(context.Context, *providersv1.ListRepositoryChangeSignalsRequest, ...grpc.CallOption) (*providersv1.ListRepositoryChangeSignalsResponse, error)
	ListRepositoryAdoptionScanSnapshots(context.Context, *providersv1.ListRepositoryAdoptionScanSnapshotsRequest, ...grpc.CallOption) (*providersv1.ListRepositoryAdoptionScanSnapshotsResponse, error)
}

type accessManagerAPI interface {
	PutAccessRule(context.Context, *accessaccountsv1.PutAccessRuleRequest, ...grpc.CallOption) (*accessaccountsv1.AccessRuleResponse, error)
	CheckAccess(context.Context, *accessaccountsv1.CheckAccessRequest, ...grpc.CallOption) (*accessaccountsv1.CheckAccessResponse, error)
}

type runnerClients struct {
	ProjectCatalog projectCatalogAPI
	ProviderHub    providerHubAPI
	AccessManager  accessManagerAPI
}

type runnerOptions struct {
	AccessManagerAddr             string
	ProjectCatalogAddr            string
	ProviderHubAddr               string
	AccessManagerAuthTokenEnv     string
	ProjectCatalogAuthTokenEnv    string
	ProviderHubAuthTokenEnv       string
	ScenarioFilePath              string
	OrganizationID                string
	ProjectID                     string
	ProjectSlug                   string
	ProjectDisplayName            string
	ProjectDescription            string
	RepositoryID                  string
	ProviderSlug                  string
	ProviderOwner                 string
	ProviderName                  string
	RepositoryFullName            string
	RepositoryDefaultBranch       string
	ProviderRepositoryID          string
	ExternalAccountID             string
	AllowedProviderOwner          string
	RepositoryNamePrefix          string
	AllowedProviderRepository     string
	OwnerUserRef                  string
	IdempotencyKey                string
	RequestID                     string
	ActorID                       string
	Kind                          string
	CheckedPayloadFilePath        string
	WatermarkJSONFilePath         string
	CheckedSourcePath             string
	CheckedArtifactRef            string
	CheckedArtifactVersion        string
	ServicesPolicyFilePath        string
	ServicesPolicySourcePath      string
	ServicesPolicySourceRef       string
	ServicesPolicySourceCommitSHA string
	ServicesPolicySourceBlobSHA   string
	Apply                         bool
	Timeout                       time.Duration
}

type onboardingScenario struct {
	OrganizationID       string                        `json:"organization_id,omitempty"`
	ProjectSlug          string                        `json:"project_slug,omitempty"`
	ProjectDisplayName   string                        `json:"project_display_name,omitempty"`
	ProjectDescription   string                        `json:"project_description,omitempty"`
	ProjectID            string                        `json:"project_id,omitempty"`
	RepositoryID         string                        `json:"repository_id,omitempty"`
	ProviderSlug         string                        `json:"provider_slug,omitempty"`
	RepositoryFullName   string                        `json:"repository_full_name,omitempty"`
	ProviderRepositoryID string                        `json:"provider_repository_id,omitempty"`
	OwnerUserRef         string                        `json:"owner_user_ref,omitempty"`
	RepositoryBinding    *repositoryBindingScenario    `json:"repository_binding,omitempty"`
	RepositoryChange     *repositoryChangeScenario     `json:"repository_change,omitempty"`
	BootstrapSetup       *bootstrapSetup               `json:"bootstrap_setup,omitempty"`
	Bootstrap            *reconcileScenario            `json:"bootstrap,omitempty"`
	Adoption             *reconcileScenario            `json:"adoption,omitempty"`
	ServicesPolicyImport *servicesPolicyImportScenario `json:"services_policy_import,omitempty"`
}

type repositoryBindingScenario struct {
	ProviderOwner        string `json:"provider_owner,omitempty"`
	ProviderName         string `json:"provider_name,omitempty"`
	WebURL               string `json:"web_url,omitempty"`
	DefaultBranch        string `json:"default_branch,omitempty"`
	ProviderRepositoryID string `json:"provider_repository_id,omitempty"`
	Status               string `json:"status,omitempty"`
}

type repositoryChangeScenario struct {
	SignalKey    string                          `json:"signal_key,omitempty"`
	EventName    string                          `json:"event_name,omitempty"`
	Ref          string                          `json:"ref,omitempty"`
	BaseBranch   string                          `json:"base_branch,omitempty"`
	CommitSHA    string                          `json:"commit_sha,omitempty"`
	BeforeSHA    string                          `json:"before_sha,omitempty"`
	ChangedPaths []repositoryChangedPathScenario `json:"changed_paths,omitempty"`
}

type repositoryChangedPathScenario struct {
	Path         string `json:"path"`
	Action       string `json:"action,omitempty"`
	ObjectDigest string `json:"object_digest,omitempty"`
}

type bootstrapSetup struct {
	CreateRepository *bootstrapCreateRepositoryScenario `json:"create_repository,omitempty"`
	PullRequest      *bootstrapPullRequestScenario      `json:"pull_request,omitempty"`
}

type bootstrapCreateRepositoryScenario struct {
	OwnerKind         string `json:"owner_kind,omitempty"`
	ProviderOwner     string `json:"provider_owner,omitempty"`
	ProviderName      string `json:"provider_name,omitempty"`
	Visibility        string `json:"visibility,omitempty"`
	Description       string `json:"description,omitempty"`
	ExternalAccountID string `json:"external_account_id,omitempty"`
}

type bootstrapPullRequestScenario struct {
	BaseBranch        string                          `json:"base_branch,omitempty"`
	BootstrapBranch   string                          `json:"bootstrap_branch,omitempty"`
	CommitMessage     string                          `json:"commit_message,omitempty"`
	Title             string                          `json:"title,omitempty"`
	Body              string                          `json:"body,omitempty"`
	Draft             bool                            `json:"draft,omitempty"`
	Files             []bootstrapPreparedFile         `json:"files,omitempty"`
	WatermarkJSON     string                          `json:"watermark_json,omitempty"`
	ServicesPolicy    bootstrapServicesPolicyScenario `json:"services_policy,omitempty"`
	ExternalAccountID string                          `json:"external_account_id,omitempty"`
}

type bootstrapPreparedFile struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	Executable bool   `json:"executable,omitempty"`
}

type bootstrapServicesPolicyScenario struct {
	SourcePath           string `json:"source_path"`
	ContentHash          string `json:"content_hash"`
	ValidatedPayloadJSON string `json:"validated_payload_json"`
}

type reconcileScenario struct {
	SignalKey     string                `json:"signal_key,omitempty"`
	WatermarkJSON string                `json:"watermark_json,omitempty"`
	CheckedPolicy checkedPolicyScenario `json:"checked_policy"`
}

type servicesPolicyImportScenario struct {
	FilePath        string `json:"file_path,omitempty"`
	SourcePath      string `json:"source_path,omitempty"`
	SourceRef       string `json:"source_ref,omitempty"`
	SourceCommitSHA string `json:"source_commit_sha,omitempty"`
	SourceBlobSHA   string `json:"source_blob_sha,omitempty"`
}

type checkedPolicyScenario struct {
	ArtifactRef          string `json:"artifact_ref"`
	ArtifactDigest       string `json:"artifact_digest"`
	ArtifactVersion      string `json:"artifact_version"`
	SourcePath           string `json:"source_path"`
	ContentHash          string `json:"content_hash"`
	ValidatedPayloadJSON string `json:"validated_payload_json"`
}

type mergeSignalSet struct {
	Bootstrap *providersv1.RepositoryMergeSignal
	Adoption  *providersv1.RepositoryMergeSignal
}

type checkedArtifactProducer struct {
	Enabled         bool
	PayloadJSON     string
	WatermarkJSON   string
	SourcePath      string
	ArtifactRef     string
	ArtifactVersion string
}

type preparedCheckedInputs struct {
	Bootstrap bool
	Adoption  bool
}

type bootstrapSetupResult struct {
	Repository           *projectsv1.Repository
	ProviderRepositoryID string
}

type repositoryChangeSummary struct {
	EventName             string
	BaseBranch            string
	CommitSHA             string
	ChangeDigest          string
	TotalPaths            int
	ServicesPolicyChanged bool
	DeployRelevantChanged bool
	Categories            map[string]int
}

type servicesPolicyImportPlan struct {
	Enabled              bool
	SourcePath           string
	SourceRef            string
	SourceCommitSHA      string
	SourceBlobSHA        string
	ContentHash          string
	ValidatedPayloadJSON string
}

func run(ctx context.Context, options runnerOptions, clients runnerClients, output io.Writer) error {
	if clients.ProjectCatalog == nil {
		return errors.New("project-catalog client is required")
	}
	if clients.ProviderHub == nil {
		return errors.New("provider-hub client is required")
	}
	options = normalizeOptions(options)

	scenario, err := loadScenario(options.ScenarioFilePath)
	if err != nil {
		return err
	}
	options = mergeScenarioOptions(options, scenario)
	if err := validateOptions(options, scenario); err != nil {
		return err
	}
	scenario = ensureRepositoryBindingScenario(options, scenario)
	scenario = ensureServicesPolicyImportScenario(options, scenario)
	needsRunnerRepositoryReadAccess := shouldEnsureRunnerRepositoryReadAccess(scenario)
	needsSelfDeployServiceAccess := shouldEnsureSelfDeployServiceAccess(scenario)
	needsSelfDeployOwnerAccess := shouldEnsureSelfDeployOwnerAccess(options, scenario)
	needsServicesPolicyImportAccess := shouldEnsureServicesPolicyImportAccess(scenario)
	if options.Apply && (needsRunnerRepositoryReadAccess || needsSelfDeployServiceAccess || needsSelfDeployOwnerAccess || needsServicesPolicyImportAccess) {
		if err := validateApplyPolicy(options, scenario); err != nil {
			return err
		}
		if clients.AccessManager == nil {
			if needsServicesPolicyImportAccess {
				return errors.New("access-manager client is required for services policy import apply")
			}
			if needsRunnerRepositoryReadAccess && options.RepositoryID != "" {
				return errors.New("access-manager client is required for onboarding runner repository read access apply")
			}
			if needsSelfDeployOwnerAccess {
				return errors.New("access-manager client is required for self-deploy owner access apply")
			}
			return errors.New("access-manager client is required for self-deploy service access apply")
		}
	}
	options, _, err = ensureRunnerProject(ctx, clients.ProjectCatalog, options, output)
	if err != nil {
		return err
	}
	producer, err := loadCheckedArtifactProducer(options)
	if err != nil {
		return err
	}

	modeLabel := "dry-run"
	if options.Apply {
		modeLabel = "apply"
	}
	logLine(output, "PLAN", "mode=%s project_id=%s repository_id=%s provider=%s", modeLabel, safeValue(options.ProjectID), safeValue(options.RepositoryID), safeValue(options.ProviderSlug))

	var repository *projectsv1.Repository
	runnerRepositoryReadAccessEnsured := false
	if needsRunnerRepositoryReadAccess && options.RepositoryID != "" {
		if err := ensureRunnerRepositoryReadAccess(ctx, clients.AccessManager, options, output); err != nil {
			return err
		}
		runnerRepositoryReadAccessEnsured = true
	}
	if options.RepositoryID != "" {
		repository, err = checkProjectRepository(ctx, clients.ProjectCatalog, options, output)
		if err != nil {
			return err
		}
		options, err = bindOptionsToRepository(options, repository)
		if err != nil {
			return err
		}
		if scenario.RepositoryBinding != nil {
			if err := validateRepositoryBindingMatchesRepository(options, scenario.RepositoryBinding, repository); err != nil {
				return err
			}
		}
	} else if scenario.RepositoryBinding != nil {
		repository, err = findRepositoryBinding(ctx, clients.ProjectCatalog, options, scenario.RepositoryBinding, output)
		if err != nil {
			return err
		}
		if repository != nil {
			options.RepositoryID = repository.GetRepositoryId()
			options, err = bindOptionsToRepository(options, repository)
			if err != nil {
				return err
			}
			if err := validateRepositoryBindingMatchesRepository(options, scenario.RepositoryBinding, repository); err != nil {
				return err
			}
		} else {
			if err := describeRepositoryBinding(options, scenario.RepositoryBinding, output); err != nil {
				return err
			}
		}
	} else {
		logLine(output, "BLOCKED", "project repository binding is not available before bootstrap repository create")
	}
	if err := describeBootstrapSetup(options, scenario.BootstrapSetup, output); err != nil {
		return err
	}
	if err := describeRepositoryChange(options, scenario.RepositoryChange, output); err != nil {
		return err
	}
	if options.Apply {
		if err := validateApplyPolicy(options, scenario); err != nil {
			return err
		}
		if options.RepositoryID == "" && scenario.RepositoryBinding != nil {
			repository, err = attachRepositoryBinding(ctx, clients.ProjectCatalog, options, scenario.RepositoryBinding, output)
			if err != nil {
				return err
			}
			options.RepositoryID = repository.GetRepositoryId()
			options, err = bindOptionsToRepository(options, repository)
			if err != nil {
				return err
			}
		}
		setupResult, err := applyBootstrapSetup(ctx, clients.ProjectCatalog, options, scenario.BootstrapSetup, output)
		if err != nil {
			return err
		}
		if setupResult.Repository != nil {
			repository = setupResult.Repository
			if options.RepositoryID == "" {
				options.RepositoryID = repository.GetRepositoryId()
			}
			options, err = bindOptionsToRepository(options, repository)
			if err != nil {
				return err
			}
		}
		if setupResult.ProviderRepositoryID != "" {
			options.ProviderRepositoryID = setupResult.ProviderRepositoryID
		}
	}
	if needsRunnerRepositoryReadAccess && !runnerRepositoryReadAccessEnsured && options.RepositoryID != "" {
		if err := ensureRunnerRepositoryReadAccess(ctx, clients.AccessManager, options, output); err != nil {
			return err
		}
	}
	repositoryChangeSignal, err := readRepositoryChangeSignal(ctx, clients.ProviderHub, options, scenario.RepositoryChange, output)
	if err != nil {
		return err
	}
	if options.RepositoryID == "" {
		if !options.Apply {
			logLine(output, "PLAN", "apply disabled; no mutating product API calls executed")
			return nil
		}
		return errors.New("repository_id is required after bootstrap repository create")
	}
	if shouldEnsureSelfDeployServiceAccess(scenario) {
		if err := ensureSelfDeployServiceAccess(ctx, clients.AccessManager, options, output); err != nil {
			return err
		}
	}
	if shouldEnsureSelfDeployOwnerAccess(options, scenario) {
		if err := ensureSelfDeployOwnerAccess(ctx, clients.AccessManager, options, output); err != nil {
			return err
		}
	}
	if shouldEnsureServicesPolicyImportAccess(scenario) {
		if err := ensureServicesPolicyImportAccess(ctx, clients.AccessManager, options, output); err != nil {
			return err
		}
	}
	servicesPolicyImportPlan, err := prepareServicesPolicyImportPlan(options, scenario.ServicesPolicyImport, repositoryChangeSignal)
	if err != nil {
		return err
	}
	describeServicesPolicyImportPlan(output, servicesPolicyImportPlan)
	signals, err := readProviderSignals(ctx, clients.ProviderHub, options, scenario, output)
	if err != nil {
		return err
	}
	if err := readAdoptionSnapshots(ctx, clients.ProviderHub, options, output); err != nil {
		return err
	}
	scenario, prepared, err := prepareCheckedScenarioInputs(options, scenario, signals, producer)
	if err != nil {
		return err
	}
	describePreparedCheckedInputs(output, prepared)

	if wantsKind(options, "bootstrap") {
		describeCheckedInput(output, "bootstrap", scenario.Bootstrap)
	}
	if wantsKind(options, "adoption") {
		describeCheckedInput(output, "adoption", scenario.Adoption)
	}

	if !options.Apply {
		logLine(output, "PLAN", "apply disabled; no mutating product API calls executed")
		return nil
	}
	if servicesPolicyImportPlan.Enabled {
		if err := applyServicesPolicyImport(ctx, clients.ProjectCatalog, options, servicesPolicyImportPlan, output); err != nil {
			return err
		}
	}
	if err := applyReconciliation(ctx, clients.ProjectCatalog, options, scenario, signals, output, hasBootstrapSetup(scenario) || hasRepositoryBindingSetup(scenario)); err != nil {
		return err
	}
	logLine(output, "OK", "onboarding product API path completed project_id=%s repository_id=%s", safeValue(options.ProjectID), safeValue(options.RepositoryID))
	return nil
}

func newGRPCClients(options runnerOptions) (runnerClients, func(), error) {
	if strings.TrimSpace(options.ProjectCatalogAddr) == "" {
		return runnerClients{}, func() {}, errors.New("project-catalog gRPC address is required")
	}
	if strings.TrimSpace(options.ProviderHubAddr) == "" {
		return runnerClients{}, func() {}, errors.New("provider-hub gRPC address is required")
	}
	accessAuthToken := ""
	if strings.TrimSpace(options.AccessManagerAddr) != "" {
		var err error
		accessAuthToken, err = tokenFromEnvRef(options.AccessManagerAuthTokenEnv, "access-manager")
		if err != nil {
			return runnerClients{}, func() {}, err
		}
	}
	projectAuthToken, err := tokenFromEnvRef(options.ProjectCatalogAuthTokenEnv, "project-catalog")
	if err != nil {
		return runnerClients{}, func() {}, err
	}
	providerAuthToken, err := tokenFromEnvRef(options.ProviderHubAuthTokenEnv, "provider-hub")
	if err != nil {
		return runnerClients{}, func() {}, err
	}
	projectConn, err := grpc.NewClient(options.ProjectCatalogAddr, grpcClientDialOptions(projectAuthToken, options.ActorID)...)
	if err != nil {
		return runnerClients{}, func() {}, fmt.Errorf("connect project-catalog: %w", err)
	}
	providerConn, err := grpc.NewClient(options.ProviderHubAddr, grpcClientDialOptions(providerAuthToken, options.ActorID)...)
	if err != nil {
		_ = projectConn.Close()
		return runnerClients{}, func() {}, fmt.Errorf("connect provider-hub: %w", err)
	}
	var accessConn *grpc.ClientConn
	if strings.TrimSpace(options.AccessManagerAddr) != "" {
		accessConn, err = grpc.NewClient(options.AccessManagerAddr, grpcClientDialOptions(accessAuthToken, options.ActorID)...)
		if err != nil {
			_ = providerConn.Close()
			_ = projectConn.Close()
			return runnerClients{}, func() {}, fmt.Errorf("connect access-manager: %w", err)
		}
	}
	closeClients := func() {
		if accessConn != nil {
			_ = accessConn.Close()
		}
		_ = providerConn.Close()
		_ = projectConn.Close()
	}
	var accessClient accessManagerAPI
	if accessConn != nil {
		accessClient = accessaccountsv1.NewAccessManagerServiceClient(accessConn)
	}
	return runnerClients{
		ProjectCatalog: projectsv1.NewProjectCatalogServiceClient(projectConn),
		ProviderHub:    providersv1.NewProviderHubServiceClient(providerConn),
		AccessManager:  accessClient,
	}, closeClients, nil
}

func grpcClientDialOptions(authToken string, callerID string) []grpc.DialOption {
	options := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if strings.TrimSpace(authToken) != "" {
		options = append(options, grpc.WithUnaryInterceptor(outgoingAuthUnaryInterceptor(authToken, callerID)))
	}
	return options
}

func outgoingAuthUnaryInterceptor(authToken string, callerID string) grpc.UnaryClientInterceptor {
	token := strings.TrimSpace(authToken)
	caller := defaultString(strings.TrimSpace(callerID), defaultActorID)
	return func(
		ctx context.Context,
		method string,
		req any,
		reply any,
		conn *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		options ...grpc.CallOption,
	) error {
		authenticatedCtx := metadata.AppendToOutgoingContext(
			ctx,
			grpcserver.MetadataAuthorization,
			"Bearer "+token,
			grpcserver.MetadataCallerType,
			"service",
			grpcserver.MetadataCallerID,
			caller,
		)
		return invoker(authenticatedCtx, method, req, reply, conn, options...)
	}
}

func tokenFromEnvRef(envRef string, service string) (string, error) {
	envRef = strings.TrimSpace(envRef)
	if envRef == "" {
		return "", nil
	}
	token := strings.TrimSpace(os.Getenv(envRef))
	if token == "" {
		return "", fmt.Errorf("%s auth token env %s is not set", service, envRef)
	}
	return token, nil
}

func normalizeOptions(options runnerOptions) runnerOptions {
	options.AccessManagerAddr = strings.TrimSpace(options.AccessManagerAddr)
	options.ProjectCatalogAuthTokenEnv = strings.TrimSpace(options.ProjectCatalogAuthTokenEnv)
	options.ProviderHubAuthTokenEnv = strings.TrimSpace(options.ProviderHubAuthTokenEnv)
	options.AccessManagerAuthTokenEnv = strings.TrimSpace(options.AccessManagerAuthTokenEnv)
	options.OrganizationID = strings.TrimSpace(options.OrganizationID)
	options.ProjectID = strings.TrimSpace(options.ProjectID)
	options.ProjectSlug = defaultString(strings.TrimSpace(options.ProjectSlug), defaultProjectSlug)
	options.ProjectDisplayName = defaultString(strings.TrimSpace(options.ProjectDisplayName), options.ProjectSlug)
	options.ProjectDescription = strings.TrimSpace(options.ProjectDescription)
	options.RepositoryID = strings.TrimSpace(options.RepositoryID)
	options.ProviderSlug = defaultString(strings.TrimSpace(options.ProviderSlug), defaultProviderSlug)
	options.ProviderOwner = strings.TrimSpace(options.ProviderOwner)
	options.ProviderName = strings.TrimSpace(options.ProviderName)
	options.RepositoryFullName = strings.TrimSpace(options.RepositoryFullName)
	options.RepositoryDefaultBranch = defaultString(strings.TrimSpace(options.RepositoryDefaultBranch), defaultRepositoryDefaultBranch)
	options.ProviderRepositoryID = strings.TrimSpace(options.ProviderRepositoryID)
	options.ExternalAccountID = strings.TrimSpace(options.ExternalAccountID)
	options.AllowedProviderOwner = strings.TrimSpace(options.AllowedProviderOwner)
	options.RepositoryNamePrefix = strings.TrimSpace(options.RepositoryNamePrefix)
	options.AllowedProviderRepository = strings.TrimSpace(options.AllowedProviderRepository)
	options.OwnerUserRef = normalizeOwnerUserRef(options.OwnerUserRef)
	options.IdempotencyKey = strings.TrimSpace(options.IdempotencyKey)
	options.RequestID = defaultString(strings.TrimSpace(options.RequestID), "onboarding-runner-"+time.Now().UTC().Format("20060102T150405Z"))
	options.ActorID = defaultString(strings.TrimSpace(options.ActorID), defaultActorID)
	options.Kind = strings.ToLower(strings.TrimSpace(options.Kind))
	options.CheckedPayloadFilePath = strings.TrimSpace(options.CheckedPayloadFilePath)
	options.WatermarkJSONFilePath = strings.TrimSpace(options.WatermarkJSONFilePath)
	options.CheckedSourcePath = defaultString(strings.TrimSpace(options.CheckedSourcePath), defaultCheckedSourcePath)
	options.CheckedArtifactRef = strings.TrimSpace(options.CheckedArtifactRef)
	options.CheckedArtifactVersion = strings.TrimSpace(options.CheckedArtifactVersion)
	options.ServicesPolicyFilePath = strings.TrimSpace(options.ServicesPolicyFilePath)
	options.ServicesPolicySourcePath = defaultString(strings.TrimSpace(options.ServicesPolicySourcePath), defaultCheckedSourcePath)
	options.ServicesPolicySourceRef = strings.TrimSpace(options.ServicesPolicySourceRef)
	options.ServicesPolicySourceCommitSHA = strings.ToLower(strings.TrimSpace(options.ServicesPolicySourceCommitSHA))
	options.ServicesPolicySourceBlobSHA = strings.TrimSpace(options.ServicesPolicySourceBlobSHA)
	if options.Kind == "" {
		options.Kind = "both"
	}
	return options
}

func mergeScenarioOptions(options runnerOptions, scenario onboardingScenario) runnerOptions {
	if options.OrganizationID == "" {
		options.OrganizationID = strings.TrimSpace(scenario.OrganizationID)
	}
	if options.ProjectID == "" {
		options.ProjectID = strings.TrimSpace(scenario.ProjectID)
	}
	if options.ProjectSlug == "" || options.ProjectSlug == defaultProjectSlug {
		options.ProjectSlug = defaultString(strings.TrimSpace(scenario.ProjectSlug), options.ProjectSlug)
	}
	if options.ProjectDisplayName == "" || options.ProjectDisplayName == defaultProjectDisplayName {
		options.ProjectDisplayName = defaultString(strings.TrimSpace(scenario.ProjectDisplayName), options.ProjectDisplayName)
	}
	if options.ProjectDescription == "" {
		options.ProjectDescription = strings.TrimSpace(scenario.ProjectDescription)
	}
	if options.RepositoryID == "" {
		options.RepositoryID = strings.TrimSpace(scenario.RepositoryID)
	}
	if options.ProviderSlug == "" || options.ProviderSlug == defaultProviderSlug {
		options.ProviderSlug = defaultString(strings.TrimSpace(scenario.ProviderSlug), options.ProviderSlug)
	}
	if options.RepositoryFullName == "" {
		options.RepositoryFullName = strings.TrimSpace(scenario.RepositoryFullName)
	}
	if options.ProviderRepositoryID == "" {
		options.ProviderRepositoryID = strings.TrimSpace(scenario.ProviderRepositoryID)
	}
	if options.OwnerUserRef == "" {
		options.OwnerUserRef = normalizeOwnerUserRef(scenario.OwnerUserRef)
	}
	if options.ProviderOwner == "" && options.ProviderName == "" && options.RepositoryFullName != "" {
		owner, name, ok := splitRepositoryFullName(options.RepositoryFullName)
		if ok {
			options.ProviderOwner = owner
			options.ProviderName = name
		}
	}
	if options.RepositoryFullName == "" && options.ProviderOwner != "" && options.ProviderName != "" {
		options.RepositoryFullName = options.ProviderOwner + "/" + options.ProviderName
	}
	if options.RepositoryFullName == "" && scenario.RepositoryBinding != nil {
		binding := scenario.RepositoryBinding
		if strings.TrimSpace(binding.ProviderOwner) != "" && strings.TrimSpace(binding.ProviderName) != "" {
			options.RepositoryFullName = strings.TrimSpace(binding.ProviderOwner) + "/" + strings.TrimSpace(binding.ProviderName)
		}
	}
	if options.ProviderRepositoryID == "" && scenario.RepositoryBinding != nil {
		options.ProviderRepositoryID = strings.TrimSpace(scenario.RepositoryBinding.ProviderRepositoryID)
	}
	if scenario.ServicesPolicyImport != nil {
		if options.ServicesPolicyFilePath == "" {
			options.ServicesPolicyFilePath = strings.TrimSpace(scenario.ServicesPolicyImport.FilePath)
		}
		if options.ServicesPolicySourcePath == "" || options.ServicesPolicySourcePath == defaultCheckedSourcePath {
			options.ServicesPolicySourcePath = defaultString(strings.TrimSpace(scenario.ServicesPolicyImport.SourcePath), options.ServicesPolicySourcePath)
		}
		if options.ServicesPolicySourceRef == "" {
			options.ServicesPolicySourceRef = strings.TrimSpace(scenario.ServicesPolicyImport.SourceRef)
		}
		if options.ServicesPolicySourceCommitSHA == "" {
			options.ServicesPolicySourceCommitSHA = strings.ToLower(strings.TrimSpace(scenario.ServicesPolicyImport.SourceCommitSHA))
		}
		if options.ServicesPolicySourceBlobSHA == "" {
			options.ServicesPolicySourceBlobSHA = strings.TrimSpace(scenario.ServicesPolicyImport.SourceBlobSHA)
		}
	}
	if options.RepositoryFullName == "" && scenario.BootstrapSetup != nil && scenario.BootstrapSetup.CreateRepository != nil {
		create := scenario.BootstrapSetup.CreateRepository
		if strings.TrimSpace(create.ProviderOwner) != "" && strings.TrimSpace(create.ProviderName) != "" {
			options.RepositoryFullName = strings.TrimSpace(create.ProviderOwner) + "/" + strings.TrimSpace(create.ProviderName)
		}
	}
	if options.ExternalAccountID == "" && scenario.BootstrapSetup != nil {
		if scenario.BootstrapSetup.CreateRepository != nil {
			options.ExternalAccountID = strings.TrimSpace(scenario.BootstrapSetup.CreateRepository.ExternalAccountID)
		}
		if options.ExternalAccountID == "" && scenario.BootstrapSetup.PullRequest != nil {
			options.ExternalAccountID = strings.TrimSpace(scenario.BootstrapSetup.PullRequest.ExternalAccountID)
		}
	}
	return options
}

func validateOptions(options runnerOptions, scenario onboardingScenario) error {
	if options.ProjectID == "" {
		if options.OrganizationID == "" {
			return errors.New("organization_id is required when project_id is not provided")
		}
		if options.ProjectSlug == "" {
			return errors.New("project_slug is required when project_id is not provided")
		}
	}
	if options.RepositoryID == "" && !hasBootstrapCreateRepository(scenario) && !hasRepositoryBindingSetup(scenario) {
		if options.ProviderOwner == "" || options.ProviderName == "" {
			return errors.New("repository_id or provider owner/name is required")
		}
	}
	if options.ProviderSlug == "" {
		return errors.New("provider_slug is required")
	}
	if err := validateOwnerUserRef(options.OwnerUserRef); err != nil {
		return err
	}
	switch options.Kind {
	case "bootstrap", "adoption", "both":
	default:
		return fmt.Errorf("kind must be bootstrap, adoption or both")
	}
	if hasBootstrapSetup(scenario) && !wantsKind(options, "bootstrap") {
		return errors.New("bootstrap_setup requires kind bootstrap or both")
	}
	if hasRepositoryBindingSetup(scenario) && hasBootstrapCreateRepository(scenario) {
		return errors.New("repository_binding cannot be combined with bootstrap_setup.create_repository")
	}
	if options.CheckedPayloadFilePath != "" {
		if options.Kind == "both" {
			return errors.New("checked payload producer requires kind bootstrap or adoption")
		}
		if options.CheckedSourcePath == "" {
			return errors.New("checked payload producer requires checked source path")
		}
	}
	if options.ServicesPolicyFilePath != "" && options.ServicesPolicySourcePath == "" {
		return errors.New("services policy import requires source path")
	}
	return nil
}

func ensureRunnerProject(ctx context.Context, client projectCatalogAPI, options runnerOptions, output io.Writer) (runnerOptions, *projectsv1.Project, error) {
	if options.ProjectID != "" {
		return options, nil, nil
	}
	project, err := findRunnerProject(ctx, client, options)
	if err != nil {
		return options, nil, err
	}
	if project != nil {
		options.ProjectID = project.GetProjectId()
		logLine(output, "OK", "project ready project_id=%s slug=%s status=%s version=%d",
			safeValue(project.GetProjectId()),
			safeValue(project.GetSlug()),
			project.GetStatus().String(),
			project.GetVersion(),
		)
		return options, project, nil
	}
	if !options.Apply {
		logLine(output, "PLAN", "project create ready organization_id=%s slug=%s display_name=%s",
			safeValue(options.OrganizationID),
			safeValue(options.ProjectSlug),
			safeValue(options.ProjectDisplayName),
		)
		return options, nil, nil
	}
	response, err := client.CreateProject(ctx, &projectsv1.CreateProjectRequest{
		OrganizationId: options.OrganizationID,
		Slug:           options.ProjectSlug,
		DisplayName:    options.ProjectDisplayName,
		Description:    optionalString(options.ProjectDescription),
		Status:         optionalProjectStatus(projectsv1.ProjectStatus_PROJECT_STATUS_ACTIVE),
		Meta:           projectCreateCommandMeta(options),
	})
	if err != nil {
		return options, nil, safeError("project-catalog CreateProject failed", err)
	}
	project = response.GetProject()
	if project == nil {
		return options, nil, errors.New("project-catalog CreateProject returned empty project")
	}
	if project.GetStatus() != projectsv1.ProjectStatus_PROJECT_STATUS_ACTIVE {
		return options, nil, fmt.Errorf("created project status %s is not active", project.GetStatus().String())
	}
	options.ProjectID = project.GetProjectId()
	logLine(output, "OK", "project created project_id=%s slug=%s version=%d",
		safeValue(project.GetProjectId()),
		safeValue(project.GetSlug()),
		project.GetVersion(),
	)
	return options, project, nil
}

func findRunnerProject(ctx context.Context, client projectCatalogAPI, options runnerOptions) (*projectsv1.Project, error) {
	pageToken := ""
	var displayNameMatch *projectsv1.Project
	for {
		response, err := client.ListProjects(ctx, &projectsv1.ListProjectsRequest{
			OrganizationId: optionalString(options.OrganizationID),
			Page:           &projectsv1.PageRequest{PageSize: 100, PageToken: optionalString(pageToken)},
			Meta:           projectQueryMeta(options),
		})
		if err != nil {
			return nil, safeError("project-catalog ListProjects failed", err)
		}
		for _, project := range response.GetProjects() {
			if project.GetSlug() == options.ProjectSlug {
				return activeRunnerProject(project, "slug")
			}
			if options.ProjectDisplayName != "" && project.GetDisplayName() == options.ProjectDisplayName {
				if displayNameMatch != nil {
					return nil, errors.New("project lookup by display name is ambiguous")
				}
				displayNameMatch = project
			}
		}
		pageToken = response.GetPage().GetNextPageToken()
		if pageToken == "" {
			break
		}
	}
	if displayNameMatch != nil {
		return activeRunnerProject(displayNameMatch, "display_name")
	}
	return nil, nil
}

func activeRunnerProject(project *projectsv1.Project, matchedBy string) (*projectsv1.Project, error) {
	if project.GetStatus() != projectsv1.ProjectStatus_PROJECT_STATUS_ACTIVE {
		return nil, fmt.Errorf("project matched by %s is %s, active project is required", matchedBy, project.GetStatus().String())
	}
	return project, nil
}

func ensureRepositoryBindingScenario(options runnerOptions, scenario onboardingScenario) onboardingScenario {
	if scenario.RepositoryBinding != nil || options.RepositoryID != "" || hasBootstrapCreateRepository(scenario) {
		return scenario
	}
	owner := options.ProviderOwner
	name := options.ProviderName
	if owner == "" || name == "" {
		fullOwner, fullName, ok := splitRepositoryFullName(options.RepositoryFullName)
		if !ok {
			return scenario
		}
		owner = fullOwner
		name = fullName
	}
	scenario.RepositoryBinding = &repositoryBindingScenario{
		ProviderOwner:        owner,
		ProviderName:         name,
		DefaultBranch:        defaultString(options.RepositoryDefaultBranch, defaultRepositoryDefaultBranch),
		ProviderRepositoryID: options.ProviderRepositoryID,
		Status:               "active",
	}
	return scenario
}

func ensureServicesPolicyImportScenario(options runnerOptions, scenario onboardingScenario) onboardingScenario {
	if scenario.ServicesPolicyImport != nil || options.ServicesPolicyFilePath == "" {
		return scenario
	}
	scenario.ServicesPolicyImport = &servicesPolicyImportScenario{
		FilePath:        options.ServicesPolicyFilePath,
		SourcePath:      options.ServicesPolicySourcePath,
		SourceRef:       options.ServicesPolicySourceRef,
		SourceCommitSHA: options.ServicesPolicySourceCommitSHA,
		SourceBlobSHA:   options.ServicesPolicySourceBlobSHA,
	}
	return scenario
}

func loadCheckedArtifactProducer(options runnerOptions) (checkedArtifactProducer, error) {
	if options.CheckedPayloadFilePath == "" {
		return checkedArtifactProducer{}, nil
	}
	payloadJSON, err := readBoundedJSONObject(options.CheckedPayloadFilePath, maxCheckedPayloadBytes, "checked payload file")
	if err != nil {
		return checkedArtifactProducer{}, err
	}
	watermarkJSON := ""
	if options.WatermarkJSONFilePath != "" {
		watermarkJSON, err = readBoundedJSONObject(options.WatermarkJSONFilePath, maxWatermarkJSONBytes, "watermark JSON file")
		if err != nil {
			return checkedArtifactProducer{}, err
		}
	}
	return checkedArtifactProducer{
		Enabled:         true,
		PayloadJSON:     payloadJSON,
		WatermarkJSON:   watermarkJSON,
		SourcePath:      options.CheckedSourcePath,
		ArtifactRef:     options.CheckedArtifactRef,
		ArtifactVersion: options.CheckedArtifactVersion,
	}, nil
}

func readBoundedJSONObject(path string, limit int, label string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	if len(content) > limit {
		return "", fmt.Errorf("%s exceeds %d bytes", label, limit)
	}
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return "", fmt.Errorf("%s is empty", label)
	}
	if !json.Valid([]byte(trimmed)) {
		return "", fmt.Errorf("%s must contain valid JSON", label)
	}
	if !strings.HasPrefix(trimmed, "{") {
		return "", fmt.Errorf("%s must contain a JSON object", label)
	}
	return trimmed, nil
}

func prepareCheckedScenarioInputs(options runnerOptions, scenario onboardingScenario, signals mergeSignalSet, producer checkedArtifactProducer) (onboardingScenario, preparedCheckedInputs, error) {
	if !producer.Enabled {
		return scenario, preparedCheckedInputs{}, nil
	}
	switch options.Kind {
	case "bootstrap":
		updated, prepared, err := prepareCheckedScenarioInput("bootstrap", scenario.Bootstrap, signals.Bootstrap, producer)
		if err != nil {
			return scenario, preparedCheckedInputs{}, err
		}
		scenario.Bootstrap = updated
		return scenario, preparedCheckedInputs{Bootstrap: prepared}, nil
	case "adoption":
		updated, prepared, err := prepareCheckedScenarioInput("adoption", scenario.Adoption, signals.Adoption, producer)
		if err != nil {
			return scenario, preparedCheckedInputs{}, err
		}
		scenario.Adoption = updated
		return scenario, preparedCheckedInputs{Adoption: prepared}, nil
	default:
		return scenario, preparedCheckedInputs{}, errors.New("checked payload producer requires a single kind")
	}
}

func prepareCheckedScenarioInput(kind string, scenario *reconcileScenario, signal *providersv1.RepositoryMergeSignal, producer checkedArtifactProducer) (*reconcileScenario, bool, error) {
	if scenario == nil {
		scenario = &reconcileScenario{}
	}
	if hasCheckedPolicyValues(scenario.CheckedPolicy) {
		return scenario, false, fmt.Errorf("%s checked payload producer cannot override existing checked_policy", kind)
	}
	watermarkJSON := firstNonEmpty(scenario.WatermarkJSON, producer.WatermarkJSON)
	if watermarkJSON == "" {
		return scenario, false, fmt.Errorf("%s checked payload producer requires watermark_json in scenario or watermark-json-file", kind)
	}
	contentHash := contentSHA256(producer.PayloadJSON)
	artifactDigest := contentHash
	artifactVersion := firstNonEmpty(producer.ArtifactVersion)
	if artifactVersion == "" && signal != nil {
		artifactVersion = signal.GetMergeCommitSha()
	}
	if artifactVersion == "" {
		artifactVersion = "pending-provider-merge-signal"
	}
	artifactRef := firstNonEmpty(producer.ArtifactRef)
	if artifactRef == "" {
		artifactRef = defaultCheckedArtifactRef(kind, artifactDigest)
	}
	scenario.WatermarkJSON = watermarkJSON
	scenario.CheckedPolicy = checkedPolicyScenario{
		ArtifactRef:          artifactRef,
		ArtifactDigest:       artifactDigest,
		ArtifactVersion:      artifactVersion,
		SourcePath:           producer.SourcePath,
		ContentHash:          contentHash,
		ValidatedPayloadJSON: producer.PayloadJSON,
	}
	return scenario, true, nil
}

func hasCheckedPolicyValues(policy checkedPolicyScenario) bool {
	return strings.TrimSpace(policy.ArtifactRef) != "" ||
		strings.TrimSpace(policy.ArtifactDigest) != "" ||
		strings.TrimSpace(policy.ArtifactVersion) != "" ||
		strings.TrimSpace(policy.SourcePath) != "" ||
		strings.TrimSpace(policy.ContentHash) != "" ||
		strings.TrimSpace(policy.ValidatedPayloadJSON) != ""
}

func prepareServicesPolicyImportPlan(options runnerOptions, scenario *servicesPolicyImportScenario, signal *providersv1.RepositoryChangeSignal) (servicesPolicyImportPlan, error) {
	if scenario == nil {
		return servicesPolicyImportPlan{}, nil
	}
	filePath := firstNonEmpty(scenario.FilePath, options.ServicesPolicyFilePath)
	if strings.TrimSpace(filePath) == "" {
		return servicesPolicyImportPlan{}, errors.New("services_policy_import requires file_path or --services-policy-file")
	}
	sourcePath := defaultString(firstNonEmpty(scenario.SourcePath, options.ServicesPolicySourcePath), defaultCheckedSourcePath)
	sourceRef := servicesPolicyImportSourceRef(options, scenario, signal)
	sourceCommitSHA := servicesPolicyImportSourceCommitSHA(options, scenario, signal)
	if !validGitCommitSHA(sourceCommitSHA) {
		return servicesPolicyImportPlan{}, errors.New("services_policy_import requires source_commit_sha or repository_change commit_sha")
	}
	sourceBlobSHA := firstNonEmpty(scenario.SourceBlobSHA, options.ServicesPolicySourceBlobSHA)
	payloadJSON, contentHash, err := readServicesPolicyImportPayload(filePath)
	if err != nil {
		return servicesPolicyImportPlan{}, err
	}
	return servicesPolicyImportPlan{
		Enabled:              true,
		SourcePath:           sourcePath,
		SourceRef:            sourceRef,
		SourceCommitSHA:      sourceCommitSHA,
		SourceBlobSHA:        sourceBlobSHA,
		ContentHash:          contentHash,
		ValidatedPayloadJSON: payloadJSON,
	}, nil
}

func servicesPolicyImportSourceRef(options runnerOptions, scenario *servicesPolicyImportScenario, signal *providersv1.RepositoryChangeSignal) string {
	value := firstNonEmpty(scenario.SourceRef, options.ServicesPolicySourceRef)
	if value != "" {
		return value
	}
	if signal != nil && signal.GetRef() != "" {
		return signal.GetRef()
	}
	branch := firstNonEmpty(options.RepositoryDefaultBranch, defaultRepositoryDefaultBranch)
	return "refs/heads/" + branch
}

func servicesPolicyImportSourceCommitSHA(options runnerOptions, scenario *servicesPolicyImportScenario, signal *providersv1.RepositoryChangeSignal) string {
	if commit := strings.ToLower(firstNonEmpty(scenario.SourceCommitSHA, options.ServicesPolicySourceCommitSHA)); commit != "" {
		return commit
	}
	if signal != nil {
		return strings.ToLower(strings.TrimSpace(signal.GetCommitSha()))
	}
	return ""
}

func readServicesPolicyImportPayload(path string) (string, string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read services policy file: %w", err)
	}
	if len(content) > maxServicesPolicyImportBytes {
		return "", "", fmt.Errorf("services policy file exceeds %d bytes", maxServicesPolicyImportBytes)
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return "", "", errors.New("services policy file is empty")
	}
	var decoded any
	if err := yaml.Unmarshal(content, &decoded); err != nil {
		return "", "", fmt.Errorf("parse services policy file: %w", err)
	}
	normalized, err := yamlJSONValue(decoded)
	if err != nil {
		return "", "", err
	}
	if _, ok := normalized.(map[string]any); !ok {
		return "", "", errors.New("services policy file must contain an object")
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", "", fmt.Errorf("normalize services policy file: %w", err)
	}
	payloadJSON := string(payload)
	return payloadJSON, contentSHA256(payloadJSON), nil
}

func yamlJSONValue(value any) (any, error) {
	switch typed := value.(type) {
	case nil, string, bool, int, int64, float64:
		return typed, nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			normalized, err := yamlJSONValue(nested)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case map[any]any:
		result := make(map[string]any, len(typed))
		for rawKey, nested := range typed {
			key, ok := rawKey.(string)
			if !ok {
				return nil, errors.New("services policy file contains a non-string key")
			}
			normalized, err := yamlJSONValue(nested)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case []any:
		result := make([]any, 0, len(typed))
		for _, nested := range typed {
			normalized, err := yamlJSONValue(nested)
			if err != nil {
				return nil, err
			}
			result = append(result, normalized)
		}
		return result, nil
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return nil, errors.New("services policy file contains an unsupported value")
		}
		var normalized any
		if err := json.Unmarshal(encoded, &normalized); err != nil {
			return nil, errors.New("services policy file contains an unsupported value")
		}
		return normalized, nil
	}
}

func describeServicesPolicyImportPlan(output io.Writer, plan servicesPolicyImportPlan) {
	if !plan.Enabled {
		return
	}
	logLine(output, "PLAN", "services policy import ready source_path=%s source_ref=%s commit=%s content_hash=%s checked_input=present_hidden",
		safeValue(plan.SourcePath),
		safeValue(plan.SourceRef),
		safeValue(plan.SourceCommitSHA),
		safeValue(plan.ContentHash),
	)
}

func applyServicesPolicyImport(ctx context.Context, client projectCatalogAPI, options runnerOptions, plan servicesPolicyImportPlan, output io.Writer) error {
	if !plan.Enabled {
		return nil
	}
	response, err := client.ImportServicesPolicy(ctx, &projectsv1.ImportServicesPolicyRequest{
		ProjectId:            options.ProjectID,
		SourceRepositoryId:   optionalString(options.RepositoryID),
		SourcePath:           plan.SourcePath,
		SourceRef:            optionalString(plan.SourceRef),
		SourceCommitSha:      plan.SourceCommitSHA,
		SourceBlobSha:        optionalString(plan.SourceBlobSHA),
		ContentHash:          plan.ContentHash,
		ValidatedPayloadJson: plan.ValidatedPayloadJSON,
		ValidationStatus:     projectsv1.ServicesPolicyValidationStatus_SERVICES_POLICY_VALIDATION_STATUS_VALID,
		Meta:                 projectStageCommandMeta(options, "services-policy-import", plan.SourcePath+"@"+plan.SourceCommitSHA, plan.ContentHash),
	})
	if err != nil {
		return safeError("project-catalog ImportServicesPolicy failed", err)
	}
	policy := response.GetServicesPolicy()
	if policy == nil {
		return errors.New("project-catalog ImportServicesPolicy returned empty services policy")
	}
	logLine(output, "OK", "services policy import completed policy_id=%s source_path=%s source_ref=%s commit=%s content_hash=%s version=%d validation_status=%s",
		safeValue(policy.GetServicesPolicyId()),
		safeValue(policy.GetSourcePath()),
		safeValue(policy.GetSourceRef()),
		safeValue(policy.GetSourceCommitSha()),
		safeValue(policy.GetContentHash()),
		policy.GetPolicyVersion(),
		policy.GetValidationStatus().String(),
	)
	return nil
}

func contentSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func defaultCheckedArtifactRef(kind string, artifactDigest string) string {
	shortDigest := strings.TrimPrefix(artifactDigest, "sha256:")
	if len(shortDigest) > 24 {
		shortDigest = shortDigest[:24]
	}
	return "checked://onboarding/" + kind + "/" + shortDigest
}

func validateApplyPolicy(options runnerOptions, scenario onboardingScenario) error {
	if options.AllowedProviderOwner == "" {
		return errors.New("apply requires allowed provider owner")
	}
	repositoryFullName := applyTargetRepositoryFullName(options, scenario)
	owner, name, ok := splitRepositoryFullName(repositoryFullName)
	if !ok {
		return errors.New("apply requires repository_full_name as owner/name")
	}
	if owner != options.AllowedProviderOwner {
		return fmt.Errorf("apply target owner %q is not allowed", owner)
	}
	if hasRepositoryBindingSetup(scenario) {
		if options.AllowedProviderRepository == "" {
			return errors.New("repository_binding apply requires allowed provider repository")
		}
		if name != options.AllowedProviderRepository {
			return fmt.Errorf("apply target repository %q is not allowed", name)
		}
		return nil
	}
	if options.RepositoryNamePrefix == "" && options.AllowedProviderRepository == "" {
		return errors.New("apply requires repository name prefix or allowed provider repository")
	}
	if options.RepositoryNamePrefix != "" && !strings.HasPrefix(name, options.RepositoryNamePrefix) {
		return fmt.Errorf("apply target repository must start with %q", options.RepositoryNamePrefix)
	}
	if options.AllowedProviderRepository != "" && name != options.AllowedProviderRepository {
		return fmt.Errorf("apply target repository %q is not allowed", name)
	}
	return nil
}

func applyTargetRepositoryFullName(options runnerOptions, scenario onboardingScenario) string {
	if scenario.BootstrapSetup != nil && scenario.BootstrapSetup.CreateRepository != nil {
		create := scenario.BootstrapSetup.CreateRepository
		return strings.TrimSpace(create.ProviderOwner) + "/" + strings.TrimSpace(create.ProviderName)
	}
	if scenario.RepositoryBinding != nil {
		binding := scenario.RepositoryBinding
		return strings.TrimSpace(binding.ProviderOwner) + "/" + strings.TrimSpace(binding.ProviderName)
	}
	return options.RepositoryFullName
}

func hasBootstrapSetup(scenario onboardingScenario) bool {
	return scenario.BootstrapSetup != nil && (scenario.BootstrapSetup.CreateRepository != nil || scenario.BootstrapSetup.PullRequest != nil)
}

func hasBootstrapCreateRepository(scenario onboardingScenario) bool {
	return scenario.BootstrapSetup != nil && scenario.BootstrapSetup.CreateRepository != nil
}

func hasRepositoryBindingSetup(scenario onboardingScenario) bool {
	return scenario.RepositoryBinding != nil
}

func shouldEnsureSelfDeployServiceAccess(scenario onboardingScenario) bool {
	return scenario.RepositoryBinding != nil || scenario.RepositoryChange != nil
}

func shouldEnsureSelfDeployOwnerAccess(options runnerOptions, scenario onboardingScenario) bool {
	return shouldEnsureSelfDeployServiceAccess(scenario) && options.OwnerUserRef != ""
}

func shouldEnsureServicesPolicyImportAccess(scenario onboardingScenario) bool {
	return scenario.ServicesPolicyImport != nil
}

func shouldEnsureRunnerRepositoryReadAccess(scenario onboardingScenario) bool {
	return scenario.RepositoryBinding != nil || scenario.RepositoryChange != nil || scenario.ServicesPolicyImport != nil
}

func selfDeployOwnerAccessScopeID(options runnerOptions, grant selfDeployOwnerAccessGrant) string {
	if grant.scopeType == accesscatalog.ScopeProject {
		return options.ProjectID
	}
	return ""
}

func selfDeployOwnerAccessScopeLabel(options runnerOptions, grant selfDeployOwnerAccessGrant) string {
	if grant.scopeType == accesscatalog.ScopeProject {
		return accesscatalog.ScopeProject + ":" + safeValue(options.ProjectID)
	}
	return accesscatalog.ScopeGlobal
}

func normalizeOwnerUserRef(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "user/")
	return strings.TrimSpace(value)
}

func validateOwnerUserRef(value string) error {
	if value == "" {
		return nil
	}
	if strings.Contains(value, "@") || strings.ContainsAny(value, "{}\n\r\t") {
		return errors.New("owner_user_ref must be a safe user ref, not email or raw identity data")
	}
	if len(value) > 256 {
		return errors.New("owner_user_ref is too long")
	}
	return nil
}

func loadScenario(path string) (onboardingScenario, error) {
	if strings.TrimSpace(path) == "" {
		return onboardingScenario{}, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return onboardingScenario{}, fmt.Errorf("read scenario file: %w", err)
	}
	var scenario onboardingScenario
	if err := json.Unmarshal(content, &scenario); err != nil {
		return onboardingScenario{}, fmt.Errorf("parse scenario file: %w", err)
	}
	return scenario, nil
}

func checkProjectRepository(ctx context.Context, client projectCatalogAPI, options runnerOptions, output io.Writer) (*projectsv1.Repository, error) {
	response, err := client.GetRepository(ctx, &projectsv1.GetRepositoryRequest{
		RepositoryId: options.RepositoryID,
		Meta:         projectQueryMeta(options),
	})
	if err != nil {
		return nil, safeError("project-catalog GetRepository failed", err)
	}
	repository := response.GetRepository()
	if repository == nil {
		return nil, errors.New("project-catalog GetRepository returned empty repository")
	}
	logLine(output, "OK", "project repository binding ready status=%s version=%d provider_owner=%s provider_name=%s base_branch=%s",
		repository.GetStatus().String(),
		repository.GetVersion(),
		safeValue(repository.GetProviderOwner()),
		safeValue(repository.GetProviderName()),
		safeValue(repository.GetDefaultBranch()),
	)
	return repository, nil
}

func bindOptionsToRepository(options runnerOptions, repository *projectsv1.Repository) (runnerOptions, error) {
	if repository.GetProjectId() != options.ProjectID {
		return options, fmt.Errorf("repository binding project_id mismatch")
	}
	if repository.GetRepositoryId() != options.RepositoryID {
		return options, fmt.Errorf("repository binding repository_id mismatch")
	}
	providerSlug, err := projectRepositoryProviderSlug(repository.GetProvider())
	if err != nil {
		return options, err
	}
	if providerSlug != options.ProviderSlug {
		return options, fmt.Errorf("repository binding provider mismatch")
	}
	repositoryFullName := repository.GetProviderOwner() + "/" + repository.GetProviderName()
	if _, _, ok := splitRepositoryFullName(repositoryFullName); !ok {
		return options, fmt.Errorf("repository binding provider owner/name is incomplete")
	}
	if options.RepositoryFullName != "" && options.RepositoryFullName != repositoryFullName {
		return options, fmt.Errorf("repository binding repository_full_name mismatch")
	}
	options.RepositoryFullName = repositoryFullName

	bindingProviderRepositoryID := repository.GetProviderRepositoryId()
	if options.ProviderRepositoryID != "" && bindingProviderRepositoryID == "" {
		return options, fmt.Errorf("repository binding provider_repository_id is not available for verification")
	}
	if options.ProviderRepositoryID != "" && options.ProviderRepositoryID != bindingProviderRepositoryID {
		return options, fmt.Errorf("repository binding provider_repository_id mismatch")
	}
	options.ProviderRepositoryID = bindingProviderRepositoryID
	return options, nil
}

func describeRepositoryBinding(options runnerOptions, binding *repositoryBindingScenario, output io.Writer) error {
	if binding == nil {
		return nil
	}
	if err := validateRepositoryBindingScenario(options, binding); err != nil {
		return err
	}
	logLine(output, "PLAN", "repository binding attach ready target=%s/%s base=%s provider_repository_id=%s",
		safeValue(binding.ProviderOwner),
		safeValue(binding.ProviderName),
		safeValue(binding.DefaultBranch),
		safeValue(binding.ProviderRepositoryID),
	)
	return nil
}

func attachRepositoryBinding(ctx context.Context, client projectCatalogAPI, options runnerOptions, scenario *repositoryBindingScenario, output io.Writer) (*projectsv1.Repository, error) {
	if err := validateRepositoryBindingScenario(options, scenario); err != nil {
		return nil, err
	}
	status, err := repositoryStatusFromString(scenario.Status)
	if err != nil {
		return nil, err
	}
	target := strings.TrimSpace(scenario.ProviderOwner) + "/" + strings.TrimSpace(scenario.ProviderName)
	response, err := client.AttachRepository(ctx, &projectsv1.AttachRepositoryRequest{
		ProjectId:            options.ProjectID,
		Provider:             projectRepositoryProviderFromSlug(options.ProviderSlug),
		ProviderOwner:        strings.TrimSpace(scenario.ProviderOwner),
		ProviderName:         strings.TrimSpace(scenario.ProviderName),
		WebUrl:               strings.TrimSpace(scenario.WebURL),
		DefaultBranch:        strings.TrimSpace(scenario.DefaultBranch),
		ProviderRepositoryId: optionalString(scenario.ProviderRepositoryID),
		Status:               optionalRepositoryStatus(status),
		Meta:                 projectStageCommandMeta(options, "repository-binding", target, ""),
	})
	if err != nil {
		return nil, safeError("project-catalog AttachRepository failed", err)
	}
	repository := response.GetRepository()
	if repository == nil {
		return nil, errors.New("project-catalog AttachRepository returned empty repository")
	}
	logLine(output, "OK", "repository binding attached repository_id=%s target=%s/%s base=%s version=%d",
		safeValue(repository.GetRepositoryId()),
		safeValue(repository.GetProviderOwner()),
		safeValue(repository.GetProviderName()),
		safeValue(repository.GetDefaultBranch()),
		repository.GetVersion(),
	)
	return repository, nil
}

func findRepositoryBinding(ctx context.Context, client projectCatalogAPI, options runnerOptions, scenario *repositoryBindingScenario, output io.Writer) (*projectsv1.Repository, error) {
	if scenario == nil || options.ProjectID == "" {
		return nil, nil
	}
	if err := validateRepositoryBindingScenario(options, scenario); err != nil {
		return nil, err
	}
	targetOwner := strings.TrimSpace(scenario.ProviderOwner)
	targetName := strings.TrimSpace(scenario.ProviderName)
	pageToken := ""
	for {
		response, err := client.ListRepositories(ctx, &projectsv1.ListRepositoriesRequest{
			ProjectId: options.ProjectID,
			Statuses: []projectsv1.RepositoryStatus{
				projectsv1.RepositoryStatus_REPOSITORY_STATUS_ACTIVE,
				projectsv1.RepositoryStatus_REPOSITORY_STATUS_PENDING,
				projectsv1.RepositoryStatus_REPOSITORY_STATUS_BLOCKED,
			},
			Page: &projectsv1.PageRequest{PageSize: 100, PageToken: optionalString(pageToken)},
			Meta: projectQueryMeta(options),
		})
		if err != nil {
			return nil, safeError("project-catalog ListRepositories failed", err)
		}
		for _, repository := range response.GetRepositories() {
			if repository.GetProvider() != projectRepositoryProviderFromSlug(options.ProviderSlug) {
				continue
			}
			if repository.GetProviderOwner() != targetOwner || repository.GetProviderName() != targetName {
				continue
			}
			if repository.GetStatus() != projectsv1.RepositoryStatus_REPOSITORY_STATUS_ACTIVE {
				return nil, fmt.Errorf("repository binding exists with status %s, active binding is required", repository.GetStatus().String())
			}
			logLine(output, "OK", "repository binding ready repository_id=%s target=%s/%s base=%s version=%d",
				safeValue(repository.GetRepositoryId()),
				safeValue(repository.GetProviderOwner()),
				safeValue(repository.GetProviderName()),
				safeValue(repository.GetDefaultBranch()),
				repository.GetVersion(),
			)
			return repository, nil
		}
		pageToken = response.GetPage().GetNextPageToken()
		if pageToken == "" {
			break
		}
	}
	return nil, nil
}

func validateRepositoryBindingScenario(options runnerOptions, scenario *repositoryBindingScenario) error {
	if scenario == nil {
		return nil
	}
	if projectRepositoryProviderFromSlug(options.ProviderSlug) == projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_UNSPECIFIED {
		return fmt.Errorf("repository binding supports provider_slug github")
	}
	for name, value := range map[string]string{
		"provider_owner": scenario.ProviderOwner,
		"provider_name":  scenario.ProviderName,
		"default_branch": scenario.DefaultBranch,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("repository binding requires %s", name)
		}
	}
	if _, _, ok := splitRepositoryFullName(strings.TrimSpace(scenario.ProviderOwner) + "/" + strings.TrimSpace(scenario.ProviderName)); !ok {
		return errors.New("repository binding requires provider owner/name")
	}
	if strings.TrimSpace(scenario.WebURL) != "" && !safeProviderURL(scenario.WebURL) {
		return errors.New("repository binding web_url must be an http(s) URL without credentials")
	}
	if _, err := repositoryStatusFromString(scenario.Status); err != nil {
		return err
	}
	return nil
}

func validateRepositoryBindingMatchesRepository(options runnerOptions, scenario *repositoryBindingScenario, repository *projectsv1.Repository) error {
	if scenario == nil {
		return nil
	}
	if err := validateRepositoryBindingScenario(options, scenario); err != nil {
		return err
	}
	for field, values := range map[string][2]string{
		"provider_owner": {strings.TrimSpace(scenario.ProviderOwner), repository.GetProviderOwner()},
		"provider_name":  {strings.TrimSpace(scenario.ProviderName), repository.GetProviderName()},
		"default_branch": {strings.TrimSpace(scenario.DefaultBranch), repository.GetDefaultBranch()},
	} {
		if values[0] != values[1] {
			return fmt.Errorf("repository binding %s mismatch", field)
		}
	}
	if strings.TrimSpace(scenario.ProviderRepositoryID) != "" && strings.TrimSpace(scenario.ProviderRepositoryID) != repository.GetProviderRepositoryId() {
		return errors.New("repository binding provider_repository_id mismatch")
	}
	return nil
}

func describeRepositoryChange(_ runnerOptions, scenario *repositoryChangeScenario, output io.Writer) error {
	if scenario == nil {
		return nil
	}
	summary, err := repositoryChangeSummaryFromScenario(scenario)
	if err != nil {
		return err
	}
	logLine(output, "PLAN", "repository change ready event=%s base=%s commit=%s paths=%d services_policy_changed=%t deploy_relevant_changed=%t change_digest=%s categories=%s",
		safeValue(summary.EventName),
		safeValue(summary.BaseBranch),
		safeValue(summary.CommitSHA),
		summary.TotalPaths,
		summary.ServicesPolicyChanged,
		summary.DeployRelevantChanged,
		safeValue(summary.ChangeDigest),
		safeValue(formatPathCategories(summary.Categories)),
	)
	return nil
}

func repositoryChangeSummaryFromScenario(scenario *repositoryChangeScenario) (repositoryChangeSummary, error) {
	if scenario == nil {
		return repositoryChangeSummary{}, nil
	}
	eventName := strings.ToLower(strings.TrimSpace(scenario.EventName))
	if eventName == "" {
		eventName = "push"
	}
	switch eventName {
	case "push", "merge", "pull_request_merge":
	default:
		return repositoryChangeSummary{}, errors.New("repository_change event_name must be push, merge or pull_request_merge")
	}
	baseBranch := firstNonEmpty(scenario.BaseBranch, refBranchName(scenario.Ref))
	if baseBranch == "" {
		return repositoryChangeSummary{}, errors.New("repository_change requires base_branch or ref")
	}
	ref := strings.TrimSpace(scenario.Ref)
	if ref != "" && ref != "refs/heads/"+baseBranch && ref != baseBranch {
		return repositoryChangeSummary{}, errors.New("repository_change ref must match base_branch")
	}
	commitSHA := strings.ToLower(strings.TrimSpace(scenario.CommitSHA))
	if commitSHA == "" {
		commitSHA = strings.ToLower(strings.TrimSpace(scenario.BeforeSHA))
	}
	if !validGitCommitSHA(commitSHA) {
		return repositoryChangeSummary{}, errors.New("repository_change requires commit_sha")
	}
	if len(scenario.ChangedPaths) == 0 {
		return repositoryChangeSummary{}, errors.New("repository_change requires changed_paths")
	}
	if len(scenario.ChangedPaths) > 256 {
		return repositoryChangeSummary{}, errors.New("repository_change changed_paths exceeds 256")
	}
	categories := map[string]int{}
	seen := map[string]struct{}{}
	hash := sha256.New()
	_, _ = hash.Write([]byte(eventName))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(baseBranch))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(commitSHA))
	_, _ = hash.Write([]byte{0})
	summary := repositoryChangeSummary{
		EventName:  eventName,
		BaseBranch: baseBranch,
		CommitSHA:  commitSHA,
		TotalPaths: len(scenario.ChangedPaths),
		Categories: categories,
	}
	for _, changed := range scenario.ChangedPaths {
		path := strings.TrimSpace(changed.Path)
		if !validRepositoryChangePath(path) {
			return repositoryChangeSummary{}, fmt.Errorf("repository_change path %q is unsafe", path)
		}
		action := repositoryChangeAction(changed.Action)
		if action == "" {
			return repositoryChangeSummary{}, errors.New("repository_change action must be added, modified, removed, renamed or changed")
		}
		objectDigest := strings.ToLower(strings.TrimSpace(changed.ObjectDigest))
		if objectDigest != "" && !validSHA256ContentHash(objectDigest) {
			return repositoryChangeSummary{}, errors.New("repository_change object_digest must be sha256")
		}
		identity := path + "\x00" + action
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		category := repositoryChangePathCategory(path)
		categories[category]++
		if category == "services_policy" {
			summary.ServicesPolicyChanged = true
			summary.DeployRelevantChanged = true
		}
		if repositoryChangeCategoryDeployRelevant(category) {
			summary.DeployRelevantChanged = true
		}
		_, _ = hash.Write([]byte(path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(action))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(category))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(objectDigest))
		_, _ = hash.Write([]byte{0})
	}
	summary.TotalPaths = len(seen)
	summary.ChangeDigest = "sha256:" + hex.EncodeToString(hash.Sum(nil))
	return summary, nil
}

func describeBootstrapSetup(options runnerOptions, setup *bootstrapSetup, output io.Writer) error {
	if setup == nil {
		return nil
	}
	if setup.CreateRepository != nil {
		if err := validateBootstrapCreateRepositoryScenario(options, setup.CreateRepository); err != nil {
			return err
		}
		logLine(output, "PLAN", "bootstrap repository create ready target=%s/%s visibility=%s",
			safeValue(setup.CreateRepository.ProviderOwner),
			safeValue(setup.CreateRepository.ProviderName),
			safeValue(setup.CreateRepository.Visibility),
		)
	}
	if setup.PullRequest != nil {
		if err := validateBootstrapPullRequestScenario(options, setup.PullRequest); err != nil {
			return err
		}
		logLine(output, "PLAN", "bootstrap pull request ready base=%s branch=%s files=%d services_policy_source=%s content_hash=%s checked_input=present_hidden",
			safeValue(setup.PullRequest.BaseBranch),
			safeValue(setup.PullRequest.BootstrapBranch),
			len(setup.PullRequest.Files),
			safeValue(setup.PullRequest.ServicesPolicy.SourcePath),
			safeValue(setup.PullRequest.ServicesPolicy.ContentHash),
		)
	}
	return nil
}

func applyBootstrapSetup(ctx context.Context, client projectCatalogAPI, options runnerOptions, setup *bootstrapSetup, output io.Writer) (bootstrapSetupResult, error) {
	var result bootstrapSetupResult
	if setup == nil {
		return result, nil
	}
	if setup.CreateRepository != nil {
		response, err := createProviderRepository(ctx, client, options, setup.CreateRepository)
		if err != nil {
			return result, err
		}
		repository := response.GetRepository()
		if repository == nil {
			return result, errors.New("project-catalog CreateProviderRepository returned empty repository")
		}
		result.Repository = repository
		result.ProviderRepositoryID = firstNonEmpty(response.GetProviderRepositoryId(), repository.GetProviderRepositoryId())
		if options.RepositoryID == "" {
			options.RepositoryID = repository.GetRepositoryId()
		}
		if result.ProviderRepositoryID != "" {
			options.ProviderRepositoryID = result.ProviderRepositoryID
		}
		boundOptions, err := bindOptionsToRepository(options, repository)
		if err != nil {
			return result, err
		}
		options = boundOptions
		logLine(output, "OK", "bootstrap repository create completed repository_id=%s base_branch=%s provider_repository_id=%s",
			safeValue(repository.GetRepositoryId()),
			safeValue(firstNonEmpty(response.GetBaseBranch(), repository.GetDefaultBranch())),
			safeValue(result.ProviderRepositoryID),
		)
	}
	if setup.PullRequest != nil {
		response, err := createBootstrapPullRequest(ctx, client, options, setup.PullRequest)
		if err != nil {
			return result, err
		}
		if response.GetRepository() != nil {
			result.Repository = response.GetRepository()
		}
		logLine(output, "OK", "bootstrap pull request completed base=%s branch=%s services_policy_source=%s content_hash=%s provider_operation_id=%s",
			safeValue(response.GetBaseBranch()),
			safeValue(response.GetBootstrapBranch()),
			safeValue(response.GetServicesPolicySourcePath()),
			safeValue(response.GetServicesPolicyContentHash()),
			safeValue(response.GetProviderOperationId()),
		)
	}
	return result, nil
}

func createProviderRepository(ctx context.Context, client projectCatalogAPI, options runnerOptions, scenario *bootstrapCreateRepositoryScenario) (*projectsv1.RepositoryProviderCreateResponse, error) {
	if err := validateBootstrapCreateRepositoryScenario(options, scenario); err != nil {
		return nil, err
	}
	ownerKind, err := repositoryOwnerKindFromString(scenario.OwnerKind)
	if err != nil {
		return nil, err
	}
	visibility, err := repositoryVisibilityFromString(scenario.Visibility)
	if err != nil {
		return nil, err
	}
	response, err := client.CreateProviderRepository(ctx, &projectsv1.CreateProviderRepositoryRequest{
		ProjectId:         options.ProjectID,
		Provider:          projectRepositoryProviderFromSlug(options.ProviderSlug),
		OwnerKind:         ownerKind,
		ProviderOwner:     strings.TrimSpace(scenario.ProviderOwner),
		ProviderName:      strings.TrimSpace(scenario.ProviderName),
		Visibility:        visibility,
		Description:       optionalString(scenario.Description),
		ExternalAccountId: bootstrapExternalAccountID(options, scenario.ExternalAccountID),
		Meta:              projectStageCommandMeta(options, "bootstrap-create-repository", strings.TrimSpace(scenario.ProviderOwner)+"/"+strings.TrimSpace(scenario.ProviderName), ""),
	})
	if err != nil {
		return nil, safeError("project-catalog CreateProviderRepository failed", err)
	}
	return response, nil
}

func createBootstrapPullRequest(ctx context.Context, client projectCatalogAPI, options runnerOptions, scenario *bootstrapPullRequestScenario) (*projectsv1.RepositoryBootstrapPullRequestResponse, error) {
	if err := validateBootstrapPullRequestScenario(options, scenario); err != nil {
		return nil, err
	}
	if options.RepositoryID == "" {
		return nil, errors.New("bootstrap pull_request requires repository_id")
	}
	response, err := client.CreateRepositoryBootstrapPullRequest(ctx, &projectsv1.CreateRepositoryBootstrapPullRequestRequest{
		ProjectId:         options.ProjectID,
		RepositoryId:      options.RepositoryID,
		BaseBranch:        strings.TrimSpace(scenario.BaseBranch),
		BootstrapBranch:   strings.TrimSpace(scenario.BootstrapBranch),
		CommitMessage:     strings.TrimSpace(scenario.CommitMessage),
		Title:             strings.TrimSpace(scenario.Title),
		Body:              strings.TrimSpace(scenario.Body),
		Draft:             scenario.Draft,
		Files:             bootstrapFilesToProto(scenario.Files),
		WatermarkJson:     strings.TrimSpace(scenario.WatermarkJSON),
		ServicesPolicy:    bootstrapServicesPolicyToProto(scenario.ServicesPolicy),
		ExternalAccountId: bootstrapExternalAccountID(options, scenario.ExternalAccountID),
		Meta:              projectStageCommandMeta(options, "bootstrap-pull-request", options.RepositoryID, scenario.ServicesPolicy.ContentHash),
	})
	if err != nil {
		return nil, safeError("project-catalog CreateRepositoryBootstrapPullRequest failed", err)
	}
	return response, nil
}

func validateBootstrapCreateRepositoryScenario(options runnerOptions, scenario *bootstrapCreateRepositoryScenario) error {
	if scenario == nil {
		return nil
	}
	if projectRepositoryProviderFromSlug(options.ProviderSlug) == projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_UNSPECIFIED {
		return fmt.Errorf("bootstrap repository create supports provider_slug github")
	}
	ownerKind, err := repositoryOwnerKindFromString(scenario.OwnerKind)
	if err != nil {
		return err
	}
	if options.Apply && options.RepositoryID != "" {
		return errors.New("bootstrap create_repository apply requires repository_id to be empty")
	}
	if options.Apply && ownerKind == projectsv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_AUTHENTICATED_USER {
		return errors.New("bootstrap create_repository apply requires owner_kind organization until external account owner identity is verified")
	}
	if _, err := repositoryVisibilityFromString(scenario.Visibility); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"provider_owner":      scenario.ProviderOwner,
		"provider_name":       scenario.ProviderName,
		"external_account_id": bootstrapExternalAccountID(options, scenario.ExternalAccountID),
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("bootstrap create_repository requires %s", name)
		}
	}
	return nil
}

func validateBootstrapPullRequestScenario(options runnerOptions, scenario *bootstrapPullRequestScenario) error {
	if scenario == nil {
		return nil
	}
	for name, value := range map[string]string{
		"base_branch":         scenario.BaseBranch,
		"bootstrap_branch":    scenario.BootstrapBranch,
		"commit_message":      scenario.CommitMessage,
		"title":               scenario.Title,
		"watermark_json":      scenario.WatermarkJSON,
		"external_account_id": bootstrapExternalAccountID(options, scenario.ExternalAccountID),
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("bootstrap pull_request requires %s", name)
		}
	}
	if strings.TrimSpace(scenario.BaseBranch) == strings.TrimSpace(scenario.BootstrapBranch) {
		return errors.New("bootstrap pull_request base_branch must differ from bootstrap_branch")
	}
	if !json.Valid([]byte(strings.TrimSpace(scenario.WatermarkJSON))) || !strings.HasPrefix(strings.TrimSpace(scenario.WatermarkJSON), "{") {
		return errors.New("bootstrap pull_request watermark_json must be a JSON object")
	}
	if len(scenario.Files) == 0 {
		return errors.New("bootstrap pull_request requires files")
	}
	if len(scenario.Files) > maxBootstrapSetupFiles {
		return fmt.Errorf("bootstrap pull_request files exceeds %d", maxBootstrapSetupFiles)
	}
	totalBytes := 0
	for _, file := range scenario.Files {
		if strings.TrimSpace(file.Path) == "" {
			return errors.New("bootstrap pull_request file path is required")
		}
		totalBytes += len([]byte(file.Content))
	}
	if totalBytes > maxBootstrapSetupBytes {
		return fmt.Errorf("bootstrap pull_request file content exceeds %d bytes", maxBootstrapSetupBytes)
	}
	policy := scenario.ServicesPolicy
	for name, value := range map[string]string{
		"services_policy.source_path":            policy.SourcePath,
		"services_policy.content_hash":           policy.ContentHash,
		"services_policy.validated_payload_json": policy.ValidatedPayloadJSON,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("bootstrap pull_request requires %s", name)
		}
	}
	if !json.Valid([]byte(strings.TrimSpace(policy.ValidatedPayloadJSON))) || !strings.HasPrefix(strings.TrimSpace(policy.ValidatedPayloadJSON), "{") {
		return errors.New("bootstrap pull_request services_policy.validated_payload_json must be a JSON object")
	}
	return nil
}

func bootstrapFilesToProto(files []bootstrapPreparedFile) []*projectsv1.RepositoryBootstrapFile {
	result := make([]*projectsv1.RepositoryBootstrapFile, 0, len(files))
	for _, file := range files {
		result = append(result, &projectsv1.RepositoryBootstrapFile{
			Path:       strings.TrimSpace(file.Path),
			Content:    file.Content,
			Executable: file.Executable,
		})
	}
	return result
}

func bootstrapServicesPolicyToProto(policy bootstrapServicesPolicyScenario) *projectsv1.RepositoryBootstrapServicesPolicy {
	return &projectsv1.RepositoryBootstrapServicesPolicy{
		SourcePath:           strings.TrimSpace(policy.SourcePath),
		ContentHash:          strings.TrimSpace(policy.ContentHash),
		ValidatedPayloadJson: strings.TrimSpace(policy.ValidatedPayloadJSON),
	}
}

func bootstrapExternalAccountID(options runnerOptions, value string) string {
	return firstNonEmpty(value, options.ExternalAccountID)
}

func repositoryOwnerKindFromString(value string) (projectsv1.RepositoryOwnerKind, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "organization", "org":
		return projectsv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_ORGANIZATION, nil
	case "authenticated_user", "user":
		return projectsv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_AUTHENTICATED_USER, nil
	default:
		return projectsv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_UNSPECIFIED, fmt.Errorf("bootstrap create_repository owner_kind must be organization or authenticated_user")
	}
}

func repositoryVisibilityFromString(value string) (projectsv1.RepositoryVisibility, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "public":
		return projectsv1.RepositoryVisibility_REPOSITORY_VISIBILITY_PUBLIC, nil
	case "private":
		return projectsv1.RepositoryVisibility_REPOSITORY_VISIBILITY_PRIVATE, nil
	case "internal":
		return projectsv1.RepositoryVisibility_REPOSITORY_VISIBILITY_INTERNAL, nil
	default:
		return projectsv1.RepositoryVisibility_REPOSITORY_VISIBILITY_UNSPECIFIED, fmt.Errorf("bootstrap create_repository visibility must be public, private or internal")
	}
}

func repositoryStatusFromString(value string) (projectsv1.RepositoryStatus, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return projectsv1.RepositoryStatus_REPOSITORY_STATUS_UNSPECIFIED, nil
	case "active":
		return projectsv1.RepositoryStatus_REPOSITORY_STATUS_ACTIVE, nil
	case "pending":
		return projectsv1.RepositoryStatus_REPOSITORY_STATUS_PENDING, nil
	case "blocked":
		return projectsv1.RepositoryStatus_REPOSITORY_STATUS_BLOCKED, nil
	default:
		return projectsv1.RepositoryStatus_REPOSITORY_STATUS_UNSPECIFIED, fmt.Errorf("repository binding status must be active, pending or blocked")
	}
}

func optionalRepositoryStatus(value projectsv1.RepositoryStatus) *projectsv1.RepositoryStatus {
	if value == projectsv1.RepositoryStatus_REPOSITORY_STATUS_UNSPECIFIED {
		return nil
	}
	return &value
}

func optionalProjectStatus(value projectsv1.ProjectStatus) *projectsv1.ProjectStatus {
	if value == projectsv1.ProjectStatus_PROJECT_STATUS_UNSPECIFIED {
		return nil
	}
	return &value
}

func projectRepositoryProviderFromSlug(value string) projectsv1.RepositoryProvider {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "github":
		return projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_GITHUB
	default:
		return projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_UNSPECIFIED
	}
}

func safeProviderURL(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return true
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.User != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "https" || parsed.Scheme == "http"
}

func projectRepositoryProviderSlug(provider projectsv1.RepositoryProvider) (string, error) {
	switch provider {
	case projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_GITHUB:
		return "github", nil
	case projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_GITLAB:
		return "gitlab", nil
	default:
		return "", fmt.Errorf("repository binding provider is unsupported")
	}
}

func ensureSelfDeployServiceAccess(ctx context.Context, client accessManagerAPI, options runnerOptions, output io.Writer) error {
	if options.ProjectID == "" || options.RepositoryID == "" {
		return nil
	}
	if !options.Apply {
		for _, grant := range selfDeployServiceAccessGrants {
			logLine(output, "PLAN", "self-deploy service access ready subject=service/%s action=%s resource=%s scope=project:%s",
				grant.subjectID,
				grant.actionKey,
				grant.resourceType,
				safeValue(options.ProjectID),
			)
		}
		return nil
	}
	if client == nil {
		return errors.New("access-manager client is required for self-deploy service access apply")
	}
	for _, grant := range selfDeployServiceAccessGrants {
		rule, err := putSelfDeployServiceAccessRule(ctx, client, options, grant)
		if err != nil {
			return err
		}
		if err := checkSelfDeployServiceAccess(ctx, client, options, grant); err != nil {
			return err
		}
		logLine(output, "OK", "self-deploy service access ready subject=service/%s action=%s resource=%s scope=project:%s rule_id=%s version=%d",
			grant.subjectID,
			grant.actionKey,
			grant.resourceType,
			safeValue(options.ProjectID),
			safeValue(rule.GetAccessRuleId()),
			rule.GetVersion(),
		)
	}
	return nil
}

func ensureSelfDeployOwnerAccess(ctx context.Context, client accessManagerAPI, options runnerOptions, output io.Writer) error {
	if options.ProjectID == "" || options.OwnerUserRef == "" {
		return nil
	}
	if !options.Apply {
		for _, grant := range selfDeployOwnerAccessGrants {
			logLine(output, "PLAN", "self-deploy owner access ready subject=user/%s action=%s resource=%s scope=%s",
				safeValue(options.OwnerUserRef),
				grant.actionKey,
				grant.resourceType,
				selfDeployOwnerAccessScopeLabel(options, grant),
			)
		}
		return nil
	}
	if client == nil {
		return errors.New("access-manager client is required for self-deploy owner access apply")
	}
	for _, grant := range selfDeployOwnerAccessGrants {
		rule, err := putSelfDeployOwnerAccessRule(ctx, client, options, grant)
		if err != nil {
			return err
		}
		if err := checkSelfDeployOwnerAccess(ctx, client, options, grant); err != nil {
			return err
		}
		logLine(output, "OK", "self-deploy owner access ready subject=user/%s action=%s resource=%s scope=%s rule_id=%s version=%d",
			safeValue(options.OwnerUserRef),
			grant.actionKey,
			grant.resourceType,
			selfDeployOwnerAccessScopeLabel(options, grant),
			safeValue(rule.GetAccessRuleId()),
			rule.GetVersion(),
		)
	}
	return nil
}

func ensureRunnerRepositoryReadAccess(ctx context.Context, client accessManagerAPI, options runnerOptions, output io.Writer) error {
	if options.ProjectID == "" || options.RepositoryID == "" {
		return nil
	}
	if !options.Apply {
		logLine(output, "PLAN", "onboarding runner repository read access ready subject=service/%s action=%s resource=%s resource_id=%s scope=project:%s",
			safeValue(options.ActorID),
			accesscatalog.ActionRepositoryRead,
			accesscatalog.ResourceRepository,
			safeValue(options.RepositoryID),
			safeValue(options.ProjectID),
		)
		return nil
	}
	if client == nil {
		return errors.New("access-manager client is required for onboarding runner repository read access apply")
	}
	rule, err := putRunnerRepositoryReadAccessRule(ctx, client, options)
	if err != nil {
		return err
	}
	if err := checkRunnerRepositoryReadAccess(ctx, client, options); err != nil {
		return err
	}
	logLine(output, "OK", "onboarding runner repository read access ready subject=service/%s action=%s resource=%s resource_id=%s scope=project:%s rule_id=%s version=%d",
		safeValue(options.ActorID),
		accesscatalog.ActionRepositoryRead,
		accesscatalog.ResourceRepository,
		safeValue(options.RepositoryID),
		safeValue(options.ProjectID),
		safeValue(rule.GetAccessRuleId()),
		rule.GetVersion(),
	)
	return nil
}

func ensureServicesPolicyImportAccess(ctx context.Context, client accessManagerAPI, options runnerOptions, output io.Writer) error {
	if options.ProjectID == "" || options.RepositoryID == "" {
		return nil
	}
	if !options.Apply {
		logLine(output, "PLAN", "services policy import access ready subject=service/%s action=%s resource=%s scope=project:%s",
			safeValue(options.ActorID),
			accesscatalog.ActionProjectPolicyImport,
			accesscatalog.ResourceServicesPolicy,
			safeValue(options.ProjectID),
		)
		return nil
	}
	if client == nil {
		return errors.New("access-manager client is required for services policy import access apply")
	}
	rule, err := putServicesPolicyImportAccessRule(ctx, client, options)
	if err != nil {
		return err
	}
	if err := checkServicesPolicyImportAccess(ctx, client, options); err != nil {
		return err
	}
	logLine(output, "OK", "services policy import access ready subject=service/%s action=%s resource=%s scope=project:%s rule_id=%s version=%d",
		safeValue(options.ActorID),
		accesscatalog.ActionProjectPolicyImport,
		accesscatalog.ResourceServicesPolicy,
		safeValue(options.ProjectID),
		safeValue(rule.GetAccessRuleId()),
		rule.GetVersion(),
	)
	return nil
}

func putSelfDeployServiceAccessRule(ctx context.Context, client accessManagerAPI, options runnerOptions, grant selfDeployServiceAccessGrant) (*accessaccountsv1.AccessRuleResponse, error) {
	request := selfDeployServiceAccessRuleRequest(options, grant)
	response, err := client.PutAccessRule(ctx, request)
	if err != nil {
		return nil, safeError("access-manager PutAccessRule failed", err)
	}
	if !selfDeployAccessRuleMatches(response, request) {
		return nil, fmt.Errorf("access-manager PutAccessRule returned mismatched self-deploy access rule for service/%s action=%s", grant.subjectID, grant.actionKey)
	}
	return response, nil
}

func putSelfDeployOwnerAccessRule(ctx context.Context, client accessManagerAPI, options runnerOptions, grant selfDeployOwnerAccessGrant) (*accessaccountsv1.AccessRuleResponse, error) {
	request := selfDeployOwnerAccessRuleRequest(options, grant)
	response, err := client.PutAccessRule(ctx, request)
	if err != nil {
		return nil, safeError("access-manager PutAccessRule failed", err)
	}
	if !selfDeployAccessRuleMatches(response, request) {
		return nil, fmt.Errorf("access-manager PutAccessRule returned mismatched self-deploy owner access rule for user/%s action=%s", options.OwnerUserRef, grant.actionKey)
	}
	return response, nil
}

func putRunnerRepositoryReadAccessRule(ctx context.Context, client accessManagerAPI, options runnerOptions) (*accessaccountsv1.AccessRuleResponse, error) {
	request := runnerRepositoryReadAccessRuleRequest(options)
	response, err := client.PutAccessRule(ctx, request)
	if err != nil {
		return nil, safeError("access-manager PutAccessRule failed", err)
	}
	if !selfDeployAccessRuleMatches(response, request) {
		return nil, fmt.Errorf("access-manager PutAccessRule returned mismatched onboarding runner repository read access rule for service/%s", options.ActorID)
	}
	return response, nil
}

func putServicesPolicyImportAccessRule(ctx context.Context, client accessManagerAPI, options runnerOptions) (*accessaccountsv1.AccessRuleResponse, error) {
	request := servicesPolicyImportAccessRuleRequest(options)
	response, err := client.PutAccessRule(ctx, request)
	if err != nil {
		return nil, safeError("access-manager PutAccessRule failed", err)
	}
	if !selfDeployAccessRuleMatches(response, request) {
		return nil, fmt.Errorf("access-manager PutAccessRule returned mismatched services policy import access rule for service/%s", options.ActorID)
	}
	return response, nil
}

func checkSelfDeployServiceAccess(ctx context.Context, client accessManagerAPI, options runnerOptions, grant selfDeployServiceAccessGrant) error {
	response, err := client.CheckAccess(ctx, selfDeployServiceAccessCheckRequest(options, grant))
	if err != nil {
		return safeError("access-manager CheckAccess failed", err)
	}
	if response.GetDecision() != accessaccountsv1.AccessDecision_ACCESS_DECISION_ALLOW {
		return fmt.Errorf("self-deploy access check denied for service/%s action=%s reason=%s", grant.subjectID, grant.actionKey, safeValue(response.GetReasonCode()))
	}
	return nil
}

func checkSelfDeployOwnerAccess(ctx context.Context, client accessManagerAPI, options runnerOptions, grant selfDeployOwnerAccessGrant) error {
	response, err := client.CheckAccess(ctx, selfDeployOwnerAccessCheckRequest(options, grant))
	if err != nil {
		return safeError("access-manager CheckAccess failed", err)
	}
	if response.GetDecision() != accessaccountsv1.AccessDecision_ACCESS_DECISION_ALLOW {
		return fmt.Errorf("self-deploy owner access check denied for user/%s action=%s reason=%s", options.OwnerUserRef, grant.actionKey, safeValue(response.GetReasonCode()))
	}
	return nil
}

func checkRunnerRepositoryReadAccess(ctx context.Context, client accessManagerAPI, options runnerOptions) error {
	response, err := client.CheckAccess(ctx, runnerRepositoryReadAccessCheckRequest(options))
	if err != nil {
		return safeError("access-manager CheckAccess failed", err)
	}
	if response.GetDecision() != accessaccountsv1.AccessDecision_ACCESS_DECISION_ALLOW {
		return fmt.Errorf("onboarding runner repository read access check denied for service/%s reason=%s", options.ActorID, safeValue(response.GetReasonCode()))
	}
	return nil
}

func checkServicesPolicyImportAccess(ctx context.Context, client accessManagerAPI, options runnerOptions) error {
	response, err := client.CheckAccess(ctx, servicesPolicyImportAccessCheckRequest(options))
	if err != nil {
		return safeError("access-manager CheckAccess failed", err)
	}
	if response.GetDecision() != accessaccountsv1.AccessDecision_ACCESS_DECISION_ALLOW {
		return fmt.Errorf("services policy import access check denied for service/%s reason=%s", options.ActorID, safeValue(response.GetReasonCode()))
	}
	return nil
}

func selfDeployServiceAccessRuleRequest(options runnerOptions, grant selfDeployServiceAccessGrant) *accessaccountsv1.PutAccessRuleRequest {
	return &accessaccountsv1.PutAccessRuleRequest{
		Effect:       accessaccountsv1.AccessEffect_ACCESS_EFFECT_ALLOW,
		SubjectType:  "service",
		SubjectId:    strings.TrimSpace(grant.subjectID),
		ActionKey:    grant.actionKey,
		ResourceType: grant.resourceType,
		ScopeType:    accesscatalog.ScopeProject,
		ScopeId:      options.ProjectID,
		Priority:     selfDeployServiceAccessPriority,
		Status:       accessaccountsv1.AccessRuleStatus_ACCESS_RULE_STATUS_ACTIVE,
		Meta:         accessRuleCommandMeta(options, grant.subjectID),
	}
}

func selfDeployOwnerAccessRuleRequest(options runnerOptions, grant selfDeployOwnerAccessGrant) *accessaccountsv1.PutAccessRuleRequest {
	return &accessaccountsv1.PutAccessRuleRequest{
		Effect:       accessaccountsv1.AccessEffect_ACCESS_EFFECT_ALLOW,
		SubjectType:  "user",
		SubjectId:    options.OwnerUserRef,
		ActionKey:    grant.actionKey,
		ResourceType: grant.resourceType,
		ScopeType:    grant.scopeType,
		ScopeId:      selfDeployOwnerAccessScopeID(options, grant),
		Priority:     selfDeployOwnerAccessPriority,
		Status:       accessaccountsv1.AccessRuleStatus_ACCESS_RULE_STATUS_ACTIVE,
		Meta:         selfDeployOwnerAccessCommandMeta(options, grant),
	}
}

func runnerRepositoryReadAccessRuleRequest(options runnerOptions) *accessaccountsv1.PutAccessRuleRequest {
	return &accessaccountsv1.PutAccessRuleRequest{
		Effect:       accessaccountsv1.AccessEffect_ACCESS_EFFECT_ALLOW,
		SubjectType:  "service",
		SubjectId:    strings.TrimSpace(options.ActorID),
		ActionKey:    accesscatalog.ActionRepositoryRead,
		ResourceType: accesscatalog.ResourceRepository,
		ResourceId:   options.RepositoryID,
		ScopeType:    accesscatalog.ScopeProject,
		ScopeId:      options.ProjectID,
		Priority:     runnerRepositoryReadAccessPriority,
		Status:       accessaccountsv1.AccessRuleStatus_ACCESS_RULE_STATUS_ACTIVE,
		Meta:         runnerRepositoryReadAccessCommandMeta(options),
	}
}

func servicesPolicyImportAccessRuleRequest(options runnerOptions) *accessaccountsv1.PutAccessRuleRequest {
	return &accessaccountsv1.PutAccessRuleRequest{
		Effect:       accessaccountsv1.AccessEffect_ACCESS_EFFECT_ALLOW,
		SubjectType:  "service",
		SubjectId:    strings.TrimSpace(options.ActorID),
		ActionKey:    accesscatalog.ActionProjectPolicyImport,
		ResourceType: accesscatalog.ResourceServicesPolicy,
		ScopeType:    accesscatalog.ScopeProject,
		ScopeId:      options.ProjectID,
		Priority:     servicesPolicyImportAccessPriority,
		Status:       accessaccountsv1.AccessRuleStatus_ACCESS_RULE_STATUS_ACTIVE,
		Meta:         servicesPolicyImportAccessCommandMeta(options),
	}
}

func selfDeployServiceAccessCheckRequest(options runnerOptions, grant selfDeployServiceAccessGrant) *accessaccountsv1.CheckAccessRequest {
	return &accessaccountsv1.CheckAccessRequest{
		Subject:   &accessaccountsv1.SubjectRef{Type: "service", Id: strings.TrimSpace(grant.subjectID)},
		ActionKey: grant.actionKey,
		Resource: &accessaccountsv1.ResourceRef{
			Type: grant.resourceType,
			Id:   "",
		},
		Scope: &accessaccountsv1.ScopeRef{
			Type: accesscatalog.ScopeProject,
			Id:   options.ProjectID,
		},
		Audit: true,
		Meta:  accessRuleCheckMeta(options, grant.subjectID),
	}
}

func selfDeployOwnerAccessCheckRequest(options runnerOptions, grant selfDeployOwnerAccessGrant) *accessaccountsv1.CheckAccessRequest {
	return &accessaccountsv1.CheckAccessRequest{
		Subject:   &accessaccountsv1.SubjectRef{Type: "user", Id: options.OwnerUserRef},
		ActionKey: grant.actionKey,
		Resource: &accessaccountsv1.ResourceRef{
			Type: grant.resourceType,
			Id:   grant.probeID,
		},
		Scope: &accessaccountsv1.ScopeRef{
			Type: grant.scopeType,
			Id:   selfDeployOwnerAccessScopeID(options, grant),
		},
		Audit: true,
		Meta:  selfDeployOwnerAccessCheckMeta(options),
	}
}

func runnerRepositoryReadAccessCheckRequest(options runnerOptions) *accessaccountsv1.CheckAccessRequest {
	return &accessaccountsv1.CheckAccessRequest{
		Subject:   &accessaccountsv1.SubjectRef{Type: "service", Id: strings.TrimSpace(options.ActorID)},
		ActionKey: accesscatalog.ActionRepositoryRead,
		Resource: &accessaccountsv1.ResourceRef{
			Type: accesscatalog.ResourceRepository,
			Id:   options.RepositoryID,
		},
		Scope: &accessaccountsv1.ScopeRef{
			Type: accesscatalog.ScopeProject,
			Id:   options.ProjectID,
		},
		Audit: true,
		Meta:  runnerRepositoryReadAccessCheckMeta(options),
	}
}

func servicesPolicyImportAccessCheckRequest(options runnerOptions) *accessaccountsv1.CheckAccessRequest {
	return &accessaccountsv1.CheckAccessRequest{
		Subject:   &accessaccountsv1.SubjectRef{Type: "service", Id: strings.TrimSpace(options.ActorID)},
		ActionKey: accesscatalog.ActionProjectPolicyImport,
		Resource: &accessaccountsv1.ResourceRef{
			Type: accesscatalog.ResourceServicesPolicy,
			Id:   "",
		},
		Scope: &accessaccountsv1.ScopeRef{
			Type: accesscatalog.ScopeProject,
			Id:   options.ProjectID,
		},
		Audit: true,
		Meta:  servicesPolicyImportAccessCheckMeta(options),
	}
}

func selfDeployAccessRuleMatches(response *accessaccountsv1.AccessRuleResponse, request *accessaccountsv1.PutAccessRuleRequest) bool {
	if response == nil {
		return false
	}
	return response.GetEffect() == request.GetEffect() &&
		response.GetSubjectType() == request.GetSubjectType() &&
		response.GetSubjectId() == request.GetSubjectId() &&
		response.GetActionKey() == request.GetActionKey() &&
		response.GetResourceType() == request.GetResourceType() &&
		response.GetResourceId() == request.GetResourceId() &&
		response.GetScopeType() == request.GetScopeType() &&
		response.GetScopeId() == request.GetScopeId() &&
		response.GetPriority() == request.GetPriority() &&
		response.GetStatus() == request.GetStatus()
}

func accessRuleCommandMeta(options runnerOptions, subjectID string) *accessaccountsv1.CommandMeta {
	return &accessaccountsv1.CommandMeta{
		IdempotencyKey: deterministicKey(options.ProjectID, options.RepositoryID, "self-deploy-service-access", subjectID, accesscatalog.ActionProjectPolicyRead, accesscatalog.ResourceServicesPolicy),
		Actor:          &accessaccountsv1.Actor{Type: "service", Id: options.ActorID},
		Reason:         "self_deploy_service_access_bootstrap",
		RequestId:      options.RequestID,
		RequestContext: &accessaccountsv1.RequestContext{Source: defaultSource},
	}
}

func selfDeployOwnerAccessCommandMeta(options runnerOptions, grant selfDeployOwnerAccessGrant) *accessaccountsv1.CommandMeta {
	return &accessaccountsv1.CommandMeta{
		IdempotencyKey: deterministicKey(options.ProjectID, options.RepositoryID, "self-deploy-owner-access", options.OwnerUserRef, grant.actionKey, grant.resourceType, grant.scopeType),
		Actor:          &accessaccountsv1.Actor{Type: "service", Id: options.ActorID},
		Reason:         "self_deploy_owner_access_bootstrap",
		RequestId:      options.RequestID,
		RequestContext: &accessaccountsv1.RequestContext{Source: defaultSource},
	}
}

func runnerRepositoryReadAccessCommandMeta(options runnerOptions) *accessaccountsv1.CommandMeta {
	return &accessaccountsv1.CommandMeta{
		IdempotencyKey: deterministicKey(options.ProjectID, options.RepositoryID, "onboarding-runner-repository-read-access", options.ActorID, accesscatalog.ActionRepositoryRead, accesscatalog.ResourceRepository),
		Actor:          &accessaccountsv1.Actor{Type: "service", Id: options.ActorID},
		Reason:         "self_repo_repository_read_bootstrap",
		RequestId:      options.RequestID,
		RequestContext: &accessaccountsv1.RequestContext{Source: defaultSource},
	}
}

func servicesPolicyImportAccessCommandMeta(options runnerOptions) *accessaccountsv1.CommandMeta {
	return &accessaccountsv1.CommandMeta{
		IdempotencyKey: deterministicKey(options.ProjectID, options.RepositoryID, "services-policy-import-access", options.ActorID, accesscatalog.ActionProjectPolicyImport, accesscatalog.ResourceServicesPolicy),
		Actor:          &accessaccountsv1.Actor{Type: "service", Id: options.ActorID},
		Reason:         "self_repo_services_policy_import_bootstrap",
		RequestId:      options.RequestID,
		RequestContext: &accessaccountsv1.RequestContext{Source: defaultSource},
	}
}

func accessRuleCheckMeta(options runnerOptions, subjectID string) *accessaccountsv1.CommandMeta {
	return &accessaccountsv1.CommandMeta{
		Actor:          &accessaccountsv1.Actor{Type: "service", Id: strings.TrimSpace(subjectID)},
		Reason:         "self_deploy_service_access_check",
		RequestId:      options.RequestID,
		RequestContext: &accessaccountsv1.RequestContext{Source: defaultSource},
	}
}

func selfDeployOwnerAccessCheckMeta(options runnerOptions) *accessaccountsv1.CommandMeta {
	return &accessaccountsv1.CommandMeta{
		Actor:          &accessaccountsv1.Actor{Type: "user", Id: options.OwnerUserRef},
		Reason:         "self_deploy_owner_access_check",
		RequestId:      options.RequestID,
		RequestContext: &accessaccountsv1.RequestContext{Source: defaultSource},
	}
}

func runnerRepositoryReadAccessCheckMeta(options runnerOptions) *accessaccountsv1.CommandMeta {
	return &accessaccountsv1.CommandMeta{
		Actor:          &accessaccountsv1.Actor{Type: "service", Id: options.ActorID},
		Reason:         "self_repo_repository_read_access_check",
		RequestId:      options.RequestID,
		RequestContext: &accessaccountsv1.RequestContext{Source: defaultSource},
	}
}

func servicesPolicyImportAccessCheckMeta(options runnerOptions) *accessaccountsv1.CommandMeta {
	return &accessaccountsv1.CommandMeta{
		Actor:          &accessaccountsv1.Actor{Type: "service", Id: options.ActorID},
		Reason:         "self_repo_services_policy_import_access_check",
		RequestId:      options.RequestID,
		RequestContext: &accessaccountsv1.RequestContext{Source: defaultSource},
	}
}

func readProviderSignals(ctx context.Context, client providerHubAPI, options runnerOptions, scenario onboardingScenario, output io.Writer) (mergeSignalSet, error) {
	var signals mergeSignalSet
	if wantsKind(options, "bootstrap") {
		signalKey := ""
		if scenario.Bootstrap != nil {
			signalKey = scenario.Bootstrap.SignalKey
		}
		signal, err := readProviderSignal(ctx, client, options, providersv1.RepositoryMergeSignalKind_REPOSITORY_MERGE_SIGNAL_KIND_BOOTSTRAP, signalKey)
		if err != nil {
			return signals, err
		}
		signals.Bootstrap = signal
		describeSignal(output, "bootstrap", signal)
	}
	if wantsKind(options, "adoption") {
		signalKey := ""
		if scenario.Adoption != nil {
			signalKey = scenario.Adoption.SignalKey
		}
		signal, err := readProviderSignal(ctx, client, options, providersv1.RepositoryMergeSignalKind_REPOSITORY_MERGE_SIGNAL_KIND_ADOPTION, signalKey)
		if err != nil {
			return signals, err
		}
		signals.Adoption = signal
		describeSignal(output, "adoption", signal)
	}
	return signals, nil
}

func readProviderSignal(ctx context.Context, client providerHubAPI, options runnerOptions, kind providersv1.RepositoryMergeSignalKind, signalKey string) (*providersv1.RepositoryMergeSignal, error) {
	if strings.TrimSpace(signalKey) != "" {
		response, err := client.GetRepositoryMergeSignal(ctx, &providersv1.GetRepositoryMergeSignalRequest{
			SignalKey: optionalString(signalKey),
			Meta:      providerQueryMeta(options),
		})
		if err != nil {
			return nil, safeError("provider-hub GetRepositoryMergeSignal failed", err)
		}
		if response.GetReadStatus() != providersv1.ProviderOwnedDataStatus_PROVIDER_OWNED_DATA_STATUS_READY || response.GetMergeSignal() == nil {
			return nil, nil
		}
		if err := validateProviderSignal(response.GetMergeSignal(), options, kind); err != nil {
			return nil, err
		}
		return response.GetMergeSignal(), nil
	}
	response, err := client.ListRepositoryMergeSignals(ctx, &providersv1.ListRepositoryMergeSignalsRequest{
		ProjectId:            optionalString(options.ProjectID),
		RepositoryId:         optionalString(options.RepositoryID),
		ProviderSlug:         optionalString(options.ProviderSlug),
		RepositoryFullName:   optionalString(options.RepositoryFullName),
		ProviderRepositoryId: optionalString(options.ProviderRepositoryID),
		Kinds:                []providersv1.RepositoryMergeSignalKind{kind},
		Statuses:             []providersv1.RepositoryMergeSignalStatus{providersv1.RepositoryMergeSignalStatus_REPOSITORY_MERGE_SIGNAL_STATUS_MERGED},
		Page:                 &providersv1.PageRequest{PageSize: 1},
		Meta:                 providerQueryMeta(options),
	})
	if err != nil {
		return nil, safeError("provider-hub ListRepositoryMergeSignals failed", err)
	}
	signals := response.GetMergeSignals()
	if len(signals) == 0 {
		return nil, nil
	}
	if err := validateProviderSignal(signals[0], options, kind); err != nil {
		return nil, err
	}
	return signals[0], nil
}

func validateProviderSignal(signal *providersv1.RepositoryMergeSignal, options runnerOptions, kind providersv1.RepositoryMergeSignalKind) error {
	if signal.GetKind() != kind {
		return fmt.Errorf("provider merge signal has unexpected kind")
	}
	if signal.GetStatus() != providersv1.RepositoryMergeSignalStatus_REPOSITORY_MERGE_SIGNAL_STATUS_MERGED {
		return fmt.Errorf("provider merge signal is not merged")
	}
	for field, values := range map[string][2]string{
		"project_id":           {signal.GetProjectId(), options.ProjectID},
		"repository_id":        {signal.GetRepositoryId(), options.RepositoryID},
		"provider_slug":        {signal.GetProviderSlug(), options.ProviderSlug},
		"repository_full_name": {signal.GetRepositoryFullName(), options.RepositoryFullName},
	} {
		if values[0] != values[1] {
			return fmt.Errorf("provider merge signal %s mismatch", field)
		}
	}
	if options.ProviderRepositoryID != "" && signal.GetProviderRepositoryId() != options.ProviderRepositoryID {
		return fmt.Errorf("provider merge signal provider_repository_id mismatch")
	}
	return nil
}

func readAdoptionSnapshots(ctx context.Context, client providerHubAPI, options runnerOptions, output io.Writer) error {
	if !wantsKind(options, "adoption") {
		return nil
	}
	response, err := client.ListRepositoryAdoptionScanSnapshots(ctx, &providersv1.ListRepositoryAdoptionScanSnapshotsRequest{
		ProjectId:            optionalString(options.ProjectID),
		RepositoryId:         optionalString(options.RepositoryID),
		ProviderSlug:         optionalString(options.ProviderSlug),
		RepositoryFullName:   optionalString(options.RepositoryFullName),
		ProviderRepositoryId: optionalString(options.ProviderRepositoryID),
		Page:                 &providersv1.PageRequest{PageSize: 1},
		Meta:                 providerQueryMeta(options),
	})
	if err != nil {
		return safeError("provider-hub ListRepositoryAdoptionScanSnapshots failed", err)
	}
	logLine(output, "OK", "adoption scan read surface reachable snapshots=%d", len(response.GetAdoptionScanSnapshots()))
	return nil
}

func readRepositoryChangeSignal(ctx context.Context, client providerHubAPI, options runnerOptions, scenario *repositoryChangeScenario, output io.Writer) (*providersv1.RepositoryChangeSignal, error) {
	if scenario == nil {
		return nil, nil
	}
	signal, err := readProviderRepositoryChangeSignal(ctx, client, options, scenario)
	if err != nil {
		return nil, err
	}
	if signal == nil {
		logLine(output, "BLOCKED", "live repository change signal is not available; waiting for provider webhook delivery")
		return nil, nil
	}
	logLine(output, "OK", "repository change signal ready kind=%s base=%s commit=%s path_status=%s paths=%d services_policy_changed=%t deploy_relevant_changed=%t change_fingerprint=%s",
		safeValue(signal.GetKind().String()),
		safeValue(signal.GetBaseBranch()),
		safeValue(signal.GetCommitSha()),
		safeValue(signal.GetPathSummaryStatus().String()),
		signal.GetChangedPathCount(),
		signal.GetServicesPolicyChanged(),
		signal.GetDeployRelevantChanged(),
		safeValue(signal.GetChangeFingerprint()),
	)
	return signal, nil
}

func readProviderRepositoryChangeSignal(ctx context.Context, client providerHubAPI, options runnerOptions, scenario *repositoryChangeScenario) (*providersv1.RepositoryChangeSignal, error) {
	signalKey := strings.TrimSpace(scenario.SignalKey)
	if signalKey != "" {
		response, err := client.GetRepositoryChangeSignal(ctx, &providersv1.GetRepositoryChangeSignalRequest{
			SignalKey: optionalString(signalKey),
			Meta:      providerQueryMeta(options),
		})
		if err != nil {
			return nil, safeError("provider-hub GetRepositoryChangeSignal failed", err)
		}
		if response.GetReadStatus() != providersv1.ProviderOwnedDataStatus_PROVIDER_OWNED_DATA_STATUS_READY || response.GetChangeSignal() == nil {
			return nil, nil
		}
		if err := validateRepositoryChangeSignal(response.GetChangeSignal(), options, scenario); err != nil {
			return nil, err
		}
		return response.GetChangeSignal(), nil
	}
	response, err := client.ListRepositoryChangeSignals(ctx, &providersv1.ListRepositoryChangeSignalsRequest{
		ProviderSlug:          optionalString(options.ProviderSlug),
		RepositoryFullName:    optionalString(options.RepositoryFullName),
		ProviderRepositoryId:  optionalString(options.ProviderRepositoryID),
		Kinds:                 repositoryChangeSignalKinds(scenario),
		Statuses:              []providersv1.RepositoryChangeSignalStatus{providersv1.RepositoryChangeSignalStatus_REPOSITORY_CHANGE_SIGNAL_STATUS_OBSERVED},
		BaseBranch:            optionalString(firstNonEmpty(scenario.BaseBranch, refBranchName(scenario.Ref))),
		DeployRelevantChanged: optionalBool(true),
		Page:                  &providersv1.PageRequest{PageSize: 1},
		Meta:                  providerQueryMeta(options),
	})
	if err != nil {
		return nil, safeError("provider-hub ListRepositoryChangeSignals failed", err)
	}
	signals := response.GetChangeSignals()
	if len(signals) == 0 {
		return nil, nil
	}
	if err := validateRepositoryChangeSignal(signals[0], options, scenario); err != nil {
		return nil, err
	}
	return signals[0], nil
}

func repositoryChangeSignalKinds(scenario *repositoryChangeScenario) []providersv1.RepositoryChangeSignalKind {
	switch strings.ToLower(strings.TrimSpace(scenario.EventName)) {
	case "merge", "pull_request_merge":
		return []providersv1.RepositoryChangeSignalKind{providersv1.RepositoryChangeSignalKind_REPOSITORY_CHANGE_SIGNAL_KIND_PULL_REQUEST_MERGED}
	default:
		return []providersv1.RepositoryChangeSignalKind{
			providersv1.RepositoryChangeSignalKind_REPOSITORY_CHANGE_SIGNAL_KIND_PUSH,
			providersv1.RepositoryChangeSignalKind_REPOSITORY_CHANGE_SIGNAL_KIND_PULL_REQUEST_MERGED,
		}
	}
}

func validateRepositoryChangeSignal(signal *providersv1.RepositoryChangeSignal, options runnerOptions, scenario *repositoryChangeScenario) error {
	if signal.GetStatus() != providersv1.RepositoryChangeSignalStatus_REPOSITORY_CHANGE_SIGNAL_STATUS_OBSERVED {
		return errors.New("provider repository change signal is not observed")
	}
	for field, values := range map[string][2]string{
		"provider_slug":        {signal.GetProviderSlug(), options.ProviderSlug},
		"repository_full_name": {signal.GetRepositoryFullName(), options.RepositoryFullName},
	} {
		if values[0] != values[1] {
			return fmt.Errorf("provider repository change signal %s mismatch", field)
		}
	}
	if options.ProviderRepositoryID != "" && signal.GetProviderRepositoryId() != options.ProviderRepositoryID {
		return errors.New("provider repository change signal provider_repository_id mismatch")
	}
	baseBranch := firstNonEmpty(scenario.BaseBranch, refBranchName(scenario.Ref))
	if baseBranch != "" && signal.GetBaseBranch() != baseBranch {
		return errors.New("provider repository change signal base_branch mismatch")
	}
	if commitSHA := strings.TrimSpace(scenario.CommitSHA); commitSHA != "" && signal.GetCommitSha() != strings.ToLower(commitSHA) && signal.GetCommitSha() != commitSHA {
		return errors.New("provider repository change signal commit_sha mismatch")
	}
	if strings.ToLower(strings.TrimSpace(scenario.EventName)) == "push" && signal.GetPathSummaryStatus() == providersv1.RepositoryChangePathSummaryStatus_REPOSITORY_CHANGE_PATH_SUMMARY_STATUS_UNAVAILABLE {
		return errors.New("provider repository change signal path summary is unavailable")
	}
	return nil
}

func applyReconciliation(ctx context.Context, client projectCatalogAPI, options runnerOptions, scenario onboardingScenario, signals mergeSignalSet, output io.Writer, setupMode bool) error {
	if wantsKind(options, "bootstrap") {
		if setupMode && !canReconcile(scenario.Bootstrap, signals.Bootstrap) {
			logLine(output, "PLAN", "bootstrap reconcile skipped; provider merge signal or checked input is not ready")
		} else {
			if err := applyBootstrap(ctx, client, options, scenario.Bootstrap, signals.Bootstrap, output); err != nil {
				return err
			}
		}
	}
	if wantsKind(options, "adoption") {
		if setupMode && !canReconcile(scenario.Adoption, signals.Adoption) {
			logLine(output, "PLAN", "adoption reconcile skipped; provider merge signal or checked input is not ready")
		} else {
			if err := applyAdoption(ctx, client, options, scenario.Adoption, signals.Adoption, output); err != nil {
				return err
			}
		}
	}
	return nil
}

func canReconcile(scenario *reconcileScenario, signal *providersv1.RepositoryMergeSignal) bool {
	return scenario != nil && signal != nil && hasCheckedPolicyValues(scenario.CheckedPolicy)
}

func applyBootstrap(ctx context.Context, client projectCatalogAPI, options runnerOptions, scenario *reconcileScenario, signal *providersv1.RepositoryMergeSignal, output io.Writer) error {
	if scenario == nil {
		return errors.New("bootstrap apply requires checked bootstrap scenario input")
	}
	if signal == nil {
		return errors.New("bootstrap apply requires provider merge signal")
	}
	if err := validateCheckedScenario("bootstrap", scenario); err != nil {
		return err
	}
	response, err := client.ReconcileBootstrapMergeSignal(ctx, &projectsv1.ReconcileBootstrapMergeSignalRequest{
		ProjectId:     options.ProjectID,
		RepositoryId:  options.RepositoryID,
		MergeSignal:   projectBootstrapSignal(signal, scenario),
		CheckedPolicy: projectBootstrapPolicy(scenario.CheckedPolicy),
		Meta:          projectCommandMeta(options, "bootstrap", signal.GetSignalKey(), scenario.CheckedPolicy.ArtifactDigest),
	})
	if err != nil {
		return safeError("project-catalog ReconcileBootstrapMergeSignal failed", err)
	}
	logLine(output, "OK", "bootstrap reconcile completed source_ref=%s commit=%s summary=%s",
		safeValue(response.GetSourceRef()),
		safeValue(response.GetSourceCommitSha()),
		safeValue(response.GetSummary()),
	)
	return nil
}

func applyAdoption(ctx context.Context, client projectCatalogAPI, options runnerOptions, scenario *reconcileScenario, signal *providersv1.RepositoryMergeSignal, output io.Writer) error {
	if scenario == nil {
		return errors.New("adoption apply requires checked adoption scenario input")
	}
	if signal == nil {
		return errors.New("adoption apply requires provider merge signal")
	}
	if err := validateCheckedScenario("adoption", scenario); err != nil {
		return err
	}
	response, err := client.ReconcileAdoptionMergeSignal(ctx, &projectsv1.ReconcileAdoptionMergeSignalRequest{
		ProjectId:     options.ProjectID,
		RepositoryId:  options.RepositoryID,
		MergeSignal:   projectAdoptionSignal(signal, scenario),
		CheckedPolicy: projectAdoptionPolicy(scenario.CheckedPolicy),
		Meta:          projectCommandMeta(options, "adoption", signal.GetSignalKey(), scenario.CheckedPolicy.ArtifactDigest),
	})
	if err != nil {
		return safeError("project-catalog ReconcileAdoptionMergeSignal failed", err)
	}
	logLine(output, "OK", "adoption reconcile completed source_ref=%s commit=%s summary=%s",
		safeValue(response.GetSourceRef()),
		safeValue(response.GetSourceCommitSha()),
		safeValue(response.GetSummary()),
	)
	return nil
}

func validateCheckedScenario(kind string, scenario *reconcileScenario) error {
	if strings.TrimSpace(scenario.WatermarkJSON) == "" {
		return fmt.Errorf("%s checked scenario requires watermark_json", kind)
	}
	policy := scenario.CheckedPolicy
	for name, value := range map[string]string{
		"artifact_ref":           policy.ArtifactRef,
		"artifact_digest":        policy.ArtifactDigest,
		"artifact_version":       policy.ArtifactVersion,
		"source_path":            policy.SourcePath,
		"content_hash":           policy.ContentHash,
		"validated_payload_json": policy.ValidatedPayloadJSON,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s checked scenario requires %s", kind, name)
		}
	}
	if !json.Valid([]byte(policy.ValidatedPayloadJSON)) {
		return fmt.Errorf("%s checked scenario has invalid validated_payload_json", kind)
	}
	return nil
}

func projectBootstrapSignal(signal *providersv1.RepositoryMergeSignal, scenario *reconcileScenario) *projectsv1.BootstrapRepositoryMergeSignal {
	return &projectsv1.BootstrapRepositoryMergeSignal{
		SignalId:                     optionalString(signal.GetSignalId()),
		SignalKey:                    firstNonEmpty(scenario.SignalKey, signal.GetSignalKey()),
		SignalKind:                   "bootstrap",
		ProviderTarget:               projectProviderTarget(signal),
		BaseBranch:                   signal.GetBaseBranch(),
		SourceRef:                    signal.GetSourceRef(),
		MergeCommitSha:               signal.GetMergeCommitSha(),
		WatermarkDigest:              signal.GetWatermarkDigest(),
		WatermarkJson:                scenario.WatermarkJSON,
		ProviderWorkItemProjectionId: optionalString(signal.GetWorkItemProjectionId()),
		ProviderObjectId:             optionalString(signal.GetPullRequestProviderId()),
		MergeObservedAt:              optionalString(signal.GetObservedAt()),
		MergedAt:                     optionalString(signal.GetMergedAt()),
	}
}

func projectAdoptionSignal(signal *providersv1.RepositoryMergeSignal, scenario *reconcileScenario) *projectsv1.RepositoryAdoptionMergeSignal {
	return &projectsv1.RepositoryAdoptionMergeSignal{
		SignalId:                     optionalString(signal.GetSignalId()),
		SignalKey:                    firstNonEmpty(scenario.SignalKey, signal.GetSignalKey()),
		SignalKind:                   "adoption",
		ProviderTarget:               projectProviderTarget(signal),
		BaseBranch:                   signal.GetBaseBranch(),
		SourceRef:                    signal.GetSourceRef(),
		MergeCommitSha:               signal.GetMergeCommitSha(),
		WatermarkDigest:              signal.GetWatermarkDigest(),
		WatermarkJson:                scenario.WatermarkJSON,
		ProviderWorkItemProjectionId: optionalString(signal.GetWorkItemProjectionId()),
		ProviderObjectId:             optionalString(signal.GetPullRequestProviderId()),
		MergeObservedAt:              optionalString(signal.GetObservedAt()),
		MergedAt:                     optionalString(signal.GetMergedAt()),
	}
}

func projectProviderTarget(signal *providersv1.RepositoryMergeSignal) *projectsv1.RepositoryBootstrapProviderTarget {
	return &projectsv1.RepositoryBootstrapProviderTarget{
		ProviderSlug:         signal.GetProviderSlug(),
		RepositoryFullName:   signal.GetRepositoryFullName(),
		ProviderRepositoryId: optionalString(signal.GetProviderRepositoryId()),
	}
}

func projectBootstrapPolicy(policy checkedPolicyScenario) *projectsv1.CheckedBootstrapServicesPolicyArtifact {
	return &projectsv1.CheckedBootstrapServicesPolicyArtifact{
		ArtifactRef:          policy.ArtifactRef,
		ArtifactDigest:       policy.ArtifactDigest,
		ArtifactVersion:      policy.ArtifactVersion,
		SourcePath:           policy.SourcePath,
		ContentHash:          policy.ContentHash,
		ValidatedPayloadJson: policy.ValidatedPayloadJSON,
	}
}

func projectAdoptionPolicy(policy checkedPolicyScenario) *projectsv1.CheckedAdoptionServicesPolicyArtifact {
	return &projectsv1.CheckedAdoptionServicesPolicyArtifact{
		ArtifactRef:          policy.ArtifactRef,
		ArtifactDigest:       policy.ArtifactDigest,
		ArtifactVersion:      policy.ArtifactVersion,
		SourcePath:           policy.SourcePath,
		ContentHash:          policy.ContentHash,
		ValidatedPayloadJson: policy.ValidatedPayloadJSON,
	}
}

func describeSignal(output io.Writer, kind string, signal *providersv1.RepositoryMergeSignal) {
	if signal == nil {
		logLine(output, "BLOCKED", "%s merge signal is not ready", kind)
		return
	}
	logLine(output, "OK", "%s merge signal ready key=%s commit=%s base=%s head=%s version=%d etag=%s",
		kind,
		safeValue(signal.GetSignalKey()),
		safeValue(signal.GetMergeCommitSha()),
		safeValue(signal.GetBaseBranch()),
		safeValue(signal.GetHeadBranch()),
		signal.GetVersion(),
		safeValue(signal.GetEtag()),
	)
}

func describeCheckedInput(output io.Writer, kind string, scenario *reconcileScenario) {
	if scenario == nil {
		logLine(output, "BLOCKED", "%s checked artifact input is not configured", kind)
		return
	}
	if err := validateCheckedScenario(kind, scenario); err != nil {
		logLine(output, "BLOCKED", "%s checked artifact input is incomplete: %s", kind, err.Error())
		return
	}
	policy := scenario.CheckedPolicy
	logLine(output, "OK", "%s checked artifact configured ref=%s digest=%s version=%s source_path=%s content_hash=%s checked_input=present_hidden",
		kind,
		safeValue(policy.ArtifactRef),
		safeValue(policy.ArtifactDigest),
		safeValue(policy.ArtifactVersion),
		safeValue(policy.SourcePath),
		safeValue(policy.ContentHash),
	)
}

func describePreparedCheckedInputs(output io.Writer, prepared preparedCheckedInputs) {
	if prepared.Bootstrap {
		logLine(output, "OK", "bootstrap checked artifact input produced from normalized payload checked_input=present_hidden")
	}
	if prepared.Adoption {
		logLine(output, "OK", "adoption checked artifact input produced from normalized payload checked_input=present_hidden")
	}
}

func projectQueryMeta(options runnerOptions) *projectsv1.QueryMeta {
	return &projectsv1.QueryMeta{
		Actor:     &projectsv1.Actor{Type: "service", Id: options.ActorID},
		RequestId: options.RequestID,
		RequestContext: &projectsv1.RequestContext{
			Source: defaultSource,
		},
	}
}

func providerQueryMeta(options runnerOptions) *providersv1.QueryMeta {
	return &providersv1.QueryMeta{
		Actor:     &providersv1.Actor{Type: "service", Id: options.ActorID},
		RequestId: options.RequestID,
		RequestContext: &providersv1.RequestContext{
			Source: defaultSource,
		},
	}
}

func projectCommandMeta(options runnerOptions, kind string, signalKey string, artifactDigest string) *projectsv1.CommandMeta {
	idempotency := options.IdempotencyKey
	if idempotency == "" {
		idempotency = deterministicKey(options.ProjectID, options.RepositoryID, kind, signalKey, artifactDigest)
	}
	return &projectsv1.CommandMeta{
		IdempotencyKey: optionalString(idempotency),
		Actor:          &projectsv1.Actor{Type: "service", Id: options.ActorID},
		Reason:         "onboarding_product_api_runner",
		RequestId:      options.RequestID,
		RequestContext: &projectsv1.RequestContext{
			Source: defaultSource,
		},
	}
}

func projectStageCommandMeta(options runnerOptions, stage string, target string, artifactDigest string) *projectsv1.CommandMeta {
	idempotency := options.IdempotencyKey
	if idempotency == "" {
		idempotency = deterministicKey(options.ProjectID, options.RepositoryID, stage, target, artifactDigest)
	} else {
		idempotency = strings.TrimSpace(idempotency) + "-" + stage
	}
	return &projectsv1.CommandMeta{
		IdempotencyKey: optionalString(idempotency),
		Actor:          &projectsv1.Actor{Type: "service", Id: options.ActorID},
		Reason:         "onboarding_product_api_runner",
		RequestId:      options.RequestID,
		RequestContext: &projectsv1.RequestContext{
			Source: defaultSource,
		},
	}
}

func projectCreateCommandMeta(options runnerOptions) *projectsv1.CommandMeta {
	idempotency := options.IdempotencyKey
	if idempotency == "" {
		idempotency = deterministicKey(options.OrganizationID, options.ProjectSlug, "project-bootstrap")
	} else {
		idempotency = strings.TrimSpace(idempotency) + "-project-bootstrap"
	}
	return &projectsv1.CommandMeta{
		IdempotencyKey: optionalString(idempotency),
		Actor:          &projectsv1.Actor{Type: "service", Id: options.ActorID},
		Reason:         "onboarding_product_api_runner",
		RequestId:      options.RequestID,
		RequestContext: &projectsv1.RequestContext{
			Source: defaultSource,
		},
	}
}

func wantsKind(options runnerOptions, kind string) bool {
	return options.Kind == "both" || options.Kind == kind
}

func deterministicKey(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(strings.TrimSpace(part)))
		_, _ = hash.Write([]byte{0})
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	return "onboarding-runner-" + sum[:24]
}

func refBranchName(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "refs/heads/") {
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	return ""
}

func validRepositoryChangePath(value string) bool {
	path := strings.TrimSpace(value)
	if path == "" || len(path) > 256 || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return false
	}
	if strings.Contains(path, "\\") || strings.Contains(path, "\x00") {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func repositoryChangeAction(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "changed":
		return "changed"
	case "added", "modified", "removed", "renamed":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func repositoryChangePathCategory(path string) string {
	path = strings.TrimSpace(path)
	switch {
	case path == "services.yaml":
		return "services_policy"
	case path == "Dockerfile" || strings.HasSuffix(path, "/Dockerfile"):
		return "deploy_relevant"
	case path == "go.mod" || path == "go.sum" || path == "Makefile":
		return "deploy_relevant"
	case strings.HasPrefix(path, "deploy/") ||
		strings.HasPrefix(path, "services/") ||
		strings.HasPrefix(path, "packages/") ||
		strings.HasPrefix(path, "proto/") ||
		strings.HasPrefix(path, "specs/"):
		return "deploy_relevant"
	case strings.HasPrefix(path, ".github/workflows/"):
		return "provider_workflow"
	case strings.HasPrefix(path, "docs/") || path == "AGENTS.md":
		return "documentation"
	default:
		return "other"
	}
}

func repositoryChangeCategoryDeployRelevant(category string) bool {
	switch category {
	case "services_policy", "deploy_relevant", "provider_workflow":
		return true
	default:
		return false
	}
}

func formatPathCategories(categories map[string]int) string {
	if len(categories) == 0 {
		return "n/a"
	}
	order := []string{"services_policy", "deploy_relevant", "provider_workflow", "documentation", "other"}
	parts := make([]string, 0, len(categories))
	for _, category := range order {
		count := categories[category]
		if count > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d", category, count))
		}
	}
	return strings.Join(parts, ",")
}

func validGitCommitSHA(text string) bool {
	if len(text) != 40 && len(text) != 64 {
		return false
	}
	for _, char := range text {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F') {
			continue
		}
		return false
	}
	return true
}

func validSHA256ContentHash(text string) bool {
	hash := strings.TrimSpace(strings.ToLower(text))
	const prefix = "sha256:"
	if !strings.HasPrefix(hash, prefix) {
		return false
	}
	encoded := strings.TrimPrefix(hash, prefix)
	if len(encoded) != 64 {
		return false
	}
	for _, char := range encoded {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') {
			continue
		}
		return false
	}
	return true
}

func splitRepositoryFullName(fullName string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(fullName), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalBool(value bool) *bool {
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func envOr(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func logLine(output io.Writer, level string, format string, args ...any) {
	_, _ = fmt.Fprintf(output, "%s %s\n", level, redact(fmt.Sprintf(format, args...)))
}

func safeValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "n/a"
	}
	return value
}

func safeError(prefix string, err error) error {
	if err == nil {
		return errors.New(prefix)
	}
	return fmt.Errorf("%s: %s", prefix, redact(err.Error()))
}

var (
	urlPattern       = regexp.MustCompile(`https?://[^\s)"']+`)
	secretKeyPattern = regexp.MustCompile(`(?i)(token|secret|authorization|password|payload|validated_payload_json|webhook_body|provider_response)(\s*[:=]\s*)[^\s,;]+`)
)

func redact(value string) string {
	value = urlPattern.ReplaceAllString(value, "[url hidden]")
	value = secretKeyPattern.ReplaceAllString(value, "$1$2[hidden]")
	return value
}
