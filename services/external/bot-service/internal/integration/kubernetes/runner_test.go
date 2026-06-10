package kubernetes

import (
	"context"
	"strings"
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
		RunID:            "dev-test",
		Profile:          "developer",
		GitHubSecretName: "matter-codex-github-agent",
		Provider:         "github",
		Owner:            "codex-k8s",
		Name:             "matter-codex",
		BaseBranch:       "main",
		HeadBranch:       "matter-codex-dev-test",
		Title:            "Matter Codex developer smoke",
		Task:             "Update a safe smoke document.",
		Prompt:           "Developer prompt",
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
	if got := podSpec.Containers[0].Command[0]; got != "matter-codex-agent-runner" {
		t.Fatalf("command = %q", got)
	}
	if got := podSpec.Containers[0].Args[0]; got != "developer" {
		t.Fatalf("args = %q", got)
	}
	if len(podSpec.Volumes) != 4 {
		t.Fatalf("volumes len = %d", len(podSpec.Volumes))
	}
	if podSpec.Volumes[1].Secret.SecretName != "matter-codex-codex-auth" || podSpec.Volumes[2].Secret.SecretName != "matter-codex-github-agent" {
		t.Fatalf("secret volumes = %#v", podSpec.Volumes)
	}
	if podSpec.Volumes[3].ConfigMap.Name != "mc-prompt-dev-test" {
		t.Fatalf("prompt configmap volume = %#v", podSpec.Volumes[3])
	}
	if podSpec.Volumes[1].Secret.Items[0].Key != "auth.json" {
		t.Fatalf("codex auth secret items = %#v", podSpec.Volumes[1].Secret.Items)
	}
	if got := secretItemKeys(podSpec.Volumes[2].Secret.Items); got != "github-token,github-username,github-email" {
		t.Fatalf("github secret items = %q", got)
	}
	configMap, err := client.CoreV1().ConfigMaps("mattermost").Get(context.Background(), "mc-prompt-dev-test", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get prompt configmap error = %v", err)
	}
	if configMap.Data["prompt.md"] != "Developer prompt" {
		t.Fatalf("prompt configmap data = %#v", configMap.Data)
	}
}

func TestStartReviewRunCreatesPVCAndJob(t *testing.T) {
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

	started, err := runner.StartReviewRun(context.Background(), runtimerepo.ReviewRunInput{
		RunID:               "review-test",
		Profile:             "reviewer",
		CodexAuthSecretName: "matter-codex-codex-auth-primary",
		GitHubSecretName:    "matter-codex-github",
		Provider:            "github",
		Owner:               "codex-k8s",
		Name:                "matter-codex",
		PRNumber:            12,
		Prompt:              "Reviewer prompt",
	})
	if err != nil {
		t.Fatalf("StartReviewRun() error = %v", err)
	}
	if !started.Created || started.JobName != "mc-run-review-test" || started.PVCName != "mc-ws-review-test" {
		t.Fatalf("started = %#v", started)
	}
	job, err := client.BatchV1().Jobs("mattermost").Get(context.Background(), "mc-run-review-test", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get job error = %v", err)
	}
	podSpec := job.Spec.Template.Spec
	if podSpec.ServiceAccountName != "matter-codex-agent-runner" {
		t.Fatalf("ServiceAccountName = %q", podSpec.ServiceAccountName)
	}
	if podSpec.AutomountServiceAccountToken == nil || *podSpec.AutomountServiceAccountToken {
		t.Fatal("reviewer job should not automount service account token")
	}
	if got := podSpec.Containers[0].Image; got != "matter-codex-agent-runner:test" {
		t.Fatalf("runner image = %q", got)
	}
	if got := podSpec.Containers[0].Command[0]; got != "matter-codex-agent-runner" {
		t.Fatalf("command = %q", got)
	}
	if got := podSpec.Containers[0].Args[0]; got != "reviewer" {
		t.Fatalf("args = %q", got)
	}
	if got := envValue(podSpec.Containers[0].Env, "MATTERCODEX_PR_NUMBER"); got != "12" {
		t.Fatalf("MATTERCODEX_PR_NUMBER = %q", got)
	}
	if len(podSpec.Volumes) != 4 {
		t.Fatalf("volumes len = %d", len(podSpec.Volumes))
	}
	if podSpec.Volumes[1].Secret.SecretName != "matter-codex-codex-auth-primary" || podSpec.Volumes[2].Secret.SecretName != "matter-codex-github" {
		t.Fatalf("secret volumes = %#v", podSpec.Volumes)
	}
	if podSpec.Volumes[3].ConfigMap.Name != "mc-prompt-review-test" {
		t.Fatalf("prompt configmap volume = %#v", podSpec.Volumes[3])
	}
	configMap, err := client.CoreV1().ConfigMaps("mattermost").Get(context.Background(), "mc-prompt-review-test", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get prompt configmap error = %v", err)
	}
	if configMap.Data["prompt.md"] != "Reviewer prompt" {
		t.Fatalf("prompt configmap data = %#v", configMap.Data)
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
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "mc-prompt-smoke-test", Namespace: "mattermost"}},
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
	if _, err := client.CoreV1().ConfigMaps("mattermost").Get(context.Background(), "mc-prompt-smoke-test", metav1.GetOptions{}); err == nil {
		t.Fatal("prompt configmap still exists")
	}
}

func envValue(env []corev1.EnvVar, name string) string {
	for _, item := range env {
		if item.Name == name {
			return item.Value
		}
	}
	return ""
}

func secretItemKeys(items []corev1.KeyToPath) string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	return strings.Join(keys, ",")
}
