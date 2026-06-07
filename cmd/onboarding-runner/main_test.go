package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestRunDryRunPlansWithoutMutationsAndRedactsCheckedPayload(t *testing.T) {
	scenarioPath := writeScenario(t, checkedPayloadFixture())
	clients := runnerClients{
		ProjectCatalog: &fakeProjectCatalogClient{repository: repositoryFixture()},
		ProviderHub: &fakeProviderHubClient{
			bootstrapSignal: mergeSignalFixture("bootstrap"),
			adoptionSignal:  mergeSignalFixture("adoption"),
			snapshotCount:   1,
		},
	}

	var output bytes.Buffer
	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:   scenarioPath,
		ProjectID:          "project-1",
		RepositoryID:       "repo-1",
		RepositoryFullName: "codex-k8s/kodex-onboarding-test",
		RequestID:          "req-1",
		Kind:               "both",
	}, clients, &output)
	if err != nil {
		t.Fatalf("run dry-run: %v", err)
	}

	projectClient := clients.ProjectCatalog.(*fakeProjectCatalogClient)
	if projectClient.bootstrapReconcileCalls != 0 || projectClient.adoptionReconcileCalls != 0 {
		t.Fatalf("dry-run called mutating reconcile methods: bootstrap=%d adoption=%d", projectClient.bootstrapReconcileCalls, projectClient.adoptionReconcileCalls)
	}
	assertNotContains(t, output.String(), "raw_services_yaml_secret")
	assertNotContains(t, output.String(), "provider_payload_secret")
	assertContains(t, output.String(), "apply disabled")
	assertContains(t, output.String(), "checked_input=present_hidden")
}

func TestRunApplyRequiresExplicitSafeTargetPolicy(t *testing.T) {
	scenarioPath := writeScenario(t, checkedPayloadFixture())
	clients := runnerClients{
		ProjectCatalog: &fakeProjectCatalogClient{repository: repositoryFixture()},
		ProviderHub:    &fakeProviderHubClient{bootstrapSignal: mergeSignalFixture("bootstrap")},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:   scenarioPath,
		ProjectID:          "project-1",
		RepositoryID:       "repo-1",
		RepositoryFullName: "codex-k8s/kodex-onboarding-test",
		RequestID:          "req-1",
		Kind:               "bootstrap",
		Apply:              true,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected apply policy error")
	}
	assertContains(t, err.Error(), "allowed provider owner")
}

func TestNewGRPCClientsApplyDoesNotRequireAccessManagerAddress(t *testing.T) {
	clients, closeClients, err := newGRPCClients(runnerOptions{
		ProjectCatalogAddr:         "127.0.0.1:1",
		ProviderHubAddr:            "127.0.0.1:2",
		ProjectCatalogAuthTokenEnv: "",
		ProviderHubAuthTokenEnv:    "",
		AccessManagerAuthTokenEnv:  "",
		RequestID:                  "req-1",
		ActorID:                    "runner-test",
		Apply:                      true,
	})
	if err != nil {
		t.Fatalf("newGRPCClients apply without access-manager address: %v", err)
	}
	defer closeClients()

	if clients.ProjectCatalog == nil || clients.ProviderHub == nil {
		t.Fatal("expected project-catalog and provider-hub clients")
	}
	if clients.AccessManager != nil {
		t.Fatal("expected nil access-manager client when address is not configured")
	}
}

func TestRunApplyReconcilesBootstrapAndAdoptionThroughProductAPIs(t *testing.T) {
	scenarioPath := writeScenario(t, checkedPayloadFixture())
	projectClient := &fakeProjectCatalogClient{repository: repositoryFixture()}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub: &fakeProviderHubClient{
			bootstrapSignal: mergeSignalFixture("bootstrap"),
			adoptionSignal:  mergeSignalFixture("adoption"),
		},
	}

	var output bytes.Buffer
	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:     scenarioPath,
		ProjectID:            "project-1",
		RepositoryID:         "repo-1",
		RepositoryFullName:   "codex-k8s/kodex-onboarding-test",
		AllowedProviderOwner: "codex-k8s",
		RepositoryNamePrefix: "kodex-onboarding-",
		RequestID:            "req-1",
		Kind:                 "both",
		Apply:                true,
	}, clients, &output)
	if err != nil {
		t.Fatalf("run apply: %v", err)
	}

	if projectClient.bootstrapReconcileCalls != 1 {
		t.Fatalf("expected one bootstrap reconcile call, got %d", projectClient.bootstrapReconcileCalls)
	}
	if projectClient.adoptionReconcileCalls != 1 {
		t.Fatalf("expected one adoption reconcile call, got %d", projectClient.adoptionReconcileCalls)
	}
	if got := projectClient.lastBootstrapRequest.GetCheckedPolicy().GetValidatedPayloadJson(); !strings.Contains(got, "raw_services_yaml_secret") {
		t.Fatalf("checked bootstrap payload was not passed to product API")
	}
	if got := projectClient.lastAdoptionRequest.GetCheckedPolicy().GetValidatedPayloadJson(); !strings.Contains(got, "provider_payload_secret") {
		t.Fatalf("checked adoption payload was not passed to product API")
	}
	assertNotContains(t, output.String(), "raw_services_yaml_secret")
	assertNotContains(t, output.String(), "provider_payload_secret")
	assertContains(t, output.String(), "bootstrap reconcile completed")
	assertContains(t, output.String(), "adoption reconcile completed")
}

func TestRunApplyProducesBootstrapCheckedArtifactFromValidatedPayloadFile(t *testing.T) {
	scenarioPath := writeScenario(t, signalOnlyScenario("bootstrap"))
	payloadJSON := `{"services":[{"key":"api"}],"marker":"raw_services_yaml_secret"}`
	payloadPath := writeTextFile(t, "validated-payload.json", payloadJSON)
	projectClient := &fakeProjectCatalogClient{repository: repositoryFixture()}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{bootstrapSignal: mergeSignalFixture("bootstrap")},
	}

	var output bytes.Buffer
	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:       scenarioPath,
		ProjectID:              "project-1",
		RepositoryID:           "repo-1",
		RepositoryFullName:     "codex-k8s/kodex-onboarding-test",
		AllowedProviderOwner:   "codex-k8s",
		RepositoryNamePrefix:   "kodex-onboarding-",
		RequestID:              "req-1",
		Kind:                   "bootstrap",
		CheckedPayloadFilePath: payloadPath,
		CheckedSourcePath:      "services.yaml",
		CheckedArtifactVersion: "",
		CheckedArtifactRef:     "",
		WatermarkJSONFilePath:  "",
		ProviderRepositoryID:   "provider-repo-1",
		Apply:                  true,
	}, clients, &output)
	if err != nil {
		t.Fatalf("run apply with produced checked artifact: %v", err)
	}

	request := projectClient.lastBootstrapRequest
	if request == nil {
		t.Fatal("expected bootstrap reconcile request")
	}
	checkedPolicy := request.GetCheckedPolicy()
	if got := checkedPolicy.GetValidatedPayloadJson(); !strings.Contains(got, "raw_services_yaml_secret") {
		t.Fatalf("checked payload was not passed to product API")
	}
	if got := checkedPolicy.GetArtifactVersion(); got != "bootstrap-merge-commit" {
		t.Fatalf("expected artifact version from merge commit, got %q", got)
	}
	if got := checkedPolicy.GetSourcePath(); got != "services.yaml" {
		t.Fatalf("expected services.yaml source path, got %q", got)
	}
	if got := checkedPolicy.GetArtifactRef(); !strings.HasPrefix(got, "checked://onboarding/bootstrap/") {
		t.Fatalf("expected generated checked artifact ref, got %q", got)
	}
	if got := checkedPolicy.GetArtifactDigest(); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("expected sha256 artifact digest, got %q", got)
	}
	if got := checkedPolicy.GetContentHash(); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("expected sha256 content hash, got %q", got)
	}
	if checkedPolicy.GetArtifactDigest() != checkedPolicy.GetContentHash() {
		t.Fatalf("expected artifact digest to match content hash, got digest=%q content_hash=%q", checkedPolicy.GetArtifactDigest(), checkedPolicy.GetContentHash())
	}
	if got, want := checkedPolicy.GetContentHash(), expectedContentHash(payloadJSON); got != want {
		t.Fatalf("expected content hash %q, got %q", want, got)
	}
	assertNotContains(t, output.String(), "raw_services_yaml_secret")
	assertContains(t, output.String(), "checked artifact input produced")
	assertContains(t, output.String(), "bootstrap reconcile completed")
}

func TestRunDryRunProducesAdoptionCheckedArtifactWithoutMutations(t *testing.T) {
	scenarioPath := writeScenario(t, signalOnlyScenario("adoption"))
	payloadPath := writeTextFile(t, "validated-payload.json", `{"services":[{"key":"api"}],"marker":"provider_payload_secret"}`)
	projectClient := &fakeProjectCatalogClient{repository: repositoryFixture()}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{adoptionSignal: mergeSignalFixture("adoption"), snapshotCount: 1},
	}

	var output bytes.Buffer
	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:       scenarioPath,
		ProjectID:              "project-1",
		RepositoryID:           "repo-1",
		RepositoryFullName:     "codex-k8s/kodex-onboarding-test",
		RequestID:              "req-1",
		Kind:                   "adoption",
		CheckedPayloadFilePath: payloadPath,
	}, clients, &output)
	if err != nil {
		t.Fatalf("run dry-run with produced checked artifact: %v", err)
	}

	if projectClient.bootstrapReconcileCalls != 0 || projectClient.adoptionReconcileCalls != 0 {
		t.Fatalf("dry-run called mutating reconcile methods: bootstrap=%d adoption=%d", projectClient.bootstrapReconcileCalls, projectClient.adoptionReconcileCalls)
	}
	assertNotContains(t, output.String(), "provider_payload_secret")
	assertContains(t, output.String(), "adoption checked artifact input produced")
	assertContains(t, output.String(), "apply disabled")
}

func TestRunDryRunPlansBootstrapSetupWithoutMutationsAndRedactsPreparedFiles(t *testing.T) {
	scenarioPath := writeScenario(t, bootstrapSetupFixture())
	projectClient := &fakeProjectCatalogClient{}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{},
	}

	var output bytes.Buffer
	err := run(context.Background(), runnerOptions{
		ScenarioFilePath: scenarioPath,
		ProjectID:        "project-1",
		RequestID:        "req-1",
		Kind:             "bootstrap",
	}, clients, &output)
	if err != nil {
		t.Fatalf("run dry-run bootstrap setup: %v", err)
	}

	if projectClient.createRepositoryCalls != 0 || projectClient.bootstrapPullRequestCalls != 0 {
		t.Fatalf("dry-run called mutating setup methods: create=%d bootstrap_pr=%d", projectClient.createRepositoryCalls, projectClient.bootstrapPullRequestCalls)
	}
	assertContains(t, output.String(), "bootstrap repository create ready")
	assertContains(t, output.String(), "bootstrap pull request ready")
	assertContains(t, output.String(), "apply disabled")
	assertNotContains(t, output.String(), "prepared_services_yaml_secret")
	assertNotContains(t, output.String(), "normalized_payload_secret")
}

func TestRunApplyCreatesRepositoryAndBootstrapPullRequestThroughProductAPIs(t *testing.T) {
	scenarioPath := writeScenario(t, bootstrapSetupFixture())
	projectClient := &fakeProjectCatalogClient{createRepositoryResponse: repositoryCreateResponseFixture()}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{},
	}

	var output bytes.Buffer
	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:     scenarioPath,
		ProjectID:            "project-1",
		AllowedProviderOwner: "codex-k8s",
		RepositoryNamePrefix: "kodex-onboarding-",
		RequestID:            "req-1",
		Kind:                 "bootstrap",
		Apply:                true,
	}, clients, &output)
	if err != nil {
		t.Fatalf("run apply bootstrap setup: %v", err)
	}

	if projectClient.createRepositoryCalls != 1 {
		t.Fatalf("expected one create repository call, got %d", projectClient.createRepositoryCalls)
	}
	if projectClient.bootstrapPullRequestCalls != 1 {
		t.Fatalf("expected one bootstrap PR call, got %d", projectClient.bootstrapPullRequestCalls)
	}
	if projectClient.bootstrapReconcileCalls != 0 {
		t.Fatalf("expected no reconcile before merge signal, got %d", projectClient.bootstrapReconcileCalls)
	}
	createRequest := projectClient.lastCreateRepositoryRequest
	if createRequest == nil {
		t.Fatal("expected create repository request")
	}
	if got := createRequest.GetProviderOwner() + "/" + createRequest.GetProviderName(); got != "codex-k8s/kodex-onboarding-live" {
		t.Fatalf("unexpected create target %q", got)
	}
	if got := createRequest.GetExternalAccountId(); got != "external-account-1" {
		t.Fatalf("unexpected external account %q", got)
	}
	prRequest := projectClient.lastBootstrapPullRequestRequest
	if prRequest == nil {
		t.Fatal("expected bootstrap pull request request")
	}
	if got := prRequest.GetRepositoryId(); got != "repo-1" {
		t.Fatalf("expected repository id from create response, got %q", got)
	}
	if got := prRequest.GetServicesPolicy().GetValidatedPayloadJson(); !strings.Contains(got, "normalized_payload_secret") {
		t.Fatalf("expected checked services policy payload to reach product API")
	}
	if got := prRequest.GetFiles()[0].GetContent(); !strings.Contains(got, "prepared_services_yaml_secret") {
		t.Fatalf("expected prepared file content to reach product API")
	}
	assertContains(t, output.String(), "bootstrap repository create completed")
	assertContains(t, output.String(), "bootstrap pull request completed")
	assertContains(t, output.String(), "bootstrap reconcile skipped")
	assertNotContains(t, output.String(), "prepared_services_yaml_secret")
	assertNotContains(t, output.String(), "normalized_payload_secret")
}

func TestRunApplyRejectsUnsafeBootstrapSetupTarget(t *testing.T) {
	scenario := bootstrapSetupFixture()
	scenario.BootstrapSetup.CreateRepository.ProviderName = "production-repository"
	scenarioPath := writeScenario(t, scenario)
	projectClient := &fakeProjectCatalogClient{createRepositoryResponse: repositoryCreateResponseFixture()}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:     scenarioPath,
		ProjectID:            "project-1",
		AllowedProviderOwner: "codex-k8s",
		RepositoryNamePrefix: "kodex-onboarding-",
		RequestID:            "req-1",
		Kind:                 "bootstrap",
		Apply:                true,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected unsafe setup target error")
	}
	assertContains(t, err.Error(), "must start")
	if projectClient.createRepositoryCalls != 0 || projectClient.bootstrapPullRequestCalls != 0 {
		t.Fatalf("unsafe target must not mutate product APIs: create=%d bootstrap_pr=%d", projectClient.createRepositoryCalls, projectClient.bootstrapPullRequestCalls)
	}
}

func TestRunApplyRejectsBootstrapCreateRepositoryWhenBindingAlreadyKnown(t *testing.T) {
	scenario := bootstrapSetupFixture()
	scenario.RepositoryID = "repo-1"
	scenario.RepositoryFullName = "codex-k8s/kodex-onboarding-test"
	scenarioPath := writeScenario(t, scenario)
	projectClient := &fakeProjectCatalogClient{repository: repositoryFixture()}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:     scenarioPath,
		ProjectID:            "project-1",
		RepositoryID:         "repo-1",
		RepositoryFullName:   "codex-k8s/kodex-onboarding-test",
		AllowedProviderOwner: "codex-k8s",
		RepositoryNamePrefix: "kodex-onboarding-",
		RequestID:            "req-1",
		Kind:                 "bootstrap",
		Apply:                true,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected known binding create_repository guard error")
	}
	assertContains(t, err.Error(), "repository_id to be empty")
	if projectClient.createRepositoryCalls != 0 || projectClient.bootstrapPullRequestCalls != 0 {
		t.Fatalf("known binding create_repository guard must not mutate product APIs: create=%d bootstrap_pr=%d", projectClient.createRepositoryCalls, projectClient.bootstrapPullRequestCalls)
	}
}

func TestRunApplyRejectsAuthenticatedUserBootstrapCreateRepository(t *testing.T) {
	scenario := bootstrapSetupFixture()
	scenario.BootstrapSetup.CreateRepository.OwnerKind = "authenticated_user"
	scenarioPath := writeScenario(t, scenario)
	projectClient := &fakeProjectCatalogClient{createRepositoryResponse: repositoryCreateResponseFixture()}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:     scenarioPath,
		ProjectID:            "project-1",
		AllowedProviderOwner: "codex-k8s",
		RepositoryNamePrefix: "kodex-onboarding-",
		RequestID:            "req-1",
		Kind:                 "bootstrap",
		Apply:                true,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected authenticated_user owner guard error")
	}
	assertContains(t, err.Error(), "owner_kind organization")
	if projectClient.createRepositoryCalls != 0 || projectClient.bootstrapPullRequestCalls != 0 {
		t.Fatalf("authenticated_user guard must not mutate product APIs: create=%d bootstrap_pr=%d", projectClient.createRepositoryCalls, projectClient.bootstrapPullRequestCalls)
	}
}

func TestRunDryRunPlansSelfRepositoryBindingAndChangeWithoutMutations(t *testing.T) {
	scenarioPath := writeScenario(t, selfRepositoryScenario())
	projectClient := &fakeProjectCatalogClient{}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{},
	}

	var output bytes.Buffer
	err := run(context.Background(), runnerOptions{
		ScenarioFilePath: scenarioPath,
		ProjectID:        "project-1",
		RequestID:        "req-1",
		Kind:             "adoption",
	}, clients, &output)
	if err != nil {
		t.Fatalf("run dry-run self repository: %v", err)
	}

	if projectClient.attachRepositoryCalls != 0 ||
		projectClient.createRepositoryCalls != 0 ||
		projectClient.bootstrapPullRequestCalls != 0 ||
		projectClient.adoptionReconcileCalls != 0 {
		t.Fatalf("dry-run mutated product APIs: attach=%d create=%d bootstrap_pr=%d adoption=%d",
			projectClient.attachRepositoryCalls,
			projectClient.createRepositoryCalls,
			projectClient.bootstrapPullRequestCalls,
			projectClient.adoptionReconcileCalls,
		)
	}
	text := output.String()
	assertContains(t, text, "repository binding attach ready")
	assertContains(t, text, "target=codex-k8s/kodex")
	assertContains(t, text, "repository change ready")
	assertContains(t, text, "services_policy_changed=true")
	assertContains(t, text, "deploy_relevant_changed=true")
	assertContains(t, text, "categories=services_policy:1,deploy_relevant:1,documentation:1")
	assertContains(t, text, "live repository change signal is not available")
	assertContains(t, text, "apply disabled")
	assertNotContains(t, text, "raw_services_yaml_secret")
}

func TestRunApplyAttachesSelfRepositoryAndReconcilesAdoption(t *testing.T) {
	scenarioPath := writeScenario(t, selfRepositoryScenario())
	projectClient := &fakeProjectCatalogClient{attachRepositoryResponse: selfRepositoryFixture()}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub: &fakeProviderHubClient{
			adoptionSignal: selfRepositoryMergeSignalFixture(),
			changeSignal:   selfRepositoryChangeSignalFixture(),
			snapshotCount:  1,
		},
		AccessManager: &fakeAccessManagerClient{},
	}

	var output bytes.Buffer
	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:          scenarioPath,
		ProjectID:                 "project-1",
		AllowedProviderOwner:      "codex-k8s",
		AllowedProviderRepository: "kodex",
		RequestID:                 "req-1",
		Kind:                      "adoption",
		Apply:                     true,
	}, clients, &output)
	if err != nil {
		t.Fatalf("run apply self repository: %v", err)
	}

	if projectClient.attachRepositoryCalls != 1 {
		t.Fatalf("expected one attach repository call, got %d", projectClient.attachRepositoryCalls)
	}
	if projectClient.adoptionReconcileCalls != 1 {
		t.Fatalf("expected one adoption reconcile call, got %d", projectClient.adoptionReconcileCalls)
	}
	attachRequest := projectClient.lastAttachRepositoryRequest
	if attachRequest == nil {
		t.Fatal("expected attach repository request")
	}
	if attachRequest.GetProviderOwner() != "codex-k8s" ||
		attachRequest.GetProviderName() != "kodex" ||
		attachRequest.GetDefaultBranch() != "main" ||
		attachRequest.GetProviderRepositoryId() != "provider-repo-self" {
		t.Fatalf("unexpected attach request target: %+v", attachRequest)
	}
	reconcileRequest := projectClient.lastAdoptionRequest
	if reconcileRequest == nil {
		t.Fatal("expected adoption reconcile request")
	}
	if reconcileRequest.GetRepositoryId() != "repo-1" ||
		reconcileRequest.GetMergeSignal().GetProviderTarget().GetRepositoryFullName() != "codex-k8s/kodex" ||
		reconcileRequest.GetCheckedPolicy().GetSourcePath() != "services.yaml" {
		t.Fatalf("unexpected adoption reconcile request: %+v", reconcileRequest)
	}
	text := output.String()
	assertContains(t, text, "repository binding attached")
	assertContains(t, text, "adoption reconcile completed")
	assertNotContains(t, text, "raw_services_yaml_secret")
}

func TestRunApplyCreatesProjectAndAttachesSelfRepositoryWhenProjectIDMissing(t *testing.T) {
	scenarioPath := writeScenario(t, selfRepositoryBindingOnlyScenario())
	project := projectFixture("11111111-1111-1111-1111-111111111111", "kodex", "project-self")
	repository := selfRepositoryFixture()
	repository.ProjectId = "project-self"
	repository.RepositoryId = "repo-self"
	projectClient := &fakeProjectCatalogClient{
		createProjectResponse:    project,
		attachRepositoryResponse: repository,
	}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{},
		AccessManager:  &fakeAccessManagerClient{},
	}

	var output bytes.Buffer
	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:          scenarioPath,
		OrganizationID:            "11111111-1111-1111-1111-111111111111",
		ProjectSlug:               "kodex",
		ProjectDisplayName:        "kodex",
		AllowedProviderOwner:      "codex-k8s",
		AllowedProviderRepository: "kodex",
		RequestID:                 "req-1",
		Kind:                      "adoption",
		Apply:                     true,
	}, clients, &output)
	if err != nil {
		t.Fatalf("run apply project bootstrap: %v", err)
	}

	if projectClient.createProjectCalls != 1 || projectClient.attachRepositoryCalls != 1 {
		t.Fatalf("expected create project and attach repository calls, got create=%d attach=%d", projectClient.createProjectCalls, projectClient.attachRepositoryCalls)
	}
	if projectClient.adoptionReconcileCalls != 0 {
		t.Fatalf("expected no adoption reconcile without checked input, got %d", projectClient.adoptionReconcileCalls)
	}
	if got := projectClient.lastCreateProjectRequest.GetSlug(); got != "kodex" {
		t.Fatalf("project slug = %q, want kodex", got)
	}
	if got := projectClient.lastAttachRepositoryRequest.GetProjectId(); got != "project-self" {
		t.Fatalf("attach project_id = %q, want project-self", got)
	}
	text := output.String()
	assertContains(t, text, "project created")
	assertContains(t, text, "repository binding attached")
	assertContains(t, text, "project_id=project-self")
	assertContains(t, text, "repository_id=repo-self")
}

func TestRunApplyReusesExistingProjectAndRepositoryBinding(t *testing.T) {
	scenarioPath := writeScenario(t, selfRepositoryBindingOnlyScenario())
	project := projectFixture("11111111-1111-1111-1111-111111111111", "kodex", "project-self")
	repository := selfRepositoryFixture()
	repository.ProjectId = "project-self"
	repository.RepositoryId = "repo-self"
	projectClient := &fakeProjectCatalogClient{
		projects:     []*projectsv1.Project{project},
		repositories: []*projectsv1.Repository{repository},
	}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{},
		AccessManager:  &fakeAccessManagerClient{},
	}

	var output bytes.Buffer
	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:          scenarioPath,
		OrganizationID:            "11111111-1111-1111-1111-111111111111",
		ProjectSlug:               "kodex",
		ProjectDisplayName:        "kodex",
		AllowedProviderOwner:      "codex-k8s",
		AllowedProviderRepository: "kodex",
		RequestID:                 "req-1",
		Kind:                      "adoption",
		Apply:                     true,
	}, clients, &output)
	if err != nil {
		t.Fatalf("run apply existing project/repository: %v", err)
	}

	if projectClient.createProjectCalls != 0 || projectClient.attachRepositoryCalls != 0 {
		t.Fatalf("expected no duplicate project/repository writes, got create=%d attach=%d", projectClient.createProjectCalls, projectClient.attachRepositoryCalls)
	}
	if projectClient.listProjectsCalls != 1 || projectClient.listRepositoriesCalls != 1 {
		t.Fatalf("expected project/repository reads, got projects=%d repositories=%d", projectClient.listProjectsCalls, projectClient.listRepositoriesCalls)
	}
	text := output.String()
	assertContains(t, text, "project ready project_id=project-self")
	assertContains(t, text, "repository binding ready repository_id=repo-self")
	assertContains(t, text, "project_id=project-self")
}

func TestRunApplyCreatesSelfDeployServiceAccessRules(t *testing.T) {
	scenarioPath := writeScenario(t, selfRepositoryBindingOnlyScenario())
	project := projectFixture("11111111-1111-1111-1111-111111111111", "kodex", "project-self")
	repository := selfRepositoryFixture()
	repository.ProjectId = "project-self"
	repository.RepositoryId = "repo-self"
	accessClient := &fakeAccessManagerClient{}
	clients := runnerClients{
		ProjectCatalog: &fakeProjectCatalogClient{
			projects:     []*projectsv1.Project{project},
			repositories: []*projectsv1.Repository{repository},
		},
		ProviderHub:   &fakeProviderHubClient{},
		AccessManager: accessClient,
	}

	var output bytes.Buffer
	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:          scenarioPath,
		OrganizationID:            "11111111-1111-1111-1111-111111111111",
		ProjectSlug:               "kodex",
		ProjectDisplayName:        "kodex",
		AllowedProviderOwner:      "codex-k8s",
		AllowedProviderRepository: "kodex",
		RequestID:                 "req-1",
		Kind:                      "adoption",
		Apply:                     true,
	}, clients, &output)
	if err != nil {
		t.Fatalf("run apply service access rules: %v", err)
	}

	if accessClient.createdRuleCount != 2 {
		t.Fatalf("expected two created access rules, got %d", accessClient.createdRuleCount)
	}
	if accessClient.putAccessRuleCalls != 2 || accessClient.checkAccessCalls != 2 {
		t.Fatalf("expected put/check calls for two services, got put=%d check=%d", accessClient.putAccessRuleCalls, accessClient.checkAccessCalls)
	}
	assertSelfDeployAccessRule(t, accessClient.lastPutAccessRuleBySubject["agent-manager"], "project-self")
	assertSelfDeployAccessCheck(t, accessClient.lastCheckAccessBySubject["agent-manager"], "project-self")
	assertSelfDeployAccessRule(t, accessClient.lastPutAccessRuleBySubject["staff-gateway"], "project-self")
	text := output.String()
	assertContains(t, text, "self-deploy service access ready subject=service/agent-manager")
	assertContains(t, text, "self-deploy service access ready subject=service/staff-gateway")
}

func TestRunApplyRequiresSelfDeployAccessManagerBeforeProjectMutation(t *testing.T) {
	scenarioPath := writeScenario(t, selfRepositoryBindingOnlyScenario())
	projectClient := &fakeProjectCatalogClient{}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:          scenarioPath,
		OrganizationID:            "11111111-1111-1111-1111-111111111111",
		ProjectSlug:               "kodex",
		ProjectDisplayName:        "kodex",
		AllowedProviderOwner:      "codex-k8s",
		AllowedProviderRepository: "kodex",
		RequestID:                 "req-1",
		Kind:                      "adoption",
		Apply:                     true,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected self-deploy service access dependency error")
	}
	assertContains(t, err.Error(), "access-manager client is required for self-deploy service access apply")
	if projectClient.createProjectCalls != 0 || projectClient.attachRepositoryCalls != 0 {
		t.Fatalf("self-deploy access preflight mutated project API: create=%d attach=%d", projectClient.createProjectCalls, projectClient.attachRepositoryCalls)
	}
}

func TestRunApplyReusesSelfDeployServiceAccessRules(t *testing.T) {
	scenarioPath := writeScenario(t, selfRepositoryBindingOnlyScenario())
	project := projectFixture("11111111-1111-1111-1111-111111111111", "kodex", "project-self")
	repository := selfRepositoryFixture()
	repository.ProjectId = "project-self"
	repository.RepositoryId = "repo-self"
	accessClient := &fakeAccessManagerClient{}
	clients := runnerClients{
		ProjectCatalog: &fakeProjectCatalogClient{
			projects:     []*projectsv1.Project{project},
			repositories: []*projectsv1.Repository{repository},
		},
		ProviderHub:   &fakeProviderHubClient{},
		AccessManager: accessClient,
	}
	options := runnerOptions{
		ScenarioFilePath:          scenarioPath,
		OrganizationID:            "11111111-1111-1111-1111-111111111111",
		ProjectSlug:               "kodex",
		ProjectDisplayName:        "kodex",
		AllowedProviderOwner:      "codex-k8s",
		AllowedProviderRepository: "kodex",
		RequestID:                 "req-1",
		Kind:                      "adoption",
		Apply:                     true,
	}
	if err := run(context.Background(), options, clients, ioDiscard{}); err != nil {
		t.Fatalf("first run apply service access rules: %v", err)
	}
	if err := run(context.Background(), options, clients, ioDiscard{}); err != nil {
		t.Fatalf("second run apply service access rules: %v", err)
	}

	if accessClient.createdRuleCount != 2 {
		t.Fatalf("expected second run to reuse two access rules, created=%d", accessClient.createdRuleCount)
	}
	if accessClient.putAccessRuleCalls != 4 || accessClient.checkAccessCalls != 4 {
		t.Fatalf("expected repeated put/check calls without duplicates, got put=%d check=%d", accessClient.putAccessRuleCalls, accessClient.checkAccessCalls)
	}
}

func TestRunApplyReportsSelfDeployServiceAccessConflict(t *testing.T) {
	scenarioPath := writeScenario(t, selfRepositoryBindingOnlyScenario())
	project := projectFixture("11111111-1111-1111-1111-111111111111", "kodex", "project-self")
	repository := selfRepositoryFixture()
	repository.ProjectId = "project-self"
	repository.RepositoryId = "repo-self"
	accessClient := &fakeAccessManagerClient{denySubjects: map[string]string{"agent-manager": "explicit_deny"}}
	clients := runnerClients{
		ProjectCatalog: &fakeProjectCatalogClient{
			projects:     []*projectsv1.Project{project},
			repositories: []*projectsv1.Repository{repository},
		},
		ProviderHub:   &fakeProviderHubClient{},
		AccessManager: accessClient,
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:          scenarioPath,
		OrganizationID:            "11111111-1111-1111-1111-111111111111",
		ProjectSlug:               "kodex",
		ProjectDisplayName:        "kodex",
		AllowedProviderOwner:      "codex-k8s",
		AllowedProviderRepository: "kodex",
		RequestID:                 "req-1",
		Kind:                      "adoption",
		Apply:                     true,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected self-deploy service access conflict")
	}
	assertContains(t, err.Error(), "self-deploy access check denied")
	assertContains(t, err.Error(), "explicit_deny")
	assertNotContains(t, err.Error(), "token=")
	assertNotContains(t, err.Error(), "webhook_body")
}

func TestRunApplyRedactsSelfDeployServiceAccessDependencyError(t *testing.T) {
	scenarioPath := writeScenario(t, selfRepositoryBindingOnlyScenario())
	project := projectFixture("11111111-1111-1111-1111-111111111111", "kodex", "project-self")
	repository := selfRepositoryFixture()
	repository.ProjectId = "project-self"
	repository.RepositoryId = "repo-self"
	clients := runnerClients{
		ProjectCatalog: &fakeProjectCatalogClient{
			projects:     []*projectsv1.Project{project},
			repositories: []*projectsv1.Repository{repository},
		},
		ProviderHub: &fakeProviderHubClient{},
		AccessManager: &fakeAccessManagerClient{
			putAccessRuleErr: errors.New("token=secret-token https://private.example.internal webhook_body=raw-body"),
		},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:          scenarioPath,
		OrganizationID:            "11111111-1111-1111-1111-111111111111",
		ProjectSlug:               "kodex",
		ProjectDisplayName:        "kodex",
		AllowedProviderOwner:      "codex-k8s",
		AllowedProviderRepository: "kodex",
		RequestID:                 "req-1",
		Kind:                      "adoption",
		Apply:                     true,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected access-manager dependency error")
	}
	assertNotContains(t, err.Error(), "secret-token")
	assertNotContains(t, err.Error(), "private.example")
	assertNotContains(t, err.Error(), "raw-body")
	assertContains(t, err.Error(), "[hidden]")
}

func TestRunApplyReusesRepositoryBindingAcrossListRepositoryPages(t *testing.T) {
	scenarioPath := writeScenario(t, selfRepositoryBindingOnlyScenario())
	project := projectFixture("11111111-1111-1111-1111-111111111111", "kodex", "project-self")
	otherRepository := selfRepositoryFixture()
	otherRepository.RepositoryId = "repo-other"
	otherRepository.ProviderName = "other"
	repository := selfRepositoryFixture()
	repository.ProjectId = "project-self"
	repository.RepositoryId = "repo-self"
	projectClient := &fakeProjectCatalogClient{
		projects:        []*projectsv1.Project{project},
		repositoryPages: [][]*projectsv1.Repository{{otherRepository}, {repository}},
		repositories:    []*projectsv1.Repository{},
	}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{},
		AccessManager:  &fakeAccessManagerClient{},
	}

	var output bytes.Buffer
	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:          scenarioPath,
		OrganizationID:            "11111111-1111-1111-1111-111111111111",
		ProjectSlug:               "kodex",
		ProjectDisplayName:        "kodex",
		AllowedProviderOwner:      "codex-k8s",
		AllowedProviderRepository: "kodex",
		RequestID:                 "req-1",
		Kind:                      "adoption",
		Apply:                     true,
	}, clients, &output)
	if err != nil {
		t.Fatalf("run apply existing repository across pages: %v", err)
	}

	if projectClient.listRepositoriesCalls != 2 {
		t.Fatalf("expected two ListRepositories calls, got %d", projectClient.listRepositoriesCalls)
	}
	if projectClient.attachRepositoryCalls != 0 {
		t.Fatalf("expected no duplicate repository attach, got %d", projectClient.attachRepositoryCalls)
	}
	if got := projectClient.lastListRepositoriesRequest.GetPage().GetPageToken(); got != "1" {
		t.Fatalf("last repository page token = %q, want 1", got)
	}
	text := output.String()
	assertContains(t, text, "repository binding ready repository_id=repo-self")
	assertContains(t, text, "project_id=project-self")
}

func TestRunDryRunPlansProjectAndRepositoryBootstrapWithoutProjectID(t *testing.T) {
	clients := runnerClients{
		ProjectCatalog: &fakeProjectCatalogClient{},
		ProviderHub:    &fakeProviderHubClient{},
	}

	var output bytes.Buffer
	err := run(context.Background(), runnerOptions{
		OrganizationID:            "11111111-1111-1111-1111-111111111111",
		ProjectSlug:               "kodex",
		ProjectDisplayName:        "kodex",
		ProviderOwner:             "codex-k8s",
		ProviderName:              "kodex",
		ProviderRepositoryID:      "provider-repo-self",
		AllowedProviderOwner:      "codex-k8s",
		AllowedProviderRepository: "kodex",
		RequestID:                 "req-1",
		Kind:                      "adoption",
	}, clients, &output)
	if err != nil {
		t.Fatalf("run dry-run project bootstrap: %v", err)
	}

	projectClient := clients.ProjectCatalog.(*fakeProjectCatalogClient)
	if projectClient.createProjectCalls != 0 || projectClient.attachRepositoryCalls != 0 {
		t.Fatalf("dry-run mutated project API: create=%d attach=%d", projectClient.createProjectCalls, projectClient.attachRepositoryCalls)
	}
	text := output.String()
	assertContains(t, text, "project create ready")
	assertContains(t, text, "repository binding attach ready")
	assertContains(t, text, "apply disabled")
}

func TestRunApplyRejectsUnsafeSelfRepositoryTargetBeforeAttach(t *testing.T) {
	scenario := selfRepositoryScenario()
	scenario.RepositoryBinding.ProviderName = "production"
	scenario.RepositoryFullName = "codex-k8s/production"
	scenarioPath := writeScenario(t, scenario)
	projectClient := &fakeProjectCatalogClient{attachRepositoryResponse: selfRepositoryFixture()}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:          scenarioPath,
		ProjectID:                 "project-1",
		AllowedProviderOwner:      "codex-k8s",
		AllowedProviderRepository: "kodex",
		RequestID:                 "req-1",
		Kind:                      "adoption",
		Apply:                     true,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected unsafe self repository target error")
	}
	assertContains(t, err.Error(), "is not allowed")
	if projectClient.attachRepositoryCalls != 0 || projectClient.adoptionReconcileCalls != 0 {
		t.Fatalf("unsafe target must not mutate product APIs: attach=%d adoption=%d", projectClient.attachRepositoryCalls, projectClient.adoptionReconcileCalls)
	}
}

func TestRunApplyRejectsSelfRepositoryBindingPrefixOnlyPolicyBeforeAttach(t *testing.T) {
	scenarioPath := writeScenario(t, selfRepositoryScenario())
	projectClient := &fakeProjectCatalogClient{attachRepositoryResponse: selfRepositoryFixture()}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:     scenarioPath,
		ProjectID:            "project-1",
		AllowedProviderOwner: "codex-k8s",
		RepositoryNamePrefix: "kodex",
		RequestID:            "req-1",
		Kind:                 "adoption",
		Apply:                true,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected prefix-only repository_binding guard error")
	}
	assertContains(t, err.Error(), "repository_binding apply requires allowed provider repository")
	if projectClient.attachRepositoryCalls != 0 || projectClient.adoptionReconcileCalls != 0 {
		t.Fatalf("prefix-only repository_binding guard must not mutate product APIs: attach=%d adoption=%d", projectClient.attachRepositoryCalls, projectClient.adoptionReconcileCalls)
	}
}

func TestRunRejectsUnsafeRepositoryChangePath(t *testing.T) {
	scenario := selfRepositoryScenario()
	scenario.RepositoryChange.ChangedPaths[0].Path = "../services.yaml"
	scenarioPath := writeScenario(t, scenario)
	clients := runnerClients{
		ProjectCatalog: &fakeProjectCatalogClient{},
		ProviderHub:    &fakeProviderHubClient{},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath: scenarioPath,
		ProjectID:        "project-1",
		RequestID:        "req-1",
		Kind:             "adoption",
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected unsafe repository change path error")
	}
	assertContains(t, err.Error(), "unsafe")
}

func TestRunRejectsCheckedPayloadProducerForBothKinds(t *testing.T) {
	payloadPath := writeTextFile(t, "validated-payload.json", `{"services":[]}`)
	clients := runnerClients{
		ProjectCatalog: &fakeProjectCatalogClient{repository: repositoryFixture()},
		ProviderHub:    &fakeProviderHubClient{},
	}

	err := run(context.Background(), runnerOptions{
		ProjectID:              "project-1",
		RepositoryID:           "repo-1",
		RepositoryFullName:     "codex-k8s/kodex-onboarding-test",
		RequestID:              "req-1",
		Kind:                   "both",
		CheckedPayloadFilePath: payloadPath,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected producer kind error")
	}
	assertContains(t, err.Error(), "requires kind bootstrap or adoption")
}

func TestRunRejectsRawYAMLCheckedPayloadProducerInput(t *testing.T) {
	payloadPath := writeTextFile(t, "services.yaml", "services:\n  - key: api\n")
	clients := runnerClients{
		ProjectCatalog: &fakeProjectCatalogClient{repository: repositoryFixture()},
		ProviderHub:    &fakeProviderHubClient{},
	}

	err := run(context.Background(), runnerOptions{
		ProjectID:              "project-1",
		RepositoryID:           "repo-1",
		RepositoryFullName:     "codex-k8s/kodex-onboarding-test",
		RequestID:              "req-1",
		Kind:                   "bootstrap",
		CheckedPayloadFilePath: payloadPath,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected invalid checked payload error")
	}
	assertContains(t, err.Error(), "must contain valid JSON")
}

func TestRunRejectsCheckedPayloadProducerWithoutWatermark(t *testing.T) {
	scenario := signalOnlyScenario("bootstrap")
	scenario.Bootstrap.WatermarkJSON = ""
	scenarioPath := writeScenario(t, scenario)
	payloadPath := writeTextFile(t, "validated-payload.json", `{"services":[]}`)
	clients := runnerClients{
		ProjectCatalog: &fakeProjectCatalogClient{repository: repositoryFixture()},
		ProviderHub:    &fakeProviderHubClient{bootstrapSignal: mergeSignalFixture("bootstrap")},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:       scenarioPath,
		ProjectID:              "project-1",
		RepositoryID:           "repo-1",
		RepositoryFullName:     "codex-k8s/kodex-onboarding-test",
		RequestID:              "req-1",
		Kind:                   "bootstrap",
		CheckedPayloadFilePath: payloadPath,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected missing watermark error")
	}
	assertContains(t, err.Error(), "requires watermark_json")
}

func TestRunApplyUsesRepositoryBindingForTargetPolicy(t *testing.T) {
	scenario := checkedPayloadFixture()
	scenario.RepositoryFullName = ""
	scenario.ProviderRepositoryID = ""
	scenarioPath := writeScenario(t, scenario)
	projectClient := &fakeProjectCatalogClient{repository: repositoryFixture()}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{bootstrapSignal: mergeSignalFixture("bootstrap")},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:     scenarioPath,
		ProjectID:            "project-1",
		RepositoryID:         "repo-1",
		AllowedProviderOwner: "codex-k8s",
		RepositoryNamePrefix: "kodex-onboarding-",
		RequestID:            "req-1",
		Kind:                 "bootstrap",
		Apply:                true,
	}, clients, ioDiscard{})
	if err != nil {
		t.Fatalf("run apply with binding target: %v", err)
	}
	if projectClient.bootstrapReconcileCalls != 1 {
		t.Fatalf("expected one bootstrap reconcile call, got %d", projectClient.bootstrapReconcileCalls)
	}
}

func TestRunApplyRejectsUnsafeRepositoryPrefix(t *testing.T) {
	scenarioPath := writeScenario(t, checkedPayloadFixture())
	repository := repositoryFixture()
	repository.ProviderName = "production-repo"
	clients := runnerClients{
		ProjectCatalog: &fakeProjectCatalogClient{repository: repository},
		ProviderHub:    &fakeProviderHubClient{bootstrapSignal: mergeSignalFixture("bootstrap")},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:     scenarioPath,
		ProjectID:            "project-1",
		RepositoryID:         "repo-1",
		RepositoryFullName:   "codex-k8s/production-repo",
		AllowedProviderOwner: "codex-k8s",
		RepositoryNamePrefix: "kodex-onboarding-",
		RequestID:            "req-1",
		Kind:                 "bootstrap",
		Apply:                true,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected unsafe repository prefix error")
	}
	assertContains(t, err.Error(), "must start")
}

func TestRunRejectsRepositoryBindingMismatch(t *testing.T) {
	scenarioPath := writeScenario(t, checkedPayloadFixture())
	repository := repositoryFixture()
	repository.ProviderName = "other-repository"
	projectClient := &fakeProjectCatalogClient{repository: repository}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{bootstrapSignal: mergeSignalFixture("bootstrap")},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:     scenarioPath,
		ProjectID:            "project-1",
		RepositoryID:         "repo-1",
		RepositoryFullName:   "codex-k8s/kodex-onboarding-test",
		AllowedProviderOwner: "codex-k8s",
		RepositoryNamePrefix: "kodex-onboarding-",
		RequestID:            "req-1",
		Kind:                 "bootstrap",
		Apply:                true,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected repository binding mismatch")
	}
	assertContains(t, err.Error(), "repository_full_name mismatch")
	if projectClient.bootstrapReconcileCalls != 0 {
		t.Fatalf("reconcile must not run after binding mismatch, got %d calls", projectClient.bootstrapReconcileCalls)
	}
}

func TestRunRejectsSignalTargetMismatchBySignalKey(t *testing.T) {
	scenario := checkedPayloadFixture()
	scenario.Bootstrap.SignalKey = "bootstrap-signal-key"
	scenarioPath := writeScenario(t, scenario)
	signal := mergeSignalFixture("bootstrap")
	signal.RepositoryId = "other-repo"
	projectClient := &fakeProjectCatalogClient{repository: repositoryFixture()}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{bootstrapSignal: signal},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:     scenarioPath,
		ProjectID:            "project-1",
		RepositoryID:         "repo-1",
		RepositoryFullName:   "codex-k8s/kodex-onboarding-test",
		AllowedProviderOwner: "codex-k8s",
		RepositoryNamePrefix: "kodex-onboarding-",
		RequestID:            "req-1",
		Kind:                 "bootstrap",
		Apply:                true,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected signal target mismatch")
	}
	assertContains(t, err.Error(), "repository_id mismatch")
	if projectClient.bootstrapReconcileCalls != 0 {
		t.Fatalf("reconcile must not run after signal mismatch, got %d calls", projectClient.bootstrapReconcileCalls)
	}
}

func TestRunRejectsUnmergedSignal(t *testing.T) {
	scenarioPath := writeScenario(t, checkedPayloadFixture())
	signal := mergeSignalFixture("bootstrap")
	signal.Status = providersv1.RepositoryMergeSignalStatus_REPOSITORY_MERGE_SIGNAL_STATUS_UNSPECIFIED
	projectClient := &fakeProjectCatalogClient{repository: repositoryFixture()}
	clients := runnerClients{
		ProjectCatalog: projectClient,
		ProviderHub:    &fakeProviderHubClient{bootstrapSignal: signal},
	}

	err := run(context.Background(), runnerOptions{
		ScenarioFilePath:     scenarioPath,
		ProjectID:            "project-1",
		RepositoryID:         "repo-1",
		RepositoryFullName:   "codex-k8s/kodex-onboarding-test",
		AllowedProviderOwner: "codex-k8s",
		RepositoryNamePrefix: "kodex-onboarding-",
		RequestID:            "req-1",
		Kind:                 "bootstrap",
		Apply:                true,
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected unmerged signal error")
	}
	assertContains(t, err.Error(), "not merged")
	if projectClient.bootstrapReconcileCalls != 0 {
		t.Fatalf("reconcile must not run for unmerged signal, got %d calls", projectClient.bootstrapReconcileCalls)
	}
}

func TestRunReturnsSafeDiagnosticsForDependencyErrors(t *testing.T) {
	clients := runnerClients{
		ProjectCatalog: &fakeProjectCatalogClient{
			getRepositoryErr: errors.New("token=secret-token https://private.example.internal payload=raw-provider-body"),
		},
		ProviderHub: &fakeProviderHubClient{},
	}

	err := run(context.Background(), runnerOptions{
		ProjectID:          "project-1",
		RepositoryID:       "repo-1",
		RepositoryFullName: "codex-k8s/kodex-onboarding-test",
		RequestID:          "req-1",
		Kind:               "bootstrap",
	}, clients, ioDiscard{})
	if err == nil {
		t.Fatal("expected dependency error")
	}
	assertNotContains(t, err.Error(), "secret-token")
	assertNotContains(t, err.Error(), "private.example")
	assertNotContains(t, err.Error(), "raw-provider-body")
	assertContains(t, err.Error(), "[hidden]")
}

func TestOutgoingAuthUnaryInterceptorAddsSharedTokenMetadata(t *testing.T) {
	interceptor := outgoingAuthUnaryInterceptor("shared-token", "onboarding-runner-test")
	err := interceptor(context.Background(), "/kodex.projects.v1.ProjectCatalogService/ListProjects", nil, nil, nil, func(ctx context.Context, _ string, _ any, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			t.Fatal("expected outgoing metadata")
		}
		if got := firstMetadata(md, "authorization"); got != "Bearer shared-token" {
			t.Fatalf("authorization metadata = %q, want bearer token", got)
		}
		if got := firstMetadata(md, "x-kodex-caller-type"); got != "service" {
			t.Fatalf("caller type metadata = %q, want service", got)
		}
		if got := firstMetadata(md, "x-kodex-caller-id"); got != "onboarding-runner-test" {
			t.Fatalf("caller id metadata = %q, want onboarding-runner-test", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
}

type fakeProjectCatalogClient struct {
	projects                        []*projectsv1.Project
	createProjectResponse           *projectsv1.Project
	repository                      *projectsv1.Repository
	repositories                    []*projectsv1.Repository
	repositoryPages                 [][]*projectsv1.Repository
	attachRepositoryResponse        *projectsv1.Repository
	createRepositoryResponse        *projectsv1.RepositoryProviderCreateResponse
	listProjectsErr                 error
	createProjectErr                error
	listRepositoriesErr             error
	getRepositoryErr                error
	attachRepositoryErr             error
	createRepositoryErr             error
	bootstrapPullRequestErr         error
	listProjectsCalls               int
	createProjectCalls              int
	listRepositoriesCalls           int
	attachRepositoryCalls           int
	bootstrapReconcileCalls         int
	adoptionReconcileCalls          int
	createRepositoryCalls           int
	bootstrapPullRequestCalls       int
	lastListProjectsRequest         *projectsv1.ListProjectsRequest
	lastCreateProjectRequest        *projectsv1.CreateProjectRequest
	lastListRepositoriesRequest     *projectsv1.ListRepositoriesRequest
	lastAttachRepositoryRequest     *projectsv1.AttachRepositoryRequest
	lastBootstrapRequest            *projectsv1.ReconcileBootstrapMergeSignalRequest
	lastAdoptionRequest             *projectsv1.ReconcileAdoptionMergeSignalRequest
	lastCreateRepositoryRequest     *projectsv1.CreateProviderRepositoryRequest
	lastBootstrapPullRequestRequest *projectsv1.CreateRepositoryBootstrapPullRequestRequest
}

func (f *fakeProjectCatalogClient) CreateProject(_ context.Context, request *projectsv1.CreateProjectRequest, _ ...grpc.CallOption) (*projectsv1.ProjectResponse, error) {
	f.createProjectCalls++
	f.lastCreateProjectRequest = request
	if f.createProjectErr != nil {
		return nil, f.createProjectErr
	}
	project := f.createProjectResponse
	if project == nil {
		project = projectFixture(request.GetOrganizationId(), request.GetSlug(), "project-1")
	}
	f.projects = append(f.projects, project)
	return &projectsv1.ProjectResponse{Project: project}, nil
}

func (f *fakeProjectCatalogClient) ListProjects(_ context.Context, request *projectsv1.ListProjectsRequest, _ ...grpc.CallOption) (*projectsv1.ListProjectsResponse, error) {
	f.listProjectsCalls++
	f.lastListProjectsRequest = request
	if f.listProjectsErr != nil {
		return nil, f.listProjectsErr
	}
	return &projectsv1.ListProjectsResponse{Projects: f.projects}, nil
}

func (f *fakeProjectCatalogClient) AttachRepository(_ context.Context, request *projectsv1.AttachRepositoryRequest, _ ...grpc.CallOption) (*projectsv1.RepositoryResponse, error) {
	f.attachRepositoryCalls++
	f.lastAttachRepositoryRequest = request
	if f.attachRepositoryErr != nil {
		return nil, f.attachRepositoryErr
	}
	repository := f.attachRepositoryResponse
	if repository == nil {
		repository = selfRepositoryFixture()
	}
	f.repository = repository
	return &projectsv1.RepositoryResponse{Repository: repository}, nil
}

func (f *fakeProjectCatalogClient) GetRepository(context.Context, *projectsv1.GetRepositoryRequest, ...grpc.CallOption) (*projectsv1.RepositoryResponse, error) {
	if f.getRepositoryErr != nil {
		return nil, f.getRepositoryErr
	}
	return &projectsv1.RepositoryResponse{Repository: f.repository}, nil
}

func (f *fakeProjectCatalogClient) ListRepositories(_ context.Context, request *projectsv1.ListRepositoriesRequest, _ ...grpc.CallOption) (*projectsv1.ListRepositoriesResponse, error) {
	f.listRepositoriesCalls++
	f.lastListRepositoriesRequest = request
	if f.listRepositoriesErr != nil {
		return nil, f.listRepositoriesErr
	}
	if len(f.repositoryPages) > 0 {
		pageIndex := 0
		if token := request.GetPage().GetPageToken(); token != "" {
			parsed, err := strconv.Atoi(token)
			if err != nil {
				return &projectsv1.ListRepositoriesResponse{Page: &projectsv1.PageResponse{}}, nil
			}
			pageIndex = parsed
		}
		if pageIndex < 0 || pageIndex >= len(f.repositoryPages) {
			return &projectsv1.ListRepositoriesResponse{Page: &projectsv1.PageResponse{}}, nil
		}
		nextPageToken := ""
		if pageIndex+1 < len(f.repositoryPages) {
			nextPageToken = strconv.Itoa(pageIndex + 1)
		}
		return &projectsv1.ListRepositoriesResponse{
			Repositories: f.repositoryPages[pageIndex],
			Page:         &projectsv1.PageResponse{NextPageToken: optionalString(nextPageToken)},
		}, nil
	}
	repositories := f.repositories
	if len(repositories) == 0 && f.repository != nil {
		repositories = []*projectsv1.Repository{f.repository}
	}
	return &projectsv1.ListRepositoriesResponse{Repositories: repositories, Page: &projectsv1.PageResponse{}}, nil
}

func (f *fakeProjectCatalogClient) CreateProviderRepository(_ context.Context, request *projectsv1.CreateProviderRepositoryRequest, _ ...grpc.CallOption) (*projectsv1.RepositoryProviderCreateResponse, error) {
	f.createRepositoryCalls++
	f.lastCreateRepositoryRequest = request
	if f.createRepositoryErr != nil {
		return nil, f.createRepositoryErr
	}
	if f.createRepositoryResponse != nil {
		f.repository = f.createRepositoryResponse.GetRepository()
		return f.createRepositoryResponse, nil
	}
	response := repositoryCreateResponseFixture()
	f.repository = response.GetRepository()
	return response, nil
}

func (f *fakeProjectCatalogClient) CreateRepositoryBootstrapPullRequest(_ context.Context, request *projectsv1.CreateRepositoryBootstrapPullRequestRequest, _ ...grpc.CallOption) (*projectsv1.RepositoryBootstrapPullRequestResponse, error) {
	f.bootstrapPullRequestCalls++
	f.lastBootstrapPullRequestRequest = request
	if f.bootstrapPullRequestErr != nil {
		return nil, f.bootstrapPullRequestErr
	}
	repository := f.repository
	if repository == nil {
		repository = repositoryFixture()
	}
	return &projectsv1.RepositoryBootstrapPullRequestResponse{
		Repository:                repository,
		BaseBranch:                request.GetBaseBranch(),
		BootstrapBranch:           request.GetBootstrapBranch(),
		ServicesPolicySourcePath:  request.GetServicesPolicy().GetSourcePath(),
		ServicesPolicyContentHash: request.GetServicesPolicy().GetContentHash(),
		ProviderOperationId:       stringPtr("provider-operation-bootstrap-pr"),
	}, nil
}

func (f *fakeProjectCatalogClient) ReconcileBootstrapMergeSignal(_ context.Context, request *projectsv1.ReconcileBootstrapMergeSignalRequest, _ ...grpc.CallOption) (*projectsv1.BootstrapServicesPolicyImportResponse, error) {
	f.bootstrapReconcileCalls++
	f.lastBootstrapRequest = request
	return &projectsv1.BootstrapServicesPolicyImportResponse{
		SourceRef:       request.GetMergeSignal().GetSourceRef(),
		SourceCommitSha: request.GetMergeSignal().GetMergeCommitSha(),
		Summary:         "bootstrap imported",
	}, nil
}

func (f *fakeProjectCatalogClient) ReconcileAdoptionMergeSignal(_ context.Context, request *projectsv1.ReconcileAdoptionMergeSignalRequest, _ ...grpc.CallOption) (*projectsv1.BootstrapServicesPolicyImportResponse, error) {
	f.adoptionReconcileCalls++
	f.lastAdoptionRequest = request
	return &projectsv1.BootstrapServicesPolicyImportResponse{
		SourceRef:       request.GetMergeSignal().GetSourceRef(),
		SourceCommitSha: request.GetMergeSignal().GetMergeCommitSha(),
		Summary:         "adoption imported",
	}, nil
}

type fakeAccessManagerClient struct {
	rules                      map[string]*accessaccountsv1.AccessRuleResponse
	denySubjects               map[string]string
	putAccessRuleErr           error
	checkAccessErr             error
	putAccessRuleCalls         int
	checkAccessCalls           int
	createdRuleCount           int
	lastPutAccessRuleBySubject map[string]*accessaccountsv1.PutAccessRuleRequest
	lastCheckAccessBySubject   map[string]*accessaccountsv1.CheckAccessRequest
}

func (f *fakeAccessManagerClient) PutAccessRule(_ context.Context, request *accessaccountsv1.PutAccessRuleRequest, _ ...grpc.CallOption) (*accessaccountsv1.AccessRuleResponse, error) {
	f.putAccessRuleCalls++
	if f.lastPutAccessRuleBySubject == nil {
		f.lastPutAccessRuleBySubject = map[string]*accessaccountsv1.PutAccessRuleRequest{}
	}
	f.lastPutAccessRuleBySubject[request.GetSubjectId()] = request
	if f.putAccessRuleErr != nil {
		return nil, f.putAccessRuleErr
	}
	if f.rules == nil {
		f.rules = map[string]*accessaccountsv1.AccessRuleResponse{}
	}
	key := accessRuleIdentityKey(request)
	if existing := f.rules[key]; existing != nil {
		return existing, nil
	}
	f.createdRuleCount++
	response := &accessaccountsv1.AccessRuleResponse{
		AccessRuleId: "rule-" + request.GetSubjectId(),
		Effect:       request.GetEffect(),
		SubjectType:  request.GetSubjectType(),
		SubjectId:    request.GetSubjectId(),
		ActionKey:    request.GetActionKey(),
		ResourceType: request.GetResourceType(),
		ResourceId:   request.GetResourceId(),
		ScopeType:    request.GetScopeType(),
		ScopeId:      request.GetScopeId(),
		Priority:     request.GetPriority(),
		Status:       request.GetStatus(),
		Version:      1,
	}
	f.rules[key] = response
	return response, nil
}

func (f *fakeAccessManagerClient) CheckAccess(_ context.Context, request *accessaccountsv1.CheckAccessRequest, _ ...grpc.CallOption) (*accessaccountsv1.CheckAccessResponse, error) {
	f.checkAccessCalls++
	if f.lastCheckAccessBySubject == nil {
		f.lastCheckAccessBySubject = map[string]*accessaccountsv1.CheckAccessRequest{}
	}
	subjectID := request.GetSubject().GetId()
	f.lastCheckAccessBySubject[subjectID] = request
	if f.checkAccessErr != nil {
		return nil, f.checkAccessErr
	}
	if reason := f.denySubjects[subjectID]; reason != "" {
		return &accessaccountsv1.CheckAccessResponse{
			Decision:   accessaccountsv1.AccessDecision_ACCESS_DECISION_DENY,
			ReasonCode: reason,
		}, nil
	}
	if f.rules[checkAccessIdentityKey(request)] != nil {
		return &accessaccountsv1.CheckAccessResponse{
			Decision:   accessaccountsv1.AccessDecision_ACCESS_DECISION_ALLOW,
			ReasonCode: "explicit_allow",
		}, nil
	}
	return &accessaccountsv1.CheckAccessResponse{
		Decision:   accessaccountsv1.AccessDecision_ACCESS_DECISION_DENY,
		ReasonCode: "no_matching_rule",
	}, nil
}

func accessRuleIdentityKey(request *accessaccountsv1.PutAccessRuleRequest) string {
	return strings.Join([]string{
		request.GetEffect().String(),
		request.GetSubjectType(),
		request.GetSubjectId(),
		request.GetActionKey(),
		request.GetResourceType(),
		request.GetResourceId(),
		request.GetScopeType(),
		request.GetScopeId(),
	}, "\x00")
}

func checkAccessIdentityKey(request *accessaccountsv1.CheckAccessRequest) string {
	return strings.Join([]string{
		accessaccountsv1.AccessEffect_ACCESS_EFFECT_ALLOW.String(),
		request.GetSubject().GetType(),
		request.GetSubject().GetId(),
		request.GetActionKey(),
		request.GetResource().GetType(),
		request.GetResource().GetId(),
		request.GetScope().GetType(),
		request.GetScope().GetId(),
	}, "\x00")
}

type fakeProviderHubClient struct {
	bootstrapSignal  *providersv1.RepositoryMergeSignal
	adoptionSignal   *providersv1.RepositoryMergeSignal
	changeSignal     *providersv1.RepositoryChangeSignal
	snapshotCount    int
	listSignalsErr   error
	listSnapshotsErr error
}

func (f *fakeProviderHubClient) GetRepositoryMergeSignal(_ context.Context, request *providersv1.GetRepositoryMergeSignalRequest, _ ...grpc.CallOption) (*providersv1.RepositoryMergeSignalResponse, error) {
	for _, signal := range []*providersv1.RepositoryMergeSignal{f.bootstrapSignal, f.adoptionSignal} {
		if signal != nil && signal.GetSignalKey() == request.GetSignalKey() {
			return &providersv1.RepositoryMergeSignalResponse{
				ReadStatus:  providersv1.ProviderOwnedDataStatus_PROVIDER_OWNED_DATA_STATUS_READY,
				MergeSignal: signal,
			}, nil
		}
	}
	return &providersv1.RepositoryMergeSignalResponse{ReadStatus: providersv1.ProviderOwnedDataStatus_PROVIDER_OWNED_DATA_STATUS_NOT_FOUND}, nil
}

func (f *fakeProviderHubClient) ListRepositoryMergeSignals(_ context.Context, request *providersv1.ListRepositoryMergeSignalsRequest, _ ...grpc.CallOption) (*providersv1.ListRepositoryMergeSignalsResponse, error) {
	if f.listSignalsErr != nil {
		return nil, f.listSignalsErr
	}
	var signal *providersv1.RepositoryMergeSignal
	if len(request.GetKinds()) > 0 {
		switch request.GetKinds()[0] {
		case providersv1.RepositoryMergeSignalKind_REPOSITORY_MERGE_SIGNAL_KIND_BOOTSTRAP:
			signal = f.bootstrapSignal
		case providersv1.RepositoryMergeSignalKind_REPOSITORY_MERGE_SIGNAL_KIND_ADOPTION:
			signal = f.adoptionSignal
		}
	}
	if signal == nil {
		return &providersv1.ListRepositoryMergeSignalsResponse{}, nil
	}
	return &providersv1.ListRepositoryMergeSignalsResponse{MergeSignals: []*providersv1.RepositoryMergeSignal{signal}}, nil
}

func (f *fakeProviderHubClient) GetRepositoryChangeSignal(_ context.Context, request *providersv1.GetRepositoryChangeSignalRequest, _ ...grpc.CallOption) (*providersv1.RepositoryChangeSignalResponse, error) {
	if f.changeSignal != nil && f.changeSignal.GetSignalKey() == request.GetSignalKey() {
		return &providersv1.RepositoryChangeSignalResponse{
			ReadStatus:   providersv1.ProviderOwnedDataStatus_PROVIDER_OWNED_DATA_STATUS_READY,
			ChangeSignal: f.changeSignal,
		}, nil
	}
	return &providersv1.RepositoryChangeSignalResponse{ReadStatus: providersv1.ProviderOwnedDataStatus_PROVIDER_OWNED_DATA_STATUS_NOT_FOUND}, nil
}

func (f *fakeProviderHubClient) ListRepositoryChangeSignals(context.Context, *providersv1.ListRepositoryChangeSignalsRequest, ...grpc.CallOption) (*providersv1.ListRepositoryChangeSignalsResponse, error) {
	if f.changeSignal == nil {
		return &providersv1.ListRepositoryChangeSignalsResponse{}, nil
	}
	return &providersv1.ListRepositoryChangeSignalsResponse{ChangeSignals: []*providersv1.RepositoryChangeSignal{f.changeSignal}}, nil
}

func (f *fakeProviderHubClient) ListRepositoryAdoptionScanSnapshots(context.Context, *providersv1.ListRepositoryAdoptionScanSnapshotsRequest, ...grpc.CallOption) (*providersv1.ListRepositoryAdoptionScanSnapshotsResponse, error) {
	if f.listSnapshotsErr != nil {
		return nil, f.listSnapshotsErr
	}
	response := &providersv1.ListRepositoryAdoptionScanSnapshotsResponse{}
	for i := 0; i < f.snapshotCount; i++ {
		response.AdoptionScanSnapshots = append(response.AdoptionScanSnapshots, &providersv1.RepositoryAdoptionScanSnapshot{SnapshotId: "snapshot"})
	}
	return response, nil
}

func projectFixture(organizationID string, slug string, projectID string) *projectsv1.Project {
	return &projectsv1.Project{
		ProjectId:      projectID,
		OrganizationId: organizationID,
		Slug:           slug,
		DisplayName:    slug,
		Status:         projectsv1.ProjectStatus_PROJECT_STATUS_ACTIVE,
		Version:        3,
	}
}

func repositoryFixture() *projectsv1.Repository {
	return &projectsv1.Repository{
		RepositoryId:         "repo-1",
		ProjectId:            "project-1",
		Provider:             projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_GITHUB,
		ProviderOwner:        "codex-k8s",
		ProviderName:         "kodex-onboarding-test",
		DefaultBranch:        "main",
		Status:               projectsv1.RepositoryStatus_REPOSITORY_STATUS_PENDING,
		ProviderRepositoryId: stringPtr("provider-repo-1"),
		Version:              7,
	}
}

func selfRepositoryFixture() *projectsv1.Repository {
	return &projectsv1.Repository{
		RepositoryId:         "repo-1",
		ProjectId:            "project-1",
		Provider:             projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_GITHUB,
		ProviderOwner:        "codex-k8s",
		ProviderName:         "kodex",
		WebUrl:               "https://github.com/codex-k8s/kodex",
		DefaultBranch:        "main",
		Status:               projectsv1.RepositoryStatus_REPOSITORY_STATUS_ACTIVE,
		ProviderRepositoryId: stringPtr("provider-repo-self"),
		Version:              11,
	}
}

func mergeSignalFixture(kind string) *providersv1.RepositoryMergeSignal {
	signalKind := providersv1.RepositoryMergeSignalKind_REPOSITORY_MERGE_SIGNAL_KIND_BOOTSTRAP
	if kind == "adoption" {
		signalKind = providersv1.RepositoryMergeSignalKind_REPOSITORY_MERGE_SIGNAL_KIND_ADOPTION
	}
	return &providersv1.RepositoryMergeSignal{
		SignalId:              kind + "-signal-id",
		SignalKey:             kind + "-signal-key",
		Kind:                  signalKind,
		ProviderSlug:          "github",
		ProjectId:             "project-1",
		RepositoryId:          "repo-1",
		RepositoryFullName:    "codex-k8s/kodex-onboarding-test",
		ProviderRepositoryId:  "provider-repo-1",
		WorkItemProjectionId:  kind + "-projection",
		PullRequestProviderId: kind + "-pr-provider-id",
		BaseBranch:            "main",
		HeadBranch:            "kodex/" + kind,
		MergeCommitSha:        kind + "-merge-commit",
		SourceRef:             "refs/heads/kodex/" + kind,
		WatermarkDigest:       kind + "-watermark-digest",
		Status:                providersv1.RepositoryMergeSignalStatus_REPOSITORY_MERGE_SIGNAL_STATUS_MERGED,
		Version:               3,
		Etag:                  kind + "-etag",
		ObservedAt:            "2026-05-29T00:00:00Z",
		MergedAt:              "2026-05-29T00:00:00Z",
	}
}

func selfRepositoryMergeSignalFixture() *providersv1.RepositoryMergeSignal {
	signal := mergeSignalFixture("adoption")
	signal.RepositoryFullName = "codex-k8s/kodex"
	signal.ProviderRepositoryId = "provider-repo-self"
	signal.HeadBranch = "kodex/self-adoption"
	signal.SourceRef = "refs/heads/kodex/self-adoption"
	signal.MergeCommitSha = strings.Repeat("a", 40)
	signal.SignalKey = "self-adoption-signal-key"
	return signal
}

func selfRepositoryChangeSignalFixture() *providersv1.RepositoryChangeSignal {
	return &providersv1.RepositoryChangeSignal{
		SignalId:             "self-change-signal-id",
		SignalKey:            "provider:github:repository_change:push:codex-k8s/kodex:main:" + strings.Repeat("b", 40),
		Kind:                 providersv1.RepositoryChangeSignalKind_REPOSITORY_CHANGE_SIGNAL_KIND_PUSH,
		ProviderSlug:         "github",
		RepositoryFullName:   "codex-k8s/kodex",
		ProviderRepositoryId: "provider-repo-self",
		Ref:                  "refs/heads/main",
		BaseBranch:           "main",
		CommitSha:            strings.Repeat("b", 40),
		BeforeSha:            strings.Repeat("a", 40),
		PathSummaryStatus:    providersv1.RepositoryChangePathSummaryStatus_REPOSITORY_CHANGE_PATH_SUMMARY_STATUS_READY,
		ChangedPathCount:     3,
		PathDigest:           "sha256:paths",
		PathCategories: []*providersv1.RepositoryChangePathCategoryCount{
			{Category: providersv1.RepositoryChangePathCategory_REPOSITORY_CHANGE_PATH_CATEGORY_SERVICES_POLICY, Count: 1},
			{Category: providersv1.RepositoryChangePathCategory_REPOSITORY_CHANGE_PATH_CATEGORY_DEPLOY_MANIFEST, Count: 1},
			{Category: providersv1.RepositoryChangePathCategory_REPOSITORY_CHANGE_PATH_CATEGORY_DOCUMENTATION, Count: 1},
		},
		ServicesPolicyChanged: true,
		DeployRelevantChanged: true,
		ChangeFingerprint:     "sha256:self-change",
		ObservedAt:            "2026-05-29T00:00:00Z",
		Status:                providersv1.RepositoryChangeSignalStatus_REPOSITORY_CHANGE_SIGNAL_STATUS_OBSERVED,
		Version:               1,
		Etag:                  "self-change-etag",
	}
}

func checkedPayloadFixture() onboardingScenario {
	return onboardingScenario{
		ProjectID:            "project-1",
		RepositoryID:         "repo-1",
		ProviderSlug:         "github",
		RepositoryFullName:   "codex-k8s/kodex-onboarding-test",
		ProviderRepositoryID: "provider-repo-1",
		Bootstrap: &reconcileScenario{
			WatermarkJSON: `{"work_type":"repository_bootstrap","safe":"yes"}`,
			CheckedPolicy: checkedPolicyScenario{
				ArtifactRef:          "artifact://checked/bootstrap",
				ArtifactDigest:       "sha256:bootstrap",
				ArtifactVersion:      "bootstrap-merge-commit",
				SourcePath:           "services.yaml",
				ContentHash:          "sha256:bootstrap-content",
				ValidatedPayloadJSON: `{"marker":"raw_services_yaml_secret"}`,
			},
		},
		Adoption: &reconcileScenario{
			WatermarkJSON: `{"work_type":"repository_adoption","safe":"yes"}`,
			CheckedPolicy: checkedPolicyScenario{
				ArtifactRef:          "artifact://checked/adoption",
				ArtifactDigest:       "sha256:adoption",
				ArtifactVersion:      "adoption-merge-commit",
				SourcePath:           "services.yaml",
				ContentHash:          "sha256:adoption-content",
				ValidatedPayloadJSON: `{"marker":"provider_payload_secret"}`,
			},
		},
	}
}

func selfRepositoryScenario() onboardingScenario {
	return onboardingScenario{
		ProjectID:            "project-1",
		ProviderSlug:         "github",
		RepositoryFullName:   "codex-k8s/kodex",
		ProviderRepositoryID: "provider-repo-self",
		RepositoryBinding: &repositoryBindingScenario{
			ProviderOwner:        "codex-k8s",
			ProviderName:         "kodex",
			WebURL:               "https://github.com/codex-k8s/kodex",
			DefaultBranch:        "main",
			ProviderRepositoryID: "provider-repo-self",
			Status:               "active",
		},
		RepositoryChange: &repositoryChangeScenario{
			EventName:  "push",
			Ref:        "refs/heads/main",
			BaseBranch: "main",
			CommitSHA:  strings.Repeat("b", 40),
			ChangedPaths: []repositoryChangedPathScenario{
				{Path: "services.yaml", Action: "modified", ObjectDigest: expectedContentHash(`{"kind":"safe"}`)},
				{Path: "deploy/base/provider-hub/kustomization.yaml", Action: "modified"},
				{Path: "docs/platform/architecture/repository_onboarding.md", Action: "modified"},
			},
		},
		Adoption: &reconcileScenario{
			SignalKey:     "self-adoption-signal-key",
			WatermarkJSON: `{"work_type":"repository_adoption","safe":"yes"}`,
			CheckedPolicy: checkedPolicyScenario{
				ArtifactRef:          "artifact://checked/self-adoption/services.yaml",
				ArtifactDigest:       expectedContentHash(`{"services":[{"key":"project-catalog"}],"marker":"raw_services_yaml_secret"}`),
				ArtifactVersion:      strings.Repeat("a", 40),
				SourcePath:           "services.yaml",
				ContentHash:          expectedContentHash(`{"services":[{"key":"project-catalog"}],"marker":"raw_services_yaml_secret"}`),
				ValidatedPayloadJSON: `{"services":[{"key":"project-catalog"}],"marker":"raw_services_yaml_secret"}`,
			},
		},
	}
}

func selfRepositoryBindingOnlyScenario() onboardingScenario {
	return onboardingScenario{
		ProviderSlug:         "github",
		RepositoryFullName:   "codex-k8s/kodex",
		ProviderRepositoryID: "provider-repo-self",
		RepositoryBinding: &repositoryBindingScenario{
			ProviderOwner:        "codex-k8s",
			ProviderName:         "kodex",
			WebURL:               "https://github.com/codex-k8s/kodex",
			DefaultBranch:        "main",
			ProviderRepositoryID: "provider-repo-self",
			Status:               "active",
		},
	}
}

func bootstrapSetupFixture() onboardingScenario {
	return onboardingScenario{
		ProjectID:          "project-1",
		ProviderSlug:       "github",
		RepositoryFullName: "codex-k8s/kodex-onboarding-live",
		BootstrapSetup: &bootstrapSetup{
			CreateRepository: &bootstrapCreateRepositoryScenario{
				OwnerKind:         "organization",
				ProviderOwner:     "codex-k8s",
				ProviderName:      "kodex-onboarding-live",
				Visibility:        "private",
				Description:       "safe onboarding runner test repository",
				ExternalAccountID: "external-account-1",
			},
			PullRequest: &bootstrapPullRequestScenario{
				BaseBranch:      "main",
				BootstrapBranch: "kodex/bootstrap",
				CommitMessage:   "Prepare kodex bootstrap",
				Title:           "Prepare kodex bootstrap",
				Body:            "Safe bootstrap setup through product API.",
				Files: []bootstrapPreparedFile{
					{
						Path:    "services.yaml",
						Content: "services:\n  - key: prepared_services_yaml_secret\n",
					},
					{
						Path:    "README.md",
						Content: "Kodex onboarding test repository\n",
					},
				},
				WatermarkJSON:     `{"kind":"kodex_watermark","managed_by":"kodex","work_type":"repository_bootstrap","source_ref":"refs/heads/kodex/bootstrap"}`,
				ExternalAccountID: "external-account-1",
				ServicesPolicy: bootstrapServicesPolicyScenario{
					SourcePath:           "services.yaml",
					ContentHash:          "sha256:bootstrap-content",
					ValidatedPayloadJSON: `{"services":[{"key":"normalized_payload_secret"}]}`,
				},
			},
		},
	}
}

func repositoryCreateResponseFixture() *projectsv1.RepositoryProviderCreateResponse {
	repository := repositoryFixture()
	repository.ProviderName = "kodex-onboarding-live"
	repository.ProviderRepositoryId = stringPtr("provider-repo-live")
	return &projectsv1.RepositoryProviderCreateResponse{
		Repository:           repository,
		BaseBranch:           "main",
		ProviderRepositoryId: stringPtr("provider-repo-live"),
		ProviderOperationId:  stringPtr("provider-operation-create-repo"),
	}
}

func signalOnlyScenario(kind string) onboardingScenario {
	scenario := onboardingScenario{
		ProjectID:            "project-1",
		RepositoryID:         "repo-1",
		ProviderSlug:         "github",
		RepositoryFullName:   "codex-k8s/kodex-onboarding-test",
		ProviderRepositoryID: "provider-repo-1",
	}
	switch kind {
	case "bootstrap":
		scenario.Bootstrap = &reconcileScenario{
			SignalKey:     "bootstrap-signal-key",
			WatermarkJSON: `{"work_type":"repository_bootstrap","safe":"yes"}`,
		}
	case "adoption":
		scenario.Adoption = &reconcileScenario{
			SignalKey:     "adoption-signal-key",
			WatermarkJSON: `{"work_type":"repository_adoption","safe":"yes"}`,
		}
	}
	return scenario
}

func writeScenario(t *testing.T, scenario onboardingScenario) string {
	t.Helper()
	content, err := json.MarshalIndent(scenario, "", "  ")
	if err != nil {
		t.Fatalf("marshal scenario: %v", err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	return path
}

func writeTextFile(t *testing.T, name string, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write text file: %v", err)
	}
	return path
}

func expectedContentHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func stringPtr(value string) *string {
	return &value
}

func assertContains(t *testing.T, value string, needle string) {
	t.Helper()
	if !strings.Contains(value, needle) {
		t.Fatalf("expected %q to contain %q", value, needle)
	}
}

func assertNotContains(t *testing.T, value string, needle string) {
	t.Helper()
	if strings.Contains(value, needle) {
		t.Fatalf("expected %q not to contain %q", value, needle)
	}
}

func assertSelfDeployAccessRule(t *testing.T, request *accessaccountsv1.PutAccessRuleRequest, projectID string) {
	t.Helper()
	if request == nil {
		t.Fatal("expected self-deploy access rule request")
	}
	if request.GetEffect() != accessaccountsv1.AccessEffect_ACCESS_EFFECT_ALLOW ||
		request.GetSubjectType() != "service" ||
		request.GetActionKey() != accesscatalog.ActionProjectPolicyRead ||
		request.GetResourceType() != accesscatalog.ResourceServicesPolicy ||
		request.GetResourceId() != "" ||
		request.GetScopeType() != accesscatalog.ScopeProject ||
		request.GetScopeId() != projectID ||
		request.GetPriority() != selfDeployServiceAccessPriority ||
		request.GetStatus() != accessaccountsv1.AccessRuleStatus_ACCESS_RULE_STATUS_ACTIVE {
		t.Fatalf("unexpected self-deploy access rule request: %+v", request)
	}
	if request.GetMeta().GetActor().GetType() != "service" || request.GetMeta().GetActor().GetId() != defaultActorID {
		t.Fatalf("unexpected access rule actor: %+v", request.GetMeta().GetActor())
	}
}

func assertSelfDeployAccessCheck(t *testing.T, request *accessaccountsv1.CheckAccessRequest, projectID string) {
	t.Helper()
	if request == nil {
		t.Fatal("expected self-deploy access check request")
	}
	if request.GetSubject().GetType() != "service" ||
		request.GetSubject().GetId() != "agent-manager" ||
		request.GetActionKey() != accesscatalog.ActionProjectPolicyRead ||
		request.GetResource().GetType() != accesscatalog.ResourceServicesPolicy ||
		request.GetResource().GetId() != "" ||
		request.GetScope().GetType() != accesscatalog.ScopeProject ||
		request.GetScope().GetId() != projectID ||
		!request.GetAudit() {
		t.Fatalf("unexpected self-deploy access check request: %+v", request)
	}
}

func firstMetadata(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
