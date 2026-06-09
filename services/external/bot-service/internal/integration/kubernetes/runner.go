package kubernetes

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	runnerComponent      = "agent-run"
	labelRunID           = "matter-codex.dev/run-id"
	labelAgentRole       = "matter-codex.dev/agent-role"
	openAISecretVolume   = "openai-secret"
	gitHubSecretVolume   = "github-secret"
)

type Config struct {
	Namespace                 string
	KubeconfigPath            string
	SmokeImage                string
	AgentRunnerImage          string
	CodexPackage              string
	WorkspaceStorageSize      string
	JobTTLSecondsAfterFinish  int32
	LogTailLines              int64
	AgentRunnerServiceAccount string
	OpenAISecretName          string
	GitHubSecretName          string
}

type Runner struct {
	client                    kubernetes.Interface
	namespace                 string
	smokeImage                string
	agentRunnerImage          string
	codexPackage              string
	workspaceStorage          resource.Quantity
	jobTTLSecondsAfterFinish  int32
	logTailLines              int64
	agentRunnerServiceAccount string
	openAISecretName          string
	gitHubSecretName          string
}

var _ runtimerepo.Runner = (*Runner)(nil)

func NewRunner(cfg Config) (*Runner, error) {
	restConfig, err := kubernetesConfig(cfg.KubeconfigPath)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	return NewRunnerWithClient(client, cfg)
}

func NewRunnerWithClient(client kubernetes.Interface, cfg Config) (*Runner, error) {
	if client == nil {
		return nil, fmt.Errorf("kubernetes client is required")
	}
	namespace, err := runtimeNamespace(cfg.Namespace)
	if err != nil {
		return nil, err
	}
	storage, err := resource.ParseQuantity(defaultString(cfg.WorkspaceStorageSize, "1Gi"))
	if err != nil {
		return nil, fmt.Errorf("parse workspace storage size: %w", err)
	}
	return &Runner{
		client:                    client,
		namespace:                 namespace,
		smokeImage:                defaultString(cfg.SmokeImage, "busybox:1.36"),
		agentRunnerImage:          defaultString(cfg.AgentRunnerImage, "node:22-alpine"),
		codexPackage:              defaultString(cfg.CodexPackage, "@openai/codex@0.138.0"),
		workspaceStorage:          storage,
		jobTTLSecondsAfterFinish:  defaultInt32(cfg.JobTTLSecondsAfterFinish, 86400),
		logTailLines:              defaultInt64(cfg.LogTailLines, 40),
		agentRunnerServiceAccount: defaultString(cfg.AgentRunnerServiceAccount, "matter-codex-agent-runner"),
		openAISecretName:          defaultString(cfg.OpenAISecretName, "matter-codex-openai"),
		gitHubSecretName:          defaultString(cfg.GitHubSecretName, "matter-codex-github"),
	}, nil
}

func (runner *Runner) StartSmokeRun(ctx context.Context, input runtimerepo.SmokeRunInput) (runtimerepo.StartedRun, error) {
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("run id is required")
	}
	role := defaultString(input.Role, "smoke")
	pvcName := workspacePVCName(runID)
	jobName := runnerJobName(runID)

	created := false
	if _, err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).Create(ctx, runner.smokePVC(runID, role), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create workspace pvc: %w", err)
		}
	} else {
		created = true
	}
	if _, err := runner.client.BatchV1().Jobs(runner.namespace).Create(ctx, runner.smokeJob(runID, role), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create runner job: %w", err)
		}
	} else {
		created = true
	}
	return runtimerepo.StartedRun{
		RunID:     runID,
		Namespace: runner.namespace,
		JobName:   jobName,
		PVCName:   pvcName,
		Created:   created,
	}, nil
}

func (runner *Runner) StartDeveloperRun(ctx context.Context, input runtimerepo.DeveloperRunInput) (runtimerepo.StartedRun, error) {
	input = normalizeDeveloperRunInput(input)
	if input.RunID == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("run id is required")
	}
	pvcName := workspacePVCName(input.RunID)
	jobName := runnerJobName(input.RunID)

	created := false
	if _, err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).Create(ctx, runner.smokePVC(input.RunID, input.Profile), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create workspace pvc: %w", err)
		}
	} else {
		created = true
	}
	if _, err := runner.client.BatchV1().Jobs(runner.namespace).Create(ctx, runner.developerJob(input), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create developer runner job: %w", err)
		}
	} else {
		created = true
	}
	return runtimerepo.StartedRun{
		RunID:     input.RunID,
		Namespace: runner.namespace,
		JobName:   jobName,
		PVCName:   pvcName,
		Created:   created,
	}, nil
}

func (runner *Runner) GetRunStatus(ctx context.Context, runID string) (runtimerepo.RunStatus, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return runtimerepo.RunStatus{}, fmt.Errorf("run id is required")
	}
	status := runtimerepo.RunStatus{
		RunID:     runID,
		Namespace: runner.namespace,
		JobName:   runnerJobName(runID),
		PVCName:   workspacePVCName(runID),
	}
	job, err := runner.client.BatchV1().Jobs(runner.namespace).Get(ctx, status.JobName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return status, nil
		}
		return runtimerepo.RunStatus{}, fmt.Errorf("get runner job: %w", err)
	}
	status.Exists = true
	status.JobActive = job.Status.Active
	status.JobSucceeded = job.Status.Succeeded
	status.JobFailed = job.Status.Failed

	pods, err := runner.client.CoreV1().Pods(runner.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{labelRunID: runID}).String(),
	})
	if err != nil {
		return runtimerepo.RunStatus{}, fmt.Errorf("list runner pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return status, nil
	}
	sort.Slice(pods.Items, func(i int, j int) bool {
		return pods.Items[i].CreationTimestamp.After(pods.Items[j].CreationTimestamp.Time)
	})
	pod := pods.Items[0]
	status.PodName = pod.Name
	status.PodPhase = string(pod.Status.Phase)
	status.LogTail = runner.logTail(ctx, pod.Name)
	status.Artifacts = parseArtifacts(status.LogTail)
	return status, nil
}

func (runner *Runner) CleanupRun(ctx context.Context, runID string) (runtimerepo.CleanupResult, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return runtimerepo.CleanupResult{}, fmt.Errorf("run id is required")
	}
	result := runtimerepo.CleanupResult{
		RunID:     runID,
		Namespace: runner.namespace,
	}
	background := metav1.DeletePropagationBackground
	if err := runner.client.BatchV1().Jobs(runner.namespace).Delete(ctx, runnerJobName(runID), metav1.DeleteOptions{PropagationPolicy: &background}); err != nil {
		if !apierrors.IsNotFound(err) {
			return runtimerepo.CleanupResult{}, fmt.Errorf("delete runner job: %w", err)
		}
	} else {
		result.JobDeleted = true
	}
	if err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).Delete(ctx, workspacePVCName(runID), metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return runtimerepo.CleanupResult{}, fmt.Errorf("delete workspace pvc: %w", err)
		}
	} else {
		result.PVCDeleted = true
	}
	return result, nil
}

func (runner *Runner) smokePVC(runID string, role string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   workspacePVCName(runID),
			Labels: runnerLabels(runID, role),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: runner.workspaceStorage,
				},
			},
		},
	}
}

func (runner *Runner) smokeJob(runID string, role string) *batchv1.Job {
	backoffLimit := int32(0)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   runnerJobName(runID),
			Labels: runnerLabels(runID, role),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &runner.jobTTLSecondsAfterFinish,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: runnerLabels(runID, role)},
				Spec: corev1.PodSpec{
					ServiceAccountName:           runner.agentRunnerServiceAccount,
					AutomountServiceAccountToken: boolPtr(false),
					RestartPolicy:                corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "runner",
							Image:   runner.smokeImage,
							Command: []string{"sh", "-ec"},
							Args:    []string{smokeScript()},
							Env: []corev1.EnvVar{
								{Name: "MATTERCODEX_RUN_ID", Value: runID},
								{Name: "MATTERCODEX_AGENT_ROLE", Value: role},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "workspace",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: workspacePVCName(runID),
								},
							},
						},
					},
				},
			},
		},
	}
}

func (runner *Runner) developerJob(input runtimerepo.DeveloperRunInput) *batchv1.Job {
	backoffLimit := int32(0)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   runnerJobName(input.RunID),
			Labels: runnerLabels(input.RunID, input.Profile),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &runner.jobTTLSecondsAfterFinish,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: runnerLabels(input.RunID, input.Profile)},
				Spec: corev1.PodSpec{
					ServiceAccountName:           runner.agentRunnerServiceAccount,
					AutomountServiceAccountToken: boolPtr(false),
					RestartPolicy:                corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "runner",
							Image:   runner.agentRunnerImage,
							Command: []string{"sh", "-ec"},
							Args:    []string{developerScript()},
							Env: []corev1.EnvVar{
								{Name: "MATTERCODEX_RUN_ID", Value: input.RunID},
								{Name: "MATTERCODEX_AGENT_PROFILE", Value: input.Profile},
								{Name: "MATTERCODEX_REPO_PROVIDER", Value: input.Provider},
								{Name: "MATTERCODEX_REPO_OWNER", Value: input.Owner},
								{Name: "MATTERCODEX_REPO_NAME", Value: input.Name},
								{Name: "MATTERCODEX_BASE_BRANCH", Value: input.BaseBranch},
								{Name: "MATTERCODEX_HEAD_BRANCH", Value: input.HeadBranch},
								{Name: "MATTERCODEX_PR_TITLE", Value: input.Title},
								{Name: "MATTERCODEX_TASK_PROMPT", Value: input.Task},
								{Name: "MATTERCODEX_CODEX_PACKAGE", Value: runner.codexPackage},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
								{Name: openAISecretVolume, MountPath: "/var/run/secrets/matter-codex-openai", ReadOnly: true},
								{Name: gitHubSecretVolume, MountPath: "/var/run/secrets/matter-codex-github", ReadOnly: true},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "workspace",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: workspacePVCName(input.RunID),
								},
							},
						},
						{
							Name: openAISecretVolume,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: runner.openAISecretName,
									Items: []corev1.KeyToPath{
										{Key: "openai-api-key", Path: "openai-api-key"},
									},
								},
							},
						},
						{
							Name: gitHubSecretVolume,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: runner.gitHubSecretName,
									Items: []corev1.KeyToPath{
										{Key: "github-token", Path: "github-token"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (runner *Runner) logTail(ctx context.Context, podName string) string {
	limitBytes := int64(8192)
	stream, err := runner.client.CoreV1().Pods(runner.namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container:  "runner",
		TailLines:  &runner.logTailLines,
		LimitBytes: &limitBytes,
	}).Stream(ctx)
	if err != nil {
		return ""
	}
	defer stream.Close()
	body, err := io.ReadAll(stream)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

func kubernetesConfig(kubeconfigPath string) (*rest.Config, error) {
	if strings.TrimSpace(kubeconfigPath) != "" {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("load kubeconfig: %w", err)
		}
		return cfg, nil
	}
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster kubernetes config: %w", err)
	}
	return cfg, nil
}

func runtimeNamespace(configured string) (string, error) {
	if namespace := strings.TrimSpace(configured); namespace != "" {
		return namespace, nil
	}
	body, err := os.ReadFile(defaultNamespaceFile)
	if err != nil {
		return "", fmt.Errorf("read service account namespace: %w", err)
	}
	namespace := strings.TrimSpace(string(body))
	if namespace == "" {
		return "", fmt.Errorf("service account namespace is empty")
	}
	return namespace, nil
}

func runnerLabels(runID string, role string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": runnerComponent,
		labelRunID:                    runID,
		labelAgentRole:                role,
	}
}

func runnerJobName(runID string) string {
	return "mc-run-" + runID
}

func workspacePVCName(runID string) string {
	return "mc-ws-" + runID
}

func smokeScript() string {
	return strings.Join([]string{
		`printf 'matter-codex smoke start\n'`,
		`printf 'run-id: %s\n' "$MATTERCODEX_RUN_ID"`,
		`printf 'agent-role: %s\n' "$MATTERCODEX_AGENT_ROLE"`,
		`printf 'workspace before:\n'`,
		`ls -la /workspace`,
		`printf 'smoke-ok\n' > /workspace/smoke.txt`,
		`printf 'workspace after:\n'`,
		`ls -la /workspace`,
		`printf 'matter-codex smoke done\n'`,
	}, "\n")
}

func developerScript() string {
	return strings.Join([]string{
		`set -eu`,
		`printf 'matter-codex developer run start\n'`,
		`printf 'run-id: %s\n' "$MATTERCODEX_RUN_ID"`,
		`printf 'profile: %s\n' "$MATTERCODEX_AGENT_PROFILE"`,
		`mkdir -p /workspace/repo /workspace/artifacts /workspace/codex-home`,
		`umask 077`,
		`runner_failure_report() {`,
		`  code=$?`,
		`  if [ "$code" -eq 0 ]; then return 0; fi`,
		`  printf 'matter-codex artifact exit-code: %s\n' "$code"`,
		`  for f in /workspace/artifacts/codex-stderr.log /workspace/artifacts/git-push.log /workspace/artifacts/git-commit.log /workspace/artifacts/npm-install.log /workspace/artifacts/apk.log; do`,
		`    [ -f "$f" ] || continue`,
		`    printf '===== %s\n' "$f"`,
		`    tail -40 "$f" || true`,
		`  done`,
		`  exit "$code"`,
		`}`,
		`trap runner_failure_report EXIT`,
		`apk add --no-cache git github-cli jq >/workspace/artifacts/apk.log 2>&1`,
		`if ! command -v codex >/dev/null 2>&1; then`,
		`  npm install -g "$MATTERCODEX_CODEX_PACKAGE" >/workspace/artifacts/npm-install.log 2>&1`,
		`else`,
		`  codex --version >/workspace/artifacts/npm-install.log 2>&1`,
		`fi`,
		`cat >/workspace/codex-home/config.toml <<'EOF'`,
		`sandbox_mode = "workspace-write"`,
		`approval_policy = "never"`,
		`disable_response_storage = true`,
		``,
		`[shell_environment_policy]`,
		`inherit = "none"`,
		`include_only = ["PATH", "HOME"]`,
		``,
		`[mcp_servers.context7]`,
		`command = "npx"`,
		`args = ["-y", "@upstash/context7-mcp"]`,
		`startup_timeout_sec = 20`,
		`EOF`,
		`CODEX_HOME=/workspace/codex-home codex mcp list >/workspace/artifacts/mcp-list.log 2>&1 || true`,
		`cat >/workspace/git-askpass.sh <<'EOF'`,
		`#!/bin/sh`,
		`case "$1" in`,
		`  *Username*) printf '%s\n' x-access-token ;;`,
		`  *Password*) cat /var/run/secrets/matter-codex-github/github-token ;;`,
		`  *) printf '\n' ;;`,
		`esac`,
		`EOF`,
		`chmod 700 /workspace/git-askpass.sh`,
		`export GIT_ASKPASS=/workspace/git-askpass.sh`,
		`export GIT_TERMINAL_PROMPT=0`,
		`git clone "https://github.com/${MATTERCODEX_REPO_OWNER}/${MATTERCODEX_REPO_NAME}.git" /workspace/repo >/workspace/artifacts/git-clone.log 2>&1`,
		`cd /workspace/repo`,
		`git checkout -B "$MATTERCODEX_HEAD_BRANCH" "origin/$MATTERCODEX_BASE_BRANCH" >/workspace/artifacts/git-checkout.log 2>&1`,
		`git config user.name "matter-codex developer agent"`,
		`git config user.email "matter-codex-agent@local.invalid"`,
		`cat >/workspace/artifacts/prompt.txt <<'EOF'`,
		`You are the matter-codex developer agent running in an isolated Kubernetes Job.`,
		``,
		`Rules:`,
		`- Work only inside the checked out repository.`,
		`- Do not print, read, or exfiltrate secrets.`,
		`- Do not push branches and do not create pull requests; the runner does that after you finish.`,
		`- Keep the change minimal and directly related to the requested task.`,
		`- Leave the working tree with the intended changes staged or unstaged; both are acceptable.`,
		`- Final answer must summarize changed files and checks you ran.`,
		``,
		`Task:`,
		`EOF`,
		`printf '%s\n' "$MATTERCODEX_TASK_PROMPT" >>/workspace/artifacts/prompt.txt`,
		`CODEX_API_KEY="$(cat /var/run/secrets/matter-codex-openai/openai-api-key)" CODEX_HOME=/workspace/codex-home codex exec --json --cd /workspace/repo --sandbox workspace-write --output-last-message /workspace/artifacts/codex-final.md - < /workspace/artifacts/prompt.txt >/workspace/artifacts/codex-events.jsonl 2>/workspace/artifacts/codex-stderr.log`,
		`if [ -z "$(git status --porcelain)" ]; then`,
		`  printf 'matter-codex artifact no-changes: true\n'`,
		`  printf 'matter-codex developer run done\n'`,
		`  exit 0`,
		`fi`,
		`git add -A`,
		`git commit -m "Apply matter-codex developer run ${MATTERCODEX_RUN_ID}" >/workspace/artifacts/git-commit.log 2>&1`,
		`git push origin "$MATTERCODEX_HEAD_BRANCH" >/workspace/artifacts/git-push.log 2>&1`,
		`{`,
		`  printf '## Matter-codex developer run\n\n'`,
		"  printf '- run: `%s`\\n' \"$MATTERCODEX_RUN_ID\"",
		"  printf '- profile: `%s`\\n' \"$MATTERCODEX_AGENT_PROFILE\"",
		"  printf '- base: `%s`\\n' \"$MATTERCODEX_BASE_BRANCH\"",
		"  printf '- head: `%s`\\n\\n' \"$MATTERCODEX_HEAD_BRANCH\"",
		`  printf '## Codex summary\n\n'`,
		`  cat /workspace/artifacts/codex-final.md`,
		`  printf '\n'`,
		`} >/workspace/artifacts/pr-body.md`,
		`PR_URL="$(GITHUB_TOKEN="$(cat /var/run/secrets/matter-codex-github/github-token)" gh pr view "$MATTERCODEX_HEAD_BRANCH" --repo "${MATTERCODEX_REPO_OWNER}/${MATTERCODEX_REPO_NAME}" --json url --jq .url 2>/dev/null || GITHUB_TOKEN="$(cat /var/run/secrets/matter-codex-github/github-token)" gh pr create --repo "${MATTERCODEX_REPO_OWNER}/${MATTERCODEX_REPO_NAME}" --base "$MATTERCODEX_BASE_BRANCH" --head "$MATTERCODEX_HEAD_BRANCH" --title "$MATTERCODEX_PR_TITLE" --body-file /workspace/artifacts/pr-body.md --draft)"`,
		`printf 'matter-codex artifact pr-url: %s\n' "$PR_URL"`,
		`printf 'matter-codex artifact branch: %s\n' "$MATTERCODEX_HEAD_BRANCH"`,
		`printf 'matter-codex artifact commit: %s\n' "$(git rev-parse --short HEAD)"`,
		`printf 'matter-codex developer run done\n'`,
	}, "\n")
}

func normalizeDeveloperRunInput(input runtimerepo.DeveloperRunInput) runtimerepo.DeveloperRunInput {
	input.RunID = strings.TrimSpace(input.RunID)
	input.Profile = defaultString(input.Profile, "developer")
	input.Provider = defaultString(input.Provider, "github")
	input.Owner = strings.TrimSpace(input.Owner)
	input.Name = strings.TrimSpace(input.Name)
	input.BaseBranch = defaultString(input.BaseBranch, "main")
	input.HeadBranch = strings.TrimSpace(input.HeadBranch)
	input.Title = strings.TrimSpace(input.Title)
	input.Task = strings.TrimSpace(input.Task)
	return input
}

func parseArtifacts(logTail string) map[string]string {
	artifacts := make(map[string]string)
	for _, line := range strings.Split(logTail, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "matter-codex artifact ") {
			continue
		}
		keyValue := strings.TrimPrefix(line, "matter-codex artifact ")
		key, value, ok := strings.Cut(keyValue, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			artifacts[key] = value
		}
	}
	if len(artifacts) == 0 {
		return nil
	}
	return artifacts
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func defaultInt32(value int32, fallback int32) int32 {
	if value <= 0 {
		return fallback
	}
	return value
}

func defaultInt64(value int64, fallback int64) int64 {
	if value <= 0 {
		return fallback
	}
	return value
}

func boolPtr(value bool) *bool {
	return &value
}
