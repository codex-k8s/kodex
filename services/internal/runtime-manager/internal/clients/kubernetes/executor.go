// Package kubernetes contains the runtime-manager Kubernetes job executor.
package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	fleetclient "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/clients/fleet"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	managedBy                = "runtime-manager"
	runtimePartOf            = "kodex"
	runtimeJobLabel          = "kodex.k8s.io/runtime-job-id"
	runtimeJobTypeLabel      = "kodex.k8s.io/runtime-job-type"
	healthCheckContainerName = "runtime-health-check"
	agentRunContainerName    = "runtime-agent-runner"
	defaultImagePullPolicy   = "IfNotPresent"
	maxMetadataItems         = 16
	maxAgentRunEnvValueBytes = 16 * 1024
	maxAgentRunReporterBytes = 512
	workspaceVolumeName      = "workspace"
	agentRunWorkspacePath    = "/workspace"
	agentRunContextPath      = "/workspace/.kodex/context/agent-run.json"
	agentRunCommand          = "/kodex/bin/agent-runner"
	agentRunCommandKind      = "run"
	agentRunRunnerModeCodex  = "codex_agent"
	agentManagerGRPCAddrEnv  = "KODEX_AGENT_RUNNER_AGENT_MANAGER_GRPC_ADDR"
	agentManagerAuthTokenEnv = "KODEX_AGENT_RUNNER_AGENT_MANAGER_GRPC_AUTH_TOKEN"
	agentManagerTimeoutEnv   = "KODEX_AGENT_RUNNER_AGENT_MANAGER_TIMEOUT"
)

// Config constrains Kubernetes executor behavior with operator-managed settings.
type Config struct {
	DefaultNamespace        string
	DefaultServiceAccount   string
	DefaultImage            string
	ImagePullPolicy         string
	JobTimeout              time.Duration
	PollInterval            time.Duration
	BackoffLimit            int32
	TTLSecondsAfterFinished int32
	LogTailBytes            int64
	AgentManagerGRPCAddr    string
	AgentManagerAuthSecret  SecretKeyRef
	AgentManagerTimeout     time.Duration
}

// SecretKeyRef points to a Kubernetes Secret key without carrying the secret value.
type SecretKeyRef struct {
	Name string
	Key  string
}

// ClusterAccessProvider obtains safe cluster secret references through fleet-manager.
type ClusterAccessProvider interface {
	GetClusterAccess(ctx context.Context, clusterID uuid.UUID) (fleetclient.ClusterAccess, error)
}

type clientFactory interface {
	NewForKubeconfig(kubeconfig []byte) (kubernetes.Interface, error)
}

type realClientFactory struct{}

func (realClientFactory) NewForKubeconfig(kubeconfig []byte) (kubernetes.Interface, error) {
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	config.UserAgent = "kodex-runtime-manager"
	return kubernetes.NewForConfig(config)
}

// Executor creates and observes bounded Kubernetes Jobs for runtime-manager jobs.
type Executor struct {
	clusters ClusterAccessProvider
	secrets  secretresolver.Resolver
	clients  clientFactory
	config   Config
}

// StartedJob describes a Kubernetes Job created for a runtime-manager job.
type StartedJob struct {
	RuntimeJobID uuid.UUID
	ClusterID    uuid.UUID
	Namespace    string
	JobName      string
	ExternalRef  string
	ArtifactRefs []runtimeservice.RuntimeArtifactRefInput
	client       kubernetes.Interface
	config       Config
	selector     labels.Set
	collectLogs  bool
}

// ExecutionResult contains a bounded execution result for runtime-manager lifecycle commands.
type ExecutionResult struct {
	Succeeded    bool
	Interrupted  bool
	ShortLogTail string
	ErrorCode    string
	ErrorMessage string
}

// ExecutionError contains a classified error suitable for runtime-manager diagnostics.
type ExecutionError struct {
	Code    string
	Message string
}

func (e *ExecutionError) Error() string {
	return strings.TrimSpace(e.Code) + ": " + strings.TrimSpace(e.Message)
}

// NewExecutor creates a Kubernetes executor with real client-go clients.
func NewExecutor(clusters ClusterAccessProvider, secrets secretresolver.Resolver, cfg Config) (*Executor, error) {
	return NewExecutorWithClientFactory(clusters, secrets, cfg, realClientFactory{})
}

// NewExecutorWithClientFactory is used by tests without a real cluster.
func NewExecutorWithClientFactory(clusters ClusterAccessProvider, secrets secretresolver.Resolver, cfg Config, clients clientFactory) (*Executor, error) {
	if clusters == nil || secrets == nil || clients == nil {
		return nil, newExecutionError("runtime_kubernetes_executor_not_configured", "Kubernetes executor dependencies are not configured")
	}
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Executor{clusters: clusters, secrets: secrets, clients: clients, config: normalized}, nil
}

// Start creates or reuses a deterministic Kubernetes Job for the claimed runtime job.
func (e *Executor) Start(ctx context.Context, job entity.Job) (StartedJob, error) {
	if job.JobType != enum.JobTypeHealthCheck && job.JobType != enum.JobTypeAgentRun {
		return StartedJob{}, newExecutionError("unsupported_job_type", "Kubernetes executor supports only health_check and agent_run jobs")
	}
	if job.ClusterID == nil || *job.ClusterID == uuid.Nil {
		return StartedJob{}, newExecutionError("missing_cluster_ref", "Runtime job does not have a Kubernetes cluster ref")
	}
	spec, err := e.executionSpec(job)
	if err != nil {
		return StartedJob{}, err
	}
	access, err := e.clusters.GetClusterAccess(ctx, *job.ClusterID)
	if err != nil {
		return StartedJob{}, clusterAccessError(err)
	}
	if job.FleetScopeID != nil && *job.FleetScopeID != access.FleetScopeID {
		return StartedJob{}, newExecutionError("cluster_scope_mismatch", "Runtime job fleet scope does not match Kubernetes cluster scope")
	}
	client, err := e.clientForCluster(ctx, access)
	if err != nil {
		return StartedJob{}, err
	}
	jobName := runtimeJobName(job.ID)
	selector := labels.Set{runtimeJobLabel: job.ID.String()}
	kubernetesJob := buildJob(job, spec, e.config, jobName, selector)
	created, err := client.BatchV1().Jobs(spec.Namespace).Create(ctx, kubernetesJob, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = client.BatchV1().Jobs(spec.Namespace).Get(ctx, jobName, metav1.GetOptions{})
	}
	if err != nil {
		return StartedJob{}, newExecutionError("kubernetes_job_create_failed", "Kubernetes Job could not be created")
	}
	if !isManagedRuntimeJob(created, job) {
		return StartedJob{}, newExecutionError("kubernetes_job_name_conflict", "Kubernetes Job name is already used by a different object")
	}
	ref := kubernetesJobRef(access.ClusterID, spec.Namespace, created.GetName())
	return StartedJob{
		RuntimeJobID: job.ID,
		ClusterID:    access.ClusterID,
		Namespace:    spec.Namespace,
		JobName:      created.GetName(),
		ExternalRef:  ref,
		ArtifactRefs: runtimeArtifactRefs(access.ClusterID, spec, ref),
		client:       client,
		config:       e.config,
		selector:     selector,
		collectLogs:  spec.CollectPodLogs,
	}, nil
}

// Wait waits for a terminal Kubernetes Job status and returns bounded diagnostics.
func (e *Executor) Wait(ctx context.Context, started StartedJob) ExecutionResult {
	timeout := started.config.JobTimeout
	if timeout <= 0 {
		timeout = e.config.JobTimeout
	}
	pollInterval := started.config.PollInterval
	if pollInterval <= 0 {
		pollInterval = e.config.PollInterval
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if errors.Is(ctx.Err(), context.Canceled) {
			return interruptedExecutionResult()
		}
		result, done := e.observe(waitCtx, started)
		if done {
			return result
		}
		select {
		case <-waitCtx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return interruptedExecutionResult()
			}
			logCtx, logCancel := context.WithTimeout(context.Background(), 2*time.Second)
			tail := e.shortLogTail(logCtx, started)
			logCancel()
			return ExecutionResult{
				ShortLogTail: tail,
				ErrorCode:    "kubernetes_job_timeout",
				ErrorMessage: "Kubernetes Job did not finish before timeout",
			}
		case <-ticker.C:
		}
	}
}

// ErrorDiagnostic maps executor errors to safe runtime-manager diagnostics.
func ErrorDiagnostic(err error) (string, string) {
	var executionErr *ExecutionError
	if errors.As(err, &executionErr) {
		return executionErr.Code, executionErr.Message
	}
	return "runtime_kubernetes_error", "Kubernetes executor failed"
}

func (e *Executor) observe(ctx context.Context, started StartedJob) (ExecutionResult, bool) {
	job, err := started.client.BatchV1().Jobs(started.Namespace).Get(ctx, started.JobName, metav1.GetOptions{})
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return interruptedExecutionResult(), true
		}
		if apierrors.IsNotFound(err) {
			return ExecutionResult{
				ErrorCode:    "kubernetes_job_cancelled",
				ErrorMessage: "Kubernetes Job was deleted before completion",
			}, true
		}
		return ExecutionResult{
			ErrorCode:    "kubernetes_job_status_unavailable",
			ErrorMessage: "Kubernetes Job status is unavailable",
		}, true
	}
	if job.DeletionTimestamp != nil {
		return ExecutionResult{
			ShortLogTail: e.shortLogTail(ctx, started),
			ErrorCode:    "kubernetes_job_cancelled",
			ErrorMessage: "Kubernetes Job was deleted before completion",
		}, true
	}
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			return ExecutionResult{Succeeded: true, ShortLogTail: e.shortLogTail(ctx, started)}, true
		case batchv1.JobFailed:
			return ExecutionResult{
				ShortLogTail: e.shortLogTail(ctx, started),
				ErrorCode:    "kubernetes_job_failed",
				ErrorMessage: "Kubernetes Job failed",
			}, true
		}
	}
	return ExecutionResult{}, false
}

func interruptedExecutionResult() ExecutionResult {
	return ExecutionResult{
		Interrupted:  true,
		ErrorCode:    "runtime_worker_stopped",
		ErrorMessage: "Runtime worker stopped before Kubernetes Job reached a terminal state",
	}
}

func (e *Executor) clientForCluster(ctx context.Context, access fleetclient.ClusterAccess) (kubernetes.Interface, error) {
	secret, err := e.secrets.Resolve(ctx, secretresolver.SecretRef{StoreType: access.SecretStoreType, StoreRef: access.SecretStoreRef})
	if err != nil {
		return nil, secretResolverError(err)
	}
	defer secret.Clear()
	kubeconfig := secret.Bytes()
	defer clear(kubeconfig)
	client, err := e.clients.NewForKubeconfig(kubeconfig)
	if err != nil {
		return nil, newExecutionError("kubernetes_client_init_failed", "Kubernetes client could not be initialized")
	}
	return client, nil
}

type executionSpec struct {
	Namespace                string
	ServiceAccount           string
	Image                    string
	ImagePullPolicy          corev1.PullPolicy
	Labels                   map[string]string
	ContainerName            string
	Command                  []string
	Args                     []string
	Env                      []corev1.EnvVar
	Volumes                  []corev1.Volume
	VolumeMounts             []corev1.VolumeMount
	PodSecurityContext       *corev1.PodSecurityContext
	ContainerSecurityContext *corev1.SecurityContext
	ImageArtifactRef         string
	CollectPodLogs           bool
}

type restrictedJobInput struct {
	Namespace      string            `json:"namespace"`
	ServiceAccount string            `json:"service_account"`
	Image          string            `json:"image"`
	Labels         map[string]string `json:"labels"`
}

func (e *Executor) executionSpec(job entity.Job) (executionSpec, error) {
	switch job.JobType {
	case enum.JobTypeHealthCheck:
		return e.healthCheckExecutionSpec(job.JobInputJSON)
	case enum.JobTypeAgentRun:
		return e.agentRunExecutionSpec(job)
	default:
		return executionSpec{}, newExecutionError("unsupported_job_type", "Kubernetes executor supports only health_check and agent_run jobs")
	}
}

func (e *Executor) healthCheckExecutionSpec(payload []byte) (executionSpec, error) {
	input, err := parseRestrictedJobInput(payload)
	if err != nil {
		return executionSpec{}, err
	}
	spec := executionSpec{
		Namespace:       firstNonEmpty(input.Namespace, e.config.DefaultNamespace),
		ServiceAccount:  firstNonEmpty(input.ServiceAccount, e.config.DefaultServiceAccount),
		Image:           firstNonEmpty(input.Image, e.config.DefaultImage),
		ImagePullPolicy: corev1.PullPolicy(e.config.ImagePullPolicy),
		Labels:          input.Labels,
		ContainerName:   healthCheckContainerName,
		Command:         []string{"/bin/sh", "-ec"},
		Args:            []string{"echo kodex runtime health check"},
		CollectPodLogs:  true,
	}
	if err := validateExecutionSpec(spec); err != nil {
		return executionSpec{}, err
	}
	return spec, nil
}

func (e *Executor) agentRunExecutionSpec(job entity.Job) (executionSpec, error) {
	spec, ok := runtimeservice.AgentRunExecutionSpecFromJobInput(job.JobInputJSON)
	if !ok || spec == nil {
		if agentRunExecutionSpecFieldPresent(job.JobInputJSON) {
			return executionSpec{}, newExecutionError("invalid_agent_run_execution_spec", "agent_run execution spec is invalid")
		}
		return executionSpec{}, newExecutionError("agent_run_execution_spec_required", "agent_run execution spec is required before Kubernetes execution")
	}
	if job.AgentRunID == nil || *job.AgentRunID != spec.AgentRunID || job.SlotID == nil || *job.SlotID != spec.SlotID {
		return executionSpec{}, newExecutionError("agent_run_execution_spec_mismatch", "agent_run execution spec does not match runtime job refs")
	}
	pvc, err := parseWorkspacePVCRef(spec.WorkspacePVCRef, e.config.DefaultNamespace)
	if err != nil {
		return executionSpec{}, err
	}
	image, err := runnerImageFromRef(spec.RunnerImageRef)
	if err != nil {
		return executionSpec{}, err
	}
	mode, err := agentRunRunnerMode(spec.RunnerMode)
	if err != nil {
		return executionSpec{}, err
	}
	env, err := agentRunEnv(job, *spec, mode, e.config)
	if err != nil {
		return executionSpec{}, err
	}
	result := executionSpec{
		Namespace:                pvc.Namespace,
		ServiceAccount:           e.config.DefaultServiceAccount,
		Image:                    image,
		ImagePullPolicy:          corev1.PullPolicy(e.config.ImagePullPolicy),
		ContainerName:            agentRunContainerName,
		Command:                  []string{agentRunCommand},
		Args:                     []string{agentRunCommandKind},
		Env:                      env,
		ImageArtifactRef:         spec.RunnerImageRef,
		PodSecurityContext:       restrictedAgentRunPodSecurityContext(),
		ContainerSecurityContext: restrictedAgentRunContainerSecurityContext(),
		CollectPodLogs:           false,
		Volumes: []corev1.Volume{{
			Name: workspaceVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc.ClaimName},
			},
		}},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      workspaceVolumeName,
			MountPath: agentRunWorkspacePath,
			ReadOnly:  false,
		}},
	}
	if err := validateExecutionSpec(result); err != nil {
		return executionSpec{}, err
	}
	return result, nil
}

func parseRestrictedJobInput(payload []byte) (restrictedJobInput, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return restrictedJobInput{}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var input restrictedJobInput
	if err := decoder.Decode(&input); err != nil {
		return restrictedJobInput{}, newExecutionError("invalid_job_input", "Runtime job input is not a supported Kubernetes executor spec")
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return restrictedJobInput{}, newExecutionError("invalid_job_input", "Runtime job input contains multiple JSON values")
	}
	return input, nil
}

func agentRunExecutionSpecFieldPresent(payload []byte) bool {
	var document struct {
		AgentRunExecutionSpec json.RawMessage `json:"agent_run_execution_spec"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(payload), &document); err != nil {
		return false
	}
	trimmed := bytes.TrimSpace(document.AgentRunExecutionSpec)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

type workspacePVCRef struct {
	Namespace string
	ClaimName string
}

func parseWorkspacePVCRef(raw string, defaultNamespace string) (workspacePVCRef, error) {
	value := strings.TrimSpace(raw)
	namespace := strings.TrimSpace(defaultNamespace)
	claimName := ""
	switch {
	case value == "":
		return workspacePVCRef{}, newExecutionError("agent_run_workspace_pvc_ref_required", "agent_run workspace PVC ref is required")
	case strings.HasPrefix(value, "pvc://"):
		parts := strings.Split(strings.TrimPrefix(value, "pvc://"), "/")
		if len(parts) != 2 {
			return workspacePVCRef{}, newExecutionError("invalid_agent_run_workspace_pvc_ref", "agent_run workspace PVC ref is invalid")
		}
		namespace = strings.TrimSpace(parts[0])
		claimName = strings.TrimSpace(parts[1])
	case strings.HasPrefix(value, "k8s://pvc/"):
		claimName = strings.TrimSpace(strings.TrimPrefix(value, "k8s://pvc/"))
	case !strings.Contains(value, "://"):
		claimName = value
	default:
		return workspacePVCRef{}, newExecutionError("invalid_agent_run_workspace_pvc_ref", "agent_run workspace PVC ref is invalid")
	}
	if errs := validation.IsDNS1123Label(namespace); len(errs) > 0 {
		return workspacePVCRef{}, newExecutionError("invalid_agent_run_workspace_pvc_ref", "agent_run workspace PVC namespace is invalid")
	}
	if errs := validation.IsDNS1123Subdomain(claimName); len(errs) > 0 {
		return workspacePVCRef{}, newExecutionError("invalid_agent_run_workspace_pvc_ref", "agent_run workspace PVC claim name is invalid")
	}
	return workspacePVCRef{Namespace: namespace, ClaimName: claimName}, nil
}

func runnerImageFromRef(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "image://")
	if strings.TrimSpace(value) == "" || strings.ContainsAny(value, " \t\r\n") || len(value) > 512 {
		return "", newExecutionError("invalid_agent_run_runner_image_ref", "agent_run runner image ref is invalid")
	}
	return value, nil
}

func agentRunRunnerMode(mode enum.AgentRunRunnerMode) (string, error) {
	if mode != enum.AgentRunRunnerModeCodexAgent {
		return "", newExecutionError("unsupported_agent_run_runner_mode", "agent_run runner mode is not supported")
	}
	return agentRunRunnerModeCodex, nil
}

func agentRunEnv(job entity.Job, spec runtimeservice.AgentRunExecutionSpecInput, mode string, cfg Config) ([]corev1.EnvVar, error) {
	allowedSecretRefs, err := agentRunRefsJSON(spec.AllowedSecretRefs)
	if err != nil {
		return nil, err
	}
	reportingTargetRefs, err := agentRunRefsJSON(spec.ReportingTargetRefs)
	if err != nil {
		return nil, err
	}
	env := []corev1.EnvVar{
		{Name: "KODEX_AGENT_RUN_ID", Value: spec.AgentRunID.String()},
		{Name: "KODEX_RUNTIME_JOB_ID", Value: job.ID.String()},
		{Name: "KODEX_RUNTIME_SLOT_ID", Value: spec.SlotID.String()},
		{Name: "KODEX_RUNTIME_MATERIALIZATION_ID", Value: spec.ExpectedMaterializationID.String()},
		{Name: "KODEX_RUNTIME_MATERIALIZATION_FINGERPRINT", Value: spec.ExpectedMaterializationFingerprint},
		{Name: "KODEX_RUNTIME_WORKSPACE_REF", Value: spec.WorkspaceRef},
		{Name: "KODEX_RUNTIME_WORKSPACE_MOUNT_REF", Value: spec.WorkspaceMountRef},
		{Name: "KODEX_RUNTIME_WORKSPACE_MOUNT_PATH", Value: agentRunWorkspacePath},
		{Name: "KODEX_AGENT_RUN_CONTEXT_REF", Value: spec.ContextRef},
		{Name: "KODEX_AGENT_RUN_CONTEXT_DIGEST", Value: spec.ContextDigest},
		{Name: "KODEX_AGENT_RUN_CONTEXT_PATH", Value: agentRunContextPath},
		{Name: "KODEX_AGENT_RUNNER_PROFILE_REF", Value: spec.RunnerProfileRef},
		{Name: "KODEX_AGENT_RUNNER_MODE", Value: mode},
		{Name: "KODEX_AGENT_RUN_ALLOWED_SECRET_REFS_JSON", Value: allowedSecretRefs},
		{Name: "KODEX_AGENT_RUN_REPORTING_TARGET_REFS_JSON", Value: reportingTargetRefs},
	}
	if spec.CodexSessionExecutionSpec != nil {
		codexSpec, err := agentRunCodexSessionExecutionSpecJSON(*spec.CodexSessionExecutionSpec)
		if err != nil {
			return nil, err
		}
		env = append(env, corev1.EnvVar{Name: "KODEX_CODEX_SESSION_EXECUTION_SPEC_JSON", Value: codexSpec})
	}
	reporterEnv, err := agentRunReporterEnv(cfg)
	if err != nil {
		return nil, err
	}
	env = append(env, reporterEnv...)
	return env, nil
}

func agentRunReporterEnv(cfg Config) ([]corev1.EnvVar, error) {
	addr := strings.TrimSpace(cfg.AgentManagerGRPCAddr)
	secretName := strings.TrimSpace(cfg.AgentManagerAuthSecret.Name)
	secretKey := strings.TrimSpace(cfg.AgentManagerAuthSecret.Key)
	if addr == "" && secretName == "" && secretKey == "" {
		return nil, nil
	}
	if addr == "" || secretName == "" || secretKey == "" {
		return nil, newExecutionError("invalid_agent_run_reporter_config", "agent_run reporter config is incomplete")
	}
	env := []corev1.EnvVar{
		{Name: agentManagerGRPCAddrEnv, Value: addr},
		{
			Name: agentManagerAuthTokenEnv,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  secretKey,
				},
			},
		},
	}
	if cfg.AgentManagerTimeout > 0 {
		env = append(env, corev1.EnvVar{Name: agentManagerTimeoutEnv, Value: cfg.AgentManagerTimeout.String()})
	}
	return env, nil
}

func agentRunRefsJSON(refs []runtimeservice.AgentRunExecutionRefInput) (string, error) {
	if len(refs) == 0 {
		return "[]", nil
	}
	raw, err := json.Marshal(refs)
	if err != nil {
		return "", newExecutionError("invalid_agent_run_execution_refs", "agent_run execution refs are invalid")
	}
	if len(raw) > maxAgentRunEnvValueBytes {
		return "", newExecutionError("agent_run_execution_refs_too_large", "agent_run execution refs input is too large")
	}
	return string(raw), nil
}

func agentRunCodexSessionExecutionSpecJSON(spec runtimeservice.CodexSessionExecutionSpecInput) (string, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return "", newExecutionError("invalid_codex_session_execution_spec", "codex session execution spec is invalid")
	}
	if len(raw) > maxAgentRunEnvValueBytes {
		return "", newExecutionError("codex_session_execution_spec_too_large", "codex session execution spec input is too large")
	}
	return string(raw), nil
}

func validateExecutionSpec(spec executionSpec) error {
	if errs := validation.IsDNS1123Label(spec.Namespace); len(errs) > 0 {
		return newExecutionError("invalid_job_input", "Kubernetes executor namespace is invalid")
	}
	if errs := validation.IsDNS1123Label(spec.ContainerName); len(errs) > 0 {
		return newExecutionError("invalid_job_input", "Kubernetes executor container name is invalid")
	}
	if strings.TrimSpace(spec.Image) == "" || strings.ContainsAny(spec.Image, " \t\r\n") || len(spec.Image) > 512 {
		return newExecutionError("invalid_job_input", "Kubernetes executor image ref is invalid")
	}
	if spec.ServiceAccount != "" {
		if errs := validation.IsDNS1123Subdomain(spec.ServiceAccount); len(errs) > 0 {
			return newExecutionError("invalid_job_input", "Kubernetes executor service account is invalid")
		}
	}
	return validateLabels(spec.Labels)
}

func validateLabels(values map[string]string) error {
	if len(values) > maxMetadataItems {
		return newExecutionError("invalid_job_input", "Kubernetes executor labels input is too large")
	}
	for key, value := range values {
		if errs := validation.IsQualifiedName(key); len(errs) > 0 {
			return newExecutionError("invalid_job_input", "Kubernetes executor label key is invalid")
		}
		if errs := validation.IsValidLabelValue(value); len(errs) > 0 {
			return newExecutionError("invalid_job_input", "Kubernetes executor label value is invalid")
		}
	}
	return nil
}

func buildJob(job entity.Job, spec executionSpec, cfg Config, name string, selector labels.Set) *batchv1.Job {
	metadataLabels := map[string]string{}
	for key, value := range spec.Labels {
		metadataLabels[key] = value
	}
	for key, value := range managedLabels(job, selector) {
		metadataLabels[key] = value
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: spec.Namespace,
			Labels:    metadataLabels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            int32Ptr(cfg.BackoffLimit),
			TTLSecondsAfterFinished: int32Ptr(cfg.TTLSecondsAfterFinished),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: metadataLabels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyNever,
					AutomountServiceAccountToken: boolPtr(false),
					ServiceAccountName:           spec.ServiceAccount,
					SecurityContext:              spec.PodSecurityContext.DeepCopy(),
					Volumes:                      append([]corev1.Volume(nil), spec.Volumes...),
					Containers: []corev1.Container{{
						Name:            spec.ContainerName,
						Image:           spec.Image,
						ImagePullPolicy: spec.ImagePullPolicy,
						Command:         append([]string(nil), spec.Command...),
						Args:            append([]string(nil), spec.Args...),
						Env:             append([]corev1.EnvVar(nil), spec.Env...),
						VolumeMounts:    append([]corev1.VolumeMount(nil), spec.VolumeMounts...),
						SecurityContext: spec.ContainerSecurityContext.DeepCopy(),
					}},
				},
			},
		},
	}
}

func restrictedAgentRunPodSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		RunAsNonRoot: boolPtr(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func restrictedAgentRunContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: boolPtr(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{corev1.Capability("ALL")},
		},
		Privileged:   boolPtr(false),
		RunAsNonRoot: boolPtr(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func runtimeArtifactRefs(clusterID uuid.UUID, spec executionSpec, jobRef string) []runtimeservice.RuntimeArtifactRefInput {
	refs := []runtimeservice.RuntimeArtifactRefInput{
		{ArtifactType: enum.RuntimeArtifactTypeKubernetesJob, ExternalRef: jobRef, MetadataJSON: []byte(`{}`)},
		{ArtifactType: enum.RuntimeArtifactTypeNamespace, ExternalRef: namespaceRef(clusterID, spec.Namespace), MetadataJSON: []byte(`{}`)},
	}
	if strings.TrimSpace(spec.ImageArtifactRef) != "" {
		refs = append(refs, runtimeservice.RuntimeArtifactRefInput{
			ArtifactType: enum.RuntimeArtifactTypeImageRef,
			ExternalRef:  spec.ImageArtifactRef,
			MetadataJSON: []byte(`{}`),
		})
	}
	return refs
}

func managedLabels(job entity.Job, selector labels.Set) map[string]string {
	result := map[string]string{
		"app.kubernetes.io/name":       "runtime-job",
		"app.kubernetes.io/part-of":    runtimePartOf,
		"app.kubernetes.io/managed-by": managedBy,
		runtimeJobTypeLabel:            string(job.JobType),
	}
	for key, value := range selector {
		result[key] = value
	}
	return result
}

func isManagedRuntimeJob(job *batchv1.Job, runtimeJob entity.Job) bool {
	if job == nil {
		return false
	}
	return job.Labels["app.kubernetes.io/managed-by"] == managedBy &&
		job.Labels[runtimeJobLabel] == runtimeJob.ID.String() &&
		job.Labels[runtimeJobTypeLabel] == string(runtimeJob.JobType)
}

func (e *Executor) shortLogTail(ctx context.Context, started StartedJob) string {
	if !started.collectLogs {
		return ""
	}
	pods, err := started.client.CoreV1().Pods(started.Namespace).List(ctx, metav1.ListOptions{LabelSelector: started.selector.String()})
	if err != nil || len(pods.Items) == 0 {
		return ""
	}
	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].CreationTimestamp.After(pods.Items[j].CreationTimestamp.Time)
	})
	limit := started.config.LogTailBytes
	if limit <= 0 {
		limit = e.config.LogTailBytes
	}
	reader, err := started.client.CoreV1().Pods(started.Namespace).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{LimitBytes: &limit}).Stream(ctx)
	if err != nil {
		return ""
	}
	defer func() { _ = reader.Close() }()
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return ""
	}
	return boundedLogTail(string(raw), int(limit))
}

func normalizeConfig(cfg Config) (Config, error) {
	cfg.DefaultNamespace = strings.TrimSpace(cfg.DefaultNamespace)
	cfg.DefaultServiceAccount = strings.TrimSpace(cfg.DefaultServiceAccount)
	cfg.DefaultImage = strings.TrimSpace(cfg.DefaultImage)
	cfg.AgentManagerGRPCAddr = strings.TrimSpace(cfg.AgentManagerGRPCAddr)
	cfg.AgentManagerAuthSecret = SecretKeyRef{
		Name: strings.TrimSpace(cfg.AgentManagerAuthSecret.Name),
		Key:  strings.TrimSpace(cfg.AgentManagerAuthSecret.Key),
	}
	cfg.ImagePullPolicy = firstNonEmpty(cfg.ImagePullPolicy, defaultImagePullPolicy)
	if cfg.JobTimeout <= 0 {
		cfg.JobTimeout = 2 * time.Minute
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.LogTailBytes <= 0 {
		cfg.LogTailBytes = 16 * 1024
	}
	switch corev1.PullPolicy(cfg.ImagePullPolicy) {
	case corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever:
	default:
		return Config{}, newExecutionError("invalid_executor_config", "Kubernetes executor image pull policy is invalid")
	}
	if strings.TrimSpace(cfg.DefaultNamespace) == "" || strings.TrimSpace(cfg.DefaultImage) == "" {
		return Config{}, newExecutionError("invalid_executor_config", "Kubernetes executor namespace and image must be configured")
	}
	if err := validateAgentRunReporterConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateAgentRunReporterConfig(cfg Config) error {
	addr := strings.TrimSpace(cfg.AgentManagerGRPCAddr)
	secretName := strings.TrimSpace(cfg.AgentManagerAuthSecret.Name)
	secretKey := strings.TrimSpace(cfg.AgentManagerAuthSecret.Key)
	if addr == "" && secretName == "" && secretKey == "" {
		return nil
	}
	if !safeAgentRunReporterValue(addr) || secretName == "" || secretKey == "" {
		return newExecutionError("invalid_agent_run_reporter_config", "agent_run reporter config is incomplete")
	}
	if errs := validation.IsDNS1123Subdomain(secretName); len(errs) > 0 {
		return newExecutionError("invalid_agent_run_reporter_config", "agent_run reporter secret ref is invalid")
	}
	if errs := validation.IsConfigMapKey(secretKey); len(errs) > 0 {
		return newExecutionError("invalid_agent_run_reporter_config", "agent_run reporter secret key is invalid")
	}
	if cfg.AgentManagerTimeout < 0 {
		return newExecutionError("invalid_agent_run_reporter_config", "agent_run reporter timeout is invalid")
	}
	return nil
}

func safeAgentRunReporterValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" &&
		len(trimmed) <= maxAgentRunReporterBytes &&
		utf8.ValidString(trimmed) &&
		!strings.ContainsAny(trimmed, " \t\r\n{}")
}

func clusterAccessError(err error) error {
	switch {
	case errors.Is(err, errs.ErrInvalidArgument):
		return newExecutionError("invalid_cluster_ref", "Runtime job cluster ref is invalid")
	case errors.Is(err, errs.ErrPlacementRejected):
		return newExecutionError("cluster_unavailable", "Runtime job cluster is not active")
	case errors.Is(err, errs.ErrPreconditionFailed), errors.Is(err, errs.ErrNotFound):
		return newExecutionError("cluster_ref_unavailable", "Runtime job cluster access ref is unavailable")
	default:
		return newExecutionError("cluster_ref_unavailable", "Runtime job cluster access ref is unavailable")
	}
}

func secretResolverError(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return newExecutionError("cluster_secret_timeout", "Kubernetes cluster access secret resolve timed out")
	case errors.Is(err, secretresolver.ErrInvalidRef),
		errors.Is(err, secretresolver.ErrUnsupportedStoreType),
		errors.Is(err, secretresolver.ErrSecretNotFound):
		return newExecutionError("cluster_secret_unavailable", "Kubernetes cluster access secret is unavailable")
	default:
		return newExecutionError("cluster_secret_unavailable", "Kubernetes cluster access secret is unavailable")
	}
}

func newExecutionError(code string, message string) *ExecutionError {
	return &ExecutionError{Code: strings.TrimSpace(code), Message: strings.TrimSpace(message)}
}

func kubernetesJobRef(clusterID uuid.UUID, namespace string, name string) string {
	return fmt.Sprintf("kubernetes://%s/namespaces/%s/jobs/%s", clusterID, namespace, name)
}

func namespaceRef(clusterID uuid.UUID, namespace string) string {
	return fmt.Sprintf("kubernetes://%s/namespaces/%s", clusterID, namespace)
}

func runtimeJobName(jobID uuid.UUID) string {
	return "kodex-rt-" + strings.ReplaceAll(jobID.String(), "-", "")[:24]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func int32Ptr(value int32) *int32 {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func boundedLogTail(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return strings.ToValidUTF8(text, "")
	}
	tail := text[len(text)-limit:]
	for len(tail) > 0 && !utf8.ValidString(tail) {
		_, size := utf8.DecodeRuneInString(tail)
		if size < 1 {
			return ""
		}
		tail = tail[size:]
	}
	return strings.ToValidUTF8(tail, "")
}
