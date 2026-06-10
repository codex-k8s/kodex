package kubernetes

import (
	"context"
	"testing"

	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestStartSmokeRunCreatesPVCAndJob(t *testing.T) {
	client := fake.NewSimpleClientset()
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                 "mattermost",
		SmokeImage:                "busybox:1.36",
		WorkspaceStorageSize:      "1Gi",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	started, err := runner.StartSmokeRun(context.Background(), runtimerepo.SmokeRunInput{RunID: "smoke-test", Role: "smoke"})
	if err != nil {
		t.Fatalf("StartSmokeRun() error = %v", err)
	}
	if !started.Created || started.JobName != "mc-run-smoke-test" || started.PVCName != "mc-ws-smoke-test" {
		t.Fatalf("started = %#v", started)
	}
	job, err := client.BatchV1().Jobs("mattermost").Get(context.Background(), "mc-run-smoke-test", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get job error = %v", err)
	}
	if job.Spec.Template.Spec.ServiceAccountName != "matter-codex-agent-runner" {
		t.Fatalf("ServiceAccountName = %q", job.Spec.Template.Spec.ServiceAccountName)
	}
	if job.Spec.Template.Spec.AutomountServiceAccountToken == nil || *job.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatal("agent job should not automount service account token")
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("mattermost").Get(context.Background(), "mc-ws-smoke-test", metav1.GetOptions{}); err != nil {
		t.Fatalf("Get pvc error = %v", err)
	}
}

func TestStartDeveloperRunCreatesPVCAndJob(t *testing.T) {
	client := fake.NewSimpleClientset()
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                 "mattermost",
		AgentRunnerImage:          "matter-codex-agent-runner:test",
		CodexPackage:              "@openai/codex@0.138.0",
		WorkspaceStorageSize:      "1Gi",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
		CodexAuthSecretName:       "matter-codex-codex-auth",
		GitHubSecretName:          "matter-codex-github",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	started, err := runner.StartDeveloperRun(context.Background(), runtimerepo.DeveloperRunInput{
		RunID:      "dev-test",
		Profile:    "developer",
		Provider:   "github",
		Owner:      "codex-k8s",
		Name:       "matter-codex",
		BaseBranch: "main",
		HeadBranch: "matter-codex-dev-test",
		Title:      "Matter Codex developer smoke",
		Task:       "Update a safe smoke document.",
	})
	if err != nil {
		t.Fatalf("StartDeveloperRun() error = %v", err)
	}
	if !started.Created || started.JobName != "mc-run-dev-test" || started.PVCName != "mc-ws-dev-test" {
		t.Fatalf("started = %#v", started)
	}
	job, err := client.BatchV1().Jobs("mattermost").Get(context.Background(), "mc-run-dev-test", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get job error = %v", err)
	}
	podSpec := job.Spec.Template.Spec
	if podSpec.ServiceAccountName != "matter-codex-agent-runner" {
		t.Fatalf("ServiceAccountName = %q", podSpec.ServiceAccountName)
	}
	if podSpec.AutomountServiceAccountToken == nil || *podSpec.AutomountServiceAccountToken {
		t.Fatal("developer job should not automount service account token")
	}
	if got := podSpec.Containers[0].Image; got != "matter-codex-agent-runner:test" {
		t.Fatalf("runner image = %q", got)
	}
	if len(podSpec.Volumes) != 3 {
		t.Fatalf("volumes len = %d", len(podSpec.Volumes))
	}
	if podSpec.Volumes[1].Secret.SecretName != "matter-codex-codex-auth" || podSpec.Volumes[2].Secret.SecretName != "matter-codex-github" {
		t.Fatalf("secret volumes = %#v", podSpec.Volumes)
	}
	if podSpec.Volumes[1].Secret.Items[0].Key != "auth.json" {
		t.Fatalf("codex auth secret items = %#v", podSpec.Volumes[1].Secret.Items)
	}
}

func TestGetRunStatusReadsJobAndPodStatus(t *testing.T) {
	client := fake.NewSimpleClientset(
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-run-smoke-test", Namespace: "mattermost", Labels: runnerLabels("smoke-test", "smoke")},
			Status:     batchv1.JobStatus{Succeeded: 1},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-run-smoke-test-pod", Namespace: "mattermost", Labels: runnerLabels("smoke-test", "smoke")},
			Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
		},
	)
	runner, err := NewRunnerWithClient(client, Config{Namespace: "mattermost"})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	status, err := runner.GetRunStatus(context.Background(), "smoke-test")
	if err != nil {
		t.Fatalf("GetRunStatus() error = %v", err)
	}
	if !status.Exists || status.JobSucceeded != 1 || status.PodName != "mc-run-smoke-test-pod" || status.PodPhase != "Succeeded" {
		t.Fatalf("status = %#v", status)
	}
}

func TestParseArtifacts(t *testing.T) {
	artifacts := parseArtifacts("line\nmatter-codex artifact pr-url: https://github.com/codex-k8s/matter-codex/pull/8\nmatter-codex artifact branch: smoke\n")
	if artifacts["pr-url"] != "https://github.com/codex-k8s/matter-codex/pull/8" || artifacts["branch"] != "smoke" {
		t.Fatalf("artifacts = %#v", artifacts)
	}
}

func TestCleanupRunDeletesJobAndPVC(t *testing.T) {
	client := fake.NewSimpleClientset(
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "mc-run-smoke-test", Namespace: "mattermost"}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "mc-ws-smoke-test", Namespace: "mattermost"}},
	)
	runner, err := NewRunnerWithClient(client, Config{Namespace: "mattermost"})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	result, err := runner.CleanupRun(context.Background(), "smoke-test")
	if err != nil {
		t.Fatalf("CleanupRun() error = %v", err)
	}
	if !result.JobDeleted || !result.PVCDeleted {
		t.Fatalf("result = %#v", result)
	}
}
