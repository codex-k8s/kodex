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
	"strconv"
	"strings"
	"time"

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
	promptVolume          = "agent-prompt"
	runnerHomeVolume      = "runner-home"
	runnerTmpVolume       = "runner-tmp"
	runnerHomePath        = "/home/matter-codex"
	runnerTmpPath         = "/tmp"
	runnerUID             = int64(10001)
	runnerGID             = int64(10001)
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
		agentRunnerImage:          defaultString(cfg.AgentRunnerImage, "matter-codex-agent-runner:dev"),
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
	authJSON, err := runner.execPod(ctx, pod.Name, []string{"matter-codex-agent-runner", "print-auth-json"})
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

func (runner *Runner) DeleteCodexAuthAccount(ctx context.Context, accountName string, secretName string) (runtimerepo.CodexAuthAccountDeleteResult, error) {
	accountName = strings.TrimSpace(accountName)
	secretName = strings.TrimSpace(secretName)
	if accountName == "" {
		return runtimerepo.CodexAuthAccountDeleteResult{}, fmt.Errorf("openai account name is required")
	}
	if secretName == "" {
		return runtimerepo.CodexAuthAccountDeleteResult{}, fmt.Errorf("codex auth secret name is required")
	}
	cleanup, err := runner.CleanupCodexAuthSession(ctx, accountName)
	if err != nil {
		return runtimerepo.CodexAuthAccountDeleteResult{}, err
	}
	result := runtimerepo.CodexAuthAccountDeleteResult{
		AccountName: accountName,
		SecretName:  secretName,
		Namespace:   runner.namespace,
		JobDeleted:  cleanup.JobDeleted,
	}
	if err := runner.client.CoreV1().Secrets(runner.namespace).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return runtimerepo.CodexAuthAccountDeleteResult{}, fmt.Errorf("delete codex auth secret: %w", err)
		}
	} else {
		result.SecretDeleted = true
	}
	return result, nil
}

func (runner *Runner) StartDeveloperRun(ctx context.Context, input runtimerepo.DeveloperRunInput) (runtimerepo.StartedRun, error) {
	input = normalizeDeveloperRunInput(input)
	if input.RunID == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("run id is required")
	}
	if input.Prompt == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("prompt is required")
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
	if _, err := runner.client.CoreV1().ConfigMaps(runner.namespace).Create(ctx, runner.promptConfigMap(input.RunID, input.Profile, input.Prompt), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create prompt configmap: %w", err)
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

func (runner *Runner) StartReviewRun(ctx context.Context, input runtimerepo.ReviewRunInput) (runtimerepo.StartedRun, error) {
	input = normalizeReviewRunInput(input)
	if input.RunID == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("run id is required")
	}
	if input.Owner == "" || input.Name == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("repository is required")
	}
	if input.PRNumber <= 0 {
		return runtimerepo.StartedRun{}, fmt.Errorf("pr number is required")
	}
	if input.Prompt == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("prompt is required")
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
	if _, err := runner.client.CoreV1().ConfigMaps(runner.namespace).Create(ctx, runner.promptConfigMap(input.RunID, input.Profile, input.Prompt), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create prompt configmap: %w", err)
		}
	} else {
		created = true
	}
	if _, err := runner.client.BatchV1().Jobs(runner.namespace).Create(ctx, runner.reviewJob(input), metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return runtimerepo.StartedRun{}, fmt.Errorf("create reviewer runner job: %w", err)
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
	if err := runner.client.CoreV1().ConfigMaps(runner.namespace).Delete(ctx, promptConfigMapName(runID), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return runtimerepo.CleanupResult{}, fmt.Errorf("delete prompt configmap: %w", err)
	}
	return result, nil
}

func (runner *Runner) CleanupExpiredRuns(ctx context.Context, input runtimerepo.RetentionCleanupInput) (runtimerepo.RetentionCleanupResult, error) {
	if input.OlderThan <= 0 {
		return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("retention duration must be positive")
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := now.Add(-input.OlderThan)
	result := runtimerepo.RetentionCleanupResult{
		Namespace: runner.namespace,
		DryRun:    input.DryRun,
		OlderThan: input.OlderThan,
	}

	jobs, err := runner.client.BatchV1().Jobs(runner.namespace).List(ctx, metav1.ListOptions{LabelSelector: runnerLabelSelector()})
	if err != nil {
		return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("list runner jobs: %w", err)
	}
	jobByRunID := make(map[string]batchv1.Job, len(jobs.Items))
	matchedRunIDs := make(map[string]struct{})
	background := metav1.DeletePropagationBackground
	for _, job := range jobs.Items {
		runID := job.Labels[labelRunID]
		if runID == "" {
			continue
		}
		jobByRunID[runID] = job
		if job.Status.Active > 0 {
			result.SkippedActiveJobs++
			continue
		}
		if job.Status.Succeeded == 0 && job.Status.Failed == 0 {
			continue
		}
		if !olderThan(jobCompletedOrCreatedAt(job), cutoff) {
			continue
		}
		result.JobsMatched++
		matchedRunIDs[runID] = struct{}{}
		if input.DryRun {
			continue
		}
		if err := runner.client.BatchV1().Jobs(runner.namespace).Delete(ctx, job.Name, metav1.DeleteOptions{PropagationPolicy: &background}); err != nil {
			if !apierrors.IsNotFound(err) {
				return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("delete runner job %s: %w", job.Name, err)
			}
		} else {
			result.JobsDeleted++
		}
	}

	pvcs, err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).List(ctx, metav1.ListOptions{LabelSelector: runnerLabelSelector()})
	if err != nil {
		return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("list runner pvcs: %w", err)
	}
	for _, pvc := range pvcs.Items {
		if !runnerRetentionResourceExpired(pvc.Labels[labelRunID], pvc.CreationTimestamp.Time, cutoff, jobByRunID, matchedRunIDs) {
			continue
		}
		result.PVCsMatched++
		matchedRunIDs[pvc.Labels[labelRunID]] = struct{}{}
		if input.DryRun {
			continue
		}
		if err := runner.client.CoreV1().PersistentVolumeClaims(runner.namespace).Delete(ctx, pvc.Name, metav1.DeleteOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("delete workspace pvc %s: %w", pvc.Name, err)
			}
		} else {
			result.PVCsDeleted++
		}
	}

	configMaps, err := runner.client.CoreV1().ConfigMaps(runner.namespace).List(ctx, metav1.ListOptions{LabelSelector: runnerLabelSelector()})
	if err != nil {
		return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("list runner configmaps: %w", err)
	}
	for _, configMap := range configMaps.Items {
		if !runnerRetentionResourceExpired(configMap.Labels[labelRunID], configMap.CreationTimestamp.Time, cutoff, jobByRunID, matchedRunIDs) {
			continue
		}
		result.ConfigMapsMatched++
		matchedRunIDs[configMap.Labels[labelRunID]] = struct{}{}
		if input.DryRun {
			continue
		}
		if err := runner.client.CoreV1().ConfigMaps(runner.namespace).Delete(ctx, configMap.Name, metav1.DeleteOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				return runtimerepo.RetentionCleanupResult{}, fmt.Errorf("delete prompt configmap %s: %w", configMap.Name, err)
			}
		} else {
			result.ConfigMapsDeleted++
		}
	}
	result.MatchedRunIDs = sortedRunIDs(matchedRunIDs)
	result.RunsMatched = len(result.MatchedRunIDs)
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

func (runner *Runner) promptConfigMap(runID string, role string, prompt string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:   promptConfigMapName(runID),
			Labels: runnerLabels(runID, role),
		},
		Data: map[string]string{
			"prompt.md": prompt,
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
					SecurityContext:              runnerPodSecurityContext(),
					RestartPolicy:                corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "runner",
							Image:           runner.agentRunnerImage,
							Command:         []string{"matter-codex-agent-runner"},
							Args:            []string{"smoke"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: runnerContainerSecurityContext(),
							Env: []corev1.EnvVar{
								{Name: "MATTERCODEX_RUN_ID", Value: runID},
								{Name: "MATTERCODEX_AGENT_ROLE", Value: role},
							},
							VolumeMounts: append([]corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
							}, runnerWritableVolumeMounts()...),
						},
					},
					Volumes: append([]corev1.Volume{
						{
							Name: "workspace",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: workspacePVCName(runID),
								},
							},
						},
					}, runnerWritableVolumes()...),
				},
			},
		},
	}
}

func (runner *Runner) developerJob(input runtimerepo.DeveloperRunInput) *batchv1.Job {
	backoffLimit := int32(0)
	codexAuthSecretName := defaultString(input.CodexAuthSecretName, runner.codexAuthSecretName)
	gitHubSecretName := defaultString(input.GitHubSecretName, runner.gitHubSecretName)
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
					SecurityContext:              runnerPodSecurityContext(),
					RestartPolicy:                corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "runner",
							Image:           runner.agentRunnerImage,
							Command:         []string{"matter-codex-agent-runner"},
							Args:            []string{"developer"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: runnerContainerSecurityContext(),
							Env: []corev1.EnvVar{
								{Name: "MATTERCODEX_RUN_ID", Value: input.RunID},
								{Name: "MATTERCODEX_AGENT_PROFILE", Value: input.Profile},
								{Name: "MATTERCODEX_REPO_PROVIDER", Value: input.Provider},
								{Name: "MATTERCODEX_REPO_OWNER", Value: input.Owner},
								{Name: "MATTERCODEX_REPO_NAME", Value: input.Name},
								{Name: "MATTERCODEX_BASE_BRANCH", Value: input.BaseBranch},
								{Name: "MATTERCODEX_HEAD_BRANCH", Value: input.HeadBranch},
								{Name: "MATTERCODEX_PR_TITLE", Value: input.Title},
							},
							VolumeMounts: append([]corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
								{Name: codexAuthSecretVolume, MountPath: "/var/run/secrets/matter-codex-codex", ReadOnly: true},
								{Name: gitHubSecretVolume, MountPath: "/var/run/secrets/matter-codex-github", ReadOnly: true},
								{Name: promptVolume, MountPath: "/var/run/matter-codex-prompt", ReadOnly: true},
							}, runnerWritableVolumeMounts()...),
						},
					},
					Volumes: append([]corev1.Volume{
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
									SecretName: gitHubSecretName,
									Items: []corev1.KeyToPath{
										{Key: "github-token", Path: "github-token"},
										{Key: "github-username", Path: "github-username"},
										{Key: "github-email", Path: "github-email"},
									},
								},
							},
						},
						{
							Name: promptVolume,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: promptConfigMapName(input.RunID)},
									Items: []corev1.KeyToPath{
										{Key: "prompt.md", Path: "prompt.md"},
									},
								},
							},
						},
					}, runnerWritableVolumes()...),
				},
			},
		},
	}
}

func (runner *Runner) reviewJob(input runtimerepo.ReviewRunInput) *batchv1.Job {
	backoffLimit := int32(0)
	codexAuthSecretName := defaultString(input.CodexAuthSecretName, runner.codexAuthSecretName)
	gitHubSecretName := defaultString(input.GitHubSecretName, runner.gitHubSecretName)
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
					SecurityContext:              runnerPodSecurityContext(),
					RestartPolicy:                corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "runner",
							Image:           runner.agentRunnerImage,
							Command:         []string{"matter-codex-agent-runner"},
							Args:            []string{"reviewer"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: runnerContainerSecurityContext(),
							Env: []corev1.EnvVar{
								{Name: "MATTERCODEX_RUN_ID", Value: input.RunID},
								{Name: "MATTERCODEX_AGENT_PROFILE", Value: input.Profile},
								{Name: "MATTERCODEX_REPO_PROVIDER", Value: input.Provider},
								{Name: "MATTERCODEX_REPO_OWNER", Value: input.Owner},
								{Name: "MATTERCODEX_REPO_NAME", Value: input.Name},
								{Name: "MATTERCODEX_PR_NUMBER", Value: strconv.Itoa(input.PRNumber)},
							},
							VolumeMounts: append([]corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
								{Name: codexAuthSecretVolume, MountPath: "/var/run/secrets/matter-codex-codex", ReadOnly: true},
								{Name: gitHubSecretVolume, MountPath: "/var/run/secrets/matter-codex-github", ReadOnly: true},
								{Name: promptVolume, MountPath: "/var/run/matter-codex-prompt", ReadOnly: true},
							}, runnerWritableVolumeMounts()...),
						},
					},
					Volumes: append([]corev1.Volume{
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
									SecretName: gitHubSecretName,
									Items: []corev1.KeyToPath{
										{Key: "github-token", Path: "github-token"},
										{Key: "github-username", Path: "github-username"},
										{Key: "github-email", Path: "github-email"},
									},
								},
							},
						},
						{
							Name: promptVolume,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: promptConfigMapName(input.RunID)},
									Items: []corev1.KeyToPath{
										{Key: "prompt.md", Path: "prompt.md"},
									},
								},
							},
						},
					}, runnerWritableVolumes()...),
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
					SecurityContext:              runnerPodSecurityContext(),
					RestartPolicy:                corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "runner",
							Image:           runner.agentRunnerImage,
							Command:         []string{"matter-codex-agent-runner"},
							Args:            []string{"codex-auth"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: runnerContainerSecurityContext(),
							Env: []corev1.EnvVar{
								{Name: "MATTERCODEX_OPENAI_ACCOUNT", Value: accountName},
								{Name: "MATTERCODEX_CODEX_AUTH_SECRET", Value: secretName},
							},
							VolumeMounts: append([]corev1.VolumeMount{
								{Name: "codex-home", MountPath: "/codex-home"},
							}, runnerWritableVolumeMounts()...),
						},
					},
					Volumes: append([]corev1.Volume{
						{
							Name: "codex-home",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					}, runnerWritableVolumes()...),
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
	_, err := runner.execPod(ctx, podName, []string{"matter-codex-agent-runner", "auth-ready-check"})
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

func runnerLabelSelector() string {
	return labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/name":      "matter-codex-agent-runner",
		"app.kubernetes.io/component": runnerComponent,
	}).String()
}

func jobCompletedOrCreatedAt(job batchv1.Job) time.Time {
	if job.Status.CompletionTime != nil && !job.Status.CompletionTime.IsZero() {
		return job.Status.CompletionTime.Time
	}
	return job.CreationTimestamp.Time
}

func olderThan(value time.Time, cutoff time.Time) bool {
	return !value.IsZero() && value.Before(cutoff)
}

func runnerRetentionResourceExpired(runID string, createdAt time.Time, cutoff time.Time, jobByRunID map[string]batchv1.Job, matchedRunIDs map[string]struct{}) bool {
	if runID == "" {
		return false
	}
	if _, ok := matchedRunIDs[runID]; ok {
		return true
	}
	job, ok := jobByRunID[runID]
	if !ok {
		return olderThan(createdAt, cutoff)
	}
	if job.Status.Active > 0 {
		return false
	}
	if job.Status.Succeeded == 0 && job.Status.Failed == 0 {
		return false
	}
	return olderThan(jobCompletedOrCreatedAt(job), cutoff)
}

func sortedRunIDs(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	runIDs := make([]string, 0, len(values))
	for runID := range values {
		if runID != "" {
			runIDs = append(runIDs, runID)
		}
	}
	sort.Strings(runIDs)
	return runIDs
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

func promptConfigMapName(runID string) string {
	return "mc-prompt-" + runID
}

func normalizeDeveloperRunInput(input runtimerepo.DeveloperRunInput) runtimerepo.DeveloperRunInput {
	input.RunID = strings.TrimSpace(input.RunID)
	input.Profile = defaultString(input.Profile, "developer")
	input.CodexAuthSecretName = strings.TrimSpace(input.CodexAuthSecretName)
	input.GitHubSecretName = strings.TrimSpace(input.GitHubSecretName)
	input.Provider = defaultString(input.Provider, "github")
	input.Owner = strings.TrimSpace(input.Owner)
	input.Name = strings.TrimSpace(input.Name)
	input.BaseBranch = defaultString(input.BaseBranch, "main")
	input.HeadBranch = strings.TrimSpace(input.HeadBranch)
	input.Title = strings.TrimSpace(input.Title)
	input.Task = strings.TrimSpace(input.Task)
	return input
}

func normalizeReviewRunInput(input runtimerepo.ReviewRunInput) runtimerepo.ReviewRunInput {
	input.RunID = strings.TrimSpace(input.RunID)
	input.Profile = defaultString(input.Profile, "reviewer")
	input.CodexAuthSecretName = strings.TrimSpace(input.CodexAuthSecretName)
	input.GitHubSecretName = strings.TrimSpace(input.GitHubSecretName)
	input.Provider = defaultString(input.Provider, "github")
	input.Owner = strings.TrimSpace(input.Owner)
	input.Name = strings.TrimSpace(input.Name)
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

func runnerPodSecurityContext() *corev1.PodSecurityContext {
	fsGroupChangePolicy := corev1.FSGroupChangeOnRootMismatch
	return &corev1.PodSecurityContext{
		RunAsNonRoot:        boolPtr(true),
		RunAsUser:           int64Ptr(runnerUID),
		RunAsGroup:          int64Ptr(runnerGID),
		FSGroup:             int64Ptr(runnerGID),
		FSGroupChangePolicy: &fsGroupChangePolicy,
		SeccompProfile:      &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
}

func runnerContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		RunAsNonRoot:             boolPtr(true),
		RunAsUser:                int64Ptr(runnerUID),
		RunAsGroup:               int64Ptr(runnerGID),
		AllowPrivilegeEscalation: boolPtr(false),
		ReadOnlyRootFilesystem:   boolPtr(true),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

func runnerWritableVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: runnerHomeVolume, MountPath: runnerHomePath},
		{Name: runnerTmpVolume, MountPath: runnerTmpPath},
	}
}

func runnerWritableVolumes() []corev1.Volume {
	return []corev1.Volume{
		{Name: runnerHomeVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: runnerTmpVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}
