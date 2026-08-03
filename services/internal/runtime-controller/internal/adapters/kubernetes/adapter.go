// Package kubernetes материализует server-owned RuntimeExecution в Kubernetes.
package kubernetes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/errs"
	runtimerepo "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

const (
	managedLabel       = "runtime.mattercodex.dev/managed"
	executionLabel     = "runtime.mattercodex.dev/execution"
	sessionLabel       = "runtime.mattercodex.dev/session"
	roleLabel          = "runtime.mattercodex.dev/role"
	accessLabel        = "runtime.mattercodex.dev/access-profile"
	componentLabel     = "app.kubernetes.io/component"
	journalComponent   = "runtime-journal"
	roleComponent      = "role-runtime"
	archiveComponent   = "runtime-archive"
	restoreComponent   = "runtime-restore-verifier"
	cleanupComponent   = "runtime-cleanup-authorizer"
	journalDataKey     = "journal.json"
	leaseTokenKey      = "lease-token"
	maximumJournalSize = 1 << 20
	maximumPVCBytes    = int64(30 << 30)
)

type Config struct {
	Environment           string
	Namespace             string
	RoleImageRepository   string
	ControllerImage       string
	AuthorityImage        string
	StorageClass          string
	PVCSize               string
	RuntimeServiceAccount string
	ReadClusterRole       string
	AdminClusterRole      string
	ArchiveServiceAccount string
	RestoreServiceAccount string
	CleanupServiceAccount string
	MaximumPods           int
	MaximumCPU            int64
	MaximumMemoryBytes    int64
	JobTTL                time.Duration
	S3Endpoint            string
	S3TLSServerName       string
	S3Bucket              string
	S3Region              string
}

type Adapter struct {
	client kubernetes.Interface
	config Config
	now    func() time.Time
}

type journalDocument = entity.RuntimeJournal

func InCluster(config Config) (*Adapter, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.New("load in-cluster Kubernetes configuration")
	}
	restConfig.QPS = 20
	restConfig.Burst = 40
	restConfig.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, errors.New("create Kubernetes client")
	}
	return New(client, config)
}

func New(client kubernetes.Interface, config Config) (*Adapter, error) {
	if client == nil || (config.Environment != "staging" && config.Environment != "production") ||
		config.Namespace == "" || config.RoleImageRepository == "" ||
		config.ControllerImage == "" || config.AuthorityImage == "" ||
		config.StorageClass == "" || config.PVCSize == "" || config.MaximumPods < 1 ||
		config.MaximumCPU < 1 || config.MaximumMemoryBytes < 1 ||
		config.JobTTL < time.Minute || config.JobTTL > 24*time.Hour ||
		config.S3Endpoint == "" || config.S3TLSServerName == "" || config.S3Bucket == "" || config.S3Region == "" {
		return nil, errors.New("kubernetes adapter configuration is invalid")
	}
	for _, serviceAccount := range []string{
		config.RuntimeServiceAccount, config.ReadClusterRole, config.AdminClusterRole,
		config.ArchiveServiceAccount, config.RestoreServiceAccount, config.CleanupServiceAccount,
	} {
		if serviceAccount == "" {
			return nil, errors.New("runtime service account configuration is invalid")
		}
	}
	pvcQuantity, err := resource.ParseQuantity(config.PVCSize)
	if err != nil || pvcQuantity.Value() <= 0 || pvcQuantity.Value() > maximumPVCBytes {
		return nil, errors.New("runtime PVC size is invalid")
	}
	return &Adapter{client: client, config: config, now: time.Now}, nil
}

// RunAsLeader допускает ровно один controller к durable consumer и mutation loops.
// Потеря аренды завершает процесс: продолжение со stale local state запрещено.
func (adapter *Adapter) RunAsLeader(
	ctx context.Context,
	identity string,
	run func(context.Context) error,
) error {
	if identity == "" || run == nil {
		return errs.ErrInvalidInput
	}
	electionContext, cancelElection := context.WithCancel(ctx)
	defer cancelElection()
	runResult := make(chan error, 1)
	elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta:  metav1.ObjectMeta{Name: "runtime-controller-leader", Namespace: adapter.config.Namespace},
			Client:     adapter.client.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{Identity: identity},
		},
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		ReleaseOnCancel: true,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leaderContext context.Context) {
				runResult <- run(leaderContext)
				cancelElection()
			},
			OnStoppedLeading: func() {},
		},
	})
	if err != nil {
		return errors.New("configure runtime-controller leader election")
	}
	elector.Run(electionContext)
	select {
	case err := <-runResult:
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("runtime-controller leadership lost")
	default:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("runtime-controller leadership lost")
	}
}

// PatchWorkerJournal сохраняет exact control-plane result worker без lease token.
func PatchWorkerJournal(
	ctx context.Context,
	namespace, name, expectedExecutionID string,
	execution entity.Execution,
) error {
	if namespace == "" || name != journalName(expectedExecutionID) || execution.ID != expectedExecutionID ||
		execution.Validate() != nil {
		return errs.ErrInvalidInput
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("load worker Kubernetes configuration")
	}
	config.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.New("create worker Kubernetes client")
	}
	configMap, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return errors.New("read worker runtime journal")
	}
	var document journalDocument
	if decodeJournal([]byte(configMap.Data[journalDataKey]), &document) != nil ||
		document.Execution.ID != expectedExecutionID || execution.Version < document.Execution.Version ||
		execution.Fence < document.Execution.Fence ||
		execution.GrantGeneration != document.Execution.GrantGeneration {
		return errs.ErrStateConflict
	}
	document.Execution = execution
	refreshCommandKeys(&document)
	document.LastTransition = time.Now().UTC()
	raw, err := marshalJournal(document)
	if err != nil {
		return err
	}
	configMap.Data[journalDataKey] = string(raw)
	configMap.Labels["runtime.mattercodex.dev/state"] = strings.ToLower(string(execution.State))
	if _, err := client.CoreV1().ConfigMaps(namespace).Update(ctx, configMap, metav1.UpdateOptions{}); err != nil {
		if apierrors.IsConflict(err) {
			return errs.ErrStateConflict
		}
		return errors.New("update worker runtime journal")
	}
	return nil
}

func (adapter *Adapter) Check(ctx context.Context) error {
	if _, err := adapter.client.CoreV1().Namespaces().Get(
		ctx, adapter.config.Namespace, metav1.GetOptions{},
	); err != nil {
		return errors.New("runtime Kubernetes namespace is unavailable")
	}
	if _, err := adapter.client.CoreV1().ResourceQuotas(adapter.config.Namespace).List(
		ctx, metav1.ListOptions{Limit: 100},
	); err != nil {
		return errors.New("runtime Kubernetes quota read is unavailable")
	}
	return nil
}

func (adapter *Adapter) EnsureJournal(
	ctx context.Context,
	execution entity.Execution,
) (runtimerepo.Journal, error) {
	name := journalName(execution.ID)
	existing, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(
		ctx, name, metav1.GetOptions{},
	)
	if err == nil {
		return adapter.castJournal(ctx, existing)
	}
	if !apierrors.IsNotFound(err) {
		return runtimerepo.Journal{}, errors.New("read runtime journal")
	}
	now := adapter.now().UTC()
	document := journalDocument{
		Execution:       execution,
		AdmitKey:        commandKey(execution, "admit"),
		HeartbeatKey:    commandKey(execution, "heartbeat"),
		CompleteKey:     commandKey(execution, "complete"),
		IncidentKey:     commandKey(execution, "incident"),
		ArchiveKey:      commandKey(execution, "archive"),
		RestoreKey:      commandKey(execution, "restore"),
		CleanupKey:      commandKey(execution, "cleanup"),
		LeaseSecretName: leaseName(execution.ID), PodName: podName(execution),
		PVCName: pvcName(execution), CreatedAt: now, LastTransition: now,
	}
	raw, err := marshalJournal(document)
	if err != nil {
		return runtimerepo.Journal{}, err
	}
	created, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Create(
		ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: name, Labels: labels(execution, journalComponent),
		}, Data: map[string]string{journalDataKey: string(raw)}}, metav1.CreateOptions{},
	)
	if apierrors.IsAlreadyExists(err) {
		created, err = adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(
			ctx, name, metav1.GetOptions{},
		)
	}
	if err != nil {
		return runtimerepo.Journal{}, errors.New("create runtime journal")
	}
	return adapter.castJournal(ctx, created)
}

func (adapter *Adapter) LoadJournal(
	ctx context.Context,
	status entity.RuntimeStatus,
) (runtimerepo.Journal, error) {
	name := status.JournalName
	if name == "" {
		name = journalName(status.ExecutionID)
	}
	configMap, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(
		ctx, name, metav1.GetOptions{},
	)
	if err != nil {
		return runtimerepo.Journal{}, errors.New("load runtime journal")
	}
	return adapter.castJournal(ctx, configMap)
}

func (adapter *Adapter) castJournal(
	ctx context.Context,
	configMap *corev1.ConfigMap,
) (runtimerepo.Journal, error) {
	raw := configMap.Data[journalDataKey]
	if len(raw) == 0 || len(raw) > maximumJournalSize {
		return runtimerepo.Journal{}, errs.ErrStateConflict
	}
	var document journalDocument
	if err := decodeJournal([]byte(raw), &document); err != nil ||
		document.Execution.Validate() != nil || configMap.Name != journalName(document.Execution.ID) {
		return runtimerepo.Journal{}, errs.ErrStateConflict
	}
	if configMap.Labels[managedLabel] != "true" || configMap.Labels[componentLabel] != journalComponent ||
		configMap.Labels[executionLabel] != shortID(document.Execution.ID) {
		return runtimerepo.Journal{}, errs.ErrStateConflict
	}
	leaseToken := ""
	secret, err := adapter.client.CoreV1().Secrets(adapter.config.Namespace).Get(
		ctx, document.LeaseSecretName, metav1.GetOptions{},
	)
	if err == nil {
		leaseToken = string(secret.Data[leaseTokenKey])
	} else if !apierrors.IsNotFound(err) {
		return runtimerepo.Journal{}, errors.New("read runtime lease secret")
	}
	return runtimerepo.Journal{
		Execution: document.Execution, AdmitIdempotencyKey: document.AdmitKey,
		HeartbeatIdempotencyKey: document.HeartbeatKey,
		CompleteIdempotencyKey:  document.CompleteKey,
		IncidentIdempotencyKey:  document.IncidentKey,
		ArchiveIdempotencyKey:   document.ArchiveKey,
		RestoreIdempotencyKey:   document.RestoreKey,
		CleanupIdempotencyKey:   document.CleanupKey,
		LeaseTokenSecretName:    document.LeaseSecretName, LeaseToken: leaseToken,
	}, nil
}

func (adapter *Adapter) Capacity(
	ctx context.Context,
	execution entity.Execution,
) (entity.CapacityDecision, error) {
	pods, err := adapter.client.CoreV1().Pods(adapter.config.Namespace).List(
		ctx, metav1.ListOptions{LabelSelector: fields.OneTermEqualSelector(managedLabel, "true").String()},
	)
	if err != nil {
		return entity.CapacityDecision{}, errors.New("list runtime pods for capacity")
	}
	requestedCPU, requestedMemory, accelerator := resourcesFor(execution.ResourceClass)
	var usedCPU, usedMemory int64
	var active int
	var idle []entity.RuntimeStatus
	stableName := podName(execution)
	for index := range pods.Items {
		pod := &pods.Items[index]
		if pod.Labels[componentLabel] != roleComponent {
			continue
		}
		active++
		for _, container := range pod.Spec.Containers {
			usedCPU += container.Resources.Requests.Cpu().MilliValue()
			usedMemory += container.Resources.Requests.Memory().Value()
		}
		status := castStatus(pod, nil)
		if pod.Name == stableName && pod.Labels[executionLabel] != shortID(execution.ID) {
			return entity.CapacityDecision{Reason: "session_replacement", Eviction: &status}, nil
		}
		if terminalLabel(pod.Labels["runtime.mattercodex.dev/state"]) &&
			status.AccessProfile != enum.AccessClusterAdmin {
			idle = append(idle, status)
		}
	}
	quotaBlocked, err := adapter.quotaBlocked(ctx, requestedCPU, requestedMemory, accelerator)
	if err != nil {
		return entity.CapacityDecision{}, err
	}
	nodePressure, err := adapter.nodePressure(ctx, accelerator)
	if err != nil {
		return entity.CapacityDecision{}, err
	}
	admitted := active < adapter.config.MaximumPods &&
		usedCPU+requestedCPU <= adapter.config.MaximumCPU &&
		usedMemory+requestedMemory <= adapter.config.MaximumMemoryBytes &&
		!quotaBlocked && !nodePressure
	if admitted {
		return entity.CapacityDecision{Admitted: true, Reason: "admitted"}, nil
	}
	sort.Slice(idle, func(left, right int) bool {
		return idle[left].LastTransition.Before(idle[right].LastTransition)
	})
	decision := entity.CapacityDecision{Reason: "bounded_capacity"}
	if len(idle) > 0 {
		decision.Eviction = &idle[0]
	}
	return decision, nil
}

func (adapter *Adapter) quotaBlocked(
	ctx context.Context,
	cpuMilli, memoryBytes int64,
	accelerator bool,
) (bool, error) {
	quotas, err := adapter.client.CoreV1().ResourceQuotas(adapter.config.Namespace).List(
		ctx, metav1.ListOptions{Limit: 100},
	)
	if err != nil {
		return false, errors.New("read runtime resource quota")
	}
	for _, quota := range quotas.Items {
		if hard, bounded := quota.Status.Hard[corev1.ResourcePods]; bounded {
			used := quota.Status.Used[corev1.ResourcePods]
			if used.Value()+1 > hard.Value() {
				return true, nil
			}
		}
		for name, requested := range map[corev1.ResourceName]int64{
			corev1.ResourceRequestsCPU:    cpuMilli,
			corev1.ResourceRequestsMemory: memoryBytes,
		} {
			hard, bounded := quota.Status.Hard[name]
			used := quota.Status.Used[name]
			if bounded && ((name == corev1.ResourceRequestsCPU && used.MilliValue()+requested > hard.MilliValue()) ||
				(name == corev1.ResourceRequestsMemory && used.Value()+requested > hard.Value())) {
				return true, nil
			}
		}
		if accelerator {
			name := corev1.ResourceName("requests.nvidia.com/gpu")
			if hard, bounded := quota.Status.Hard[name]; bounded {
				used := quota.Status.Used[name]
				if used.Value()+1 > hard.Value() {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func (adapter *Adapter) nodePressure(ctx context.Context, accelerator bool) (bool, error) {
	nodes, err := adapter.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 500})
	if err != nil {
		return false, errors.New("read runtime node pressure")
	}
	eligible := 0
	for _, node := range nodes.Items {
		gpu := node.Status.Allocatable["nvidia.com/gpu"]
		if node.Spec.Unschedulable || accelerator && gpu.IsZero() {
			continue
		}
		pressured := false
		for _, condition := range node.Status.Conditions {
			if (condition.Type == corev1.NodeMemoryPressure || condition.Type == corev1.NodeDiskPressure ||
				condition.Type == corev1.NodePIDPressure || condition.Type == corev1.NodeNetworkUnavailable) &&
				condition.Status == corev1.ConditionTrue {
				pressured = true
			}
		}
		if !pressured {
			eligible++
		}
	}
	return eligible == 0, nil
}

func (adapter *Adapter) Materialize(
	ctx context.Context,
	execution entity.Execution,
	revision entity.Revision,
	leaseToken string,
	journal runtimerepo.Journal,
) (entity.RuntimeStatus, error) {
	if err := revision.ValidateFor(execution); err != nil || leaseToken == "" {
		return entity.RuntimeStatus{}, errs.ErrStateConflict
	}
	if err := adapter.ensurePVC(ctx, execution); err != nil {
		return entity.RuntimeStatus{}, err
	}
	if err := adapter.ensureExecutionConfig(ctx, execution, revision); err != nil {
		return entity.RuntimeStatus{}, err
	}
	if err := adapter.ensureAccessProfile(ctx, execution); err != nil {
		return entity.RuntimeStatus{}, err
	}
	if err := adapter.updateJournalAndLease(ctx, execution, leaseToken); err != nil {
		return entity.RuntimeStatus{}, err
	}
	desired, err := adapter.rolePod(execution, revision)
	if err != nil {
		return entity.RuntimeStatus{}, err
	}
	existing, err := adapter.client.CoreV1().Pods(adapter.config.Namespace).Get(
		ctx, desired.Name, metav1.GetOptions{},
	)
	if err == nil {
		if existing.Labels[executionLabel] != shortID(execution.ID) ||
			existing.Annotations["runtime.mattercodex.dev/revision-sha256"] != execution.RuntimeRevisionSHA256 ||
			existing.Annotations["runtime.mattercodex.dev/input-sha256"] != execution.ImmutableInputSHA256 ||
			!podMatches(existing, desired) {
			return entity.RuntimeStatus{}, errs.ErrStateConflict
		}
		return castStatus(existing, nil), nil
	}
	if !apierrors.IsNotFound(err) {
		return entity.RuntimeStatus{}, errors.New("read role runtime pod")
	}
	created, err := adapter.client.CoreV1().Pods(adapter.config.Namespace).Create(
		ctx, desired, metav1.CreateOptions{},
	)
	if err != nil {
		return entity.RuntimeStatus{}, errors.New("create role runtime pod")
	}
	return castStatus(created, nil), nil
}

func (adapter *Adapter) ensurePVC(ctx context.Context, execution entity.Execution) error {
	quantity := resource.MustParse(adapter.config.PVCSize)
	mode := corev1.PersistentVolumeFilesystem
	desired := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Name: pvcName(execution), Labels: labels(execution, "session-storage"),
		Annotations: map[string]string{
			"runtime.mattercodex.dev/organization-id":              execution.OrganizationID,
			"runtime.mattercodex.dev/project-id":                   execution.ProjectID,
			"runtime.mattercodex.dev/session-id":                   execution.SessionID,
			"runtime.mattercodex.dev/retention-owner-execution-id": execution.ID,
			"runtime.mattercodex.dev/retention-owner-journal":      journalName(execution.ID),
		},
	}, Spec: corev1.PersistentVolumeClaimSpec{
		AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		StorageClassName: &adapter.config.StorageClass, VolumeMode: &mode,
		Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceStorage: quantity,
		}},
	}}
	existing, err := adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).Get(
		ctx, desired.Name, metav1.GetOptions{},
	)
	if err == nil {
		if existing.Labels[sessionLabel] != shortID(execution.SessionID) ||
			existing.Labels[componentLabel] != "session-storage" || existing.DeletionTimestamp != nil ||
			existing.Annotations["runtime.mattercodex.dev/organization-id"] != execution.OrganizationID ||
			existing.Annotations["runtime.mattercodex.dev/project-id"] != execution.ProjectID ||
			existing.Annotations["runtime.mattercodex.dev/session-id"] != execution.SessionID ||
			existing.Spec.StorageClassName == nil || *existing.Spec.StorageClassName != adapter.config.StorageClass ||
			existing.Spec.VolumeMode == nil || *existing.Spec.VolumeMode != mode ||
			!reflect.DeepEqual(existing.Spec.AccessModes, desired.Spec.AccessModes) ||
			existing.Spec.Resources.Requests.Storage().Cmp(quantity) != 0 {
			return errs.ErrStateConflict
		}
		ownerID := existing.Annotations["runtime.mattercodex.dev/retention-owner-execution-id"]
		ownerJournal := existing.Annotations["runtime.mattercodex.dev/retention-owner-journal"]
		if uuid.Validate(ownerID) != nil || ownerJournal != journalName(ownerID) {
			return errs.ErrStateConflict
		}
		if ownerID != execution.ID {
			updated := existing.DeepCopy()
			updated.Annotations["runtime.mattercodex.dev/retention-owner-execution-id"] = execution.ID
			updated.Annotations["runtime.mattercodex.dev/retention-owner-journal"] = journalName(execution.ID)
			if _, err := adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).Update(
				ctx, updated, metav1.UpdateOptions{},
			); err != nil {
				if apierrors.IsConflict(err) {
					return errs.ErrStateConflict
				}
				return errors.New("advance runtime PVC retention owner")
			}
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return errors.New("read runtime PVC")
	}
	if _, err := adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).Create(
		ctx, desired, metav1.CreateOptions{},
	); err != nil && !apierrors.IsAlreadyExists(err) {
		return errors.New("create runtime PVC")
	}
	return nil
}

func (adapter *Adapter) ensureExecutionConfig(
	ctx context.Context,
	execution entity.Execution,
	revision entity.Revision,
) error {
	snapshot := struct {
		Execution entity.Execution `json:"execution"`
		Revision  entity.Revision  `json:"runtime_revision"`
	}{Execution: execution, Revision: revision}
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) > maximumJournalSize {
		return errs.ErrStateConflict
	}
	desired := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: configName(execution), Labels: labels(execution, "immutable-input"),
		Annotations: map[string]string{"runtime.mattercodex.dev/immutable": "true"},
	}, Immutable: boolPointer(true), BinaryData: map[string][]byte{"runtime.json": raw}}
	existing, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(
		ctx, desired.Name, metav1.GetOptions{},
	)
	if err == nil {
		if existing.Immutable == nil || !*existing.Immutable ||
			existing.Labels[executionLabel] != shortID(execution.ID) ||
			existing.Labels[componentLabel] != "immutable-input" ||
			existing.Annotations["runtime.mattercodex.dev/immutable"] != "true" ||
			!bytesEqual(existing.BinaryData["runtime.json"], raw) {
			return errs.ErrStateConflict
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return errors.New("read immutable runtime config")
	}
	if _, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Create(
		ctx, desired, metav1.CreateOptions{},
	); err != nil && !apierrors.IsAlreadyExists(err) {
		return errors.New("create immutable runtime config")
	}
	return nil
}

func (adapter *Adapter) updateJournalAndLease(
	ctx context.Context,
	execution entity.Execution,
	leaseToken string,
) error {
	configMaps := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace)
	configMap, err := configMaps.Get(ctx, journalName(execution.ID), metav1.GetOptions{})
	if err != nil {
		return errors.New("read runtime journal for update")
	}
	var document journalDocument
	if err := decodeJournal([]byte(configMap.Data[journalDataKey]), &document); err != nil ||
		document.Execution.ID != execution.ID || execution.Version < document.Execution.Version ||
		execution.Fence < document.Execution.Fence ||
		execution.GrantGeneration != document.Execution.GrantGeneration {
		return errs.ErrStateConflict
	}
	document.Execution = execution
	refreshCommandKeys(&document)
	document.LastTransition = adapter.now().UTC()
	raw, err := marshalJournal(document)
	if err != nil {
		return err
	}
	configMap.Data[journalDataKey] = string(raw)
	configMap.Labels["runtime.mattercodex.dev/state"] = strings.ToLower(string(execution.State))
	if _, err := configMaps.Update(ctx, configMap, metav1.UpdateOptions{}); err != nil {
		return errors.New("update runtime journal")
	}
	pod, podErr := adapter.client.CoreV1().Pods(adapter.config.Namespace).Get(
		ctx, document.PodName, metav1.GetOptions{},
	)
	if podErr == nil && pod.Annotations["runtime.mattercodex.dev/execution-id"] == execution.ID {
		pod.Labels["runtime.mattercodex.dev/state"] = strings.ToLower(string(execution.State))
		pod.Annotations["runtime.mattercodex.dev/version"] = strconv.FormatUint(execution.Version, 10)
		pod.Annotations["runtime.mattercodex.dev/fence"] = strconv.FormatUint(execution.Fence, 10)
		pod.Annotations["runtime.mattercodex.dev/grant-generation"] = strconv.FormatUint(execution.GrantGeneration, 10)
		pod.Annotations["runtime.mattercodex.dev/last-transition-at"] = document.LastTransition.Format(time.RFC3339Nano)
		if _, err := adapter.client.CoreV1().Pods(adapter.config.Namespace).Update(
			ctx, pod, metav1.UpdateOptions{},
		); err != nil {
			return errors.New("update runtime pod lifecycle label")
		}
		if execution.State.Terminal() {
			if err := adapter.revokeAccessProfile(ctx, pod); err != nil {
				return err
			}
		}
	} else if podErr != nil && !apierrors.IsNotFound(podErr) {
		return errors.New("read runtime pod lifecycle label")
	}
	secrets := adapter.client.CoreV1().Secrets(adapter.config.Namespace)
	if leaseToken == "" {
		existing, err := secrets.Get(ctx, document.LeaseSecretName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return errors.New("read runtime lease secret for revocation")
		}
		uid := existing.UID
		resourceVersion := existing.ResourceVersion
		if err := secrets.Delete(ctx, existing.Name, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion},
		}); err != nil && !apierrors.IsNotFound(err) {
			return errors.New("revoke runtime lease secret")
		}
		return nil
	}
	if len(leaseToken) > 16<<10 {
		return errs.ErrStateConflict
	}
	desired := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: document.LeaseSecretName, Labels: labels(execution, "lease"),
	}, Immutable: boolPointer(true), Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{leaseTokenKey: []byte(leaseToken)}}
	existing, err := secrets.Get(ctx, desired.Name, metav1.GetOptions{})
	if err == nil {
		if existing.Immutable == nil || !*existing.Immutable || existing.Type != corev1.SecretTypeOpaque ||
			existing.Labels[executionLabel] != shortID(execution.ID) ||
			existing.Labels[componentLabel] != "lease" ||
			!bytesEqual(existing.Data[leaseTokenKey], desired.Data[leaseTokenKey]) {
			return errs.ErrStateConflict
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return errors.New("read runtime lease secret")
	}
	if _, err := secrets.Create(ctx, desired, metav1.CreateOptions{}); err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return errors.New("create runtime lease secret")
	}
	return nil
}

func (adapter *Adapter) UpdateJournal(
	ctx context.Context,
	execution entity.Execution,
	leaseToken string,
) error {
	return adapter.updateJournalAndLease(ctx, execution, leaseToken)
}

func (adapter *Adapter) rolePod(
	execution entity.Execution,
	revision entity.Revision,
) (*corev1.Pod, error) {
	volumes := []corev1.Volume{
		{Name: "session", VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName(execution)},
		}},
		{Name: "runtime-config", VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configName(execution)}},
		}},
		{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{
			SizeLimit: quantityPointer(resource.MustParse("256Mi")),
		}}},
	}
	mounts := []corev1.VolumeMount{
		{Name: "session", MountPath: "/workspace"},
		{Name: "runtime-config", MountPath: "/var/run/config/mattercodex/runtime", ReadOnly: true},
		{Name: "tmp", MountPath: "/tmp"},
	}
	for index, credential := range revision.Credentials {
		name := "credential-" + strconv.Itoa(index)
		volume, mount, err := credentialVolume(name, credential)
		if err != nil {
			return nil, err
		}
		volumes = append(volumes, volume)
		mounts = append(mounts, mount)
	}
	serviceAccount := adapter.config.RuntimeServiceAccount
	if execution.AccessProfile != enum.AccessNone {
		volumes = append(volumes, corev1.Volume{Name: "kube-api-access", VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{DefaultMode: int32Pointer(0o440), Sources: []corev1.VolumeProjection{
				{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
					Audience: "https://kubernetes.default.svc", ExpirationSeconds: int64Pointer(600), Path: "token",
				}},
				{ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"}, Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}},
				{DownwardAPI: &corev1.DownwardAPIProjection{Items: []corev1.DownwardAPIVolumeFile{{Path: "namespace", FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}}}},
			}},
		}})
		mounts = append(mounts, corev1.VolumeMount{Name: "kube-api-access", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true})
	}
	switch execution.AccessProfile {
	case enum.AccessProjectRead:
		serviceAccount = accessServiceAccountName(execution)
	case enum.AccessClusterAdmin:
		serviceAccount = accessServiceAccountName(execution)
	}
	cpu, memory, accelerator := resourcesFor(execution.ResourceClass)
	requests := corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewMilliQuantity(cpu, resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewQuantity(memory, resource.BinarySI),
	}
	limits := requests.DeepCopy()
	if accelerator {
		requests["nvidia.com/gpu"] = resource.MustParse("1")
		limits["nvidia.com/gpu"] = resource.MustParse("1")
	}
	labels := labels(execution, roleComponent)
	labels["mattercodex.dev/environment"] = adapter.config.Environment
	labels["runtime.mattercodex.dev/resource-class"] = strings.ToLower(string(execution.ResourceClass))
	labels[accessLabel] = strings.ToLower(string(execution.AccessProfile))
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: podName(execution), Labels: labels,
		Annotations: map[string]string{
			"runtime.mattercodex.dev/execution-id":      execution.ID,
			"runtime.mattercodex.dev/version":           strconv.FormatUint(execution.Version, 10),
			"runtime.mattercodex.dev/fence":             strconv.FormatUint(execution.Fence, 10),
			"runtime.mattercodex.dev/grant-generation":  strconv.FormatUint(execution.GrantGeneration, 10),
			"runtime.mattercodex.dev/revision-sha256":   execution.RuntimeRevisionSHA256,
			"runtime.mattercodex.dev/input-sha256":      execution.ImmutableInputSHA256,
			"runtime.mattercodex.dev/manifest-sha256":   revision.ManifestSHA256,
			"runtime.mattercodex.dev/project-namespace": projectNamespaceName(execution.ProjectID),
		},
	}, Spec: corev1.PodSpec{
		ServiceAccountName: serviceAccount, AutomountServiceAccountToken: boolPointer(false),
		EnableServiceLinks: boolPointer(false), RestartPolicy: corev1.RestartPolicyNever,
		TerminationGracePeriodSeconds: int64Pointer(30),
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
		Containers: []corev1.Container{{
			Name: "role-runtime", Image: adapter.config.RoleImageRepository + "@" + revision.ImageDigest,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Env: []corev1.EnvVar{
				{Name: "MATTERCODEX_RUNTIME_REVISION_FILE", Value: "/var/run/config/mattercodex/runtime/runtime.json"},
				{Name: "MATTERCODEX_EXECUTION_ID", Value: execution.ID},
			},
			Resources:       corev1.ResourceRequirements{Requests: requests, Limits: limits},
			SecurityContext: restrictedSecurityContext(10001), VolumeMounts: mounts,
			ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt32(9090)}}, PeriodSeconds: 5, TimeoutSeconds: 2, FailureThreshold: 3},
			LivenessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/livez", Port: intstr.FromInt32(9090)}}, PeriodSeconds: 10, TimeoutSeconds: 2, FailureThreshold: 3},
		}}, Volumes: volumes,
	}}, nil
}

func (adapter *Adapter) ensureAccessProfile(ctx context.Context, execution entity.Execution) error {
	if execution.AccessProfile == enum.AccessNone {
		return nil
	}
	name := accessServiceAccountName(execution)
	serviceAccounts := adapter.client.CoreV1().ServiceAccounts(adapter.config.Namespace)
	existing, err := serviceAccounts.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		existing, err = serviceAccounts.Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name: name, Labels: labels(execution, "runtime-access"),
			Annotations: map[string]string{"runtime.mattercodex.dev/execution-id": execution.ID},
		}, AutomountServiceAccountToken: boolPointer(false)}, metav1.CreateOptions{})
	}
	if err != nil {
		return errors.New("ensure exact runtime access service account")
	}
	if existing.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID {
		return errs.ErrStateConflict
	}
	subject := rbacv1.Subject{Kind: "ServiceAccount", Name: name, Namespace: adapter.config.Namespace}
	switch execution.AccessProfile {
	case enum.AccessProjectRead:
		projectNamespace := projectNamespaceName(execution.ProjectID)
		namespace, err := adapter.client.CoreV1().Namespaces().Get(ctx, projectNamespace, metav1.GetOptions{})
		if err != nil {
			return errors.New("read server-managed project namespace")
		}
		if namespace.Annotations["mattercodex.dev/project-id"] != execution.ProjectID ||
			namespace.Annotations["mattercodex.dev/organization-id"] != execution.OrganizationID {
			return errs.ErrStateConflict
		}
		bindings := adapter.client.RbacV1().RoleBindings(projectNamespace)
		desired := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels(execution, "runtime-access")},
			Subjects: []rbacv1.Subject{subject}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: adapter.config.ReadClusterRole}}
		binding, err := bindings.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			binding, err = bindings.Create(ctx, desired, metav1.CreateOptions{})
		}
		if err != nil {
			return errors.New("ensure exact project read binding")
		}
		if !sameBinding(binding.Subjects, subject) || binding.RoleRef != desired.RoleRef {
			return errs.ErrStateConflict
		}
	case enum.AccessClusterAdmin:
		bindings := adapter.client.RbacV1().ClusterRoleBindings()
		desired := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels(execution, "runtime-access")},
			Subjects: []rbacv1.Subject{subject}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: adapter.config.AdminClusterRole}}
		binding, err := bindings.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			binding, err = bindings.Create(ctx, desired, metav1.CreateOptions{})
		}
		if err != nil {
			return errors.New("ensure exact cluster admin binding")
		}
		if !sameBinding(binding.Subjects, subject) || binding.RoleRef != desired.RoleRef {
			return errs.ErrStateConflict
		}
	default:
		return errs.ErrStateConflict
	}
	return nil
}

func credentialVolume(name string, credential entity.CredentialRef) (corev1.Volume, corev1.VolumeMount, error) {
	path := "/var/run/secrets/mattercodex/runtime/" + name
	if secret, ok := strings.CutPrefix(credential.Reference, "k8s-secret://"); ok && validDNSLabel(secret) {
		return corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName: secret, DefaultMode: int32Pointer(0o440),
		}}}, corev1.VolumeMount{Name: name, MountPath: path, ReadOnly: true}, nil
	}
	if provider, ok := strings.CutPrefix(credential.Reference, "vault-csi://"); ok && validDNSLabel(provider) {
		return corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{CSI: &corev1.CSIVolumeSource{
			Driver: "secrets-store.csi.k8s.io", ReadOnly: boolPointer(true),
			VolumeAttributes: map[string]string{"secretProviderClass": provider},
		}}}, corev1.VolumeMount{Name: name, MountPath: path, ReadOnly: true}, nil
	}
	return corev1.Volume{}, corev1.VolumeMount{}, errs.ErrStateConflict
}

func (adapter *Adapter) List(ctx context.Context) ([]entity.RuntimeStatus, error) {
	configMaps := &corev1.ConfigMapList{}
	continueToken := ""
	for {
		page, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: managedLabel + "=true," + componentLabel + "=" + journalComponent,
			Limit:         1000, Continue: continueToken,
		})
		if err != nil {
			return nil, errors.New("list runtime journals")
		}
		configMaps.Items = append(configMaps.Items, page.Items...)
		if len(configMaps.Items) > 100_000 {
			return nil, errors.New("runtime journal inventory exceeds safety limit")
		}
		continueToken = page.Continue
		if continueToken == "" {
			break
		}
	}
	pods := &corev1.PodList{}
	continueToken = ""
	for {
		page, err := adapter.client.CoreV1().Pods(adapter.config.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: managedLabel + "=true," + componentLabel + "=" + roleComponent,
			Limit:         1000, Continue: continueToken,
		})
		if err != nil {
			return nil, errors.New("list role runtime pods")
		}
		pods.Items = append(pods.Items, page.Items...)
		if len(pods.Items) > 100_000 {
			return nil, errors.New("runtime pod inventory exceeds safety limit")
		}
		continueToken = page.Continue
		if continueToken == "" {
			break
		}
	}
	podByExecution := make(map[string]*corev1.Pod, len(pods.Items))
	for index := range pods.Items {
		pod := &pods.Items[index]
		podByExecution[pod.Labels[executionLabel]] = pod
	}
	result := make([]entity.RuntimeStatus, 0, len(configMaps.Items))
	presentPVC := make(map[string]bool, len(configMaps.Items))
	for index := range configMaps.Items {
		configMap := &configMaps.Items[index]
		raw := configMap.Data[journalDataKey]
		var document journalDocument
		if decodeJournal([]byte(raw), &document) != nil || document.Execution.Validate() != nil {
			return nil, errors.New("runtime journal is invalid")
		}
		var pvc *corev1.PersistentVolumeClaim
		foundPVC, pvcErr := adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).Get(
			ctx, document.PVCName, metav1.GetOptions{},
		)
		if pvcErr == nil {
			if foundPVC.Labels[componentLabel] != "session-storage" ||
				foundPVC.Annotations["runtime.mattercodex.dev/organization-id"] != document.Execution.OrganizationID ||
				foundPVC.Annotations["runtime.mattercodex.dev/project-id"] != document.Execution.ProjectID ||
				foundPVC.Annotations["runtime.mattercodex.dev/session-id"] != document.Execution.SessionID {
				return nil, errors.New("runtime PVC ownership is invalid")
			}
			pvc = foundPVC
			presentPVC[document.PVCName] = true
		} else if !apierrors.IsNotFound(pvcErr) {
			return nil, errors.New("read runtime PVC status")
		}
		pod := podByExecution[shortID(document.Execution.ID)]
		if pod != nil && pod.Annotations["runtime.mattercodex.dev/execution-id"] != document.Execution.ID {
			return nil, errors.New("runtime pod ownership is invalid")
		}
		status := castStatus(pod, pvc)
		status.ExecutionID = document.Execution.ID
		status.Version = document.Execution.Version
		status.Fence = document.Execution.Fence
		status.GrantGeneration = document.Execution.GrantGeneration
		status.AccessProfile = document.Execution.AccessProfile
		status.JournalName = configMap.Name
		liveRetentionOwner := pvc != nil &&
			pvc.Annotations["runtime.mattercodex.dev/retention-owner-execution-id"] == document.Execution.ID &&
			pvc.Annotations["runtime.mattercodex.dev/retention-owner-journal"] == configMap.Name
		status.RetentionOwner = liveRetentionOwner || document.PVCDeletionOwner && pvc == nil
		status.PVCName = document.PVCName
		if document.PVCUID != "" {
			status.PVCUID = document.PVCUID
			status.PVCResourceVersion = document.PVCResourceVersion
		}
		status.PVCDeleted = document.PVCDeleted
		status.PVCDeletionStarted = document.PVCUID != ""
		if status.LastTransition.IsZero() {
			status.LastTransition = document.LastTransition
		} else if document.LastTransition.After(status.LastTransition) {
			status.LastTransition = document.LastTransition
		}
		result = append(result, status)
	}
	ownerCount := make(map[string]int, len(result))
	for index := range result {
		if result[index].RetentionOwner {
			ownerCount[result[index].PVCName]++
		}
	}
	for pvcName := range presentPVC {
		if ownerCount[pvcName] != 1 {
			return nil, errors.New("runtime PVC retention owner is invalid")
		}
	}
	return result, nil
}

func castStatus(pod *corev1.Pod, pvc *corev1.PersistentVolumeClaim) entity.RuntimeStatus {
	status := entity.RuntimeStatus{Phase: "Missing"}
	if pvc != nil {
		status.PVCName, status.PVCUID, status.PVCResourceVersion = pvc.Name, string(pvc.UID), pvc.ResourceVersion
	}
	if pod == nil {
		return status
	}
	status.PodName, status.PodUID, status.PodResourceVersion = pod.Name, string(pod.UID), pod.ResourceVersion
	status.ExecutionID = pod.Annotations["runtime.mattercodex.dev/execution-id"]
	status.Version, _ = strconv.ParseUint(pod.Annotations["runtime.mattercodex.dev/version"], 10, 64)
	status.Fence, _ = strconv.ParseUint(pod.Annotations["runtime.mattercodex.dev/fence"], 10, 64)
	status.GrantGeneration, _ = strconv.ParseUint(pod.Annotations["runtime.mattercodex.dev/grant-generation"], 10, 64)
	status.Phase = string(pod.Status.Phase)
	status.LastTransition = pod.CreationTimestamp.Time
	status.AccessProfile = enum.AccessProfile(strings.ToUpper(pod.Labels[accessLabel]))
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			status.Ready = condition.Status == corev1.ConditionTrue
			if !condition.LastTransitionTime.IsZero() {
				status.LastTransition = condition.LastTransitionTime.Time
			}
		}
	}
	if transition, err := time.Parse(time.RFC3339Nano, pod.Annotations["runtime.mattercodex.dev/last-transition-at"]); err == nil &&
		transition.After(status.LastTransition) {
		status.LastTransition = transition
	}
	return status
}

func (adapter *Adapter) DeletePod(ctx context.Context, status entity.RuntimeStatus) error {
	if status.PodName == "" {
		return nil
	}
	if status.PodUID == "" || status.PodResourceVersion == "" {
		return errs.ErrStateConflict
	}
	uid := types.UID(status.PodUID)
	resourceVersion := status.PodResourceVersion
	zero := int64(0)
	pod, err := adapter.client.CoreV1().Pods(adapter.config.Namespace).Get(
		ctx, status.PodName, metav1.GetOptions{},
	)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil || pod.UID != uid || pod.ResourceVersion != resourceVersion {
		return errs.ErrStateConflict
	}
	if err := adapter.revokeAccessProfile(ctx, pod); err != nil {
		return err
	}
	err = adapter.client.CoreV1().Pods(adapter.config.Namespace).Delete(
		ctx, status.PodName, metav1.DeleteOptions{GracePeriodSeconds: &zero,
			Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion}},
	)
	if err != nil && !apierrors.IsNotFound(err) {
		if apierrors.IsConflict(err) {
			return errs.ErrStateConflict
		}
		return errors.New("delete exact runtime pod")
	}
	waitErr := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 5*time.Second, true,
		func(pollCtx context.Context) (bool, error) {
			_, getErr := adapter.client.CoreV1().Pods(adapter.config.Namespace).Get(
				pollCtx, status.PodName, metav1.GetOptions{},
			)
			if apierrors.IsNotFound(getErr) {
				return true, nil
			}
			if getErr != nil {
				return false, errors.New("read back deleted runtime pod")
			}
			return false, nil
		})
	if waitErr != nil {
		return errors.New("runtime pod deletion is not confirmed")
	}
	return nil
}

func (adapter *Adapter) revokeAccessProfile(ctx context.Context, pod *corev1.Pod) error {
	if pod == nil || pod.Labels[accessLabel] == strings.ToLower(string(enum.AccessNone)) {
		return nil
	}
	name := pod.Spec.ServiceAccountName
	executionID := pod.Annotations["runtime.mattercodex.dev/execution-id"]
	if name == "" || executionID == "" {
		return errs.ErrStateConflict
	}
	switch enum.AccessProfile(strings.ToUpper(pod.Labels[accessLabel])) {
	case enum.AccessProjectRead:
		namespace := pod.Annotations["runtime.mattercodex.dev/project-namespace"]
		binding, err := adapter.client.RbacV1().RoleBindings(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			uid, version := binding.UID, binding.ResourceVersion
			err = adapter.client.RbacV1().RoleBindings(namespace).Delete(
				ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}},
			)
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return errors.New("revoke exact project read binding")
		}
	case enum.AccessClusterAdmin:
		binding, err := adapter.client.RbacV1().ClusterRoleBindings().Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			uid, version := binding.UID, binding.ResourceVersion
			err = adapter.client.RbacV1().ClusterRoleBindings().Delete(
				ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}},
			)
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return errors.New("revoke exact cluster admin binding")
		}
	default:
		return errs.ErrStateConflict
	}
	serviceAccount, err := adapter.client.CoreV1().ServiceAccounts(adapter.config.Namespace).Get(
		ctx, name, metav1.GetOptions{},
	)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil || serviceAccount.Annotations["runtime.mattercodex.dev/execution-id"] != executionID {
		return errs.ErrStateConflict
	}
	uid, version := serviceAccount.UID, serviceAccount.ResourceVersion
	if err := adapter.client.CoreV1().ServiceAccounts(adapter.config.Namespace).Delete(
		ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}},
	); err != nil && !apierrors.IsNotFound(err) {
		return errors.New("revoke exact runtime access service account")
	}
	return nil
}

func (adapter *Adapter) RevokeAccess(ctx context.Context, execution entity.Execution) error {
	if execution.Validate() != nil || !execution.State.Terminal() {
		return errs.ErrInvalidInput
	}
	if execution.AccessProfile == enum.AccessNone {
		return nil
	}
	name := accessServiceAccountName(execution)
	subject := rbacv1.Subject{Kind: "ServiceAccount", Name: name, Namespace: adapter.config.Namespace}
	switch execution.AccessProfile {
	case enum.AccessProjectRead:
		namespaceName := projectNamespaceName(execution.ProjectID)
		namespace, err := adapter.client.CoreV1().Namespaces().Get(ctx, namespaceName, metav1.GetOptions{})
		if err == nil && (namespace.Annotations["mattercodex.dev/project-id"] != execution.ProjectID ||
			namespace.Annotations["mattercodex.dev/organization-id"] != execution.OrganizationID) {
			return errs.ErrStateConflict
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return errors.New("read project namespace before access revocation")
		}
		if err == nil {
			binding, bindingErr := adapter.client.RbacV1().RoleBindings(namespaceName).Get(ctx, name, metav1.GetOptions{})
			if bindingErr == nil {
				expectedRole := rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: adapter.config.ReadClusterRole}
				if !sameBinding(binding.Subjects, subject) || binding.RoleRef != expectedRole {
					return errs.ErrStateConflict
				}
				uid, version := binding.UID, binding.ResourceVersion
				bindingErr = adapter.client.RbacV1().RoleBindings(namespaceName).Delete(ctx, name,
					metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}})
			}
			if bindingErr != nil && !apierrors.IsNotFound(bindingErr) {
				return errors.New("revoke exact project read binding")
			}
		}
	case enum.AccessClusterAdmin:
		binding, err := adapter.client.RbacV1().ClusterRoleBindings().Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			expectedRole := rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: adapter.config.AdminClusterRole}
			if !sameBinding(binding.Subjects, subject) || binding.RoleRef != expectedRole {
				return errs.ErrStateConflict
			}
			uid, version := binding.UID, binding.ResourceVersion
			err = adapter.client.RbacV1().ClusterRoleBindings().Delete(ctx, name,
				metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}})
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return errors.New("revoke exact cluster admin binding")
		}
	default:
		return errs.ErrStateConflict
	}
	serviceAccount, err := adapter.client.CoreV1().ServiceAccounts(adapter.config.Namespace).Get(
		ctx, name, metav1.GetOptions{},
	)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return errors.New("read runtime access service account before revocation")
	}
	if serviceAccount.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID {
		return errs.ErrStateConflict
	}
	uid, version := serviceAccount.UID, serviceAccount.ResourceVersion
	if err := adapter.client.CoreV1().ServiceAccounts(adapter.config.Namespace).Delete(ctx, name,
		metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}}); err != nil && !apierrors.IsNotFound(err) {
		return errors.New("revoke exact runtime access service account")
	}
	return nil
}

func (adapter *Adapter) DeletePVC(ctx context.Context, status entity.RuntimeStatus) error {
	if status.PVCDeleted {
		return nil
	}
	if !status.RetentionOwner || status.PVCName == "" || status.PVCUID == "" || status.PVCResourceVersion == "" {
		return errs.ErrStateConflict
	}
	uid := types.UID(status.PVCUID)
	resourceVersion := status.PVCResourceVersion
	if !status.PVCDeletionStarted {
		if err := adapter.recordPVCDeletion(ctx, status, false); err != nil {
			return err
		}
	}
	pvc, readErr := adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).Get(
		ctx, status.PVCName, metav1.GetOptions{},
	)
	if apierrors.IsNotFound(readErr) {
		return adapter.recordPVCDeletion(ctx, status, true)
	}
	if readErr != nil {
		return errors.New("read runtime PVC before guarded deletion")
	}
	if pvc.UID != uid ||
		pvc.Annotations["runtime.mattercodex.dev/retention-owner-execution-id"] != status.ExecutionID ||
		pvc.Annotations["runtime.mattercodex.dev/retention-owner-journal"] != journalName(status.ExecutionID) {
		return errs.ErrStateConflict
	}
	if pvc.DeletionTimestamp == nil {
		if pvc.ResourceVersion != resourceVersion {
			return errs.ErrStateConflict
		}
		err := adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).Delete(
			ctx, status.PVCName, metav1.DeleteOptions{
				Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion},
			},
		)
		if err != nil && !apierrors.IsNotFound(err) {
			if apierrors.IsConflict(err) {
				return errs.ErrStateConflict
			}
			return errors.New("delete exact runtime PVC")
		}
	}
	_, readErr = adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).Get(
		ctx, status.PVCName, metav1.GetOptions{},
	)
	if readErr == nil {
		return errs.ErrDependency
	}
	if !apierrors.IsNotFound(readErr) {
		return errors.New("read back deleted runtime PVC")
	}
	return adapter.recordPVCDeletion(ctx, status, true)
}

func (adapter *Adapter) recordPVCDeletion(
	ctx context.Context,
	status entity.RuntimeStatus,
	deleted bool,
) error {
	configMap, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(
		ctx, journalName(status.ExecutionID), metav1.GetOptions{},
	)
	if err != nil {
		return errors.New("read runtime journal for PVC deletion")
	}
	var document journalDocument
	if decodeJournal([]byte(configMap.Data[journalDataKey]), &document) != nil ||
		document.Execution.ID != status.ExecutionID || document.PVCName != status.PVCName ||
		(document.PVCUID != "" && document.PVCUID != status.PVCUID) ||
		(document.PVCResourceVersion != "" && document.PVCResourceVersion != status.PVCResourceVersion) ||
		document.PVCDeleted && !deleted || document.PVCDeletionOwner && document.PVCUID == "" {
		return errs.ErrStateConflict
	}
	document.PVCUID = status.PVCUID
	document.PVCResourceVersion = status.PVCResourceVersion
	document.PVCDeletionOwner = true
	document.PVCDeleted = deleted
	raw, err := marshalJournal(document)
	if err != nil {
		return err
	}
	configMap.Data[journalDataKey] = string(raw)
	if _, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Update(ctx, configMap, metav1.UpdateOptions{}); err != nil {
		if apierrors.IsConflict(err) {
			return errs.ErrStateConflict
		}
		return errors.New("persist runtime PVC deletion evidence")
	}
	return nil
}

func (adapter *Adapter) EnsureArchiveJob(
	ctx context.Context,
	execution entity.Execution,
	status entity.RuntimeStatus,
) error {
	return adapter.ensureWorkerJob(ctx, execution, status, archiveComponent)
}

func (adapter *Adapter) EnsureRestoreVerifierJob(
	ctx context.Context,
	execution entity.Execution,
	status entity.RuntimeStatus,
) error {
	return adapter.ensureWorkerJob(ctx, execution, status, restoreComponent)
}

func (adapter *Adapter) EnsureCleanupAuthorizerJob(
	ctx context.Context,
	execution entity.Execution,
	status entity.RuntimeStatus,
) error {
	return adapter.ensureWorkerJob(ctx, execution, status, cleanupComponent)
}

func (adapter *Adapter) ensureWorkerJob(
	ctx context.Context,
	execution entity.Execution,
	status entity.RuntimeStatus,
	component string,
) error {
	if !execution.State.Terminal() || status.PVCName == "" {
		return errs.ErrStateConflict
	}
	name := workerJobName(component, execution)
	existing, err := adapter.client.BatchV1().Jobs(adapter.config.Namespace).Get(
		ctx, name, metav1.GetOptions{},
	)
	if err == nil {
		if existing.Labels[executionLabel] != shortID(execution.ID) ||
			existing.Annotations["runtime.mattercodex.dev/fence"] != strconv.FormatUint(execution.Fence, 10) {
			return errs.ErrStateConflict
		}
		if existing.Status.Failed > 0 && existing.Status.Active == 0 {
			return errs.ErrDependency
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return errors.New("read runtime worker job")
	}
	job, err := adapter.workerJob(execution, status, component)
	if err != nil {
		return err
	}
	if _, err := adapter.client.BatchV1().Jobs(adapter.config.Namespace).Create(
		ctx, job, metav1.CreateOptions{},
	); err != nil && !apierrors.IsAlreadyExists(err) {
		return errors.New("create runtime worker job")
	}
	return nil
}

func (adapter *Adapter) workerJob(
	execution entity.Execution,
	status entity.RuntimeStatus,
	component string,
) (*batchv1.Job, error) {
	command, serviceAccount, workload, spiffe := "", "", "", ""
	switch component {
	case archiveComponent:
		command, serviceAccount, workload = "/usr/local/bin/runtime-archive", adapter.config.ArchiveServiceAccount, "runtime-controller"
		spiffe = "spiffe://mattercodex.local/ns/" + adapter.config.Namespace + "/sa/" + serviceAccount
	case restoreComponent:
		command, serviceAccount, workload = "/usr/local/bin/runtime-restore-verifier", adapter.config.RestoreServiceAccount, "runtime-restore-verifier"
		spiffe = "spiffe://mattercodex.local/ns/" + adapter.config.Namespace + "/sa/" + serviceAccount
	case cleanupComponent:
		command, serviceAccount, workload = "/usr/local/bin/runtime-cleanup-authorizer", adapter.config.CleanupServiceAccount, "runtime-cleanup-authorizer"
		spiffe = "spiffe://mattercodex.local/ns/" + adapter.config.Namespace + "/sa/" + serviceAccount
	default:
		return nil, errs.ErrInvalidInput
	}
	backoff := int32(5)
	ttl := int32(adapter.config.JobTTL.Seconds())
	labels := labels(execution, component)
	labels["runtime.mattercodex.dev/worker"] = component
	labels["mattercodex.dev/environment"] = adapter.config.Environment
	mainVolumes, mainMounts := adapter.workerVolumes(component, execution, status)
	authorityVolumes, authorityMounts := authorityIssuerVolumes(workload)
	volumes := append(mainVolumes, authorityVolumes...)
	mainMounts = append(mainMounts, corev1.VolumeMount{Name: "authority-sockets", MountPath: "/run/mattercodex/internal-rpc-authority", ReadOnly: true})
	environment := []corev1.EnvVar{
		{Name: "RUNTIME_EXECUTION_ID", Value: execution.ID},
		{Name: "RUNTIME_EXPECTED_VERSION", Value: strconv.FormatUint(execution.Version, 10)},
		{Name: "RUNTIME_EXPECTED_FENCE", Value: strconv.FormatUint(execution.Fence, 10)},
		{Name: "RUNTIME_JOURNAL_NAME", Value: journalName(execution.ID)},
		{Name: "RUNTIME_S3_ENDPOINT", Value: adapter.config.S3Endpoint},
		{Name: "RUNTIME_S3_TLS_SERVER_NAME", Value: adapter.config.S3TLSServerName},
		{Name: "RUNTIME_S3_BUCKET", Value: adapter.config.S3Bucket},
		{Name: "RUNTIME_S3_REGION", Value: adapter.config.S3Region},
	}
	mainResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("512Mi")},
	}
	if component == archiveComponent || component == restoreComponent {
		scratch := adapter.workerScratch(component)
		mainResources.Requests[corev1.ResourceEphemeralStorage] = scratch
		mainResources.Limits[corev1.ResourceEphemeralStorage] = scratch
	}
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: workerJobName(component, execution), Labels: labels,
		Annotations: map[string]string{"runtime.mattercodex.dev/fence": strconv.FormatUint(execution.Fence, 10)}},
		Spec: batchv1.JobSpec{BackoffLimit: &backoff, TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{
				ServiceAccountName: serviceAccount, AutomountServiceAccountToken: boolPointer(false),
				EnableServiceLinks: boolPointer(false), RestartPolicy: corev1.RestartPolicyOnFailure,
				SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true), FSGroup: int64Pointer(29000),
					SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
				InitContainers: []corev1.Container{{
					Name: "internal-rpc-authority-socket-init", Image: adapter.config.AuthorityImage,
					Command:         []string{"/usr/local/bin/internal-rpc-authority-socket-init"},
					SecurityContext: restrictedSecurityContext(29000),
					VolumeMounts:    []corev1.VolumeMount{{Name: "authority-sockets", MountPath: "/run/mattercodex"}},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("5m"), corev1.ResourceMemory: resource.MustParse("8Mi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
					},
				}, {
					Name: "internal-rpc-authority-issuer", Image: adapter.config.AuthorityImage,
					Command:       []string{"/usr/local/bin/internal-rpc-authority-issuer"},
					RestartPolicy: restartPolicyPointer(corev1.ContainerRestartPolicyAlways),
					Env:           authorityIssuerEnv(workload, spiffe), SecurityContext: restrictedSecurityContext(29001),
					Ports: []corev1.ContainerPort{{Name: "authority-metrics", ContainerPort: 9091, Protocol: corev1.ProtocolTCP}},
					StartupProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
						Path: "/readyz", Port: intstr.FromInt32(9091),
					}}, PeriodSeconds: 2, TimeoutSeconds: 1, FailureThreshold: 30},
					LivenessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
						Path: "/livez", Port: intstr.FromInt32(9091),
					}}, PeriodSeconds: 10, TimeoutSeconds: 2, FailureThreshold: 3},
					VolumeMounts: authorityMounts,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("25m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
					},
				}},
				Containers: []corev1.Container{{Name: component, Image: adapter.config.ControllerImage,
					Command: []string{command}, Env: environment,
					SecurityContext: restrictedSecurityContext(10001), VolumeMounts: mainMounts,
					Resources: mainResources,
				}}, Volumes: volumes,
			}},
		},
	}, nil
}

func (adapter *Adapter) workerVolumes(
	component string,
	execution entity.Execution,
	status entity.RuntimeStatus,
) ([]corev1.Volume, []corev1.VolumeMount) {
	workload := component
	if component == archiveComponent {
		workload = "runtime-controller"
	}
	volumes := []corev1.Volume{
		{Name: "kube-api-access", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{DefaultMode: int32Pointer(0o440), Sources: []corev1.VolumeProjection{
			{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Audience: "https://kubernetes.default.svc", ExpirationSeconds: int64Pointer(600), Path: "token"}},
			{ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"}, Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}},
			{DownwardAPI: &corev1.DownwardAPIProjection{Items: []corev1.DownwardAPIVolumeFile{{Path: "namespace", FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}}}},
		}}}},
		{Name: "journal", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: journalName(execution.ID)}, DefaultMode: int32Pointer(0o440),
		}}},
		{Name: "work", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{
			SizeLimit: quantityPointer(adapter.workerScratch(component)),
		}}},
		{Name: "control-plane-tls", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName: "internal-rpc-authority-" + workload + "-workload-tls", DefaultMode: int32Pointer(0o440),
		}}},
		{Name: "control-plane-ca", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: "mattercodex-internal-ca"}, DefaultMode: int32Pointer(0o440),
		}}},
		{Name: "application-grant", VolumeSource: corev1.VolumeSource{CSI: &corev1.CSIVolumeSource{
			Driver: "secrets-store.csi.k8s.io", ReadOnly: boolPointer(true),
			VolumeAttributes: map[string]string{"secretProviderClass": workload + "-application-grant"},
		}}},
	}
	mounts := []corev1.VolumeMount{
		{Name: "kube-api-access", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true},
		{Name: "journal", MountPath: "/var/run/config/mattercodex/runtime-journal", ReadOnly: true},
		{Name: "work", MountPath: "/tmp"},
		{Name: "control-plane-tls", MountPath: "/var/run/secrets/mattercodex/runtime-worker/workload-tls", ReadOnly: true},
		{Name: "control-plane-ca", MountPath: "/var/run/config/mattercodex/runtime-worker/control-plane", ReadOnly: true},
		{Name: "application-grant", MountPath: "/var/run/secrets/mattercodex/runtime-worker/application-grant", ReadOnly: true},
	}
	if component == archiveComponent || component == restoreComponent {
		s3CredentialProfile := "runtime-controller-s3"
		if component == restoreComponent {
			s3CredentialProfile = "runtime-restore-verifier-s3"
		}
		volumes = append(volumes,
			corev1.Volume{Name: "s3-credential", VolumeSource: corev1.VolumeSource{CSI: &corev1.CSIVolumeSource{
				Driver: "secrets-store.csi.k8s.io", ReadOnly: boolPointer(true),
				VolumeAttributes: map[string]string{"secretProviderClass": s3CredentialProfile},
			}}},
			corev1.Volume{Name: "s3-ca", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "mattercodex-s3-ca"}, DefaultMode: int32Pointer(0o440),
			}}},
		)
		mounts = append(mounts,
			corev1.VolumeMount{Name: "s3-credential", MountPath: "/var/run/secrets/mattercodex/runtime-worker/s3", ReadOnly: true},
			corev1.VolumeMount{Name: "s3-ca", MountPath: "/var/run/config/mattercodex/runtime-worker/s3", ReadOnly: true},
		)
	}
	if component == archiveComponent {
		volumes = append(volumes, corev1.Volume{Name: "session", VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: status.PVCName, ReadOnly: true},
		}})
		mounts = append(mounts, corev1.VolumeMount{Name: "session", MountPath: "/archive-source", ReadOnly: true})
	}
	return volumes, mounts
}

func (adapter *Adapter) workerScratch(component string) resource.Quantity {
	if component == cleanupComponent {
		return resource.MustParse("64Mi")
	}
	pvcQuantity := resource.MustParse(adapter.config.PVCSize)
	pvc := pvcQuantity.Value()
	bytes := pvc + (1 << 30)
	if component == restoreComponent {
		bytes = pvc*2 + (1 << 30)
	}
	return *resource.NewQuantity(bytes, resource.BinarySI)
}

func authorityIssuerVolumes(workload string) ([]corev1.Volume, []corev1.VolumeMount) {
	prefix := "internal-rpc-authority-" + workload
	volumes := []corev1.Volume{
		{Name: "authority-sockets", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("8Mi"))}}},
		{Name: "authority-snapshot", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "internal-rpc-authority-snapshot", DefaultMode: int32Pointer(0o440)}}},
		{Name: "authority-manifest-trust", VolumeSource: csiVolume(prefix + "-manifest-trust")},
		{Name: "authority-proof-trust", VolumeSource: csiVolume(prefix + "-proof-trust")},
		{Name: "authority-issuer-key", VolumeSource: csiVolume(prefix + "-issuer-key")},
		{Name: "authority-workload-tls", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: prefix + "-workload-tls", DefaultMode: int32Pointer(0o440)}}},
		{Name: "authority-readback-ca", VolumeSource: configMapVolume("internal-rpc-authority-readback-attestor-ca")},
		{Name: "authority-vault-ca", VolumeSource: configMapVolume("internal-rpc-authority-vault-ca")},
		{Name: "authority-vault-token", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{DefaultMode: int32Pointer(0o400), Sources: []corev1.VolumeProjection{{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Path: "token", Audience: "vault", ExpirationSeconds: int64Pointer(600)}}}}}},
		{Name: "authority-restore-ca", VolumeSource: configMapVolume("internal-rpc-authority-restore-controller-ca")},
		{Name: "authority-restore-controller", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "internal-rpc-authority-restore-controller-tls", DefaultMode: int32Pointer(0o440)}}},
		{Name: "authority-restore-trust", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "internal-rpc-authority-restore-role-trust", DefaultMode: int32Pointer(0o440)}}},
		{Name: "authority-postgres", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: prefix + "-issuer-postgresql", DefaultMode: int32Pointer(0o440)}}},
		{Name: "authority-postgres-ca", VolumeSource: configMapVolume("internal-rpc-authority-postgresql-ca")},
		{Name: "authority-otel-ca", VolumeSource: configMapVolume("internal-rpc-authority-otel-ca")},
		{Name: "authority-sentry", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "internal-rpc-authority-sentry", DefaultMode: int32Pointer(0o440)}}},
	}
	mounts := []corev1.VolumeMount{
		{Name: "authority-sockets", MountPath: "/run/mattercodex"},
		{Name: "authority-snapshot", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/snapshot", ReadOnly: true},
		{Name: "authority-manifest-trust", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/manifest-trust", ReadOnly: true},
		{Name: "authority-proof-trust", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/authority-proof-trust", ReadOnly: true},
		{Name: "authority-issuer-key", MountPath: "/var/run/secrets/mattercodex/internal-rpc-authority/issuer", ReadOnly: true},
		{Name: "authority-workload-tls", MountPath: "/var/run/secrets/mattercodex/internal-rpc-authority/workload-tls", ReadOnly: true},
		{Name: "authority-readback-ca", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/readback", ReadOnly: true},
		{Name: "authority-vault-ca", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/vault", ReadOnly: true},
		{Name: "authority-vault-token", MountPath: "/var/run/secrets/tokens/vault", ReadOnly: true},
		{Name: "authority-restore-ca", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/restore", ReadOnly: true},
		{Name: "authority-restore-controller", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/restore/controller-trust", ReadOnly: true},
		{Name: "authority-restore-trust", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/restore/role-trust", ReadOnly: true},
		{Name: "authority-postgres", MountPath: "/var/run/secrets/mattercodex/internal-rpc-authority/postgres", ReadOnly: true},
		{Name: "authority-postgres-ca", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/postgresql", ReadOnly: true},
		{Name: "authority-otel-ca", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/observability", ReadOnly: true},
		{Name: "authority-sentry", MountPath: "/var/run/secrets/mattercodex/internal-rpc-authority/observability", ReadOnly: true},
	}
	return volumes, mounts
}

func authorityIssuerEnv(workload, spiffe string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "DEPLOYMENT_ENVIRONMENT", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.labels['mattercodex.dev/environment']"}}},
		{Name: "INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN", Value: ":9091"},
		{Name: "INTERNAL_RPC_AUTHORITY_WORKLOAD_ID", Value: workload},
		{Name: "INTERNAL_RPC_AUTHORITY_WORKLOAD_SPIFFE_ID", Value: spiffe},
		{Name: "INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_ADDRESS", Value: "internal-rpc-authority-readback-attestor.mattercodex-system.svc:8443"},
		{Name: "INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_TLS_SERVER_NAME", Value: "internal-rpc-authority-readback-attestor.mattercodex-system.svc"},
		{Name: "INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_CA_FILE", Value: "/var/run/config/mattercodex/internal-rpc-authority/readback/ca.pem"},
		{Name: "INTERNAL_RPC_AUTHORITY_VAULT_AUTH_ROLE", Value: "internal-rpc-authority-" + workload},
		{Name: "INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_CA_FILE", Value: "/var/run/config/mattercodex/internal-rpc-authority/restore/ca.pem"},
		{Name: "INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_UID", Value: "10001"},
		{Name: "INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_GID", Value: "10001"},
		{Name: "INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE", Value: "/var/run/secrets/mattercodex/internal-rpc-authority/postgres/dsn"},
		{Name: "INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "internal-rpc-authority-" + workload + "-issuer-postgresql"}, Key: "username"}}},
		{Name: "INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME", Value: "internal-rpc-authority-postgresql.mattercodex-system.svc.cluster.local"},
		{Name: "INTERNAL_RPC_AUTHORITY_POSTGRES_CA_FILE", Value: "/var/run/config/mattercodex/internal-rpc-authority/postgresql/ca.pem"},
		{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "otel-collector.observability.svc:4317"},
		{Name: "OTEL_EXPORTER_OTLP_TLS_SERVER_NAME", Value: "otel-collector.observability.svc.cluster.local"},
		{Name: "OTEL_EXPORTER_OTLP_CA_FILE", Value: "/var/run/config/mattercodex/internal-rpc-authority/observability/otel-ca.pem"},
		{Name: "SENTRY_DSN_FILE", Value: "/var/run/secrets/mattercodex/internal-rpc-authority/observability/sentry-dsn"},
		{Name: "SENTRY_EXPECTED_HOST", Value: "sentry-relay.observability.svc:8443"},
	}
}

func (adapter *Adapter) CleanupTemporary(ctx context.Context, before time.Time) (int, error) {
	jobs := &batchv1.JobList{}
	continueToken := ""
	for {
		page, err := adapter.client.BatchV1().Jobs(adapter.config.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: managedLabel + "=true,app.kubernetes.io/name=runtime-controller",
			Limit:         1000, Continue: continueToken,
		})
		if err != nil {
			return 0, errors.New("list temporary runtime jobs")
		}
		jobs.Items = append(jobs.Items, page.Items...)
		if len(jobs.Items) > 100_000 {
			return 0, errors.New("temporary runtime job inventory exceeds safety limit")
		}
		continueToken = page.Continue
		if continueToken == "" {
			break
		}
	}
	deleted := 0
	for index := range jobs.Items {
		job := &jobs.Items[index]
		switch job.Labels[componentLabel] {
		case archiveComponent, restoreComponent, cleanupComponent:
		default:
			continue
		}
		if job.Status.CompletionTime == nil || !job.Status.CompletionTime.Time.Before(before) {
			continue
		}
		uid := job.UID
		resourceVersion := job.ResourceVersion
		if err := adapter.client.BatchV1().Jobs(adapter.config.Namespace).Delete(
			ctx, job.Name, metav1.DeleteOptions{PropagationPolicy: propagationPointer(metav1.DeletePropagationBackground), Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion}},
		); err != nil && !apierrors.IsNotFound(err) {
			return deleted, errors.New("delete temporary runtime job")
		}
		deleted++
	}
	return deleted, nil
}

func labels(execution entity.Execution, component string) map[string]string {
	return map[string]string{
		managedLabel: "true", componentLabel: component,
		executionLabel: shortID(execution.ID), sessionLabel: shortID(execution.SessionID),
		roleLabel: shortID(execution.RoleID), accessLabel: strings.ToLower(string(execution.AccessProfile)),
		"app.kubernetes.io/name": "runtime-controller",
	}
}

func journalName(executionID string) string { return "runtime-journal-" + shortID(executionID) }
func leaseName(executionID string) string   { return "runtime-lease-" + shortID(executionID) }
func configName(execution entity.Execution) string {
	return "runtime-config-" + shortID(execution.ID) + "-v" + strconv.FormatUint(execution.RuntimeRevisionVersion, 10)
}
func pvcName(execution entity.Execution) string {
	return "runtime-session-" + stableHash(execution.OrganizationID+":"+execution.ProjectID+":"+execution.SessionID, 20)
}
func podName(execution entity.Execution) string {
	return "runtime-role-" + stableHash(execution.RoleID+":"+execution.ThreadID+":"+execution.SessionID, 24)
}
func accessServiceAccountName(execution entity.Execution) string {
	return "runtime-access-" + shortID(execution.ID)
}
func projectNamespaceName(projectID string) string {
	return "mattercodex-project-" + stableHash(projectID, 20)
}
func workerJobName(component string, execution entity.Execution) string {
	prefix := strings.TrimPrefix(component, "runtime-")
	return "runtime-" + prefix + "-" + shortID(execution.ID) + "-f" + strconv.FormatUint(execution.Fence, 10)
}

func commandKey(execution entity.Execution, operation string) string {
	input := "mattercodex:runtime-controller:" + execution.ID + ":" + operation + ":" +
		strconv.FormatUint(execution.Version, 10) + ":" + strconv.FormatUint(execution.Fence, 10)
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(input)).String()
}

func refreshCommandKeys(document *journalDocument) {
	document.HeartbeatKey = commandKey(document.Execution, "heartbeat")
	document.CompleteKey = commandKey(document.Execution, "complete")
	document.IncidentKey = commandKey(document.Execution, "incident")
	document.ArchiveKey = commandKey(document.Execution, "archive")
	document.RestoreKey = commandKey(document.Execution, "restore")
	document.CleanupKey = commandKey(document.Execution, "cleanup")
}

func stableHash(input string, length int) string {
	digest := sha256.Sum256([]byte(input))
	return hex.EncodeToString(digest[:])[:length]
}

func shortID(input string) string {
	parsed, err := uuid.Parse(input)
	if err == nil {
		return strings.ReplaceAll(parsed.String(), "-", "")[:20]
	}
	return stableHash(input, 20)
}

func marshalJournal(document journalDocument) ([]byte, error) {
	if document.Execution.Validate() != nil || document.AdmitKey == "" ||
		document.HeartbeatKey == "" || document.CompleteKey == "" ||
		document.IncidentKey == "" || document.ArchiveKey == "" ||
		document.RestoreKey == "" || document.CleanupKey == "" ||
		document.LeaseSecretName == "" || document.PodName == "" || document.PVCName == "" ||
		document.CreatedAt.IsZero() || document.LastTransition.Before(document.CreatedAt) ||
		document.PVCDeleted && (!document.PVCDeletionOwner || document.PVCUID == "" || document.PVCResourceVersion == "") {
		return nil, errs.ErrStateConflict
	}
	raw, err := json.Marshal(document)
	if err != nil || len(raw) > maximumJournalSize {
		return nil, errs.ErrStateConflict
	}
	return raw, nil
}

func decodeJournal(raw []byte, document *journalDocument) error {
	if len(raw) == 0 || len(raw) > maximumJournalSize || document == nil {
		return errs.ErrStateConflict
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(document); err != nil {
		return errs.ErrStateConflict
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errs.ErrStateConflict
	}
	pvcEvidenceEmpty := document.PVCUID == "" && document.PVCResourceVersion == ""
	pvcEvidencePresent := document.PVCUID != "" && document.PVCResourceVersion != ""
	if document.CreatedAt.IsZero() || document.LastTransition.Before(document.CreatedAt) ||
		(!pvcEvidenceEmpty && !pvcEvidencePresent) ||
		document.PVCDeletionOwner && !pvcEvidencePresent ||
		document.PVCDeleted && !document.PVCDeletionOwner {
		return errs.ErrStateConflict
	}
	return nil
}

func resourcesFor(class enum.ResourceClass) (cpuMilli, memoryBytes int64, accelerator bool) {
	switch class {
	case enum.ResourceHighMemory:
		return 1000, 8 << 30, false
	case enum.ResourceAccelerated:
		return 2000, 8 << 30, true
	default:
		return 500, 2 << 30, false
	}
}

func terminalLabel(value string) bool {
	switch strings.ToUpper(value) {
	case "SUCCEEDED", "FAILED", "CANCELLED", "EXPIRED", "RETRIED", "SUSPENDED":
		return true
	default:
		return false
	}
}

func validDNSLabel(value string) bool {
	if len(value) < 1 || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, symbol := range value {
		if (symbol < 'a' || symbol > 'z') && (symbol < '0' || symbol > '9') && symbol != '-' {
			return false
		}
	}
	return true
}

func sameBinding(subjects []rbacv1.Subject, expected rbacv1.Subject) bool {
	return len(subjects) == 1 && subjects[0] == expected
}

func podMatches(actual, expected *corev1.Pod) bool {
	if actual == nil || expected == nil || actual.DeletionTimestamp != nil ||
		actual.Spec.ServiceAccountName != expected.Spec.ServiceAccountName ||
		actual.Spec.AutomountServiceAccountToken == nil || *actual.Spec.AutomountServiceAccountToken ||
		actual.Spec.RestartPolicy != expected.Spec.RestartPolicy ||
		actual.Spec.EnableServiceLinks == nil || *actual.Spec.EnableServiceLinks ||
		len(actual.Spec.Containers) != 1 || len(expected.Spec.Containers) != 1 ||
		!reflect.DeepEqual(actual.Spec.Volumes, expected.Spec.Volumes) {
		return false
	}
	left, right := actual.Spec.Containers[0], expected.Spec.Containers[0]
	return left.Name == right.Name && left.Image == right.Image &&
		left.ImagePullPolicy == right.ImagePullPolicy &&
		reflect.DeepEqual(left.Env, right.Env) &&
		reflect.DeepEqual(left.Resources, right.Resources) &&
		reflect.DeepEqual(left.SecurityContext, right.SecurityContext) &&
		reflect.DeepEqual(left.VolumeMounts, right.VolumeMounts) &&
		left.ReadinessProbe != nil && right.ReadinessProbe != nil &&
		left.ReadinessProbe.HTTPGet != nil && right.ReadinessProbe.HTTPGet != nil &&
		left.ReadinessProbe.HTTPGet.Path == right.ReadinessProbe.HTTPGet.Path &&
		left.ReadinessProbe.HTTPGet.Port == right.ReadinessProbe.HTTPGet.Port &&
		left.LivenessProbe != nil && right.LivenessProbe != nil &&
		left.LivenessProbe.HTTPGet != nil && right.LivenessProbe.HTTPGet != nil &&
		left.LivenessProbe.HTTPGet.Path == right.LivenessProbe.HTTPGet.Path &&
		left.LivenessProbe.HTTPGet.Port == right.LivenessProbe.HTTPGet.Port
}

func restrictedSecurityContext(uid int64) *corev1.SecurityContext {
	return &corev1.SecurityContext{
		RunAsNonRoot: boolPointer(true), RunAsUser: int64Pointer(uid), RunAsGroup: int64Pointer(uid),
		AllowPrivilegeEscalation: boolPointer(false), ReadOnlyRootFilesystem: boolPointer(true),
		Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

func csiVolume(provider string) corev1.VolumeSource {
	return corev1.VolumeSource{CSI: &corev1.CSIVolumeSource{Driver: "secrets-store.csi.k8s.io", ReadOnly: boolPointer(true), VolumeAttributes: map[string]string{"secretProviderClass": provider}}}
}
func configMapVolume(name string) corev1.VolumeSource {
	return corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}, DefaultMode: int32Pointer(0o440)}}
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var result byte
	for index := range left {
		result |= left[index] ^ right[index]
	}
	return result == 0
}

func boolPointer(value bool) *bool                                                    { return &value }
func int32Pointer(value int32) *int32                                                 { return &value }
func int64Pointer(value int64) *int64                                                 { return &value }
func quantityPointer(value resource.Quantity) *resource.Quantity                      { return &value }
func propagationPointer(value metav1.DeletionPropagation) *metav1.DeletionPropagation { return &value }
func restartPolicyPointer(value corev1.ContainerRestartPolicy) *corev1.ContainerRestartPolicy {
	return &value
}

var _ runtimerepo.Cluster = (*Adapter)(nil)
