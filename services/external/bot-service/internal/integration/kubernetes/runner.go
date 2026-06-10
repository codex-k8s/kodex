package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
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
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	defaultNamespaceFile  = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	runnerComponent       = "agent-run"
	labelRunID            = "matter-codex.dev/run-id"
	labelAgentRole        = "matter-codex.dev/agent-role"
	labelOpenAIAccount    = "matter-codex.dev/openai-account"
	codexAuthSecretVolume = "codex-auth-secret"
	gitHubSecretVolume    = "github-secret"
)

var codexDeviceCodeRE = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{5}\b`)

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
	CodexAuthSecretName       string
	GitHubSecretName          string
}

type Runner struct {
	client                    kubernetes.Interface
	restConfig                *rest.Config
	namespace                 string
	smokeImage                string
	agentRunnerImage          string
	codexPackage              string
	workspaceStorage          resource.Quantity
	jobTTLSecondsAfterFinish  int32
	logTailLines              int64
	agentRunnerServiceAccount string
	codexAuthSecretName       string
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
	return newRunnerWithClientAndConfig(client, rest.CopyConfig(restConfig), cfg)
}

func NewRunnerWithClient(client kubernetes.Interface, cfg Config) (*Runner, error) {
	return newRunnerWithClientAndConfig(client, nil, cfg)
}

func newRunnerWithClientAndConfig(client kubernetes.Interface, restConfig *rest.Config, cfg Config) (*Runner, error) {
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
		restConfig:                restConfig,
		namespace:                 namespace,
		smokeImage:                defaultString(cfg.SmokeImage, "busybox:1.36"),
		agentRunnerImage:          defaultString(cfg.AgentRunnerImage, "node:22-alpine"),
		codexPackage:              defaultString(cfg.CodexPackage, "@openai/codex@0.138.0"),
		workspaceStorage:          storage,
		jobTTLSecondsAfterFinish:  defaultInt32(cfg.JobTTLSecondsAfterFinish, 86400),
		logTailLines:              defaultInt64(cfg.LogTailLines, 40),
		agentRunnerServiceAccount: defaultString(cfg.AgentRunnerServiceAccount, "matter-codex-agent-runner"),
		codexAuthSecretName:       defaultString(cfg.CodexAuthSecretName, "matter-codex-codex-auth"),
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

func (runner *Runner) StartCodexAuthSession(ctx context.Context, input runtimerepo.CodexAuthSessionInput) (runtimerepo.CodexAuthSession, error) {
	input.AccountName = strings.TrimSpace(input.AccountName)
	input.SecretName = strings.TrimSpace(input.SecretName)
	if input.AccountName == "" {
		return runtimerepo.CodexAuthSession{}, fmt.Errorf("openai account name is required")
	}
	if input.SecretName == "" {
		return runtimerepo.CodexAuthSession{}, fmt.Errorf("codex auth secret name is required")
	}
	jobName := codexAuthJobName(input.AccountName)
	created := false
	if _, err := runner.client.BatchV1().Jobs(runner.namespace).Create(ctx, runner.codexAuthJob(input.AccountName, input.SecretName), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.CodexAuthSession{}, fmt.Errorf("create codex auth job: %w", err)
		}
	} else {
		created = true
	}
	return runtimerepo.CodexAuthSession{
		AccountName: input.AccountName,
		SecretName:  input.SecretName,
		Namespace:   runner.namespace,
		JobName:     jobName,
		Created:     created,
	}, nil
}

func (runner *Runner) GetCodexAuthStatus(ctx context.Context, accountName string, secretName string) (runtimerepo.CodexAuthStatus, error) {
	accountName = strings.TrimSpace(accountName)
	secretName = strings.TrimSpace(secretName)
	if accountName == "" {
		return runtimerepo.CodexAuthStatus{}, fmt.Errorf("openai account name is required")
	}
	status := runtimerepo.CodexAuthStatus{
		AccountName: accountName,
		SecretName:  secretName,
		Namespace:   runner.namespace,
		JobName:     codexAuthJobName(accountName),
	}
	job, err := runner.client.BatchV1().Jobs(runner.namespace).Get(ctx, status.JobName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return status, nil
		}
		return runtimerepo.CodexAuthStatus{}, fmt.Errorf("get codex auth job: %w", err)
	}
	status.Exists = true
	status.JobActive = job.Status.Active
	status.JobSucceeded = job.Status.Succeeded
	status.JobFailed = job.Status.Failed

	pod, ok, err := runner.latestPodByLabels(ctx, labels.Set{
		"app.kubernetes.io/component": "codex-auth",
		labelOpenAIAccount:            accountName,
	})
	if err != nil {
		return runtimerepo.CodexAuthStatus{}, err
	}
	if !ok {
		return status, nil
	}
	status.PodName = pod.Name
	status.PodPhase = string(pod.Status.Phase)
	status.LogTail = runner.logTail(ctx, pod.Name)
	status.DeviceURL, status.DeviceCode = parseCodexDeviceAuth(status.LogTail)
	status.AuthReady = runner.codexAuthJSONReady(ctx, pod.Name)
	return status, nil
}

func (runner *Runner) CompleteCodexAuthSession(ctx context.Context, input runtimerepo.CodexAuthCompleteInput) (runtimerepo.CodexAuthCompleteResult, error) {
	input.AccountName = strings.TrimSpace(input.AccountName)
	input.SecretName = strings.TrimSpace(input.SecretName)
	if input.AccountName == "" {
		return runtimerepo.CodexAuthCompleteResult{}, fmt.Errorf("openai account name is required")
	}
	if input.SecretName == "" {
		return runtimerepo.CodexAuthCompleteResult{}, fmt.Errorf("codex auth secret name is required")
	}
	pod, ok, err := runner.latestPodByLabels(ctx, labels.Set{
		"app.kubernetes.io/component": "codex-auth",
		labelOpenAIAccount:            input.AccountName,
	})
	if err != nil {
		return runtimerepo.CodexAuthCompleteResult{}, err
	}
	if !ok {
		return runtimerepo.CodexAuthCompleteResult{}, fmt.Errorf("codex auth pod not found")
	}
	authJSON, err := runner.execPod(ctx, pod.Name, []string{"sh", "-ec", "test -s /codex-home/auth.json && cat /codex-home/auth.json"})
	if err != nil {
		return runtimerepo.CodexAuthCompleteResult{}, fmt.Errorf("read codex auth.json: %w", err)
	}
	if len(bytes.TrimSpace(authJSON)) == 0 {
		return runtimerepo.CodexAuthCompleteResult{}, fmt.Errorf("codex auth.json is empty")
	}
	if err := runner.upsertCodexAuthSecret(ctx, input.AccountName, input.SecretName, authJSON); err != nil {
		return runtimerepo.CodexAuthCompleteResult{}, err
	}
	return runtimerepo.CodexAuthCompleteResult{
		AccountName: input.AccountName,
		SecretName:  input.SecretName,
		Namespace:   runner.namespace,
		Saved:       true,
	}, nil
}

func (runner *Runner) CleanupCodexAuthSession(ctx context.Context, accountName string) (runtimerepo.CodexAuthCleanupResult, error) {
	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		return runtimerepo.CodexAuthCleanupResult{}, fmt.Errorf("openai account name is required")
	}
	result := runtimerepo.CodexAuthCleanupResult{
		AccountName: accountName,
		Namespace:   runner.namespace,
	}
	background := metav1.DeletePropagationBackground
	if err := runner.client.BatchV1().Jobs(runner.namespace).Delete(ctx, codexAuthJobName(accountName), metav1.DeleteOptions{PropagationPolicy: &background}); err != nil {
		if !apierrors.IsNotFound(err) {
			return runtimerepo.CodexAuthCleanupResult{}, fmt.Errorf("delete codex auth job: %w", err)
		}
	} else {
		result.JobDeleted = true
	}
	return result, nil
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

func (runner *Runner) latestPodByLabels(ctx context.Context, selector labels.Set) (corev1.Pod, bool, error) {
	pods, err := runner.client.CoreV1().Pods(runner.namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(selector).String()})
	if err != nil {
		return corev1.Pod{}, false, fmt.Errorf("list runner pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return corev1.Pod{}, false, nil
	}
	sort.Slice(pods.Items, func(i int, j int) bool {
		return pods.Items[i].CreationTimestamp.After(pods.Items[j].CreationTimestamp.Time)
	})
	return pods.Items[0], true, nil
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
	codexAuthSecretName := defaultString(input.CodexAuthSecretName, runner.codexAuthSecretName)
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
								{Name: codexAuthSecretVolume, MountPath: "/var/run/secrets/matter-codex-codex", ReadOnly: true},
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
							Name: codexAuthSecretVolume,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: codexAuthSecretName,
									Items: []corev1.KeyToPath{
										{Key: "auth.json", Path: "auth.json"},
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

func (runner *Runner) codexAuthJob(accountName string, secretName string) *batchv1.Job {
	backoffLimit := int32(0)
	labels := codexAuthLabels(accountName)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   codexAuthJobName(accountName),
			Labels: labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &runner.jobTTLSecondsAfterFinish,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					ServiceAccountName:           runner.agentRunnerServiceAccount,
					AutomountServiceAccountToken: boolPtr(false),
					RestartPolicy:                corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "runner",
							Image:   runner.agentRunnerImage,
							Command: []string{"sh", "-ec"},
							Args:    []string{codexAuthScript()},
							Env: []corev1.EnvVar{
								{Name: "MATTERCODEX_OPENAI_ACCOUNT", Value: accountName},
								{Name: "MATTERCODEX_CODEX_AUTH_SECRET", Value: secretName},
								{Name: "MATTERCODEX_CODEX_PACKAGE", Value: runner.codexPackage},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "codex-home", MountPath: "/codex-home"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "codex-home",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
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

func (runner *Runner) codexAuthJSONReady(ctx context.Context, podName string) bool {
	if runner.restConfig == nil {
		return false
	}
	_, err := runner.execPod(ctx, podName, []string{"sh", "-ec", "test -s /codex-home/auth.json && test -f /codex-home/.auth-ready"})
	return err == nil
}

func (runner *Runner) execPod(ctx context.Context, podName string, command []string) ([]byte, error) {
	if runner.restConfig == nil {
		return nil, fmt.Errorf("kubernetes rest config is required for pod exec")
	}
	request := runner.client.CoreV1().RESTClient().
		Post().
		Namespace(runner.namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "runner",
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(runner.restConfig, http.MethodPost, request.URL())
	if err != nil {
		return nil, fmt.Errorf("create pod exec: %w", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr}); err != nil {
		return nil, fmt.Errorf("stream pod exec: %w: %s", err, safeKubernetesExecError(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (runner *Runner) upsertCodexAuthSecret(ctx context.Context, accountName string, secretName string, authJSON []byte) error {
	secretClient := runner.client.CoreV1().Secrets(runner.namespace)
	secret, err := secretClient.Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get codex auth secret: %w", err)
		}
		_, err = secretClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: secretName,
				Labels: map[string]string{
					"app.kubernetes.io/name":      "matter-codex-agent-runner",
					"app.kubernetes.io/component": "codex-auth-secret",
					labelOpenAIAccount:            accountName,
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{"auth.json": authJSON},
		}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create codex auth secret: %w", err)
		}
		return nil
	}
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	if secret.Labels == nil {
		secret.Labels = make(map[string]string)
	}
	secret.Labels["app.kubernetes.io/name"] = "matter-codex-agent-runner"
	secret.Labels["app.kubernetes.io/component"] = "codex-auth-secret"
	secret.Labels[labelOpenAIAccount] = accountName
	secret.Data["auth.json"] = authJSON
	if _, err := secretClient.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update codex auth secret: %w", err)
	}
	return nil
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

func codexAuthJobName(accountName string) string {
	return "mc-codex-auth-" + accountName
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
		`cp /var/run/secrets/matter-codex-codex/auth.json /workspace/codex-home/auth.json`,
		`chmod 600 /workspace/codex-home/auth.json`,
		`CODEX_HOME=/workspace/codex-home codex login status >/workspace/artifacts/codex-login-status.log 2>&1`,
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
		`CODEX_HOME=/workspace/codex-home codex exec --json --cd /workspace/repo --sandbox workspace-write --output-last-message /workspace/artifacts/codex-final.md - < /workspace/artifacts/prompt.txt >/workspace/artifacts/codex-events.jsonl 2>/workspace/artifacts/codex-stderr.log`,
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

func codexAuthScript() string {
	return strings.Join([]string{
		`set -eu`,
		`printf 'matter-codex codex auth start\n'`,
		`printf 'account: %s\n' "$MATTERCODEX_OPENAI_ACCOUNT"`,
		`mkdir -p /codex-home`,
		`umask 077`,
		`if ! command -v codex >/dev/null 2>&1; then`,
		`  npm install -g "$MATTERCODEX_CODEX_PACKAGE"`,
		`else`,
		`  codex --version`,
		`fi`,
		`CODEX_HOME=/codex-home codex login --device-auth`,
		`touch /codex-home/.auth-ready`,
		`printf 'matter-codex codex auth ready\n'`,
		`sleep 900`,
	}, "\n")
}

func normalizeDeveloperRunInput(input runtimerepo.DeveloperRunInput) runtimerepo.DeveloperRunInput {
	input.RunID = strings.TrimSpace(input.RunID)
	input.Profile = defaultString(input.Profile, "developer")
	input.CodexAuthSecretName = strings.TrimSpace(input.CodexAuthSecretName)
	input.Provider = defaultString(input.Provider, "github")
	input.Owner = strings.TrimSpace(input.Owner)
	input.Name = strings.TrimSpace(input.Name)
	input.BaseBranch = defaultString(input.BaseBranch, "main")
	input.HeadBranch = strings.TrimSpace(input.HeadBranch)
	input.Title = strings.TrimSpace(input.Title)
	input.Task = strings.TrimSpace(input.Task)
	return input
}

func codexAuthLabels(accountName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": "codex-auth",
		labelOpenAIAccount:            accountName,
	}
}

func parseCodexDeviceAuth(logTail string) (string, string) {
	clean := stripANSI(logTail)
	deviceURL := ""
	if strings.Contains(clean, "https://auth.openai.com/codex/device") {
		deviceURL = "https://auth.openai.com/codex/device"
	}
	code := codexDeviceCodeRE.FindString(clean)
	return deviceURL, code
}

func stripANSI(value string) string {
	ansiRE := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	return ansiRE.ReplaceAllString(value, "")
}

func safeKubernetesExecError(value string) string {
	value = strings.TrimSpace(stripANSI(value))
	if value == "" {
		return "empty stderr"
	}
	lines := strings.Split(value, "\n")
	sort.Strings(lines)
	return lines[0]
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
