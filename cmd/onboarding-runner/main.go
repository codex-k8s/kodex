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
	"os"
	"regexp"
	"strings"
	"time"

	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultProviderSlug      = "github"
	defaultActorID           = "onboarding-runner"
	defaultSource            = "cmd/onboarding-runner"
	defaultCheckedSourcePath = "services.yaml"
	maxCheckedPayloadBytes   = 256 * 1024
	maxWatermarkJSONBytes    = 16 * 1024
	maxBootstrapSetupFiles   = 64
	maxBootstrapSetupBytes   = 4 * 1024 * 1024
)

func main() {
	options := runnerOptions{}
	flag.StringVar(&options.ProjectCatalogAddr, "project-catalog-addr", os.Getenv("KODEX_PROJECT_CATALOG_GRPC_ADDR"), "project-catalog gRPC address")
	flag.StringVar(&options.ProviderHubAddr, "provider-hub-addr", os.Getenv("KODEX_PROVIDER_HUB_GRPC_ADDR"), "provider-hub gRPC address")
	flag.StringVar(&options.ScenarioFilePath, "scenario-file", "", "checked onboarding scenario JSON; payload values are never printed")
	flag.StringVar(&options.ProjectID, "project-id", "", "project-catalog project id")
	flag.StringVar(&options.RepositoryID, "repository-id", "", "project-catalog repository binding id")
	flag.StringVar(&options.ProviderSlug, "provider-slug", defaultProviderSlug, "provider slug")
	flag.StringVar(&options.RepositoryFullName, "repository-full-name", "", "provider repository owner/name")
	flag.StringVar(&options.ProviderRepositoryID, "provider-repository-id", "", "provider-native repository id")
	flag.StringVar(&options.ExternalAccountID, "external-account-id", os.Getenv("KODEX_ONBOARDING_RUNNER_EXTERNAL_ACCOUNT_ID"), "external account id selected by caller policy")
	flag.StringVar(&options.AllowedProviderOwner, "allowed-provider-owner", os.Getenv("KODEX_ONBOARDING_RUNNER_ALLOWED_OWNER"), "required owner for apply mode")
	flag.StringVar(&options.RepositoryNamePrefix, "repository-name-prefix", os.Getenv("KODEX_ONBOARDING_RUNNER_REPOSITORY_PREFIX"), "required repository name prefix for apply mode")
	flag.StringVar(&options.IdempotencyKey, "idempotency-key", "", "optional idempotency key prefix")
	flag.StringVar(&options.RequestID, "request-id", "", "optional request id")
	flag.StringVar(&options.ActorID, "actor-id", defaultActorID, "safe actor id")
	flag.StringVar(&options.Kind, "kind", "both", "bootstrap, adoption or both")
	flag.StringVar(&options.CheckedPayloadFilePath, "checked-payload-file", os.Getenv("KODEX_ONBOARDING_RUNNER_CHECKED_PAYLOAD_FILE"), "normalized checked validated_payload_json file; values are never printed")
	flag.StringVar(&options.WatermarkJSONFilePath, "watermark-json-file", os.Getenv("KODEX_ONBOARDING_RUNNER_WATERMARK_JSON_FILE"), "safe watermark JSON file for checked artifact producer")
	flag.StringVar(&options.CheckedSourcePath, "checked-source-path", os.Getenv("KODEX_ONBOARDING_RUNNER_CHECKED_SOURCE_PATH"), "source path for checked services policy artifact")
	flag.StringVar(&options.CheckedArtifactRef, "checked-artifact-ref", os.Getenv("KODEX_ONBOARDING_RUNNER_CHECKED_ARTIFACT_REF"), "optional immutable checked artifact ref")
	flag.StringVar(&options.CheckedArtifactVersion, "checked-artifact-version", os.Getenv("KODEX_ONBOARDING_RUNNER_CHECKED_ARTIFACT_VERSION"), "optional checked artifact version; defaults to provider merge commit")
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
	GetRepository(context.Context, *projectsv1.GetRepositoryRequest, ...grpc.CallOption) (*projectsv1.RepositoryResponse, error)
	CreateProviderRepository(context.Context, *projectsv1.CreateProviderRepositoryRequest, ...grpc.CallOption) (*projectsv1.RepositoryProviderCreateResponse, error)
	CreateRepositoryBootstrapPullRequest(context.Context, *projectsv1.CreateRepositoryBootstrapPullRequestRequest, ...grpc.CallOption) (*projectsv1.RepositoryBootstrapPullRequestResponse, error)
	ReconcileBootstrapMergeSignal(context.Context, *projectsv1.ReconcileBootstrapMergeSignalRequest, ...grpc.CallOption) (*projectsv1.BootstrapServicesPolicyImportResponse, error)
	ReconcileAdoptionMergeSignal(context.Context, *projectsv1.ReconcileAdoptionMergeSignalRequest, ...grpc.CallOption) (*projectsv1.BootstrapServicesPolicyImportResponse, error)
}

type providerHubAPI interface {
	GetRepositoryMergeSignal(context.Context, *providersv1.GetRepositoryMergeSignalRequest, ...grpc.CallOption) (*providersv1.RepositoryMergeSignalResponse, error)
	ListRepositoryMergeSignals(context.Context, *providersv1.ListRepositoryMergeSignalsRequest, ...grpc.CallOption) (*providersv1.ListRepositoryMergeSignalsResponse, error)
	ListRepositoryAdoptionScanSnapshots(context.Context, *providersv1.ListRepositoryAdoptionScanSnapshotsRequest, ...grpc.CallOption) (*providersv1.ListRepositoryAdoptionScanSnapshotsResponse, error)
}

type runnerClients struct {
	ProjectCatalog projectCatalogAPI
	ProviderHub    providerHubAPI
}

type runnerOptions struct {
	ProjectCatalogAddr     string
	ProviderHubAddr        string
	ScenarioFilePath       string
	ProjectID              string
	RepositoryID           string
	ProviderSlug           string
	RepositoryFullName     string
	ProviderRepositoryID   string
	ExternalAccountID      string
	AllowedProviderOwner   string
	RepositoryNamePrefix   string
	IdempotencyKey         string
	RequestID              string
	ActorID                string
	Kind                   string
	CheckedPayloadFilePath string
	WatermarkJSONFilePath  string
	CheckedSourcePath      string
	CheckedArtifactRef     string
	CheckedArtifactVersion string
	Apply                  bool
	Timeout                time.Duration
}

type onboardingScenario struct {
	ProjectID            string             `json:"project_id,omitempty"`
	RepositoryID         string             `json:"repository_id,omitempty"`
	ProviderSlug         string             `json:"provider_slug,omitempty"`
	RepositoryFullName   string             `json:"repository_full_name,omitempty"`
	ProviderRepositoryID string             `json:"provider_repository_id,omitempty"`
	BootstrapSetup       *bootstrapSetup    `json:"bootstrap_setup,omitempty"`
	Bootstrap            *reconcileScenario `json:"bootstrap,omitempty"`
	Adoption             *reconcileScenario `json:"adoption,omitempty"`
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
	if options.RepositoryID != "" {
		repository, err = checkProjectRepository(ctx, clients.ProjectCatalog, options, output)
		if err != nil {
			return err
		}
		options, err = bindOptionsToRepository(options, repository)
		if err != nil {
			return err
		}
	} else {
		logLine(output, "BLOCKED", "project repository binding is not available before bootstrap repository create")
	}
	if err := describeBootstrapSetup(options, scenario.BootstrapSetup, output); err != nil {
		return err
	}
	if options.Apply {
		if err := validateApplyPolicy(options, scenario); err != nil {
			return err
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
	if options.RepositoryID == "" {
		if !options.Apply {
			logLine(output, "PLAN", "apply disabled; no mutating product API calls executed")
			return nil
		}
		return errors.New("repository_id is required after bootstrap repository create")
	}
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
	if err := applyReconciliation(ctx, clients.ProjectCatalog, options, scenario, signals, output, hasBootstrapSetup(scenario)); err != nil {
		return err
	}
	logLine(output, "OK", "onboarding product API path completed")
	return nil
}

func newGRPCClients(options runnerOptions) (runnerClients, func(), error) {
	if strings.TrimSpace(options.ProjectCatalogAddr) == "" {
		return runnerClients{}, func() {}, errors.New("project-catalog gRPC address is required")
	}
	if strings.TrimSpace(options.ProviderHubAddr) == "" {
		return runnerClients{}, func() {}, errors.New("provider-hub gRPC address is required")
	}
	projectConn, err := grpc.NewClient(options.ProjectCatalogAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return runnerClients{}, func() {}, fmt.Errorf("connect project-catalog: %w", err)
	}
	providerConn, err := grpc.NewClient(options.ProviderHubAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		_ = projectConn.Close()
		return runnerClients{}, func() {}, fmt.Errorf("connect provider-hub: %w", err)
	}
	closeClients := func() {
		_ = providerConn.Close()
		_ = projectConn.Close()
	}
	return runnerClients{
		ProjectCatalog: projectsv1.NewProjectCatalogServiceClient(projectConn),
		ProviderHub:    providersv1.NewProviderHubServiceClient(providerConn),
	}, closeClients, nil
}

func normalizeOptions(options runnerOptions) runnerOptions {
	options.ProjectID = strings.TrimSpace(options.ProjectID)
	options.RepositoryID = strings.TrimSpace(options.RepositoryID)
	options.ProviderSlug = defaultString(strings.TrimSpace(options.ProviderSlug), defaultProviderSlug)
	options.RepositoryFullName = strings.TrimSpace(options.RepositoryFullName)
	options.ProviderRepositoryID = strings.TrimSpace(options.ProviderRepositoryID)
	options.ExternalAccountID = strings.TrimSpace(options.ExternalAccountID)
	options.AllowedProviderOwner = strings.TrimSpace(options.AllowedProviderOwner)
	options.RepositoryNamePrefix = strings.TrimSpace(options.RepositoryNamePrefix)
	options.IdempotencyKey = strings.TrimSpace(options.IdempotencyKey)
	options.RequestID = defaultString(strings.TrimSpace(options.RequestID), "onboarding-runner-"+time.Now().UTC().Format("20060102T150405Z"))
	options.ActorID = defaultString(strings.TrimSpace(options.ActorID), defaultActorID)
	options.Kind = strings.ToLower(strings.TrimSpace(options.Kind))
	options.CheckedPayloadFilePath = strings.TrimSpace(options.CheckedPayloadFilePath)
	options.WatermarkJSONFilePath = strings.TrimSpace(options.WatermarkJSONFilePath)
	options.CheckedSourcePath = defaultString(strings.TrimSpace(options.CheckedSourcePath), defaultCheckedSourcePath)
	options.CheckedArtifactRef = strings.TrimSpace(options.CheckedArtifactRef)
	options.CheckedArtifactVersion = strings.TrimSpace(options.CheckedArtifactVersion)
	if options.Kind == "" {
		options.Kind = "both"
	}
	return options
}

func mergeScenarioOptions(options runnerOptions, scenario onboardingScenario) runnerOptions {
	if options.ProjectID == "" {
		options.ProjectID = strings.TrimSpace(scenario.ProjectID)
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
		return errors.New("project_id is required")
	}
	if options.RepositoryID == "" && !hasBootstrapCreateRepository(scenario) {
		return errors.New("repository_id is required")
	}
	if options.ProviderSlug == "" {
		return errors.New("provider_slug is required")
	}
	switch options.Kind {
	case "bootstrap", "adoption", "both":
	default:
		return fmt.Errorf("kind must be bootstrap, adoption or both")
	}
	if hasBootstrapSetup(scenario) && !wantsKind(options, "bootstrap") {
		return errors.New("bootstrap_setup requires kind bootstrap or both")
	}
	if options.CheckedPayloadFilePath != "" {
		if options.Kind == "both" {
			return errors.New("checked payload producer requires kind bootstrap or adoption")
		}
		if options.CheckedSourcePath == "" {
			return errors.New("checked payload producer requires checked source path")
		}
	}
	return nil
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
	if options.RepositoryNamePrefix == "" {
		return errors.New("apply requires repository name prefix")
	}
	repositoryFullName := options.RepositoryFullName
	if scenario.BootstrapSetup != nil && scenario.BootstrapSetup.CreateRepository != nil {
		create := scenario.BootstrapSetup.CreateRepository
		repositoryFullName = strings.TrimSpace(create.ProviderOwner) + "/" + strings.TrimSpace(create.ProviderName)
	}
	owner, name, ok := splitRepositoryFullName(repositoryFullName)
	if !ok {
		return errors.New("apply requires repository_full_name as owner/name")
	}
	if owner != options.AllowedProviderOwner {
		return fmt.Errorf("apply target owner %q is not allowed", owner)
	}
	if !strings.HasPrefix(name, options.RepositoryNamePrefix) {
		return fmt.Errorf("apply target repository must start with %q", options.RepositoryNamePrefix)
	}
	return nil
}

func hasBootstrapSetup(scenario onboardingScenario) bool {
	return scenario.BootstrapSetup != nil && (scenario.BootstrapSetup.CreateRepository != nil || scenario.BootstrapSetup.PullRequest != nil)
}

func hasBootstrapCreateRepository(scenario onboardingScenario) bool {
	return scenario.BootstrapSetup != nil && scenario.BootstrapSetup.CreateRepository != nil
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
	if _, err := repositoryOwnerKindFromString(scenario.OwnerKind); err != nil {
		return err
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

func projectRepositoryProviderFromSlug(value string) projectsv1.RepositoryProvider {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "github":
		return projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_GITHUB
	default:
		return projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_UNSPECIFIED
	}
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
