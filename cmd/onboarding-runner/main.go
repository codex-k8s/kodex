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
	Bootstrap            *reconcileScenario `json:"bootstrap,omitempty"`
	Adoption             *reconcileScenario `json:"adoption,omitempty"`
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
	if err := validateOptions(options); err != nil {
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

	repository, err := checkProjectRepository(ctx, clients.ProjectCatalog, options, output)
	if err != nil {
		return err
	}
	options, err = bindOptionsToRepository(options, repository)
	if err != nil {
		return err
	}
	if options.Apply {
		if err := validateApplyPolicy(options); err != nil {
			return err
		}
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
	if err := applyReconciliation(ctx, clients.ProjectCatalog, options, scenario, signals, output); err != nil {
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
	return options
}

func validateOptions(options runnerOptions) error {
	if options.ProjectID == "" {
		return errors.New("project_id is required")
	}
	if options.RepositoryID == "" {
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
	contentHash := prefixedSHA256([]byte(producer.PayloadJSON))
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

func prefixedSHA256(parts ...[]byte) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write(part)
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func defaultCheckedArtifactRef(kind string, artifactDigest string) string {
	shortDigest := strings.TrimPrefix(artifactDigest, "sha256:")
	if len(shortDigest) > 24 {
		shortDigest = shortDigest[:24]
	}
	return "checked://onboarding/" + kind + "/" + shortDigest
}

func validateApplyPolicy(options runnerOptions) error {
	if options.AllowedProviderOwner == "" {
		return errors.New("apply requires allowed provider owner")
	}
	if options.RepositoryNamePrefix == "" {
		return errors.New("apply requires repository name prefix")
	}
	owner, name, ok := splitRepositoryFullName(options.RepositoryFullName)
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

func applyReconciliation(ctx context.Context, client projectCatalogAPI, options runnerOptions, scenario onboardingScenario, signals mergeSignalSet, output io.Writer) error {
	if wantsKind(options, "bootstrap") {
		if err := applyBootstrap(ctx, client, options, scenario.Bootstrap, signals.Bootstrap, output); err != nil {
			return err
		}
	}
	if wantsKind(options, "adoption") {
		if err := applyAdoption(ctx, client, options, scenario.Adoption, signals.Adoption, output); err != nil {
			return err
		}
	}
	return nil
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
