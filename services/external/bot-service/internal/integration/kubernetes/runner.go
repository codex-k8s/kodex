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
)

type Config struct {
	Namespace                 string
	KubeconfigPath            string
	SmokeImage                string
	WorkspaceStorageSize      string
	JobTTLSecondsAfterFinish  int32
	LogTailLines              int64
	AgentRunnerServiceAccount string
}

type Runner struct {
	client                    kubernetes.Interface
	namespace                 string
	smokeImage                string
	workspaceStorage          resource.Quantity
	jobTTLSecondsAfterFinish  int32
	logTailLines              int64
	agentRunnerServiceAccount string
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
		workspaceStorage:          storage,
		jobTTLSecondsAfterFinish:  defaultInt32(cfg.JobTTLSecondsAfterFinish, 86400),
		logTailLines:              defaultInt64(cfg.LogTailLines, 40),
		agentRunnerServiceAccount: defaultString(cfg.AgentRunnerServiceAccount, "matter-codex-agent-runner"),
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
