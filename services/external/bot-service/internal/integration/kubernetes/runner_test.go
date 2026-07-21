package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"
)

func TestInspectSecretIntegrityDetectsSameRefMutationWithoutReturningValue(t *testing.T) {
	const secretValue = "synthetic-secret-content"
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "runtime-secret", Namespace: "mattermost", UID: "uid-original", ResourceVersion: "7",
		},
		Data: map[string][]byte{"token": []byte(secretValue)},
	})
	runner, err := NewRunnerWithClient(client, Config{Namespace: "mattermost", WorkspaceStorageSize: "1Gi"})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}
	integrity, err := runner.InspectSecretIntegrity(context.Background(), runtimerepo.SecretIntegrityInput{
		SecretName: "runtime-secret", SecretKey: "token",
	})
	if err != nil {
		t.Fatalf("InspectSecretIntegrity() error = %v", err)
	}
	digest := sha256.Sum256([]byte(secretValue))
	if integrity.ContentSHA256 != hex.EncodeToString(digest[:]) || integrity.UID != "uid-original" || integrity.ResourceVersion != "7" {
		t.Fatalf("integrity = %#v", integrity)
	}
	if strings.Contains(fmt.Sprintf("%#v", integrity), secretValue) {
		t.Fatal("Secret raw value leaked from integrity inspection")
	}
	secret, err := client.CoreV1().Secrets("mattermost").Get(context.Background(), "runtime-secret", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get secret error = %v", err)
	}
	secret.Data["token"] = []byte("different-synthetic-content")
	secret.ResourceVersion = "8"
	if _, err := client.CoreV1().Secrets("mattermost").Update(context.Background(), secret, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("Update secret error = %v", err)
	}
	mutated, err := runner.InspectSecretIntegrity(context.Background(), runtimerepo.SecretIntegrityInput{
		SecretName: "runtime-secret", SecretKey: "token",
	})
	if err != nil {
		t.Fatalf("InspectSecretIntegrity() after mutation error = %v", err)
	}
	if mutated.ContentSHA256 == integrity.ContentSHA256 || mutated.UID != integrity.UID {
		t.Fatalf("same-ref mutation not represented safely: before=%#v after=%#v", integrity, mutated)
	}
}

func TestInspectSecretIntegrityClassifiesMissingSecret(t *testing.T) {
	runner, err := NewRunnerWithClient(fake.NewSimpleClientset(), Config{
		Namespace: "mattermost", WorkspaceStorageSize: "1Gi",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}
	_, err = runner.InspectSecretIntegrity(context.Background(), runtimerepo.SecretIntegrityInput{
		SecretName: "missing-runtime-secret", SecretKey: "token",
	})
	if !errors.Is(err, runtimerepo.ErrSecretNotFound) {
		t.Fatalf("InspectSecretIntegrity() error = %v, want ErrSecretNotFound", err)
	}
}

func TestNewRunnerRejectsSessionMemoryRequestAboveLimit(t *testing.T) {
	_, err := NewRunnerWithClient(fake.NewSimpleClientset(), Config{
		Namespace: "mattermost", SessionMemoryRequest: "2Gi", SessionMemoryLimit: "1Gi",
	})
	if err == nil || !strings.Contains(err.Error(), "session memory request must not exceed") {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}
}

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
	assertRunnerUtilityResources(t, job.Spec.Template.Spec.Containers[0].Resources)
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
	assertRunnerUtilityResources(t, podSpec.Containers[0].Resources)
	if got := podSpec.Containers[0].Args[0]; got != "codex-auth" {
		t.Fatalf("args = %q", got)
	}
	if len(podSpec.Volumes) != 4 {
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
	assertRunnerUtilityResources(t, podSpec.Containers[0].Resources)
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

func TestCheckCodexAuthSecretReturnsInfrastructureErrorOnTimeout(t *testing.T) {
	originalWait := codexAuthSecretCheckWait
	codexAuthSecretCheckWait = time.Millisecond
	t.Cleanup(func() {
		codexAuthSecretCheckWait = originalWait
	})

	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "matter-codex-codex-auth-primary", Namespace: "mattermost"},
		Data:       map[string][]byte{"auth.json": []byte(`{"auth_mode":"chatgpt"}`)},
	})
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
		SecretName:  "matter-codex-codex-auth-primary",
	})
	if err == nil || !strings.Contains(err.Error(), "auth check timed out") {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if result.Ready {
		t.Fatalf("result = %#v", result)
	}
	jobs, listErr := client.BatchV1().Jobs("mattermost").List(context.Background(), metav1.ListOptions{})
	if listErr != nil {
		t.Fatalf("List jobs error = %v", listErr)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("auth check job was not cleaned up: %#v", jobs.Items)
	}
}

func TestCheckCodexAuthSecretReturnsCapacityErrorForUnschedulablePod(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "matter-codex-codex-auth-primary", Namespace: "mattermost"},
		Data:       map[string][]byte{"auth.json": []byte(`{"auth_mode":"chatgpt"}`)},
	})
	client.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		job := action.(k8stesting.CreateAction).GetObject().(*batchv1.Job)
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-pod", Namespace: "mattermost", Labels: job.Spec.Template.Labels},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				Conditions: []corev1.PodCondition{{
					Type: corev1.PodScheduled, Status: corev1.ConditionFalse,
					Reason: corev1.PodReasonUnschedulable, Message: "0/1 nodes are available: 1 Insufficient cpu.",
				}},
			},
		}
		if err := client.Tracker().Add(pod); err != nil {
			return true, nil, err
		}
		return false, nil, nil
	})
	runner, err := NewRunnerWithClient(client, Config{
		Namespace: "mattermost", AgentRunnerImage: "matter-codex-agent-runner:test",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	result, err := runner.CheckCodexAuthSecret(context.Background(), runtimerepo.CodexAuthSecretCheckInput{
		AccountName: "primary", SecretName: "matter-codex-codex-auth-primary",
	})
	if !runtimerepo.IsReclaimableAgentSessionCapacityError(err) || result.PodPhase != string(corev1.PodPending) {
		t.Fatalf("result=%#v error=%v", result, err)
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
		CodexPackage:              "@openai/codex@0.144.1",
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
	assertRunnerSessionResources(t, podSpec.Containers[0].Resources)
	if got := podSpec.Containers[0].Image; got != "matter-codex-agent-runner:test" {
		t.Fatalf("runner image = %q", got)
	}
	if got := podSpec.Containers[0].Command; !slices.Equal(got, runnerCommand()) {
		t.Fatalf("command = %#v", got)
	}
	if got := podSpec.Containers[0].Args[0]; got != "developer" {
		t.Fatalf("args = %q", got)
	}
	if len(podSpec.Volumes) != 7 {
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
		CodexPackage:              "@openai/codex@0.144.1",
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
	assertRunnerSessionResources(t, podSpec.Containers[0].Resources)
	if got := envValue(podSpec.Containers[0].Env, "MATTERCODEX_PR_NUMBER"); got != "12" {
		t.Fatalf("MATTERCODEX_PR_NUMBER = %q", got)
	}
	if len(podSpec.Volumes) != 7 {
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
		CodexPackage:              "@openai/codex@0.144.1",
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
	assertRunnerPodSecurity(t, podSpec)
	assertRunnerSessionResources(t, podSpec.Containers[0].Resources)
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
	if len(podSpec.Volumes) != 7 {
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
	assertRunnerSessionResources(t, podSpec.Containers[0].Resources)
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

func TestSyntheticSecretMatrixDoesNotReachRenderedWorkloadObjects(t *testing.T) {
	secrets := map[string]string{
		"OpenAI":         "mc-sentinel-openai-render-76c8b2a1",
		"GitHub":         "mc-sentinel-github-render-26a517fe",
		"Mattermost":     "mc-sentinel-mattermost-render-19d487b0",
		"Kubernetes":     "mc-sentinel-kubernetes-render-aba3e3c4",
		"PostgreSQL DSN": "postgres://mc-sentinel-postgres-render-8c0777bf@127.0.0.1/disposable",
		"session/MCP":    "mc-sentinel-session-mcp-render-a5fb2298",
	}
	client := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "synthetic-openai", Namespace: "mattermost"},
			Data:       map[string][]byte{"auth.json": []byte(secrets["OpenAI"])},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "synthetic-github", Namespace: "mattermost"},
			Data:       map[string][]byte{"github-token": []byte(secrets["GitHub"]), "github-username": []byte("synthetic"), "github-email": []byte("synthetic@example.invalid")},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "synthetic-runtime", Namespace: "mattermost"},
			Data: map[string][]byte{
				"mattermost": []byte(secrets["Mattermost"]),
				"kubernetes": []byte(secrets["Kubernetes"]),
				"postgres":   []byte(secrets["PostgreSQL DSN"]),
			},
		},
	)
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                 "mattermost",
		AgentRunnerImage:          "matter-codex-agent-runner:test",
		WorkspaceStorageSize:      "1Gi",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}
	runtimeEnv := []runtimerepo.RuntimeEnvVar{
		{Name: "SYNTHETIC_MATTERMOST_TOKEN", SecretName: "synthetic-runtime", SecretKey: "mattermost", Sensitive: true},
		{Name: "SYNTHETIC_KUBERNETES_TOKEN", SecretName: "synthetic-runtime", SecretKey: "kubernetes", Sensitive: true},
		{Name: "MATTERCODEX_DATABASE_DSN", SecretName: "synthetic-runtime", SecretKey: "postgres", Sensitive: true},
	}
	started, err := runner.StartAgentSession(context.Background(), runtimerepo.AgentSessionPodInput{
		SessionKey: "secret-matrix", Role: "developer", BotServiceURL: "http://bot-service",
		InternalToken: secrets["session/MCP"], CodexAuthSecretName: "synthetic-openai",
		GitHubSecretName: "synthetic-github", RuntimeEnv: runtimeEnv,
	})
	if err != nil {
		t.Fatalf("StartAgentSession() error = %v", err)
	}
	pod, err := client.CoreV1().Pods("mattermost").Get(context.Background(), started.PodName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get session pod error = %v", err)
	}
	renderedYAML, err := yaml.Marshal(pod)
	if err != nil {
		t.Fatalf("yaml.Marshal(pod) error = %v", err)
	}
	for class, value := range secrets {
		if strings.Contains(string(renderedYAML), value) {
			t.Fatalf("отрендерованный Pod содержит значение класса %s", class)
		}
	}
	if !strings.Contains(string(renderedYAML), "synthetic-runtime") ||
		!strings.Contains(string(renderedYAML), "mc-session-token-secret-matrix") ||
		!strings.Contains(string(renderedYAML), "secretKeyRef") {
		t.Fatal("отрендерованный Pod не сохранил ссылки на Secret и ключи")
	}
	sessionSecret, err := client.CoreV1().Secrets("mattermost").Get(context.Background(), started.SecretName, metav1.GetOptions{})
	if err != nil || string(sessionSecret.Data["token"]) != secrets["session/MCP"] {
		t.Fatalf("synthetic session Secret не материализован только в Secret: error=%v", err)
	}
}

func TestStartAgentSessionFrozenTokenSecretUsesImmutableVersionBinding(t *testing.T) {
	const token = "synthetic-frozen-session-token"
	const sessionKey = "frozen-session"
	const secretName = "mc-session-token-frozen-session"
	digest := sha256.Sum256([]byte(token))
	client := fake.NewSimpleClientset()
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                 "mattermost",
		AgentRunnerImage:          "matter-codex-agent-runner:test",
		WorkspaceStorageSize:      "1Gi",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}
	input := runtimerepo.AgentSessionPodInput{
		SessionKey: sessionKey, Role: "mattercodex-admin", KubernetesAccess: "cluster-admin",
		BotServiceURL: "http://bot-service", InternalToken: token, CodexAuthSecretName: "matter-codex-codex-auth-main",
		TokenSecretIntegrity: &runtimerepo.SecretIntegrity{
			SecretName: secretName, SecretKey: "token", ContentSHA256: hex.EncodeToString(digest[:]),
			UID: "frozen-secret-uid", ResourceVersion: "17",
		},
	}
	input.PodTokenSecretName = immutableSessionTokenSecretName(input.SessionKey, *input.TokenSecretIntegrity)
	if _, err := client.CoreV1().Secrets("mattermost").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "mattermost", UID: "frozen-secret-uid", ResourceVersion: "17"},
		Data:       map[string][]byte{"token": []byte(token)},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create secret fixture: %v", err)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("mattermost").Create(context.Background(), runner.sessionPVC(sessionKey, input.Role), metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pvc fixture: %v", err)
	}
	if _, err := client.CoreV1().Pods("mattermost").Create(context.Background(), runner.sessionPod(input), metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod fixture: %v", err)
	}
	client.ClearActions()
	if _, err := runner.StartAgentSession(context.Background(), input); err != nil {
		t.Fatalf("StartAgentSession() error = %v", err)
	}
	for _, action := range client.Actions() {
		if action.GetResource().Resource == "secrets" && action.GetVerb() != "get" && action.GetVerb() != "create" {
			t.Fatalf("frozen token secret received a non-create mutation: %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
	secret, err := client.CoreV1().Secrets("mattermost").Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get frozen token secret: %v", err)
	}
	if secret.ResourceVersion != "17" || string(secret.Data["token"]) != token {
		t.Fatalf("frozen token secret changed: resourceVersion=%q", secret.ResourceVersion)
	}
	versioned, err := client.CoreV1().Secrets("mattermost").Get(context.Background(), input.PodTokenSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get immutable token binding: %v", err)
	}
	if versioned.Immutable == nil || !*versioned.Immutable || string(versioned.Data["token"]) != token {
		t.Fatal("immutable token binding was not materialized exactly")
	}
	pod, err := client.CoreV1().Pods("mattermost").Get(context.Background(), sessionPodName(sessionKey), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get session pod: %v", err)
	}
	if !podUsesSessionTokenSecret(pod, input.PodTokenSecretName) {
		t.Fatal("session pod does not use immutable token binding")
	}
}

func TestStartAgentSessionFrozenTokenSecretDriftFailsBeforeEffects(t *testing.T) {
	const token = "synthetic-frozen-session-token"
	digest := sha256.Sum256([]byte(token))
	expected := runtimerepo.SecretIntegrity{
		SecretName: "mc-session-token-frozen-session", SecretKey: "token", ContentSHA256: hex.EncodeToString(digest[:]),
		UID: "frozen-secret-uid", ResourceVersion: "17",
	}
	tests := []struct {
		name      string
		secret    *corev1.Secret
		readError bool
	}{
		{name: "missing"},
		{name: "read error", readError: true},
		{name: "content", secret: frozenTokenSecretFixture("different-token", "frozen-secret-uid", "17")},
		{name: "uid", secret: frozenTokenSecretFixture(token, "different-uid", "17")},
		{name: "resource version", secret: frozenTokenSecretFixture(token, "frozen-secret-uid", "18")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			if test.secret != nil {
				if _, err := client.CoreV1().Secrets("mattermost").Create(context.Background(), test.secret, metav1.CreateOptions{}); err != nil {
					t.Fatalf("create secret fixture: %v", err)
				}
			}
			if test.readError {
				client.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("synthetic secret read failure")
				})
			}
			runner, err := NewRunnerWithClient(client, Config{Namespace: "mattermost", WorkspaceStorageSize: "1Gi"})
			if err != nil {
				t.Fatalf("NewRunnerWithClient() error = %v", err)
			}
			client.ClearActions()
			_, err = runner.StartAgentSession(context.Background(), runtimerepo.AgentSessionPodInput{
				SessionKey: "frozen-session", Role: "mattercodex-admin", KubernetesAccess: "cluster-admin",
				BotServiceURL: "http://bot-service", InternalToken: token, TokenSecretIntegrity: &expected,
			})
			if err == nil {
				t.Fatal("StartAgentSession() error = nil")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" || action.GetResource().Resource != "secrets" {
					t.Fatalf("drift produced an effect: %s %s", action.GetVerb(), action.GetResource().Resource)
				}
			}
		})
	}
}

func TestStartAgentSessionFrozenTokenSecretBoundaryInterleavingsFailClosed(t *testing.T) {
	tests := []struct {
		name          string
		seedOldPod    bool
		install       func(*testing.T, *fake.Clientset, runtimerepo.AgentSessionPodInput)
		forbidActions func(k8stesting.Action) bool
	}{
		{
			name: "after initial guard before pvc",
			install: func(t *testing.T, client *fake.Clientset, input runtimerepo.AgentSessionPodInput) {
				originalGets := 0
				client.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
					if action.(k8stesting.GetAction).GetName() != input.TokenSecretIntegrity.SecretName {
						return false, nil, nil
					}
					originalGets++
					if originalGets != 1 {
						return false, nil, nil
					}
					original := mustTrackedSecret(t, client, input.TokenSecretIntegrity.SecretName).DeepCopy()
					replaceTrackedSecret(t, client, frozenTokenSecretFixture("synthetic-replacement-token", "replacement-uid", "18"))
					return true, original, nil
				})
			},
			forbidActions: func(action k8stesting.Action) bool {
				return action.GetResource().Resource != "secrets" || action.GetVerb() != "get"
			},
		},
		{
			name: "after pvc mutation",
			install: func(t *testing.T, client *fake.Clientset, _ runtimerepo.AgentSessionPodInput) {
				client.PrependReactor("create", "persistentvolumeclaims", func(action k8stesting.Action) (bool, runtime.Object, error) {
					created := action.(k8stesting.CreateAction).GetObject().DeepCopyObject()
					if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("persistentvolumeclaims"), created, "mattermost"); err != nil {
						t.Fatalf("track pvc create: %v", err)
					}
					replaceTrackedSecret(t, client, frozenTokenSecretFixture("synthetic-replacement-token", "replacement-uid", "18"))
					return true, created, nil
				})
			},
			forbidActions: func(action k8stesting.Action) bool {
				return action.GetResource().Resource == "pods"
			},
		},
		{
			name: "after immutable secret materialization with same content new identity",
			install: func(t *testing.T, client *fake.Clientset, input runtimerepo.AgentSessionPodInput) {
				client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
					created := action.(k8stesting.CreateAction).GetObject().DeepCopyObject()
					if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), created, "mattermost"); err != nil {
						t.Fatalf("track immutable secret create: %v", err)
					}
					replaceTrackedSecret(t, client, frozenTokenSecretFixture(input.InternalToken, "replacement-uid", "18"))
					return true, created, nil
				})
			},
			forbidActions: func(action k8stesting.Action) bool {
				return action.GetResource().Resource == "persistentvolumeclaims" || (action.GetResource().Resource == "pods" && (action.GetVerb() == "create" || action.GetVerb() == "delete"))
			},
		},
		{
			name: "immutable binding metadata drift",
			install: func(t *testing.T, client *fake.Clientset, _ runtimerepo.AgentSessionPodInput) {
				client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
					created := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret).DeepCopy()
					tracked := created.DeepCopy()
					tracked.Labels[labelSessionKey] = "different-session"
					if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), tracked, "mattermost"); err != nil {
						t.Fatalf("track immutable secret create: %v", err)
					}
					return true, created, nil
				})
			},
			forbidActions: func(action k8stesting.Action) bool {
				return action.GetResource().Resource == "persistentvolumeclaims" || action.GetResource().Resource == "pods"
			},
		},
		{
			name:       "before old pod delete after a to b to a",
			seedOldPod: true,
			install: func(t *testing.T, client *fake.Clientset, input runtimerepo.AgentSessionPodInput) {
				client.PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
					pod, err := client.Tracker().Get(corev1.SchemeGroupVersion.WithResource("pods"), "mattermost", action.(k8stesting.GetAction).GetName())
					if err != nil {
						return true, nil, err
					}
					replaceTrackedSecret(t, client, frozenTokenSecretFixture("synthetic-replacement-token", "replacement-b-uid", "18"))
					replaceTrackedSecret(t, client, frozenTokenSecretFixture(input.InternalToken, "replacement-a-uid", "19"))
					return true, pod.DeepCopyObject(), nil
				})
			},
			forbidActions: func(action k8stesting.Action) bool {
				return action.GetResource().Resource == "pods" && (action.GetVerb() == "create" || action.GetVerb() == "delete")
			},
		},
		{
			name:       "after old pod delete",
			seedOldPod: true,
			install: func(t *testing.T, client *fake.Clientset, _ runtimerepo.AgentSessionPodInput) {
				client.PrependReactor("delete", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
					if err := client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "mattermost", action.(k8stesting.DeleteAction).GetName()); err != nil {
						return true, nil, err
					}
					replaceTrackedSecret(t, client, frozenTokenSecretFixture("synthetic-replacement-token", "replacement-uid", "18"))
					return true, nil, nil
				})
			},
			forbidActions: func(action k8stesting.Action) bool {
				return action.GetResource().Resource == "pods" && action.GetVerb() == "create"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, runner, input := frozenSessionBoundaryFixture(t)
			if test.seedOldPod {
				oldInput := input
				oldInput.PodTokenSecretName = input.TokenSecretIntegrity.SecretName
				if _, err := client.CoreV1().Pods("mattermost").Create(context.Background(), runner.sessionPod(oldInput), metav1.CreateOptions{}); err != nil {
					t.Fatalf("seed old pod: %v", err)
				}
			}
			test.install(t, client, input)
			client.ClearActions()
			if _, err := runner.StartAgentSession(context.Background(), input); err == nil {
				t.Fatal("StartAgentSession() accepted Secret identity drift")
			}
			for _, action := range client.Actions() {
				if test.forbidActions(action) {
					t.Fatalf("drift crossed a protected boundary: %s %s", action.GetVerb(), action.GetResource().Resource)
				}
			}
		})
	}
}

func TestStartAgentSessionFrozenTokenSecretPodCreateUsesVersionBindingAcrossReplacement(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *fake.Clientset, runtimerepo.AgentSessionPodInput)
	}{
		{
			name: "replacement token",
			mutate: func(t *testing.T, client *fake.Clientset, _ runtimerepo.AgentSessionPodInput) {
				replaceTrackedSecret(t, client, frozenTokenSecretFixture("synthetic-replacement-token", "replacement-uid", "18"))
			},
		},
		{
			name: "a to b to a",
			mutate: func(t *testing.T, client *fake.Clientset, input runtimerepo.AgentSessionPodInput) {
				replaceTrackedSecret(t, client, frozenTokenSecretFixture("synthetic-replacement-token", "replacement-b-uid", "18"))
				replaceTrackedSecret(t, client, frozenTokenSecretFixture(input.InternalToken, "replacement-a-uid", "19"))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, runner, input := frozenSessionBoundaryFixture(t)
			client.PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
				pod := action.(k8stesting.CreateAction).GetObject().(*corev1.Pod).DeepCopy()
				expectedName := immutableSessionTokenSecretName(input.SessionKey, *input.TokenSecretIntegrity)
				if !podUsesSessionTokenSecret(pod, expectedName) {
					t.Fatal("pod create did not use the immutable Secret identity")
				}
				versioned := mustTrackedSecret(t, client, expectedName)
				if versioned.Immutable == nil || !*versioned.Immutable || string(versioned.Data["token"]) != input.InternalToken {
					t.Fatal("immutable Secret does not retain the verified token")
				}
				test.mutate(t, client, input)
				if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), pod, "mattermost"); err != nil {
					t.Fatalf("track pod create: %v", err)
				}
				return true, pod, nil
			})
			started, err := runner.StartAgentSession(context.Background(), input)
			if err != nil {
				t.Fatalf("StartAgentSession() error = %v", err)
			}
			pod, err := client.CoreV1().Pods("mattermost").Get(context.Background(), started.PodName, metav1.GetOptions{})
			if err != nil || !podUsesSessionTokenSecret(pod, immutableSessionTokenSecretName(input.SessionKey, *input.TokenSecretIntegrity)) {
				t.Fatal("created pod lost the immutable Secret binding")
			}
		})
	}
}

func TestImmutableSessionTokenSecretExactCollisionAndBoundaryMatrix(t *testing.T) {
	_, _, input := frozenSessionBoundaryFixture(t)
	expected := *input.TokenSecretIntegrity
	name := immutableSessionTokenSecretName(input.SessionKey, expected)
	immutable := true
	created := metav1.NewTime(time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC))
	canonical := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "mattermost", UID: "server-uid", ResourceVersion: "23", Generation: 1,
			CreationTimestamp: created,
			ManagedFields:     []metav1.ManagedFieldsEntry{{Manager: "kube-apiserver", Operation: metav1.ManagedFieldsOperationUpdate}},
			Labels: map[string]string{
				"app.kubernetes.io/name": "matter-codex-agent-runner", "app.kubernetes.io/component": sessionTokenComponent,
				labelSessionKey: kubernetesLabelValue(input.SessionKey),
			},
			Finalizers: []string{sessionTokenFinalizer},
		},
		Immutable: &immutable,
		Type:      corev1.SecretTypeOpaque,
		Data:      map[string][]byte{"token": []byte(input.InternalToken)},
	}
	if !exactManagedSessionTokenSecret(canonical, "mattermost", name, kubernetesLabelValue(input.SessionKey), []byte(input.InternalToken), true, true) {
		t.Fatal("legitimate API-defaulted Secret was not accepted")
	}

	mutations := []struct {
		name   string
		mutate func(*corev1.Secret)
	}{
		{name: "wrong name", mutate: func(secret *corev1.Secret) { secret.Name += "-other" }},
		{name: "wrong namespace", mutate: func(secret *corev1.Secret) { secret.Namespace = "other" }},
		{name: "generate name", mutate: func(secret *corev1.Secret) { secret.GenerateName = "unexpected-" }},
		{name: "missing app name label", mutate: func(secret *corev1.Secret) { delete(secret.Labels, "app.kubernetes.io/name") }},
		{name: "extra label", mutate: func(secret *corev1.Secret) { secret.Labels["unexpected"] = "label" }},
		{name: "wrong session tuple", mutate: func(secret *corev1.Secret) { secret.Labels[labelSessionKey] = "other-session" }},
		{name: "extra annotation", mutate: func(secret *corev1.Secret) { secret.Annotations = map[string]string{"unexpected": "annotation"} }},
		{name: "owner reference", mutate: func(secret *corev1.Secret) {
			secret.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "foreign", UID: "foreign-uid"}}
		}},
		{name: "wrong type", mutate: func(secret *corev1.Secret) { secret.Type = corev1.SecretTypeTLS }},
		{name: "immutable missing", mutate: func(secret *corev1.Secret) { secret.Immutable = nil }},
		{name: "immutable false", mutate: func(secret *corev1.Secret) {
			value := false
			secret.Immutable = &value
		}},
		{name: "wrong content", mutate: func(secret *corev1.Secret) { secret.Data["token"] = []byte("different") }},
		{name: "extra data", mutate: func(secret *corev1.Secret) { secret.Data["unexpected"] = []byte("value") }},
		{name: "string data", mutate: func(secret *corev1.Secret) { secret.StringData = map[string]string{"unexpected": "value"} }},
		{name: "missing finalizer", mutate: func(secret *corev1.Secret) { secret.Finalizers = nil }},
		{name: "extra finalizer", mutate: func(secret *corev1.Secret) { secret.Finalizers = append(secret.Finalizers, "foreign/finalizer") }},
		{name: "deleting", mutate: func(secret *corev1.Secret) {
			deleted := metav1.NewTime(time.Date(2026, 7, 18, 12, 1, 0, 0, time.UTC))
			secret.DeletionTimestamp = &deleted
		}},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			mutated := canonical.DeepCopy()
			test.mutate(mutated)
			if exactManagedSessionTokenSecret(mutated, "mattermost", name, kubernetesLabelValue(input.SessionKey), []byte(input.InternalToken), true, true) {
				t.Fatal("non-canonical Secret was accepted by the exact validator")
			}
			client := fake.NewSimpleClientset(frozenTokenSecretFixture(input.InternalToken, expected.UID, expected.ResourceVersion), canonical.DeepCopy())
			client.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
				if action.(k8stesting.GetAction).GetName() != name {
					return false, nil, nil
				}
				return true, mutated.DeepCopy(), nil
			})
			runner, err := NewRunnerWithClient(client, Config{Namespace: "mattermost", WorkspaceStorageSize: "1Gi"})
			if err != nil {
				t.Fatalf("NewRunnerWithClient() error = %v", err)
			}
			if _, err := runner.materializeImmutableSessionTokenSecret(context.Background(), input.SessionKey, input.InternalToken, expected); err == nil {
				t.Fatal("AlreadyExists collision accepted a non-canonical Secret")
			}
			if err := runner.verifyFrozenSessionTokenBoundary(context.Background(), input.SessionKey, expected.SecretName, name, input.InternalToken, expected); err == nil {
				t.Fatal("subsequent boundary accepted a non-canonical Secret")
			}
		})
	}

	client := fake.NewSimpleClientset(frozenTokenSecretFixture(input.InternalToken, expected.UID, expected.ResourceVersion), canonical.DeepCopy())
	runner, err := NewRunnerWithClient(client, Config{Namespace: "mattermost", WorkspaceStorageSize: "1Gi"})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}
	if got, err := runner.materializeImmutableSessionTokenSecret(context.Background(), input.SessionKey, input.InternalToken, expected); err != nil || got != name {
		t.Fatalf("legitimate API-defaulted collision result=%q error=%v", got, err)
	}
	if err := runner.verifyFrozenSessionTokenBoundary(context.Background(), input.SessionKey, expected.SecretName, name, input.InternalToken, expected); err != nil {
		t.Fatalf("legitimate API-defaulted boundary error = %v", err)
	}
}

func frozenSessionBoundaryFixture(t *testing.T) (*fake.Clientset, *Runner, runtimerepo.AgentSessionPodInput) {
	t.Helper()
	const token = "synthetic-frozen-session-token"
	digest := sha256.Sum256([]byte(token))
	expected := runtimerepo.SecretIntegrity{
		SecretName: "mc-session-token-frozen-session", SecretKey: "token", ContentSHA256: hex.EncodeToString(digest[:]),
		UID: "frozen-secret-uid", ResourceVersion: "17",
	}
	client := fake.NewSimpleClientset(frozenTokenSecretFixture(token, expected.UID, expected.ResourceVersion))
	runner, err := NewRunnerWithClient(client, Config{
		Namespace: "mattermost", AgentRunnerImage: "matter-codex-agent-runner:test", WorkspaceStorageSize: "1Gi",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}
	return client, runner, runtimerepo.AgentSessionPodInput{
		SessionKey: "frozen-session", Role: "mattercodex-admin", KubernetesAccess: "cluster-admin",
		BotServiceURL: "http://bot-service", InternalToken: token, CodexAuthSecretName: "matter-codex-codex-auth-main", TokenSecretIntegrity: &expected,
	}
}

func mustTrackedSecret(t *testing.T, client *fake.Clientset, name string) *corev1.Secret {
	t.Helper()
	object, err := client.Tracker().Get(corev1.SchemeGroupVersion.WithResource("secrets"), "mattermost", name)
	if err != nil {
		t.Fatalf("get tracked Secret: %v", err)
	}
	return object.(*corev1.Secret)
}

func replaceTrackedSecret(t *testing.T, client *fake.Clientset, secret *corev1.Secret) {
	t.Helper()
	resource := corev1.SchemeGroupVersion.WithResource("secrets")
	_ = client.Tracker().Delete(resource, "mattermost", secret.Name)
	if err := client.Tracker().Create(resource, secret.DeepCopy(), "mattermost"); err != nil {
		t.Fatalf("replace tracked Secret: %v", err)
	}
}

func frozenTokenSecretFixture(token string, uid string, resourceVersion string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mc-session-token-frozen-session", Namespace: "mattermost", UID: types.UID(uid), ResourceVersion: resourceVersion,
		},
		Data: map[string][]byte{"token": []byte(token)},
	}
}

func TestStartAgentSessionReturnsTypedCapacityErrorWhenQuotaRejectsPod(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Resource: "pods"},
			"mc-session-capacity-test",
			fmt.Errorf("exceeded quota: matter-codex-runtime-quota, requested: limits.memory=16Gi"),
		)
	})
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                 "mattermost",
		AgentRunnerImage:          "matter-codex-agent-runner:test",
		WorkspaceStorageSize:      "1Gi",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	_, err = runner.StartAgentSession(context.Background(), runtimerepo.AgentSessionPodInput{
		SessionKey:          "capacity-test",
		Role:                "developer",
		BotServiceURL:       "http://bot-service",
		InternalToken:       "session-token",
		CodexAuthSecretName: "matter-codex-codex-auth-main",
	})
	if !runtimerepo.IsAgentSessionCapacityError(err) {
		t.Fatalf("StartAgentSession() error = %v, want capacity error", err)
	}
}

func TestStartAgentSessionReturnsTypedCapacityErrorWithoutCleanupWhenPVCQuotaRejected(t *testing.T) {
	oldTime := metav1.NewTime(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
	client := fake.NewSimpleClientset(
		testSessionPVC("retained-session", oldTime),
		testSessionSecret("retained-session", oldTime),
	)
	client.PrependReactor("create", "persistentvolumeclaims", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Resource: "persistentvolumeclaims"},
			action.(k8stesting.CreateAction).GetObject().(*corev1.PersistentVolumeClaim).Name,
			fmt.Errorf("exceeded quota: matter-codex-runtime-quota, requested: requests.storage=1Gi"),
		)
	})
	runner, err := NewRunnerWithClient(client, Config{
		Namespace:                 "mattermost",
		AgentRunnerImage:          "matter-codex-agent-runner:test",
		WorkspaceStorageSize:      "1Gi",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	client.ClearActions()
	_, err = runner.StartAgentSession(context.Background(), runtimerepo.AgentSessionPodInput{
		SessionKey:          "capacity-pvc-test",
		Role:                "developer",
		BotServiceURL:       "http://bot-service",
		InternalToken:       "synthetic-session-token",
		CodexAuthSecretName: "matter-codex-codex-auth-main",
	})
	if !runtimerepo.IsAgentSessionCapacityError(err) {
		t.Fatalf("StartAgentSession() error = %v, want capacity error", err)
	}
	var capacityErr *runtimerepo.AgentSessionCapacityError
	if !errors.As(err, &capacityErr) || capacityErr.Cause == nil {
		t.Fatalf("StartAgentSession() error = %#v, want typed capacity error with cause", err)
	}
	if capacityErr.Kind != runtimerepo.AgentSessionCapacityKindSessionPVCQuota || runtimerepo.IsReclaimableAgentSessionCapacityError(err) {
		t.Fatalf("StartAgentSession() capacity error = %#v, want non-reclaimable session PVC quota", capacityErr)
	}
	pvcCreates := 0
	for _, action := range client.Actions() {
		resource := action.GetResource().Resource
		if resource == "persistentvolumeclaims" && action.GetVerb() == "create" {
			pvcCreates++
		}
		if (resource == "persistentvolumeclaims" || resource == "secrets") && (action.GetVerb() == "delete" || action.GetVerb() == "patch") {
			t.Fatalf("quota path mutated retained session data: %s %s", action.GetVerb(), resource)
		}
	}
	if pvcCreates != 1 {
		t.Fatalf("session PVC create attempts = %d, want 1", pvcCreates)
	}
	assertSessionPVCExists(t, client, "retained-session")
	assertSessionSecretExists(t, client, "retained-session")
}

func TestSessionPodSchedulingCapacityErrorRecognizesInsufficientMemory(t *testing.T) {
	podName := "mc-session-scheduling-test"
	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: "mattermost"},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  corev1.PodReasonUnschedulable,
			Message: "0/1 nodes are available: 1 Insufficient memory.",
		}}},
	})
	runner, err := NewRunnerWithClient(client, Config{Namespace: "mattermost"})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}

	err = runner.sessionPodSchedulingCapacityError(context.Background(), podName)
	if !runtimerepo.IsAgentSessionCapacityError(err) {
		t.Fatalf("sessionPodSchedulingCapacityError() error = %v, want capacity error", err)
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

func TestCleanupExpiredRunsInventoriesSessionDataWithoutDeleting(t *testing.T) {
	protectedSecretName := "mc-session-token-" + strings.Repeat("a", 40)
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	oldTime := metav1.NewTime(now.Add(-48 * time.Hour))
	activeCreated := metav1.NewTime(now.Add(-48 * time.Hour))
	orphanCreated := metav1.NewTime(now.Add(-72 * time.Hour))
	recentFinished := metav1.NewTime(now.Add(-10 * time.Minute))
	immutable := true
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
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: protectedSecretName, Namespace: "mattermost", UID: "protected-secret-uid", ResourceVersion: "17",
				Labels: map[string]string{
					"app.kubernetes.io/name": "matter-codex-agent-runner", "app.kubernetes.io/component": sessionTokenComponent,
					labelSessionKey: kubernetesLabelValue("old-session"),
				},
				Finalizers: []string{sessionTokenFinalizer}, CreationTimestamp: oldTime,
			},
			Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"token": []byte("synthetic-protected-cleanup-token")},
		},
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
	if result.SessionDataMode != runtimerepo.SessionDataRetentionModeInventoryOnly || result.SessionPodsDeleted != 1 ||
		result.SessionPVCsMatched != 4 || result.SessionSecretsMatched != 5 || result.SessionPVCsDeleted != 0 || result.SessionSecretsDeleted != 0 {
		t.Fatalf("result = %#v", result)
	}
	if result.PVCsMatched != 0 || result.PVCsDeleted != 0 {
		t.Fatalf("session inventory leaked into deletable PVC counters: %#v", result)
	}
	assertSessionPodMissing(t, client, "old-session")
	for _, sessionKey := range []string{"old-session", "active-session", "recent-session", "orphan-session"} {
		assertSessionPVCExists(t, client, sessionKey)
		assertSessionSecretExists(t, client, sessionKey)
	}
	if _, err := client.CoreV1().Secrets("mattermost").Get(context.Background(), protectedSecretName, metav1.GetOptions{}); err != nil {
		t.Fatalf("protected immutable session Secret was not retained: %v", err)
	}
	for _, action := range client.Actions() {
		resource := action.GetResource().Resource
		if (resource == "persistentvolumeclaims" || resource == "secrets") && (action.GetVerb() == "delete" || action.GetVerb() == "patch") {
			t.Fatalf("retention mutated session data: %s %s", action.GetVerb(), resource)
		}
	}
	assertSessionPodExists(t, client, "active-session")
	assertSessionPodExists(t, client, "recent-session")
	for _, diagnostic := range result.SessionDiagnostics {
		if len(diagnostic.Reasons) == 0 || diagnostic.Reasons[0] != runtimerepo.SessionRetentionReasonContainment ||
			!slices.Contains(diagnostic.Reasons, runtimerepo.SessionRetentionReasonUnknownDB) ||
			!slices.Contains(diagnostic.Reasons, runtimerepo.SessionRetentionReasonUnknownS3) {
			t.Fatalf("unsafe session diagnostic = %#v", diagnostic)
		}
		if slices.Contains(diagnostic.Reasons, runtimerepo.SessionRetentionReasonNoArchive) {
			t.Fatalf("inventory falsely asserted missing archive: %#v", diagnostic)
		}
	}
}

func TestCleanupExpiredRunsRepeatedInventoryDoesNotMutateResources(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	oldTime := metav1.NewTime(now.Add(-72 * time.Hour))
	client := fake.NewSimpleClientset(testSessionPVC("orphan-session", oldTime), testSessionSecret("orphan-session", oldTime))
	runner, err := NewRunnerWithClient(client, Config{Namespace: "mattermost"})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}
	input := runtimerepo.RetentionCleanupInput{OlderThan: 24 * time.Hour, Now: now, DryRun: true}
	first, err := runner.CleanupExpiredRuns(context.Background(), input)
	if err != nil {
		t.Fatalf("first CleanupExpiredRuns() error = %v", err)
	}
	client.ClearActions()
	second, err := runner.CleanupExpiredRuns(context.Background(), input)
	if err != nil {
		t.Fatalf("second CleanupExpiredRuns() error = %v", err)
	}
	if fmt.Sprintf("%#v", first.SessionDiagnostics) != fmt.Sprintf("%#v", second.SessionDiagnostics) ||
		first.SessionPVCsMatched != second.SessionPVCsMatched || first.SessionSecretsMatched != second.SessionSecretsMatched {
		t.Fatalf("repeated inventory changed result: first=%#v second=%#v", first, second)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "list" {
			t.Fatalf("repeated inventory mutated resource: %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
	assertSessionPVCExists(t, client, "orphan-session")
	assertSessionSecretExists(t, client, "orphan-session")
}

func TestCleanupExpiredRunsDoesNotDeleteReplacementSessionPod(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	oldTime := metav1.NewTime(now.Add(-72 * time.Hour))
	listedPod := testSessionPod("replaced-session", corev1.PodSucceeded, oldTime, oldTime)
	listedPod.UID = types.UID("listed-pod-uid")
	listedPod.ResourceVersion = "7"
	replacementPod := testSessionPod("replaced-session", corev1.PodRunning, metav1.NewTime(now), metav1.Time{})
	replacementPod.UID = types.UID("replacement-pod-uid")
	replacementPod.ResourceVersion = "8"
	client := fake.NewSimpleClientset(listedPod)
	interleaved := false
	client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		if interleaved {
			return false, nil, nil
		}
		interleaved = true
		resource := corev1.SchemeGroupVersion.WithResource("pods")
		if err := client.Tracker().Delete(resource, "mattermost", listedPod.Name); err != nil {
			return true, nil, err
		}
		if err := client.Tracker().Add(replacementPod.DeepCopy()); err != nil {
			return true, nil, err
		}
		return true, &corev1.PodList{Items: []corev1.Pod{*listedPod.DeepCopy()}}, nil
	})
	deleteCalls := 0
	client.PrependReactor("delete", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		deleteCalls++
		deleteAction, ok := action.(k8stesting.DeleteAction)
		if !ok {
			return true, nil, fmt.Errorf("delete action has type %T", action)
		}
		preconditions := deleteAction.GetDeleteOptions().Preconditions
		if preconditions == nil || preconditions.UID == nil || *preconditions.UID != listedPod.UID ||
			preconditions.ResourceVersion == nil || *preconditions.ResourceVersion != listedPod.ResourceVersion {
			return true, nil, fmt.Errorf("delete preconditions = %#v, want UID %s and resourceVersion %s", preconditions, listedPod.UID, listedPod.ResourceVersion)
		}
		return true, nil, apierrors.NewConflict(
			schema.GroupResource{Resource: "pods"},
			listedPod.Name,
			fmt.Errorf("UID precondition does not match replacement"),
		)
	})
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
	if !interleaved || deleteCalls != 1 || result.SessionPodsMatched != 1 || result.SessionPodsDeleted != 0 {
		t.Fatalf("result = %#v, interleaved=%t deleteCalls=%d", result, interleaved, deleteCalls)
	}
	remaining, err := client.CoreV1().Pods("mattermost").Get(context.Background(), replacementPod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("replacement session pod was deleted: %v", err)
	}
	if remaining.UID != replacementPod.UID || remaining.Status.Phase != corev1.PodRunning {
		t.Fatalf("replacement session pod = %#v", remaining)
	}
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
			UID:               types.UID("uid-" + kubernetesLabelValue(sessionKey)),
			ResourceVersion:   "1",
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
			Name:            sessionSecretName(sessionKey),
			Namespace:       "mattermost",
			UID:             types.UID("uid-" + kubernetesLabelValue(sessionKey)),
			ResourceVersion: "1",
			Labels: map[string]string{
				"app.kubernetes.io/name":      "matter-codex-agent-runner",
				"app.kubernetes.io/component": sessionTokenComponent,
				labelSessionKey:               kubernetesLabelValue(sessionKey),
			},
			CreationTimestamp: created,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"token": []byte("synthetic-session-token")},
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

func assertSessionSecretExists(t *testing.T, client *fake.Clientset, sessionKey string) {
	t.Helper()
	if _, err := client.CoreV1().Secrets("mattermost").Get(context.Background(), sessionSecretName(sessionKey), metav1.GetOptions{}); err != nil {
		t.Fatalf("session secret %s does not exist: %v", sessionKey, err)
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
	if !hasVolume(podSpec.Volumes, runnerHomeVolume) || !hasVolume(podSpec.Volumes, runnerTmpVolume) || !hasMemoryVolumeWithSize(podSpec.Volumes, runnerDevShmVolume, runnerDevShmSizeLimit) {
		t.Fatalf("writable volumes missing: %#v", podSpec.Volumes)
	}
	if !hasVolumeMount(container.VolumeMounts, runnerHomeVolume, runnerHomePath) || !hasVolumeMount(container.VolumeMounts, runnerTmpVolume, runnerTmpPath) || !hasVolumeMount(container.VolumeMounts, runnerDevShmVolume, runnerDevShmPath) {
		t.Fatalf("writable volume mounts missing: %#v", container.VolumeMounts)
	}
}

func assertRunnerUtilityResources(t *testing.T, resources corev1.ResourceRequirements) {
	t.Helper()
	assertResourceQuantity(t, resources.Requests, corev1.ResourceCPU, runnerUtilityCPURequest)
	assertResourceQuantity(t, resources.Requests, corev1.ResourceMemory, runnerUtilityMemoryRequest)
	assertResourceQuantity(t, resources.Limits, corev1.ResourceMemory, runnerUtilityMemoryLimit)
	if _, exists := resources.Limits[corev1.ResourceCPU]; exists {
		t.Fatalf("utility runner must not have a cpu limit: %#v", resources.Limits)
	}
}

func assertRunnerSessionResources(t *testing.T, resources corev1.ResourceRequirements) {
	t.Helper()
	assertResourceQuantity(t, resources.Requests, corev1.ResourceCPU, runnerSessionCPURequest)
	assertResourceQuantity(t, resources.Requests, corev1.ResourceMemory, runnerSessionMemoryRequest)
	assertResourceQuantity(t, resources.Limits, corev1.ResourceMemory, runnerSessionMemoryLimit)
	if _, exists := resources.Limits[corev1.ResourceCPU]; exists {
		t.Fatalf("session runner must not have a cpu limit: %#v", resources.Limits)
	}
}

func assertResourceQuantity(t *testing.T, resources corev1.ResourceList, name corev1.ResourceName, expected string) {
	t.Helper()
	actual, exists := resources[name]
	if !exists {
		t.Fatalf("resource %s is missing: %#v", name, resources)
	}
	want := resource.MustParse(expected)
	if actual.Cmp(want) != 0 {
		t.Fatalf("resource %s = %s, want %s", name, actual.String(), want.String())
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

func hasMemoryVolumeWithSize(volumes []corev1.Volume, name string, expected string) bool {
	want := resource.MustParse(expected)
	for _, volume := range volumes {
		if volume.Name != name || volume.EmptyDir == nil || volume.EmptyDir.Medium != corev1.StorageMediumMemory || volume.EmptyDir.SizeLimit == nil {
			continue
		}
		if volume.EmptyDir.SizeLimit.Cmp(want) == 0 {
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
