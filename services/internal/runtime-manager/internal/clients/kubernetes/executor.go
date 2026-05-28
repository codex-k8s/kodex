// Package kubernetes содержит исполнитель Kubernetes-заданий runtime-manager.
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
	managedBy              = "runtime-manager"
	runtimePartOf          = "kodex"
	runtimeJobLabel        = "kodex.k8s.io/runtime-job-id"
	runtimeJobTypeLabel    = "kodex.k8s.io/runtime-job-type"
	defaultContainerName   = "runtime-health-check"
	defaultImagePullPolicy = "IfNotPresent"
	maxMetadataItems       = 16
)

// Config ограничивает поведение исполнителя Kubernetes настройками оператора.
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
}

// ClusterAccessProvider получает безопасные ссылки на секреты кластера через fleet-manager.
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

// Executor создаёт и наблюдает ограниченные Kubernetes Jobs для заданий runtime-manager.
type Executor struct {
	clusters ClusterAccessProvider
	secrets  secretresolver.Resolver
	clients  clientFactory
	config   Config
}

// StartedJob описывает Kubernetes Job, уже созданный для задания runtime-manager.
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
}

// ExecutionResult хранит ограниченный итог исполнения для команд жизненного цикла runtime-manager.
type ExecutionResult struct {
	Succeeded    bool
	ShortLogTail string
	ErrorCode    string
	ErrorMessage string
}

// ExecutionError хранит классифицированную ошибку, пригодную для диагностики runtime-manager.
type ExecutionError struct {
	Code    string
	Message string
}

func (e *ExecutionError) Error() string {
	return strings.TrimSpace(e.Code) + ": " + strings.TrimSpace(e.Message)
}

// NewExecutor создаёт Kubernetes executor с настоящими client-go клиентами.
func NewExecutor(clusters ClusterAccessProvider, secrets secretresolver.Resolver, cfg Config) (*Executor, error) {
	return NewExecutorWithClientFactory(clusters, secrets, cfg, realClientFactory{})
}

// NewExecutorWithClientFactory используется тестами без настоящего кластера.
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

// Start создаёт или переиспользует детерминированный Kubernetes Job для захваченного задания.
func (e *Executor) Start(ctx context.Context, job entity.Job) (StartedJob, error) {
	if job.JobType != enum.JobTypeHealthCheck {
		return StartedJob{}, newExecutionError("unsupported_job_type", "Kubernetes executor supports only health_check jobs")
	}
	if job.ClusterID == nil || *job.ClusterID == uuid.Nil {
		return StartedJob{}, newExecutionError("missing_cluster_ref", "Runtime job does not have a Kubernetes cluster ref")
	}
	spec, err := e.executionSpec(job.JobInputJSON)
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
	ref := kubernetesJobRef(access.ClusterID, spec.Namespace, created.GetName())
	return StartedJob{
		RuntimeJobID: job.ID,
		ClusterID:    access.ClusterID,
		Namespace:    spec.Namespace,
		JobName:      created.GetName(),
		ExternalRef:  ref,
		ArtifactRefs: []runtimeservice.RuntimeArtifactRefInput{
			{ArtifactType: enum.RuntimeArtifactTypeKubernetesJob, ExternalRef: ref, MetadataJSON: []byte(`{}`)},
			{ArtifactType: enum.RuntimeArtifactTypeNamespace, ExternalRef: namespaceRef(access.ClusterID, spec.Namespace), MetadataJSON: []byte(`{}`)},
		},
		client:   client,
		config:   e.config,
		selector: selector,
	}, nil
}

// Wait ждёт терминального статуса Kubernetes Job и возвращает ограниченную диагностику.
func (e *Executor) Wait(ctx context.Context, started StartedJob) ExecutionResult {
	timeout := started.config.JobTimeout
	if timeout <= 0 {
		timeout = e.config.JobTimeout
	}
	pollInterval := started.config.PollInterval
	if pollInterval <= 0 {
		pollInterval = e.config.PollInterval
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		result, done := e.observe(ctx, started)
		if done {
			return result
		}
		select {
		case <-ctx.Done():
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

// ErrorDiagnostic переводит ошибки исполнителя в безопасную диагностику runtime-manager.
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
		return ExecutionResult{
			ErrorCode:    "kubernetes_job_status_unavailable",
			ErrorMessage: "Kubernetes Job status is unavailable",
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
	Namespace       string
	ServiceAccount  string
	Image           string
	ImagePullPolicy corev1.PullPolicy
	Labels          map[string]string
}

type restrictedJobInput struct {
	Namespace      string            `json:"namespace"`
	ServiceAccount string            `json:"service_account"`
	Image          string            `json:"image"`
	Labels         map[string]string `json:"labels"`
}

func (e *Executor) executionSpec(payload []byte) (executionSpec, error) {
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
	}
	if err := validateExecutionSpec(spec); err != nil {
		return executionSpec{}, err
	}
	return spec, nil
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

func validateExecutionSpec(spec executionSpec) error {
	if errs := validation.IsDNS1123Label(spec.Namespace); len(errs) > 0 {
		return newExecutionError("invalid_job_input", "Kubernetes executor namespace is invalid")
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
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: spec.ServiceAccount,
					Containers: []corev1.Container{{
						Name:            defaultContainerName,
						Image:           spec.Image,
						ImagePullPolicy: spec.ImagePullPolicy,
						Command:         []string{"/bin/sh", "-ec"},
						Args:            []string{"echo kodex runtime health check"},
					}},
				},
			},
		},
	}
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

func (e *Executor) shortLogTail(ctx context.Context, started StartedJob) string {
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
	return cfg, nil
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
