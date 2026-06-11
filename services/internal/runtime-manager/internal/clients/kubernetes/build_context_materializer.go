package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

const buildContextMaterializerTypeLabel = "build_context"

// BuildContextMaterializer prepares a checked source tree into a runtime-owned PVC.
type BuildContextMaterializer struct {
	executor  *Executor
	clusterID uuid.UUID
}

// BuildContextMaterializationResult is the safe materialization outcome stored by runtime-manager.
type BuildContextMaterializationResult struct {
	Succeeded            bool
	SourceSnapshotRef    string
	SourceSnapshotDigest string
	BuildContextRef      string
	BuildContextDigest   string
	ErrorCode            string
	ErrorMessage         string
}

// NewBuildContextMaterializer reuses executor cluster access and Kubernetes clients.
func NewBuildContextMaterializer(executor *Executor, clusterID uuid.UUID) (*BuildContextMaterializer, error) {
	if executor == nil || clusterID == uuid.Nil {
		return nil, newExecutionError("build_context_materializer_not_configured", "build context materializer is not configured")
	}
	return &BuildContextMaterializer{executor: executor, clusterID: clusterID}, nil
}

// Materialize creates or reuses a deterministic Kubernetes Job and waits for a structured safe result.
func (m *BuildContextMaterializer) Materialize(ctx context.Context, buildContext entity.BuildContext) BuildContextMaterializationResult {
	started, err := m.start(ctx, buildContext)
	if err != nil {
		code, message := ErrorDiagnostic(err)
		return BuildContextMaterializationResult{ErrorCode: code, ErrorMessage: message}
	}
	result := m.executor.Wait(ctx, started)
	if !result.Succeeded {
		return BuildContextMaterializationResult{
			ErrorCode:    firstNonEmpty(result.ErrorCode, "build_context_materializer_failed"),
			ErrorMessage: firstNonEmpty(result.StatusSummary, result.ErrorMessage, "build context materializer failed"),
		}
	}
	report, err := parseMaterializerReport(result.ShortLogTail)
	if err != nil {
		return BuildContextMaterializationResult{ErrorCode: "build_context_materializer_report_invalid", ErrorMessage: "build context materializer report is invalid"}
	}
	sourceRef, sourceDigest := BuildContextSourceSnapshot(buildContext)
	contextRef := buildContextPVCRef(m.executor.config.DefaultNamespace, buildContextPVCName(buildContext.ID))
	if report.SourceSnapshotRef != "" && report.SourceSnapshotRef != sourceRef {
		return BuildContextMaterializationResult{ErrorCode: "build_context_materializer_source_mismatch", ErrorMessage: "build context materializer source ref mismatch"}
	}
	if report.SourceSnapshotDigest != "" && report.SourceSnapshotDigest != sourceDigest {
		return BuildContextMaterializationResult{ErrorCode: "build_context_materializer_source_mismatch", ErrorMessage: "build context materializer source digest mismatch"}
	}
	if !validMaterializerDigest(report.BuildContextDigest) {
		return BuildContextMaterializationResult{ErrorCode: "build_context_materializer_report_invalid", ErrorMessage: "build context materializer digest is invalid"}
	}
	return BuildContextMaterializationResult{
		Succeeded:            true,
		SourceSnapshotRef:    sourceRef,
		SourceSnapshotDigest: sourceDigest,
		BuildContextRef:      contextRef,
		BuildContextDigest:   report.BuildContextDigest,
	}
}

func (m *BuildContextMaterializer) start(ctx context.Context, buildContext entity.BuildContext) (StartedJob, error) {
	if err := validateBuildContextMaterializerInput(buildContext); err != nil {
		return StartedJob{}, err
	}
	access, err := m.executor.clusters.GetClusterAccess(ctx, m.clusterID)
	if err != nil {
		return StartedJob{}, clusterAccessError(err)
	}
	clients, err := m.executor.clientForCluster(ctx, access)
	if err != nil {
		return StartedJob{}, err
	}
	namespace := m.executor.config.DefaultNamespace
	pvcName := buildContextPVCName(buildContext.ID)
	if err := m.ensureBuildContextPVC(ctx, clients, namespace, pvcName); err != nil {
		return StartedJob{}, err
	}
	jobName := buildContextMaterializerJobName(buildContext.ID)
	selector := labels.Set{runtimeJobLabel: buildContext.ID.String()}
	existing, err := clients.kubernetes.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
	if err == nil {
		if !isManagedBuildContextMaterializerJob(existing, buildContext.ID) {
			return StartedJob{}, newExecutionError("kubernetes_job_name_conflict", "Kubernetes Job name is already used by a different object")
		}
		return startedMaterializerJob(access.ClusterID, namespace, jobName, selector, clients.kubernetes, m.executor.config, existing, buildContext.ID), nil
	}
	if !apierrors.IsNotFound(err) {
		return StartedJob{}, kubernetesJobLookupError(err)
	}
	job := buildContextMaterializerJob(buildContext, m.executor.config, namespace, pvcName, jobName, selector)
	created, err := clients.kubernetes.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = clients.kubernetes.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
	}
	if err != nil {
		return StartedJob{}, kubernetesJobCreateError(err)
	}
	if !isManagedBuildContextMaterializerJob(created, buildContext.ID) {
		return StartedJob{}, newExecutionError("kubernetes_job_name_conflict", "Kubernetes Job name is already used by a different object")
	}
	return startedMaterializerJob(access.ClusterID, namespace, jobName, selector, clients.kubernetes, m.executor.config, created, buildContext.ID), nil
}

func (m *BuildContextMaterializer) ensureBuildContextPVC(ctx context.Context, clients clusterClients, namespace string, name string) error {
	if _, err := clients.kubernetes.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return newExecutionError("build_context_pvc_status_unavailable", "build context PVC status is unavailable")
	}
	storage, err := resource.ParseQuantity(m.executor.config.BuildContextStorageSize)
	if err != nil {
		return newExecutionError("invalid_build_context_materializer_config", "build context PVC storage request is invalid")
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespaceSafeName(name),
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "runtime-build-context",
				"app.kubernetes.io/part-of":    runtimePartOf,
				"app.kubernetes.io/managed-by": managedBy,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: storage},
			},
		},
	}
	if storageClass := strings.TrimSpace(m.executor.config.BuildContextStorageClass); storageClass != "" {
		pvc.Spec.StorageClassName = &storageClass
	}
	if _, err := clients.kubernetes.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return kubernetesJobCreateError(err)
	}
	return nil
}

func startedMaterializerJob(clusterID uuid.UUID, namespace string, jobName string, selector labels.Set, client kubernetes.Interface, cfg Config, job *batchv1.Job, id uuid.UUID) StartedJob {
	return StartedJob{
		RuntimeJobID: id,
		ClusterID:    clusterID,
		Namespace:    namespace,
		JobName:      jobName,
		ExternalRef:  kubernetesJobRef(clusterID, namespace, job.GetName()),
		client:       client,
		config:       cfg,
		selector:     selector,
		collectLogs:  true,
	}
}

func buildContextMaterializerJob(buildContext entity.BuildContext, cfg Config, namespace string, pvcName string, jobName string, selector labels.Set) *batchv1.Job {
	labels := map[string]string{
		"app.kubernetes.io/name":       "runtime-build-context-materializer",
		"app.kubernetes.io/part-of":    runtimePartOf,
		"app.kubernetes.io/managed-by": managedBy,
		runtimeJobLabel:                buildContext.ID.String(),
		runtimeJobTypeLabel:            buildContextMaterializerTypeLabel,
	}
	for key, value := range selector {
		labels[key] = value
	}
	env := []corev1.EnvVar{}
	if cfg.SourceAuthSecret.Name != "" && cfg.SourceAuthSecret.Key != "" {
		env = append(env, corev1.EnvVar{
			Name: "GITHUB_TOKEN",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: cfg.SourceAuthSecret.Name},
				Key:                  cfg.SourceAuthSecret.Key,
				Optional:             boolPtr(true),
			}},
		})
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			ActiveDeadlineSeconds:   int64Ptr(jobTimeoutSeconds(cfg.JobTimeout)),
			BackoffLimit:            int32Ptr(cfg.BackoffLimit),
			TTLSecondsAfterFinished: int32Ptr(cfg.TTLSecondsAfterFinished),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyNever,
					AutomountServiceAccountToken: boolPtr(false),
					ServiceAccountName:           cfg.DefaultServiceAccount,
					SecurityContext:              restrictedBuildPodSecurityContext(),
					Volumes: []corev1.Volume{{
						Name:         buildContextVolumeName,
						VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName}},
					}},
					Containers: []corev1.Container{{
						Name:            materializerContainerName,
						Image:           cfg.DefaultImage,
						ImagePullPolicy: corev1.PullPolicy(cfg.ImagePullPolicy),
						Command:         []string{materializerCommand},
						Args:            buildContextMaterializerArgs(buildContext),
						Env:             env,
						VolumeMounts:    []corev1.VolumeMount{{Name: buildContextVolumeName, MountPath: buildContextMountPath, ReadOnly: false}},
						SecurityContext: restrictedBuildContainerSecurityContext(),
					}},
				},
			},
		},
	}
}

func buildContextMaterializerArgs(buildContext entity.BuildContext) []string {
	return []string{
		"github-archive",
		"--provider", buildContext.Provider,
		"--owner", buildContext.ProviderOwner,
		"--repo", buildContext.ProviderName,
		"--source-ref", buildContext.SourceRef,
		"--commit", buildContext.SourceCommitSHA,
		"--output", buildContextMountPath,
		"--result", materializerResultPath,
	}
}

func isManagedBuildContextMaterializerJob(job *batchv1.Job, id uuid.UUID) bool {
	return job != nil &&
		job.Labels["app.kubernetes.io/managed-by"] == managedBy &&
		job.Labels[runtimeJobLabel] == id.String() &&
		job.Labels[runtimeJobTypeLabel] == buildContextMaterializerTypeLabel
}

func validateBuildContextMaterializerInput(buildContext entity.BuildContext) error {
	if buildContext.ID == uuid.Nil ||
		strings.TrimSpace(buildContext.Provider) != "github" ||
		strings.TrimSpace(buildContext.ProviderOwner) == "" ||
		strings.TrimSpace(buildContext.ProviderName) == "" ||
		strings.TrimSpace(buildContext.SourceCommitSHA) == "" {
		return newExecutionError("invalid_build_context_materializer_input", "build context materializer input is invalid")
	}
	return nil
}

type materializerReport struct {
	SourceSnapshotRef    string `json:"source_snapshot_ref"`
	SourceSnapshotDigest string `json:"source_snapshot_digest"`
	BuildContextDigest   string `json:"build_context_digest"`
}

func parseMaterializerReport(logTail string) (materializerReport, error) {
	for _, line := range strings.Split(strings.TrimSpace(logTail), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") || !strings.Contains(line, "build_context_digest") {
			continue
		}
		var report materializerReport
		if err := json.Unmarshal([]byte(line), &report); err != nil {
			continue
		}
		return report, nil
	}
	return materializerReport{}, fmt.Errorf("materializer report not found")
}

// BuildContextSourceSnapshot returns deterministic safe source snapshot refs for one build context.
func BuildContextSourceSnapshot(buildContext entity.BuildContext) (string, string) {
	ref := fmt.Sprintf("github://github.com/%s/%s#%s", buildContext.ProviderOwner, buildContext.ProviderName, strings.ToLower(buildContext.SourceCommitSHA))
	return ref, sourceIdentityDigest(buildContext)
}

func sourceIdentityDigest(buildContext entity.BuildContext) string {
	raw := strings.Join([]string{
		buildContext.Provider,
		buildContext.ProviderOwner,
		buildContext.ProviderName,
		buildContext.SourceRef,
		strings.ToLower(buildContext.SourceCommitSHA),
	}, "\x00")
	return sha256Digest(raw)
}

func buildContextPVCRef(namespace string, name string) string {
	return "pvc://" + namespaceSafeName(namespace) + "/" + namespaceSafeName(name)
}

func buildContextPVCName(id uuid.UUID) string {
	return "kodex-bctx-" + strings.ReplaceAll(id.String(), "-", "")[:24]
}

func buildContextMaterializerJobName(id uuid.UUID) string {
	return "kodex-bctxmat-" + strings.ReplaceAll(id.String(), "-", "")[:20]
}

func namespaceSafeName(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), "-")
}

func validMaterializerDigest(value string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	return strings.HasPrefix(trimmed, "sha256:") && len(strings.TrimPrefix(trimmed, "sha256:")) == 64
}

func sha256Digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
