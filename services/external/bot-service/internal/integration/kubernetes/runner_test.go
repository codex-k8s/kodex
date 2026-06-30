package kubernetes

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	assertRunnerPodSecurity(t, job.Spec.Template.Spec)
	if _, err := client.CoreV1().PersistentVolumeClaims("mattermost").Get(context.Background(), "mc-ws-smoke-test", metav1.GetOptions{}); err != nil {
		t.Fatalf("Get pvc error = %v", err)
	}
}

func TestStartCodexAuthSessionCreatesHardenedJob(t *testing.T) {
	client := fake.NewSimpleClientset()
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                 "mattermost",
		AgentRunnerImage:          "matter-codex-agent-runner:test",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	session, err := runner.StartCodexAuthSession(context.Background(), runtimerepo.CodexAuthSessionInput{
		AccountName: "primary",
		SecretName:  "matter-codex-codex-auth-primary",
	})
	if err != nil {
		t.Fatalf("StartCodexAuthSession() error = %v", err)
	}
	if !session.Created || session.JobName != "mc-codex-auth-primary" {
		t.Fatalf("session = %#v", session)
	}
	job, err := client.BatchV1().Jobs("mattermost").Get(context.Background(), "mc-codex-auth-primary", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get job error = %v", err)
	}
	podSpec := job.Spec.Template.Spec
	assertRunnerPodSecurity(t, podSpec)
	if got := podSpec.Containers[0].Args[0]; got != "codex-auth" {
		t.Fatalf("args = %q", got)
	}
	if len(podSpec.Volumes) != 3 {
		t.Fatalf("volumes len = %d", len(podSpec.Volumes))
	}
	if !hasVolume(podSpec.Volumes, "codex-home") {
		t.Fatalf("codex-home volume missing: %#v", podSpec.Volumes)
	}
	if !hasVolumeMount(podSpec.Containers[0].VolumeMounts, "codex-home", "/codex-home") {
		t.Fatalf("codex-home volume mount missing: %#v", podSpec.Containers[0].VolumeMounts)
	}
}

func TestCodexAuthSecretCheckJobMountsSavedAuthSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                         "mattermost",
		AgentRunnerImage:                  "matter-codex-agent-runner:test",
		AgentRunnerServiceAccount:         "matter-codex-agent-runner",
		AuthCheckJobTTLSecondsAfterFinish: 60,
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	job := runner.codexAuthSecretCheckJob(runtimerepo.CodexAuthSecretCheckInput{
		AccountName: "primary",
		SecretName:  "matter-codex-codex-auth-primary",
	}, "mc-codex-auth-check-primary-test")
	podSpec := job.Spec.Template.Spec
	assertRunnerPodSecurity(t, podSpec)
	if got := podSpec.Containers[0].Args[0]; got != "codex-auth-secret-check" {
		t.Fatalf("args = %q", got)
	}
	if !hasVolume(podSpec.Volumes, "workspace") || !hasSecretVolume(podSpec.Volumes, codexAuthSecretVolume, "matter-codex-codex-auth-primary") {
		t.Fatalf("volumes = %#v", podSpec.Volumes)
	}
	if !hasVolumeMount(podSpec.Containers[0].VolumeMounts, codexAuthSecretVolume, "/var/run/secrets/matter-codex-codex") {
		t.Fatalf("codex auth secret mount missing: %#v", podSpec.Containers[0].VolumeMounts)
	}
	if got := envValue(podSpec.Containers[0].Env, "MATTERCODEX_CODEX_CONFIG_OVERLAY"); got != "" {
		t.Fatalf("auth check must not receive config overlay, got %q", got)
	}
	if job.Spec.TTLSecondsAfterFinished == nil || *job.Spec.TTLSecondsAfterFinished != 60 {
		var ttl int32
		if job.Spec.TTLSecondsAfterFinished != nil {
			ttl = *job.Spec.TTLSecondsAfterFinished
		}
		t.Fatalf("auth check ttl = %d", ttl)
	}
}

func TestCheckCodexAuthSecretReturnsNotReadyWhenSecretIsMissing(t *testing.T) {
	client := fake.NewSimpleClientset()
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                 "mattermost",
		AgentRunnerImage:          "matter-codex-agent-runner:test",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	result, err := runner.CheckCodexAuthSecret(context.Background(), runtimerepo.CodexAuthSecretCheckInput{
		AccountName: "primary",
		SecretName:  "missing-codex-auth",
	})
	if err != nil {
		t.Fatalf("CheckCodexAuthSecret() error = %v", err)
	}
	if result.Ready || !strings.Contains(result.LogTail, "missing") {
		t.Fatalf("result = %#v", result)
	}
	jobs, err := client.BatchV1().Jobs("mattermost").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List jobs error = %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("unexpected auth check jobs: %#v", jobs.Items)
	}
}

func TestDeleteCodexAuthAccountDeletesJobAndSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                 "mattermost",
		AgentRunnerImage:          "matter-codex-agent-runner:test",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}
	if _, err := runner.StartCodexAuthSession(context.Background(), runtimerepo.CodexAuthSessionInput{
		AccountName: "reviewer-test",
		SecretName:  "matter-codex-codex-auth-reviewer-test",
	}); err != nil {
		t.Fatalf("StartCodexAuthSession() error = %v", err)
	}
	if _, err := client.CoreV1().Secrets("mattermost").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "matter-codex-codex-auth-reviewer-test"},
		Data:       map[string][]byte{"auth.json": []byte(`{"ok":true}`)},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create secret error = %v", err)
	}

	deleted, err := runner.DeleteCodexAuthAccount(context.Background(), "reviewer-test", "matter-codex-codex-auth-reviewer-test")
	if err != nil {
		t.Fatalf("DeleteCodexAuthAccount() error = %v", err)
	}
	if !deleted.JobDeleted || !deleted.SecretDeleted || deleted.Namespace != "mattermost" {
		t.Fatalf("deleted = %#v", deleted)
	}
	if _, err := client.BatchV1().Jobs("mattermost").Get(context.Background(), "mc-codex-auth-reviewer-test", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("job should be deleted, err = %v", err)
	}
	if _, err := client.CoreV1().Secrets("mattermost").Get(context.Background(), "matter-codex-codex-auth-reviewer-test", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("secret should be deleted, err = %v", err)
	}
}

func TestUpsertGitHubTokenSecretCreatesAndUpdatesSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                 "mattermost",
		AgentRunnerImage:          "matter-codex-agent-runner:test",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	created, err := runner.UpsertGitHubTokenSecret(context.Background(), runtimerepo.GitHubTokenSecretInput{
		AccountName: "reviewer",
		SecretName:  "matter-codex-github-reviewer",
		Token:       "test-token-initial",
		Username:    "reviewer-bot",
		Email:       "reviewer@example.invalid",
	})
	if err != nil {
		t.Fatalf("UpsertGitHubTokenSecret(create) error = %v", err)
	}
	if !created.Created || created.Namespace != "mattermost" || created.SecretName != "matter-codex-github-reviewer" {
		t.Fatalf("created = %#v", created)
	}
	secret, err := client.CoreV1().Secrets("mattermost").Get(context.Background(), "matter-codex-github-reviewer", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get secret error = %v", err)
	}
	if string(secret.Data["github-token"]) != "test-token-initial" || string(secret.Data["github-username"]) != "reviewer-bot" || string(secret.Data["github-email"]) != "reviewer@example.invalid" {
		t.Fatalf("secret data = %#v", secret.Data)
	}
	if secret.Labels["matter-codex.dev/github-account"] != "reviewer" {
		t.Fatalf("secret labels = %#v", secret.Labels)
	}

	updated, err := runner.UpsertGitHubTokenSecret(context.Background(), runtimerepo.GitHubTokenSecretInput{
		AccountName: "reviewer",
		SecretName:  "matter-codex-github-reviewer",
		Token:       "test-token-updated",
		Username:    "reviewer-bot",
		Email:       "reviewer@example.invalid",
	})
	if err != nil {
		t.Fatalf("UpsertGitHubTokenSecret(update) error = %v", err)
	}
	if updated.Created {
		t.Fatalf("updated should not be created: %#v", updated)
	}
	secret, err = client.CoreV1().Secrets("mattermost").Get(context.Background(), "matter-codex-github-reviewer", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get updated secret error = %v", err)
	}
	if string(secret.Data["github-token"]) != "test-token-updated" {
		t.Fatalf("updated token = %q", secret.Data["github-token"])
	}

	deleted, err := runner.DeleteGitHubTokenSecret(context.Background(), "reviewer", "matter-codex-github-reviewer")
	if err != nil {
		t.Fatalf("DeleteGitHubTokenSecret() error = %v", err)
	}
	if !deleted.SecretDeleted {
		t.Fatalf("deleted = %#v", deleted)
	}
	if _, err := client.CoreV1().Secrets("mattermost").Get(context.Background(), "matter-codex-github-reviewer", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("secret should be deleted, err = %v", err)
	}
}

func TestStartDeveloperRunCreatesPVCAndJob(t *testing.T) {
	client := fake.NewSimpleClientset()
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                 "mattermost",
		AgentRunnerImage:          "matter-codex-agent-runner:test",
		CodexPackage:              "@openai/codex@0.141.0",
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
	if podSpec.AutomountServiceAccountToken == nil || !*podSpec.AutomountServiceAccountToken {
		t.Fatal("developer job should automount service account token for kubectl")
	}
	assertRunnerPodSecurity(t, podSpec)
	if got := podSpec.Containers[0].Image; got != "matter-codex-agent-runner:test" {
		t.Fatalf("runner image = %q", got)
	}
	if got := podSpec.Containers[0].Command; !slices.Equal(got, runnerCommand()) {
		t.Fatalf("command = %#v", got)
	}
	if got := podSpec.Containers[0].Args[0]; got != "developer" {
		t.Fatalf("args = %q", got)
	}
	if len(podSpec.Volumes) != 6 {
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
		CodexPackage:              "@openai/codex@0.141.0",
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
	if podSpec.AutomountServiceAccountToken == nil || !*podSpec.AutomountServiceAccountToken {
		t.Fatal("reviewer job should automount service account token for kubectl")
	}
	if got := podSpec.Containers[0].Image; got != "matter-codex-agent-runner:test" {
		t.Fatalf("runner image = %q", got)
	}
	if got := podSpec.Containers[0].Command; !slices.Equal(got, runnerCommand()) {
		t.Fatalf("command = %#v", got)
	}
	if got := podSpec.Containers[0].Args[0]; got != "reviewer" {
		t.Fatalf("args = %q", got)
	}
	assertRunnerPodSecurity(t, podSpec)
	if got := envValue(podSpec.Containers[0].Env, "MATTERCODEX_PR_NUMBER"); got != "12" {
		t.Fatalf("MATTERCODEX_PR_NUMBER = %q", got)
	}
	if len(podSpec.Volumes) != 6 {
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

func TestStartChatRunCreatesPVCAndJob(t *testing.T) {
	client := fake.NewSimpleClientset()
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                 "mattermost",
		AgentRunnerImage:          "matter-codex-agent-runner:test",
		CodexPackage:              "@openai/codex@0.141.0",
		WorkspaceStorageSize:      "1Gi",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
		CodexAuthSecretName:       "matter-codex-codex-auth",
		GitHubSecretName:          "matter-codex-github",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	started, err := runner.StartChatRun(context.Background(), runtimerepo.ChatRunInput{
		RunID:               "chat-test",
		Profile:             "manager",
		CodexAuthSecretName: "matter-codex-codex-auth-main",
		GitHubSecretName:    "matter-codex-github-agent",
		Prompt:              "Chat prompt",
		SandboxMode:         "danger-full-access",
		ConfigOverlay:       "model = \"gpt-5-codex\"",
	})
	if err != nil {
		t.Fatalf("StartChatRun() error = %v", err)
	}
	if !started.Created || started.JobName != "mc-run-chat-test" || started.PVCName != "mc-ws-chat-test" {
		t.Fatalf("started = %#v", started)
	}
	job, err := client.BatchV1().Jobs("mattermost").Get(context.Background(), "mc-run-chat-test", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get job error = %v", err)
	}
	podSpec := job.Spec.Template.Spec
	if podSpec.AutomountServiceAccountToken == nil || !*podSpec.AutomountServiceAccountToken {
		t.Fatal("chat job should automount service account token for kubectl")
	}
	if got := podSpec.Containers[0].Args[0]; got != "chat" {
		t.Fatalf("args = %q", got)
	}
	if got := envValue(podSpec.Containers[0].Env, "MATTERCODEX_GITHUB_ENABLED"); got != "true" {
		t.Fatalf("MATTERCODEX_GITHUB_ENABLED = %q", got)
	}
	if got := envValue(podSpec.Containers[0].Env, "MATTERCODEX_CODEX_CONFIG_OVERLAY"); got != "model = \"gpt-5-codex\"" {
		t.Fatalf("MATTERCODEX_CODEX_CONFIG_OVERLAY = %q", got)
	}
	if len(podSpec.Volumes) != 6 {
		t.Fatalf("volumes len = %d", len(podSpec.Volumes))
	}
	if podSpec.Volumes[1].Secret.SecretName != "matter-codex-codex-auth-main" || podSpec.Volumes[3].Secret.SecretName != "matter-codex-github-agent" {
		t.Fatalf("secret volumes = %#v", podSpec.Volumes)
	}
	configMap, err := client.CoreV1().ConfigMaps("mattermost").Get(context.Background(), "mc-prompt-chat-test", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get prompt configmap error = %v", err)
	}
	if configMap.Data["prompt.md"] != "Chat prompt" {
		t.Fatalf("prompt configmap data = %#v", configMap.Data)
	}
}

func TestStartAgentSessionCreatesPodWithRuntimeCredentials(t *testing.T) {
	client := fake.NewSimpleClientset()
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                 "mattermost",
		AgentRunnerImage:          "matter-codex-agent-runner:test",
		WorkspaceStorageSize:      "1Gi",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
		CodexAuthSecretName:       "matter-codex-codex-auth",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	started, err := runner.StartAgentSession(context.Background(), runtimerepo.AgentSessionPodInput{
		SessionKey:              "project-1-chat-2-role-3",
		Role:                    "manager",
		BotServiceURL:           "http://bot-service",
		InternalToken:           "session-token",
		CodexAuthSecretName:     "matter-codex-codex-auth-main",
		GitHubSecretName:        "matter-codex-github-agent",
		RepositoryProvider:      "github",
		RepositoryOwner:         "codex-k8s",
		RepositoryName:          "matter-codex",
		RepositoryDefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("StartAgentSession() error = %v", err)
	}
	if !started.Created || started.PodName != "mc-session-project-1-chat-2-role-3" || started.SecretName != "mc-session-token-project-1-chat-2-role-3" {
		t.Fatalf("started = %#v", started)
	}
	pod, err := client.CoreV1().Pods("mattermost").Get(context.Background(), started.PodName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get session pod error = %v", err)
	}
	podSpec := pod.Spec
	if podSpec.AutomountServiceAccountToken == nil || !*podSpec.AutomountServiceAccountToken {
		t.Fatal("session pod should automount service account token for kubectl")
	}
	assertRunnerPodSecurity(t, podSpec)
	container := podSpec.Containers[0]
	if got := envValue(container.Env, "MATTERCODEX_MCP_URL"); got != "http://bot-service/mcp/sessions/project-1-chat-2-role-3" {
		t.Fatalf("MATTERCODEX_MCP_URL = %q", got)
	}
	if got := envValue(container.Env, "MATTERCODEX_SESSION_REPOSITORY_ENABLED"); got != "true" {
		t.Fatalf("MATTERCODEX_SESSION_REPOSITORY_ENABLED = %q", got)
	}
	if !hasSecretVolume(podSpec.Volumes, codexAuthSecretVolume, "matter-codex-codex-auth-main") {
		t.Fatalf("codex auth secret volume missing: %#v", podSpec.Volumes)
	}
	if !hasSecretVolume(podSpec.Volumes, gitHubSecretVolume, "matter-codex-github-agent") {
		t.Fatalf("github secret volume missing: %#v", podSpec.Volumes)
	}
	if !hasSecretVolume(podSpec.Volumes, sessionSecretVolume, started.SecretName) {
		t.Fatalf("session secret volume missing: %#v", podSpec.Volumes)
	}
}

func TestStartAgentSessionUsesClusterAdminServiceAccountWhenRequested(t *testing.T) {
	client := fake.NewSimpleClientset()
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                             "mattermost",
		AgentRunnerImage:                      "matter-codex-agent-runner:test",
		WorkspaceStorageSize:                  "1Gi",
		AgentRunnerServiceAccount:             "matter-codex-agent-runner",
		AgentRunnerClusterAdminServiceAccount: "matter-codex-agent-runner-cluster-admin",
		CodexAuthSecretName:                   "matter-codex-codex-auth",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	started, err := runner.StartAgentSession(context.Background(), runtimerepo.AgentSessionPodInput{
		SessionKey:          "project-1-chat-2-role-9",
		Role:                "sre",
		KubernetesAccess:    "cluster-admin",
		BotServiceURL:       "http://bot-service",
		InternalToken:       "session-token",
		CodexAuthSecretName: "matter-codex-codex-auth-main",
	})
	if err != nil {
		t.Fatalf("StartAgentSession() error = %v", err)
	}
	pod, err := client.CoreV1().Pods("mattermost").Get(context.Background(), started.PodName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get session pod error = %v", err)
	}
	if pod.Spec.ServiceAccountName != "matter-codex-agent-runner-cluster-admin" {
		t.Fatalf("ServiceAccountName = %q", pod.Spec.ServiceAccountName)
	}
	if pod.Spec.AutomountServiceAccountToken == nil || !*pod.Spec.AutomountServiceAccountToken {
		t.Fatal("cluster-admin session pod should automount service account token")
	}
	if got := envValue(pod.Spec.Containers[0].Env, "MATTERCODEX_KUBERNETES_ACCESS"); got != "cluster-admin" {
		t.Fatalf("MATTERCODEX_KUBERNETES_ACCESS = %q", got)
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

func TestCleanupExpiredRunsDryRunDoesNotDelete(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	oldCompleted := metav1.NewTime(now.Add(-48 * time.Hour))
	client := fake.NewSimpleClientset(
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-run-old-run", Namespace: "mattermost", Labels: runnerLabels("old-run", "smoke"), CreationTimestamp: oldCompleted},
			Status:     batchv1.JobStatus{Succeeded: 1, CompletionTime: &oldCompleted},
		},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "mc-ws-old-run", Namespace: "mattermost", Labels: runnerLabels("old-run", "smoke"), CreationTimestamp: oldCompleted}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "mc-prompt-old-run", Namespace: "mattermost", Labels: runnerLabels("old-run", "smoke"), CreationTimestamp: oldCompleted}},
	)
	runner, err := NewRunnerWithClient(client, Config{Namespace: "mattermost"})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	result, err := runner.CleanupExpiredRuns(context.Background(), runtimerepo.RetentionCleanupInput{
		OlderThan: 24 * time.Hour,
		Now:       now,
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("CleanupExpiredRuns() error = %v", err)
	}
	if !result.DryRun || result.RunsMatched != 1 || result.JobsMatched != 1 || result.PVCsMatched != 1 || result.ConfigMapsMatched != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.JobsDeleted != 0 || result.PVCsDeleted != 0 || result.ConfigMapsDeleted != 0 {
		t.Fatalf("dry-run deleted resources: %#v", result)
	}
	if _, err := client.BatchV1().Jobs("mattermost").Get(context.Background(), "mc-run-old-run", metav1.GetOptions{}); err != nil {
		t.Fatalf("dry-run deleted job: %v", err)
	}
}

func TestCleanupExpiredRunsDeletesFinishedAndOrphanResources(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	oldCompleted := metav1.NewTime(now.Add(-48 * time.Hour))
	activeCreated := metav1.NewTime(now.Add(-48 * time.Hour))
	orphanCreated := metav1.NewTime(now.Add(-72 * time.Hour))
	recentCompleted := metav1.NewTime(now.Add(-10 * time.Minute))
	client := fake.NewSimpleClientset(
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-run-old-run", Namespace: "mattermost", Labels: runnerLabels("old-run", "smoke"), CreationTimestamp: oldCompleted},
			Status:     batchv1.JobStatus{Succeeded: 1, CompletionTime: &oldCompleted},
		},
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-run-active-run", Namespace: "mattermost", Labels: runnerLabels("active-run", "smoke"), CreationTimestamp: activeCreated},
			Status:     batchv1.JobStatus{Active: 1},
		},
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-run-recent-run", Namespace: "mattermost", Labels: runnerLabels("recent-run", "smoke"), CreationTimestamp: recentCompleted},
			Status:     batchv1.JobStatus{Succeeded: 1, CompletionTime: &recentCompleted},
		},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "mc-ws-old-run", Namespace: "mattermost", Labels: runnerLabels("old-run", "smoke"), CreationTimestamp: oldCompleted}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "mc-ws-orphan-run", Namespace: "mattermost", Labels: runnerLabels("orphan-run", "smoke"), CreationTimestamp: orphanCreated}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "mc-ws-active-run", Namespace: "mattermost", Labels: runnerLabels("active-run", "smoke"), CreationTimestamp: activeCreated}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "mc-prompt-old-run", Namespace: "mattermost", Labels: runnerLabels("old-run", "smoke"), CreationTimestamp: oldCompleted}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "mc-prompt-orphan-run", Namespace: "mattermost", Labels: runnerLabels("orphan-run", "smoke"), CreationTimestamp: orphanCreated}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "mc-prompt-active-run", Namespace: "mattermost", Labels: runnerLabels("active-run", "smoke"), CreationTimestamp: activeCreated}},
	)
	runner, err := NewRunnerWithClient(client, Config{Namespace: "mattermost"})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	result, err := runner.CleanupExpiredRuns(context.Background(), runtimerepo.RetentionCleanupInput{
		OlderThan: 24 * time.Hour,
		Now:       now,
		DryRun:    false,
	})
	if err != nil {
		t.Fatalf("CleanupExpiredRuns() error = %v", err)
	}
	if result.RunsMatched != 2 || result.JobsDeleted != 1 || result.PVCsDeleted != 2 || result.ConfigMapsDeleted != 2 || result.SkippedActiveJobs != 1 {
		t.Fatalf("result = %#v", result)
	}
	if strings.Join(result.MatchedRunIDs, ",") != "old-run,orphan-run" {
		t.Fatalf("MatchedRunIDs = %#v", result.MatchedRunIDs)
	}
	for _, name := range []string{"mc-run-old-run", "mc-ws-old-run", "mc-ws-orphan-run", "mc-prompt-old-run", "mc-prompt-orphan-run"} {
		if resourceStillExists(ctxResourceGetters{client: client, namespace: "mattermost"}, name) {
			t.Fatalf("%s still exists", name)
		}
	}
	if _, err := client.BatchV1().Jobs("mattermost").Get(context.Background(), "mc-run-active-run", metav1.GetOptions{}); err != nil {
		t.Fatalf("active job was deleted: %v", err)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("mattermost").Get(context.Background(), "mc-ws-active-run", metav1.GetOptions{}); err != nil {
		t.Fatalf("active pvc was deleted: %v", err)
	}
	if _, err := client.BatchV1().Jobs("mattermost").Get(context.Background(), "mc-run-recent-run", metav1.GetOptions{}); err != nil {
		t.Fatalf("recent job was deleted: %v", err)
	}
}

func TestCleanupExpiredRunsDeletesExpiredSessionResources(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	oldTime := metav1.NewTime(now.Add(-48 * time.Hour))
	activeCreated := metav1.NewTime(now.Add(-48 * time.Hour))
	orphanCreated := metav1.NewTime(now.Add(-72 * time.Hour))
	recentFinished := metav1.NewTime(now.Add(-10 * time.Minute))
	client := fake.NewSimpleClientset(
		testSessionPod("old-session", corev1.PodSucceeded, oldTime, oldTime),
		testSessionPVC("old-session", oldTime),
		testSessionSecret("old-session", oldTime),
		testSessionPod("active-session", corev1.PodRunning, activeCreated, metav1.Time{}),
		testSessionPVC("active-session", activeCreated),
		testSessionSecret("active-session", activeCreated),
		testSessionPod("recent-session", corev1.PodSucceeded, activeCreated, recentFinished),
		testSessionPVC("recent-session", activeCreated),
		testSessionSecret("recent-session", activeCreated),
		testSessionPVC("orphan-session", orphanCreated),
		testSessionSecret("orphan-session", orphanCreated),
	)
	runner, err := NewRunnerWithClient(client, Config{Namespace: "mattermost"})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	result, err := runner.CleanupExpiredRuns(context.Background(), runtimerepo.RetentionCleanupInput{
		OlderThan: 24 * time.Hour,
		Now:       now,
		DryRun:    false,
	})
	if err != nil {
		t.Fatalf("CleanupExpiredRuns() error = %v", err)
	}
	if result.SessionPodsDeleted != 1 || result.SessionPVCsDeleted != 2 || result.SessionSecretsDeleted != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.PVCsDeleted != 2 {
		t.Fatalf("PVCsDeleted = %d, want 2", result.PVCsDeleted)
	}
	assertSessionPodMissing(t, client, "old-session")
	assertSessionPVCMissing(t, client, "old-session")
	assertSessionSecretMissing(t, client, "old-session")
	assertSessionPVCMissing(t, client, "orphan-session")
	assertSessionSecretMissing(t, client, "orphan-session")
	assertSessionPodExists(t, client, "active-session")
	assertSessionPVCExists(t, client, "active-session")
	assertSessionSecretExists(t, client, "active-session")
	assertSessionPodExists(t, client, "recent-session")
	assertSessionPVCExists(t, client, "recent-session")
	assertSessionSecretExists(t, client, "recent-session")
}

func testSessionPod(sessionKey string, phase corev1.PodPhase, created metav1.Time, finished metav1.Time) *corev1.Pod {
	status := corev1.PodStatus{Phase: phase}
	if phase == corev1.PodSucceeded || phase == corev1.PodFailed {
		status.ContainerStatuses = []corev1.ContainerStatus{
			{
				Name: "runner",
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{FinishedAt: finished},
				},
			},
		}
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              sessionPodName(sessionKey),
			Namespace:         "mattermost",
			Labels:            sessionLabels(sessionKey, "manager"),
			CreationTimestamp: created,
		},
		Status: status,
	}
}

func testSessionPVC(sessionKey string, created metav1.Time) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:              sessionPVCName(sessionKey),
			Namespace:         "mattermost",
			Labels:            sessionLabels(sessionKey, "manager"),
			CreationTimestamp: created,
		},
	}
}

func testSessionSecret(sessionKey string, created metav1.Time) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sessionSecretName(sessionKey),
			Namespace: "mattermost",
			Labels: map[string]string{
				"app.kubernetes.io/name":      "matter-codex-agent-runner",
				"app.kubernetes.io/component": sessionTokenComponent,
				labelSessionKey:               kubernetesLabelValue(sessionKey),
			},
			CreationTimestamp: created,
		},
	}
}

func assertSessionPodExists(t *testing.T, client *fake.Clientset, sessionKey string) {
	t.Helper()
	if _, err := client.CoreV1().Pods("mattermost").Get(context.Background(), sessionPodName(sessionKey), metav1.GetOptions{}); err != nil {
		t.Fatalf("session pod %s does not exist: %v", sessionKey, err)
	}
}

func assertSessionPodMissing(t *testing.T, client *fake.Clientset, sessionKey string) {
	t.Helper()
	if _, err := client.CoreV1().Pods("mattermost").Get(context.Background(), sessionPodName(sessionKey), metav1.GetOptions{}); err == nil {
		t.Fatalf("session pod %s still exists", sessionKey)
	}
}

func assertSessionPVCExists(t *testing.T, client *fake.Clientset, sessionKey string) {
	t.Helper()
	if _, err := client.CoreV1().PersistentVolumeClaims("mattermost").Get(context.Background(), sessionPVCName(sessionKey), metav1.GetOptions{}); err != nil {
		t.Fatalf("session pvc %s does not exist: %v", sessionKey, err)
	}
}

func assertSessionPVCMissing(t *testing.T, client *fake.Clientset, sessionKey string) {
	t.Helper()
	if _, err := client.CoreV1().PersistentVolumeClaims("mattermost").Get(context.Background(), sessionPVCName(sessionKey), metav1.GetOptions{}); err == nil {
		t.Fatalf("session pvc %s still exists", sessionKey)
	}
}

func assertSessionSecretExists(t *testing.T, client *fake.Clientset, sessionKey string) {
	t.Helper()
	if _, err := client.CoreV1().Secrets("mattermost").Get(context.Background(), sessionSecretName(sessionKey), metav1.GetOptions{}); err != nil {
		t.Fatalf("session secret %s does not exist: %v", sessionKey, err)
	}
}

func assertSessionSecretMissing(t *testing.T, client *fake.Clientset, sessionKey string) {
	t.Helper()
	if _, err := client.CoreV1().Secrets("mattermost").Get(context.Background(), sessionSecretName(sessionKey), metav1.GetOptions{}); err == nil {
		t.Fatalf("session secret %s still exists", sessionKey)
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

func assertRunnerPodSecurity(t *testing.T, podSpec corev1.PodSpec) {
	t.Helper()
	if podSpec.SecurityContext == nil {
		t.Fatal("pod securityContext is nil")
	}
	if podSpec.SecurityContext.RunAsNonRoot == nil || !*podSpec.SecurityContext.RunAsNonRoot {
		t.Fatal("pod should run as non-root")
	}
	if podSpec.SecurityContext.RunAsUser == nil || *podSpec.SecurityContext.RunAsUser != runnerUID {
		t.Fatalf("pod RunAsUser = %v", podSpec.SecurityContext.RunAsUser)
	}
	if podSpec.SecurityContext.RunAsGroup == nil || *podSpec.SecurityContext.RunAsGroup != runnerGID {
		t.Fatalf("pod RunAsGroup = %v", podSpec.SecurityContext.RunAsGroup)
	}
	if podSpec.SecurityContext.FSGroup == nil || *podSpec.SecurityContext.FSGroup != runnerGID {
		t.Fatalf("pod FSGroup = %v", podSpec.SecurityContext.FSGroup)
	}
	if podSpec.SecurityContext.FSGroupChangePolicy == nil || *podSpec.SecurityContext.FSGroupChangePolicy != corev1.FSGroupChangeOnRootMismatch {
		t.Fatalf("pod FSGroupChangePolicy = %v", podSpec.SecurityContext.FSGroupChangePolicy)
	}
	if podSpec.SecurityContext.SeccompProfile == nil || podSpec.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("pod SeccompProfile = %#v", podSpec.SecurityContext.SeccompProfile)
	}
	if len(podSpec.Containers) != 1 {
		t.Fatalf("containers len = %d", len(podSpec.Containers))
	}
	container := podSpec.Containers[0]
	if container.SecurityContext == nil {
		t.Fatal("container securityContext is nil")
	}
	if container.SecurityContext.RunAsNonRoot == nil || !*container.SecurityContext.RunAsNonRoot {
		t.Fatal("container should run as non-root")
	}
	if container.SecurityContext.RunAsUser == nil || *container.SecurityContext.RunAsUser != runnerUID {
		t.Fatalf("container RunAsUser = %v", container.SecurityContext.RunAsUser)
	}
	if container.SecurityContext.RunAsGroup == nil || *container.SecurityContext.RunAsGroup != runnerGID {
		t.Fatalf("container RunAsGroup = %v", container.SecurityContext.RunAsGroup)
	}
	if container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("container should disallow privilege escalation")
	}
	if container.SecurityContext.ReadOnlyRootFilesystem == nil || !*container.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("container should use read-only root filesystem")
	}
	if got := strings.Join(capabilitiesToStrings(container.SecurityContext.Capabilities.Drop), ","); got != "ALL" {
		t.Fatalf("dropped capabilities = %q", got)
	}
	if !hasVolume(podSpec.Volumes, runnerHomeVolume) || !hasVolume(podSpec.Volumes, runnerTmpVolume) {
		t.Fatalf("writable volumes missing: %#v", podSpec.Volumes)
	}
	if !hasVolumeMount(container.VolumeMounts, runnerHomeVolume, runnerHomePath) || !hasVolumeMount(container.VolumeMounts, runnerTmpVolume, runnerTmpPath) {
		t.Fatalf("writable volume mounts missing: %#v", container.VolumeMounts)
	}
}

func capabilitiesToStrings(values []corev1.Capability) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func hasVolume(volumes []corev1.Volume, name string) bool {
	for _, volume := range volumes {
		if volume.Name == name && volume.EmptyDir != nil {
			return true
		}
	}
	return false
}

func hasSecretVolume(volumes []corev1.Volume, name string, secretName string) bool {
	for _, volume := range volumes {
		if volume.Name == name && volume.Secret != nil && volume.Secret.SecretName == secretName {
			return true
		}
	}
	return false
}

func hasVolumeMount(mounts []corev1.VolumeMount, name string, path string) bool {
	for _, mount := range mounts {
		if mount.Name == name && mount.MountPath == path {
			return true
		}
	}
	return false
}

type ctxResourceGetters struct {
	client    *fake.Clientset
	namespace string
}

func resourceStillExists(getters ctxResourceGetters, name string) bool {
	ctx := context.Background()
	if strings.HasPrefix(name, "mc-run-") {
		_, err := getters.client.BatchV1().Jobs(getters.namespace).Get(ctx, name, metav1.GetOptions{})
		return err == nil
	}
	if strings.HasPrefix(name, "mc-ws-") {
		_, err := getters.client.CoreV1().PersistentVolumeClaims(getters.namespace).Get(ctx, name, metav1.GetOptions{})
		return err == nil
	}
	if strings.HasPrefix(name, "mc-prompt-") {
		_, err := getters.client.CoreV1().ConfigMaps(getters.namespace).Get(ctx, name, metav1.GetOptions{})
		return err == nil
	}
	return false
}
