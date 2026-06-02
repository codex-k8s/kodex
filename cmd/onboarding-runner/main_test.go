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
	"strings"
	"testing"

	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"google.golang.org/grpc"
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
			snapshotCount:  1,
		},
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

type fakeProjectCatalogClient struct {
	repository                      *projectsv1.Repository
	attachRepositoryResponse        *projectsv1.Repository
	createRepositoryResponse        *projectsv1.RepositoryProviderCreateResponse
	getRepositoryErr                error
	attachRepositoryErr             error
	createRepositoryErr             error
	bootstrapPullRequestErr         error
	attachRepositoryCalls           int
	bootstrapReconcileCalls         int
	adoptionReconcileCalls          int
	createRepositoryCalls           int
	bootstrapPullRequestCalls       int
	lastAttachRepositoryRequest     *projectsv1.AttachRepositoryRequest
	lastBootstrapRequest            *projectsv1.ReconcileBootstrapMergeSignalRequest
	lastAdoptionRequest             *projectsv1.ReconcileAdoptionMergeSignalRequest
	lastCreateRepositoryRequest     *projectsv1.CreateProviderRepositoryRequest
	lastBootstrapPullRequestRequest *projectsv1.CreateRepositoryBootstrapPullRequestRequest
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

type fakeProviderHubClient struct {
	bootstrapSignal  *providersv1.RepositoryMergeSignal
	adoptionSignal   *providersv1.RepositoryMergeSignal
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

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
