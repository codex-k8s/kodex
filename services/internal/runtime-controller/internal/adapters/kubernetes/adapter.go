// Package kubernetes материализует server-owned RuntimeExecution в Kubernetes.
package kubernetes

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/runtimecontract"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/errs"
	runtimerepo "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/value"
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
	managedLabel        = "runtime.mattercodex.dev/managed"
	executionLabel      = "runtime.mattercodex.dev/execution"
	sessionLabel        = "runtime.mattercodex.dev/session"
	roleLabel           = "runtime.mattercodex.dev/role"
	accessLabel         = "runtime.mattercodex.dev/access-profile"
	componentLabel      = "app.kubernetes.io/component"
	journalComponent    = "runtime-journal"
	roleComponent       = "role-runtime"
	archiveComponent    = "runtime-archive"
	restoreComponent    = "runtime-restore-verifier"
	rehydrateComponent  = "runtime-rehydrate"
	cleanupComponent    = "runtime-cleanup-authorizer"
	credentialComponent = "runtime-credential-broker"
	journalDataKey      = "journal.json"
	maximumJournalSize  = 1 << 20
	maximumPVCBytes     = int64(30 << 30)
)

type Config struct {
	Environment                      string
	Namespace                        string
	RunnerControlPlaneTarget         string
	RunnerControlPlaneTLSServerName  string
	InteractionGatewayURL            string
	SessionMCPURL                    string
	ControllerImage                  string
	AuthorityImage                   string
	PromotedRoleImageRepository      string
	StorageClass                     string
	PVCSize                          string
	ReadClusterRole                  string
	AdminClusterRole                 string
	ArchiveRestoreEnabled            bool
	ArchiveServiceAccount            string
	RestoreServiceAccount            string
	CleanupServiceAccount            string
	CredentialBrokerServiceAccount   string
	ProjectReadBrokerServiceAccount  string
	ClusterAdminBrokerServiceAccount string
	S3ArchiveBrokerServiceAccount    string
	S3RestoreBrokerServiceAccount    string
	MaximumPods                      int
	MaximumOrganizationExecutions    int
	MaximumCPU                       int64
	MaximumMemoryBytes               int64
	JobTTL                           time.Duration
	S3Endpoint                       string
	S3TLSServerName                  string
	S3Bucket                         string
	S3Region                         string
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
		config.Namespace == "" ||
		config.RunnerControlPlaneTarget == "" || config.RunnerControlPlaneTLSServerName == "" ||
		config.InteractionGatewayURL == "" || config.SessionMCPURL == "" ||
		config.ControllerImage == "" || config.AuthorityImage == "" ||
		!value.ValidImageRepository(config.PromotedRoleImageRepository) ||
		config.StorageClass == "" || config.PVCSize == "" || config.MaximumPods < 1 ||
		config.MaximumOrganizationExecutions < 1 || config.MaximumOrganizationExecutions > config.MaximumPods ||
		config.MaximumCPU < 1 || config.MaximumMemoryBytes < 1 ||
		config.JobTTL < time.Minute || config.JobTTL > 24*time.Hour {
		return nil, errors.New("kubernetes adapter configuration is invalid")
	}
	for _, serviceAccount := range []string{
		config.ReadClusterRole, config.AdminClusterRole,
		config.CleanupServiceAccount, config.CredentialBrokerServiceAccount,
		config.ProjectReadBrokerServiceAccount,
		config.ClusterAdminBrokerServiceAccount,
	} {
		if serviceAccount == "" {
			return nil, errors.New("runtime service account configuration is invalid")
		}
	}
	archiveRestoreValues := []string{config.ArchiveServiceAccount, config.RestoreServiceAccount,
		config.S3ArchiveBrokerServiceAccount, config.S3RestoreBrokerServiceAccount,
		config.S3Endpoint, config.S3TLSServerName, config.S3Bucket, config.S3Region}
	for _, value := range archiveRestoreValues {
		if (config.ArchiveRestoreEnabled && value == "") || (!config.ArchiveRestoreEnabled && value != "") {
			return nil, errors.New("kubernetes archive/restore configuration is invalid")
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

// PatchWorkerRehydration фиксирует target-PVC proof без изменения owner execution.
func PatchWorkerRehydration(
	ctx context.Context,
	namespace, name string, execution entity.Execution, pvcUID, reference, proofSHA256 string,
) error {
	if namespace == "" || execution.Validate() != nil || name != journalName(execution.ID) ||
		execution.RestoreAssignmentState != "CONSUMED" || uuid.Validate(pvcUID) != nil ||
		reference != "journal://"+execution.ID+"/rehydrate-proof" || len(proofSHA256) != sha256.Size*2 {
		return errs.ErrInvalidInput
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("load rehydrate Kubernetes configuration")
	}
	config.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.New("create rehydrate Kubernetes client")
	}
	configMap, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return errors.New("read rehydrate runtime journal")
	}
	var document journalDocument
	if decodeJournal([]byte(configMap.Data[journalDataKey]), &document) != nil ||
		document.Execution.ID != execution.ID || document.RehydratePhase != "PENDING" ||
		document.Execution.RestoreSourceExecutionID == "" {
		return errs.ErrStateConflict
	}
	document.RehydratePhase = "COMPLETE"
	document.RehydratePVCUID = pvcUID
	document.RehydrateProofReference = reference
	document.RehydrateProofSHA256 = proofSHA256
	document.Execution = execution
	refreshCommandKeys(&document)
	document.LastTransition = time.Now().UTC()
	raw, err := marshalJournal(document)
	if err != nil {
		return err
	}
	configMap.Data[journalDataKey] = string(raw)
	if _, err := client.CoreV1().ConfigMaps(namespace).Update(ctx, configMap, metav1.UpdateOptions{}); err != nil {
		if apierrors.IsConflict(err) {
			return errs.ErrStateConflict
		}
		return errors.New("update rehydrate runtime journal")
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
	if _, err := adapter.handoffTrust(ctx); err != nil {
		return err
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
		Execution: execution, AdmissionRequest: execution, Phase: "CLAIMED",
		AdmitKey:     commandKey(execution, "admit"),
		HeartbeatKey: commandKey(execution, "heartbeat"),
		CompleteKey:  commandKey(execution, "complete"),
		IncidentKey:  commandKey(execution, "incident"),
		ArchiveKey:   commandKey(execution, "archive"),
		RestoreKey:   commandKey(execution, "restore"),
		CleanupKey:   commandKey(execution, "cleanup"),
		PodName:      podName(execution),
		PVCName:      pvcName(execution), CreatedAt: now, LastTransition: now,
		RehydratePhase: "NOT_REQUIRED",
	}
	if execution.RestoreSourceExecutionID != "" {
		document.RehydratePhase = "PENDING"
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
	return runtimerepo.Journal{
		Execution: document.Execution, AdmitIdempotencyKey: document.AdmitKey,
		HeartbeatIdempotencyKey: document.HeartbeatKey,
		CompleteIdempotencyKey:  document.CompleteKey,
		IncidentIdempotencyKey:  document.IncidentKey,
		ArchiveIdempotencyKey:   document.ArchiveKey,
		RestoreIdempotencyKey:   document.RestoreKey,
		CleanupIdempotencyKey:   document.CleanupKey,
		AdmissionRequest:        document.AdmissionRequest, Phase: document.Phase,
	}, nil
}

func (adapter *Adapter) Capacity(
	ctx context.Context,
	execution entity.Execution,
	revision entity.Revision,
) (entity.CapacityDecision, error) {
	if revision.ValidateFor(execution) != nil ||
		!revision.ProviderObservedAt.Add(revision.ProviderObservationMaxAge).After(adapter.now().UTC()) {
		return entity.CapacityDecision{Reason: "quota_stale"}, nil
	}
	if err := adapter.persistCapacitySnapshot(ctx, execution, revision); err != nil {
		return entity.CapacityDecision{}, err
	}
	pods, err := adapter.client.CoreV1().Pods(adapter.config.Namespace).List(
		ctx, metav1.ListOptions{LabelSelector: fields.OneTermEqualSelector(managedLabel, "true").String()},
	)
	if err != nil {
		return entity.CapacityDecision{}, errors.New("list runtime pods for capacity")
	}
	requestedCPU, requestedMemory, accelerator := resourcesFor(execution.ResourceClass)
	var usedCPU, usedMemory int64
	var active int
	organizationActive, providerActive := 0, uint64(0)
	unknownOrganizationBinding := false
	var idle []entity.RuntimeStatus
	stableName := podName(execution)
	reusable := false
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
			if pod.Annotations["runtime.mattercodex.dev/effective-runtime-sha256"] == execution.EffectiveRuntimeSHA256 &&
				pod.Annotations["runtime.mattercodex.dev/archive-gate"] == "OPEN" && status.Ready && status.Phase == "Running" {
				reusable = true
				for _, container := range pod.Spec.Containers {
					usedCPU -= container.Resources.Requests.Cpu().MilliValue()
					usedMemory -= container.Resources.Requests.Memory().Value()
				}
				active--
			} else {
				return entity.CapacityDecision{Reason: "session_replacement", Eviction: &status}, nil
			}
		}
		if terminalLabel(pod.Labels["runtime.mattercodex.dev/state"]) &&
			status.AccessProfile != enum.AccessClusterAdmin {
			idle = append(idle, status)
		}
	}
	journals, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: managedLabel + "=true," + componentLabel + "=" + journalComponent,
		Limit:         10000,
	})
	if err != nil || journals.Continue != "" {
		return entity.CapacityDecision{}, errors.New("list durable runtime capacity claims")
	}
	for index := range journals.Items {
		var document journalDocument
		if decodeJournal([]byte(journals.Items[index].Data[journalDataKey]), &document) != nil ||
			document.Execution.Validate() != nil {
			return entity.CapacityDecision{}, errors.New("runtime capacity journal is invalid")
		}
		if document.Execution.State != enum.ExecutionPending &&
			document.Execution.State != enum.ExecutionAdmitted &&
			document.Execution.State != enum.ExecutionRunning {
			continue
		}
		if document.Execution.OrganizationID == execution.OrganizationID {
			organizationActive++
			if document.CapacityProviderBindingID == "" || document.CapacityObservationRevision == 0 {
				unknownOrganizationBinding = true
			}
		}
		if document.CapacityProviderBindingID == revision.ProviderCredentialBindingID {
			providerActive++
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
		organizationActive <= adapter.config.MaximumOrganizationExecutions && !unknownOrganizationBinding &&
		revision.ProviderObservedUsage+providerActive <= revision.ProviderObservedLimit &&
		usedCPU+requestedCPU <= adapter.config.MaximumCPU &&
		usedMemory+requestedMemory <= adapter.config.MaximumMemoryBytes &&
		!quotaBlocked && !nodePressure
	if admitted {
		reason := "admitted"
		if reusable {
			reason = "warm_reuse"
		}
		return entity.CapacityDecision{Admitted: true, Reason: reason}, nil
	}
	sort.Slice(idle, func(left, right int) bool {
		return idle[left].LastTransition.Before(idle[right].LastTransition)
	})
	decision := entity.CapacityDecision{Reason: "bounded_capacity"}
	if unknownOrganizationBinding {
		decision.Reason = "quota_stale"
	} else if organizationActive > adapter.config.MaximumOrganizationExecutions {
		decision.Reason = "organization_quota"
	} else if revision.ProviderObservedUsage+providerActive > revision.ProviderObservedLimit {
		decision.Reason = "provider_quota"
	}
	if len(idle) > 0 {
		decision.Eviction = &idle[0]
	}
	return decision, nil
}

func (adapter *Adapter) persistCapacitySnapshot(ctx context.Context, execution entity.Execution, revision entity.Revision) error {
	configMaps := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace)
	current, err := configMaps.Get(ctx, journalName(execution.ID), metav1.GetOptions{})
	if err != nil {
		return errors.New("read runtime journal for capacity snapshot")
	}
	var document journalDocument
	if decodeJournal([]byte(current.Data[journalDataKey]), &document) != nil ||
		document.Execution.ID != execution.ID || document.Execution.State != enum.ExecutionPending {
		return errs.ErrStateConflict
	}
	if document.CapacityProviderBindingID != "" &&
		(document.CapacityProviderBindingID != revision.ProviderCredentialBindingID ||
			document.CapacityObservationRevision != revision.ProviderObservationRevision ||
			!document.CapacityObservedAt.Equal(revision.ProviderObservedAt) ||
			document.CapacityObservedUsage != revision.ProviderObservedUsage ||
			document.CapacityObservedLimit != revision.ProviderObservedLimit ||
			document.CapacityObservationMaxAge != int64(revision.ProviderObservationMaxAge) ||
			document.CapacityOrganizationLimit != adapter.config.MaximumOrganizationExecutions) {
		return errs.ErrStateConflict
	}
	document.CapacityProviderBindingID = revision.ProviderCredentialBindingID
	document.CapacityObservationRevision = revision.ProviderObservationRevision
	document.CapacityObservedAt = revision.ProviderObservedAt
	document.CapacityObservedUsage = revision.ProviderObservedUsage
	document.CapacityObservedLimit = revision.ProviderObservedLimit
	document.CapacityObservationMaxAge = int64(revision.ProviderObservationMaxAge)
	document.CapacityOrganizationLimit = adapter.config.MaximumOrganizationExecutions
	raw, err := marshalJournal(document)
	if err != nil {
		return err
	}
	current.Data[journalDataKey] = string(raw)
	if _, err := configMaps.Update(ctx, current, metav1.UpdateOptions{}); err != nil {
		if apierrors.IsConflict(err) {
			return errs.ErrStateConflict
		}
		return errors.New("persist immutable runtime capacity snapshot")
	}
	return nil
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
	if !adapter.config.ArchiveRestoreEnabled && execution.RestoreSourceExecutionID != "" {
		return entity.RuntimeStatus{}, errs.ErrStateConflict
	}
	if err := revision.ValidateFor(execution); err != nil ||
		(execution.State == enum.ExecutionPending && leaseToken != "") ||
		(execution.State != enum.ExecutionPending && leaseToken == "") {
		return entity.RuntimeStatus{}, errs.ErrStateConflict
	}
	if err := adapter.ensurePVC(ctx, execution); err != nil {
		return entity.RuntimeStatus{}, err
	}
	if execution.RestoreSourceExecutionID != "" {
		ready, err := adapter.ensureRehydrated(ctx, execution)
		if err != nil {
			return entity.RuntimeStatus{}, err
		}
		if !ready {
			return entity.RuntimeStatus{}, errs.ErrDependency
		}
	}
	if err := adapter.ensureExecutionConfig(ctx, execution, revision); err != nil {
		return entity.RuntimeStatus{}, err
	}
	if err := adapter.updateJournalAndLease(ctx, execution, leaseToken); err != nil {
		return entity.RuntimeStatus{}, err
	}
	credentialsReady, err := adapter.ensureCredentialBroker(ctx, execution)
	if err != nil {
		return entity.RuntimeStatus{}, err
	}
	if !credentialsReady {
		return entity.RuntimeStatus{}, errs.ErrDependency
	}
	if err := adapter.ensureAccessProfile(ctx, execution); err != nil {
		return entity.RuntimeStatus{}, err
	}
	desired, err := adapter.rolePod(ctx, execution, revision)
	if err != nil {
		return entity.RuntimeStatus{}, err
	}
	existing, err := adapter.client.CoreV1().Pods(adapter.config.Namespace).Get(
		ctx, desired.Name, metav1.GetOptions{},
	)
	if err == nil {
		if existing.Labels[executionLabel] != shortID(execution.ID) ||
			existing.Annotations["runtime.mattercodex.dev/effective-runtime-sha256"] != execution.EffectiveRuntimeSHA256 ||
			existing.Annotations["runtime.mattercodex.dev/input-sha256"] != execution.ImmutableInputSHA256 ||
			!podMatches(existing, desired) {
			return entity.RuntimeStatus{}, errs.ErrStateConflict
		}
		if err := adapter.patchJournalPhase(ctx, execution.ID, "MATERIALIZED"); err != nil {
			return entity.RuntimeStatus{}, err
		}
		return castStatus(existing, nil), nil
	}
	if !apierrors.IsNotFound(err) {
		return entity.RuntimeStatus{}, errors.New("read role runtime pod")
	}
	return entity.RuntimeStatus{}, errs.ErrDependency
}

func (adapter *Adapter) patchJournalPhase(ctx context.Context, executionID, phase string) error {
	configMap, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(
		ctx, journalName(executionID), metav1.GetOptions{},
	)
	if err != nil {
		return errors.New("read runtime journal phase")
	}
	var document journalDocument
	if decodeJournal([]byte(configMap.Data[journalDataKey]), &document) != nil || document.Execution.ID != executionID {
		return errs.ErrStateConflict
	}
	document.Phase = phase
	document.LastTransition = adapter.now().UTC()
	raw, err := marshalJournal(document)
	if err != nil {
		return err
	}
	configMap.Data[journalDataKey] = string(raw)
	if _, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Update(
		ctx, configMap, metav1.UpdateOptions{},
	); err != nil {
		return errors.New("update runtime journal phase")
	}
	return nil
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
		return adapter.ensureHandoffConfig(ctx, execution)
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

func (adapter *Adapter) ensureRehydrated(
	ctx context.Context,
	execution entity.Execution,
) (bool, error) {
	pvc, err := adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).Get(
		ctx, pvcName(execution), metav1.GetOptions{},
	)
	if err != nil {
		return false, errors.New("read rehydrate target PVC")
	}
	if pvc.Annotations["runtime.mattercodex.dev/session-id"] != execution.SessionID ||
		pvc.Annotations["runtime.mattercodex.dev/retention-owner-execution-id"] != execution.ID {
		return false, errs.ErrStateConflict
	}
	configMap, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(
		ctx, journalName(execution.ID), metav1.GetOptions{},
	)
	if err != nil {
		return false, errors.New("read rehydrate journal")
	}
	var document journalDocument
	if decodeJournal([]byte(configMap.Data[journalDataKey]), &document) != nil ||
		document.Execution.ID != execution.ID || document.Execution.RestoreSourceExecutionID == "" {
		return false, errs.ErrStateConflict
	}
	switch document.RehydratePhase {
	case "COMPLETE":
		if document.RehydratePVCUID != string(pvc.UID) ||
			document.RehydrateProofReference != "journal://"+execution.ID+"/rehydrate-proof" ||
			len(document.RehydrateProofSHA256) != sha256.Size*2 {
			return false, errs.ErrStateConflict
		}
		return true, nil
	case "PENDING":
		status := castStatus(nil, pvc)
		status.ExecutionID = execution.ID
		status.RetentionOwner = true
		if err := adapter.ensureWorkerJob(ctx, execution, status, rehydrateComponent); err != nil {
			return false, err
		}
		return false, nil
	default:
		return false, errs.ErrStateConflict
	}
}

func (adapter *Adapter) ensureExecutionConfig(
	ctx context.Context,
	execution entity.Execution,
	revision entity.Revision,
) error {
	runtimeInput, err := adapter.runnerInput(execution, revision)
	if err != nil {
		return err
	}
	desiredPod, err := adapter.rolePod(ctx, execution, revision)
	if err != nil {
		return err
	}
	snapshot := struct {
		Execution             entity.Execution `json:"execution"`
		Revision              entity.Revision  `json:"runtime_revision"`
		RunnerInput           runnerInput      `json:"runner_input"`
		WorkloadTicket        string           `json:"workload_ticket"`
		ArchiveWorkloadTicket string           `json:"archive_workload_ticket"`
		RestoreWorkloadTicket string           `json:"restore_workload_ticket"`
		DesiredPod            *corev1.Pod      `json:"desired_pod"`
	}{Execution: execution, Revision: revision, RunnerInput: runtimeInput,
		WorkloadTicket: execution.WorkloadTicket, ArchiveWorkloadTicket: execution.ArchiveWorkloadTicket,
		RestoreWorkloadTicket: execution.RestoreWorkloadTicket, DesiredPod: desiredPod}
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) > maximumJournalSize {
		return errs.ErrStateConflict
	}
	runnerRaw, err := json.Marshal(runtimeInput)
	if err != nil || len(runnerRaw) > 2<<20 {
		return errs.ErrStateConflict
	}
	desired := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: configName(execution), Labels: labels(execution, "immutable-input"),
		Annotations: map[string]string{"runtime.mattercodex.dev/immutable": "true"},
	}, Immutable: boolPointer(true), BinaryData: map[string][]byte{"runtime.json": raw, "runner.json": runnerRaw}}
	existing, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(
		ctx, desired.Name, metav1.GetOptions{},
	)
	if err == nil {
		if existing.Immutable == nil || !*existing.Immutable ||
			existing.Labels[executionLabel] != shortID(execution.ID) ||
			existing.Labels[componentLabel] != "immutable-input" ||
			existing.Annotations["runtime.mattercodex.dev/immutable"] != "true" ||
			!bytesEqual(existing.BinaryData["runtime.json"], raw) || !bytesEqual(existing.BinaryData["runner.json"], runnerRaw) {
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
	return adapter.ensureHandoffConfig(ctx, execution)
}

func (adapter *Adapter) ensureHandoffConfig(ctx context.Context, execution entity.Execution) error {
	desired := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: handoffName(execution),
		Labels: labels(execution, "runtime-handoff"), Annotations: map[string]string{
			"runtime.mattercodex.dev/execution-id": execution.ID, "runtime.mattercodex.dev/turn-id": execution.TurnID,
			"runtime.mattercodex.dev/grant-generation": strconv.FormatUint(execution.GrantGeneration, 10)}}}
	current, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return errors.New("create runtime handoff target")
		}
		return nil
	}
	if err != nil || current.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID ||
		current.Annotations["runtime.mattercodex.dev/turn-id"] != execution.TurnID ||
		current.Annotations["runtime.mattercodex.dev/grant-generation"] != strconv.FormatUint(execution.GrantGeneration, 10) {
		return errs.ErrStateConflict
	}
	return nil
}

type runnerCredentialFiles struct {
	ControlPlaneGrant    string `json:"control_plane_grant"`
	MCPToken             string `json:"mcp_token"`
	MaterializationToken string `json:"materialization_token"`
	CodexAuth            string `json:"codex_auth"`
	CodexAuthSHA256      string `json:"codex_auth_sha256"`
	HandoffPrivateKey    string `json:"handoff_private_key"`
	HandoffKeyID         string `json:"handoff_key_id"`
}

type runnerGRPCBinding struct {
	Target string           `json:"target"`
	TLS    runnerTLSBinding `json:"tls"`
}

type runnerHTTPBinding struct {
	URL string           `json:"url"`
	TLS runnerTLSBinding `json:"tls"`
}

type runnerMaterialization struct {
	Kind            string `json:"kind"`
	ArtifactID      string `json:"artifact_id"`
	ArtifactVersion uint64 `json:"artifact_version"`
	SHA256          string `json:"sha256"`
	SizeBytes       uint64 `json:"size_bytes"`
	RelativePath    string `json:"relative_path"`
	MediaType       string `json:"media_type"`
}

type runnerTLSBinding struct {
	ServerName      string `json:"server_name"`
	CAFile          string `json:"ca_file"`
	CertificateFile string `json:"certificate_file"`
	PrivateKeyFile  string `json:"private_key_file"`
	BindingSHA256   string `json:"binding_sha256"`
}

type runnerInput struct {
	Schema                                 string                          `json:"schema"`
	ExecutionID                            string                          `json:"execution_id"`
	ExecutionVersion                       uint64                          `json:"execution_version"`
	Fence                                  uint64                          `json:"fence"`
	GrantGeneration                        uint64                          `json:"grant_generation"`
	RuntimeRevisionID                      string                          `json:"runtime_revision_id"`
	RuntimeRevisionVersion                 uint64                          `json:"runtime_revision_version"`
	RuntimeRevisionSHA256                  string                          `json:"runtime_revision_sha256"`
	EffectiveRuntimeSHA256                 string                          `json:"effective_runtime_sha256"`
	ImmutableInputSHA256                   string                          `json:"immutable_input_sha256"`
	SessionID                              string                          `json:"session_id"`
	TurnID                                 string                          `json:"turn_id"`
	ScheduleOccurrenceID                   string                          `json:"schedule_occurrence_id,omitempty"`
	ScheduledResultContract                *entity.ScheduledResultContract `json:"scheduled_result_contract,omitempty"`
	Attempt                                uint32                          `json:"attempt"`
	SessionKey                             string                          `json:"session_key"`
	ProviderBindingID                      string                          `json:"provider_binding_id"`
	ProviderAccountName                    string                          `json:"provider_account_name"`
	MCPBindingVersion                      uint64                          `json:"mcp_binding_version"`
	ProviderBindingVersion                 uint64                          `json:"provider_binding_version"`
	ProviderBindingSHA256                  string                          `json:"provider_binding_sha256"`
	CredentialSnapshotSHA256               string                          `json:"credential_snapshot_sha256"`
	WorkloadTicketSHA256                   string                          `json:"workload_ticket_sha256"`
	AgentProfile                           string                          `json:"agent_profile"`
	CodexModel                             string                          `json:"codex_model"`
	CodexSandbox                           string                          `json:"codex_sandbox"`
	CodexApprovalPolicy                    string                          `json:"codex_approval_policy"`
	CodexSessionID                         string                          `json:"codex_session_id"`
	CodexArchiveRelativePath               string                          `json:"codex_archive_relative_path"`
	CodexArchiveSHA256                     string                          `json:"codex_archive_sha256"`
	CodexArchiveProvenance                 string                          `json:"codex_archive_provenance"`
	CodexDeliveryRecoverySourceExecutionID string                          `json:"codex_delivery_recovery_source_execution_id"`
	ControlPlane                           runnerGRPCBinding               `json:"control_plane"`
	MCP                                    runnerHTTPBinding               `json:"mcp"`
	InteractionGateway                     runnerHTTPBinding               `json:"interaction_gateway"`
	CredentialFiles                        runnerCredentialFiles           `json:"credential_files"`
	Materializations                       []runnerMaterialization         `json:"materializations"`
	PromptPath                             string                          `json:"prompt_path"`
	InstructionsPath                       string                          `json:"instructions_path"`
	WorkspaceRoot                          string                          `json:"workspace_root"`
	OutboxRoot                             string                          `json:"outbox_root"`
	CodexHome                              string                          `json:"codex_home"`
	MattermostPostMaximumRunes             int                             `json:"mattermost_post_max_runes"`
	HandoffConfigMap                       string                          `json:"handoff_config_map"`
	PodNamespace                           string                          `json:"pod_namespace"`
}

func (adapter *Adapter) runnerInput(execution entity.Execution, revision entity.Revision) (runnerInput, error) {
	files := runnerCredentialFiles{}
	var mcpBindingVersion uint64
	controlTLS := runnerTLSBinding{ServerName: adapter.config.RunnerControlPlaneTLSServerName}
	interactionTLS := runnerTLSBinding{ServerName: mustURLHostname(adapter.config.InteractionGatewayURL)}
	mcpTLS := runnerTLSBinding{ServerName: mustURLHostname(adapter.config.SessionMCPURL)}
	for index, credential := range revision.Credentials {
		base := "/var/run/secrets/mattercodex/runtime/credential-" + strconv.Itoa(index)
		if credential.ResourceID == revision.ProviderCredentialBindingID {
			files.CodexAuth = base + "/auth.json"
			files.CodexAuthSHA256 = credential.ContentSHA256
		}
		switch credential.Purpose {
		case "control-plane-application-grant":
			files.ControlPlaneGrant = base + "/application-grant.jws"
		case "runtime-materialization-application-grant":
			files.MaterializationToken = base + "/application-grant.jws"
		case "mcp-token":
			files.MCPToken = base + "/token"
			mcpBindingVersion = credential.Version
		case "handoff-private-key":
			files.HandoffPrivateKey = base + "/ed25519.key"
			files.HandoffKeyID = "sha256-" + credential.ContentSHA256[:16]
		case "control-plane-client-tls":
			controlTLS.CAFile, controlTLS.CertificateFile, controlTLS.PrivateKeyFile = base+"/ca.pem", base+"/tls.crt", base+"/tls.key"
			controlTLS.BindingSHA256 = credential.ContentSHA256
		case "interaction-gateway-client-tls":
			interactionTLS.CAFile, interactionTLS.CertificateFile, interactionTLS.PrivateKeyFile = base+"/ca.pem", base+"/tls.crt", base+"/tls.key"
			interactionTLS.BindingSHA256 = credential.ContentSHA256
		case "mcp-client-tls":
			mcpTLS.CAFile, mcpTLS.CertificateFile, mcpTLS.PrivateKeyFile = base+"/ca.pem", base+"/tls.crt", base+"/tls.key"
			mcpTLS.BindingSHA256 = credential.ContentSHA256
		}
	}
	if files.ControlPlaneGrant == "" || files.MaterializationToken == "" || files.MCPToken == "" || mcpBindingVersion == 0 ||
		files.CodexAuth == "" || files.HandoffPrivateKey == "" || controlTLS.BindingSHA256 == "" ||
		interactionTLS.BindingSHA256 == "" || mcpTLS.BindingSHA256 == "" || revision.AgentProfile == "" {
		return runnerInput{}, errs.ErrStateConflict
	}
	materializations := make([]runnerMaterialization, 0, len(execution.Materializations))
	for _, item := range execution.Materializations {
		materializations = append(materializations, runnerMaterialization{Kind: item.Kind, ArtifactID: item.ArtifactID,
			ArtifactVersion: item.ArtifactVersion, SHA256: item.SHA256, SizeBytes: item.SizeBytes,
			RelativePath: item.RelativePath, MediaType: item.MediaType})
	}
	var scheduledResult *entity.ScheduledResultContract
	if revision.ScheduledResultContract != nil {
		copy := *revision.ScheduledResultContract
		scheduledResult = &copy
	}
	return runnerInput{
		Schema: "mattercodex.agent-runner-input.v2", ExecutionID: execution.ID,
		ExecutionVersion: execution.Version, Fence: execution.Fence, GrantGeneration: execution.GrantGeneration,
		RuntimeRevisionID: execution.RuntimeRevisionID, RuntimeRevisionVersion: execution.RuntimeRevisionVersion,
		RuntimeRevisionSHA256:  execution.RuntimeRevisionSHA256,
		EffectiveRuntimeSHA256: execution.EffectiveRuntimeSHA256,
		ImmutableInputSHA256:   execution.ImmutableInputSHA256,
		SessionID:              execution.SessionID, TurnID: execution.TurnID, Attempt: execution.Attempt,
		ScheduleOccurrenceID:    execution.ScheduleOccurrenceID,
		ScheduledResultContract: scheduledResult,
		SessionKey:              execution.AgentSessionKey, ProviderBindingID: execution.ProviderBindingID,
		ProviderAccountName:    revision.ProviderAccountName,
		MCPBindingVersion:      mcpBindingVersion,
		ProviderBindingVersion: execution.ProviderBindingVersion, ProviderBindingSHA256: execution.ProviderBindingSHA256,
		CredentialSnapshotSHA256: execution.CredentialSnapshotSHA256,
		WorkloadTicketSHA256:     execution.WorkloadTicketSHA256,
		AgentProfile:             revision.AgentProfile, CodexModel: revision.CodexModel, CodexSandbox: revision.CodexSandbox,
		CodexApprovalPolicy: revision.CodexApprovalPolicy, CodexSessionID: execution.CodexSessionID,
		CodexArchiveRelativePath: execution.CodexArchiveRelativePath,
		CodexArchiveSHA256:       execution.CodexArchiveSHA256, CodexArchiveProvenance: execution.CodexArchiveProvenance,
		CodexDeliveryRecoverySourceExecutionID: execution.CodexDeliveryRecoverySourceExecutionID,
		ControlPlane:                           runnerGRPCBinding{Target: adapter.config.RunnerControlPlaneTarget, TLS: controlTLS},
		MCP:                                    runnerHTTPBinding{URL: strings.TrimRight(adapter.config.SessionMCPURL, "/") + "/mcp/sessions/" + url.PathEscape(execution.AgentSessionKey), TLS: mcpTLS},
		InteractionGateway:                     runnerHTTPBinding{URL: adapter.config.InteractionGatewayURL, TLS: interactionTLS},
		CredentialFiles:                        files, Materializations: materializations,
		PromptPath: ".matter-codex/inbox/prompt.md", InstructionsPath: "AGENTS.md", WorkspaceRoot: "/workspace",
		OutboxRoot: "/workspace/.matter-codex/outbox", CodexHome: "/workspace/.matter-codex/state/codex-home",
		MattermostPostMaximumRunes: 16383, HandoffConfigMap: handoffName(execution), PodNamespace: adapter.config.Namespace,
	}, nil
}

func mustURLHostname(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
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
	if document.AdmissionRequest.Validate() != nil || document.AdmissionRequest.State != enum.ExecutionPending ||
		!sameAdmissionLineage(document.AdmissionRequest, execution) {
		return errs.ErrStateConflict
	}
	refreshCommandKeys(&document)
	document.LastTransition = adapter.now().UTC()
	if leaseToken != "" {
		document.Phase = "ADMITTED_RECOVERABLE"
	} else if execution.State.Terminal() {
		document.Phase = "AUTHORITY_REVOKED"
	}
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
	} else if podErr != nil && !apierrors.IsNotFound(podErr) {
		return errors.New("read runtime pod lifecycle label")
	}
	return nil
}

func sameAdmissionLineage(request, current entity.Execution) bool {
	return request.ID == current.ID && request.OrganizationID == current.OrganizationID &&
		request.ProjectID == current.ProjectID && request.SessionID == current.SessionID &&
		request.TurnID == current.TurnID && request.Attempt == current.Attempt &&
		request.RuntimeRevisionID == current.RuntimeRevisionID &&
		request.RuntimeRevisionVersion == current.RuntimeRevisionVersion &&
		request.RuntimeRevisionSHA256 == current.RuntimeRevisionSHA256 &&
		request.ImmutableInputSHA256 == current.ImmutableInputSHA256 &&
		request.RestoreOperationID == current.RestoreOperationID &&
		request.RestoreOperationGeneration == current.RestoreOperationGeneration &&
		request.RestoreSourceAuthoritySHA256 == current.RestoreSourceAuthoritySHA256 &&
		request.RestoreSourceExecutionID == current.RestoreSourceExecutionID &&
		request.RestoreSourceVersion == current.RestoreSourceVersion &&
		request.RestoreSourceFence == current.RestoreSourceFence &&
		request.GrantGeneration == current.GrantGeneration && request.Version <= current.Version &&
		request.Fence <= current.Fence
}

func (adapter *Adapter) UpdateJournal(
	ctx context.Context,
	execution entity.Execution,
	leaseToken string,
) error {
	return adapter.updateJournalAndLease(ctx, execution, leaseToken)
}

func (adapter *Adapter) rolePod(
	ctx context.Context,
	execution entity.Execution,
	revision entity.Revision,
) (*corev1.Pod, error) {
	if !validWorkloadTicket(execution.WorkloadTicket) {
		return nil, errs.ErrStateConflict
	}
	if revision.ImageReference != adapter.config.PromotedRoleImageRepository+"@"+revision.ImageManifestDigest {
		return nil, errors.New("runtime role image reference is outside the promoted repository")
	}
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
		{Name: "authority-sockets", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("8Mi"))}}},
		{Name: "provider-socket", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("1Mi"))}}},
		{Name: "provider-tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("256Mi"))}}},
		{Name: "authority-snapshot", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "internal-rpc-authority-snapshot", DefaultMode: int32Pointer(0o440)}}},
		namedCSIVolume("authority-manifest-trust", "internal-rpc-authority-agent-runner-manifest-trust"),
		namedCSIVolume("authority-proof-trust", "internal-rpc-authority-agent-runner-proof-trust"),
		namedCSIVolume("authority-issuer-key", "internal-rpc-authority-agent-runner-issuer-key"),
		{Name: "authority-workload-tls", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "internal-rpc-authority-agent-runner-workload-tls", DefaultMode: int32Pointer(0o440)}}},
		namedConfigMapVolume("authority-readback-ca", "internal-rpc-authority-readback-attestor-ca"),
		namedConfigMapVolume("authority-vault-ca", "internal-rpc-authority-vault-ca"),
		{Name: "authority-vault-token", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
			DefaultMode: int32Pointer(0o400), Sources: []corev1.VolumeProjection{{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
				Path: "token", Audience: "vault", ExpirationSeconds: int64Pointer(600),
			}}},
		}}},
		namedConfigMapVolume("authority-restore-ca", "internal-rpc-authority-restore-controller-ca"),
		{Name: "authority-restore-certificate", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "internal-rpc-authority-restore-controller-tls", DefaultMode: int32Pointer(0o440)}}},
		{Name: "authority-restore-role-trust", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "internal-rpc-authority-restore-role-trust", DefaultMode: int32Pointer(0o440)}}},
		{Name: "authority-postgres", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "internal-rpc-authority-agent-runner-issuer-postgresql", DefaultMode: int32Pointer(0o440)}}},
		namedConfigMapVolume("authority-postgres-ca", "internal-rpc-authority-postgresql-ca"),
		{Name: "authority-observability-ca", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: "internal-rpc-authority-otel-ca"}, DefaultMode: int32Pointer(0o440),
			Items: []corev1.KeyToPath{{Key: "ca.pem", Path: "otel-ca.pem"}}}}},
		{Name: "authority-sentry-dsn", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName: "internal-rpc-authority-sentry", DefaultMode: int32Pointer(0o440),
			Items: []corev1.KeyToPath{{Key: "dsn", Path: "sentry-dsn"}}}}},
	}
	mounts := []corev1.VolumeMount{
		{Name: "session", MountPath: "/workspace"},
		{Name: "runtime-config", MountPath: "/var/run/config/mattercodex/runtime/runtime.json", SubPath: "runner.json", ReadOnly: true},
		{Name: "tmp", MountPath: "/tmp"},
		{Name: "authority-sockets", MountPath: "/run/mattercodex/internal-rpc-authority", ReadOnly: true},
		{Name: "provider-socket", MountPath: "/run/mattercodex/provider"},
		{Name: "authority-observability-ca", MountPath: "/var/run/config/mattercodex/agent-runner/observability", ReadOnly: true},
		{Name: "authority-sentry-dsn", MountPath: "/var/run/secrets/mattercodex/agent-runner/observability", ReadOnly: true},
	}
	for index, credential := range revision.Credentials {
		name := "credential-" + strconv.Itoa(index)
		purpose := credential.Purpose
		if credential.ResourceID == revision.ProviderCredentialBindingID {
			purpose = "provider-account"
		}
		volume, credentialMounts := executionCredentialVolumeForPurpose(execution, name, index, purpose)
		volumes = append(volumes, volume)
		mounts = append(mounts, credentialMounts...)
	}
	serviceAccount := accessServiceAccountName(execution)
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
			"runtime.mattercodex.dev/execution-id":               execution.ID,
			"runtime.mattercodex.dev/session-id":                 execution.SessionID,
			"runtime.mattercodex.dev/version":                    strconv.FormatUint(execution.Version, 10),
			"runtime.mattercodex.dev/fence":                      strconv.FormatUint(execution.Fence, 10),
			"runtime.mattercodex.dev/grant-generation":           strconv.FormatUint(execution.GrantGeneration, 10),
			"runtime.mattercodex.dev/revision-sha256":            execution.RuntimeRevisionSHA256,
			"runtime.mattercodex.dev/effective-runtime-sha256":   execution.EffectiveRuntimeSHA256,
			"runtime.mattercodex.dev/workload-ticket-sha256":     execution.WorkloadTicketSHA256,
			"runtime.mattercodex.dev/workload-ticket":            execution.WorkloadTicket,
			"runtime.mattercodex.dev/credential-snapshot-sha256": execution.CredentialSnapshotSHA256,
			"runtime.mattercodex.dev/input-sha256":               execution.ImmutableInputSHA256,
			"runtime.mattercodex.dev/manifest-sha256":            revision.ManifestSHA256,
			"runtime.mattercodex.dev/project-namespace":          projectNamespaceName(execution.ProjectID),
			"runtime.mattercodex.dev/archive-gate":               "CLOSED",
			"runtime.mattercodex.dev/next-input-config":          configName(execution),
		},
	}, Spec: corev1.PodSpec{
		ServiceAccountName: serviceAccount, AutomountServiceAccountToken: boolPointer(false),
		EnableServiceLinks: boolPointer(false), RestartPolicy: corev1.RestartPolicyNever,
		TerminationGracePeriodSeconds: int64Pointer(30),
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true), FSGroup: int64Pointer(29000),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
		InitContainers: []corev1.Container{{Name: "internal-rpc-authority-socket-init", Image: adapter.config.AuthorityImage,
			Command: []string{"/usr/local/bin/internal-rpc-authority-socket-init"}, SecurityContext: restrictedSecurityContext(29000),
			VolumeMounts: []corev1.VolumeMount{{Name: "authority-sockets", MountPath: "/run/mattercodex"}},
			Resources:    smallResources(),
		}, {
			Name: "workspace-init", Image: revision.ImageReference,
			ImagePullPolicy: corev1.PullIfNotPresent, Args: []string{"runtime-init-workspace"},
			Env:             runnerObservabilityEnv(adapter.config.Environment),
			SecurityContext: restrictedSecurityContext(10001),
			VolumeMounts:    mounts,
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("25m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
				Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("128Mi")}},
		}},
		Containers: []corev1.Container{{
			Name: "role-runtime", Image: revision.ImageReference,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Args:            []string{"runtime-session"},
			Env: append([]corev1.EnvVar{
				{Name: "MATTERCODEX_RUNTIME_REVISION_FILE", Value: "/var/run/config/mattercodex/runtime/runtime.json"},
				{Name: "MATTERCODEX_EXECUTION_ID", Value: execution.ID},
				{Name: "MATTERCODEX_RUNTIME_POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
				{Name: "MATTERCODEX_RUNTIME_POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}},
			}, runnerObservabilityEnv(adapter.config.Environment)...),
			Ports:           []corev1.ContainerPort{{Name: "metrics", ContainerPort: 9090}},
			Resources:       corev1.ResourceRequirements{Requests: requests, Limits: limits},
			SecurityContext: restrictedSecurityContext(10001), VolumeMounts: mounts,
			StartupProbe:   &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt32(9090)}}, PeriodSeconds: 2, TimeoutSeconds: 1, FailureThreshold: 30},
			ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt32(9090)}}, PeriodSeconds: 5, TimeoutSeconds: 2, FailureThreshold: 3},
			LivenessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/livez", Port: intstr.FromInt32(9090)}}, PeriodSeconds: 10, TimeoutSeconds: 2, FailureThreshold: 3},
		}, {
			Name: "provider-runtime", Image: revision.ImageReference,
			ImagePullPolicy: corev1.PullIfNotPresent, Args: []string{"runtime-provider"},
			Env:       []corev1.EnvVar{{Name: "HOME", Value: "/tmp"}, {Name: "CODEX_HOME", Value: "/workspace/.matter-codex/state/codex-home"}},
			Resources: smallResources(), SecurityContext: restrictedSecurityContext(10002),
			VolumeMounts: []corev1.VolumeMount{{Name: "session", MountPath: "/workspace"},
				{Name: "provider-socket", MountPath: "/run/mattercodex/provider"},
				{Name: "provider-tmp", MountPath: "/tmp"}},
			ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/usr/bin/test", "-S", "/run/mattercodex/provider/provider.sock"}}}, PeriodSeconds: 2, TimeoutSeconds: 1, FailureThreshold: 30},
		}, authorityIssuerContainer(adapter.config.AuthorityImage)}, Volumes: volumes,
	}}, nil
}

func namedCSIVolume(name, providerClass string) corev1.Volume {
	return corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{CSI: &corev1.CSIVolumeSource{Driver: "secrets-store.csi.k8s.io", ReadOnly: boolPointer(true), VolumeAttributes: map[string]string{"secretProviderClass": providerClass}}}}
}
func runnerObservabilityEnv(environment string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "DEPLOYMENT_ENVIRONMENT", Value: environment},
		{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "otel-collector.observability.svc:4317"},
		{Name: "OTEL_EXPORTER_OTLP_TLS_SERVER_NAME", Value: "otel-collector.observability.svc.cluster.local"},
		{Name: "OTEL_EXPORTER_OTLP_CA_FILE", Value: "/var/run/config/mattercodex/agent-runner/observability/otel-ca.pem"},
		{Name: "OTEL_TRACES_SAMPLER_ARG", Value: "0.1"},
		{Name: "SENTRY_DSN_FILE", Value: "/var/run/secrets/mattercodex/agent-runner/observability/sentry-dsn"},
		{Name: "SENTRY_EXPECTED_HOST", Value: "sentry-relay.observability.svc:8443"},
	}
}
func namedConfigMapVolume(name, configMap string) corev1.Volume {
	return corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configMap}, DefaultMode: int32Pointer(0o440)}}}
}
func smallResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("16Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("32Mi")}}
}

func authorityIssuerContainer(image string) corev1.Container {
	return corev1.Container{Name: "internal-rpc-authority-issuer", Image: image, Command: []string{"/usr/local/bin/internal-rpc-authority-issuer"},
		Env: []corev1.EnvVar{{Name: "DEPLOYMENT_ENVIRONMENT", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.labels['mattercodex.dev/environment']"}}},
			{Name: "INTERNAL_RPC_AUTHORITY_WORKLOAD_ID", Value: "agent-runner"},
			{Name: "INTERNAL_RPC_AUTHORITY_WORKLOAD_SPIFFE_ID", Value: "spiffe://mattercodex.local/ns/mattercodex-system/sa/agent-runner"},
			{Name: "INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_ADDRESS", Value: "internal-rpc-authority-readback-attestor.mattercodex-system.svc:8443"},
			{Name: "INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_TLS_SERVER_NAME", Value: "internal-rpc-authority-readback-attestor.mattercodex-system.svc"},
			{Name: "INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_CA_FILE", Value: "/var/run/config/mattercodex/internal-rpc-authority/readback/ca.pem"},
			{Name: "INTERNAL_RPC_AUTHORITY_VAULT_AUTH_ROLE", Value: "internal-rpc-authority-agent-runner"},
			{Name: "INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_CA_FILE", Value: "/var/run/config/mattercodex/internal-rpc-authority/restore/ca.pem"},
			{Name: "INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN", Value: ":9091"},
			{Name: "INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_UID", Value: "10001"}, {Name: "INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_GID", Value: "10001"},
			{Name: "INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE", Value: "/var/run/secrets/mattercodex/internal-rpc-authority/postgres/dsn"},
			{Name: "INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "internal-rpc-authority-agent-runner-issuer-postgresql"}, Key: "username"}}},
			{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "otel-collector.observability.svc:4317"}, {Name: "OTEL_EXPORTER_OTLP_TLS_SERVER_NAME", Value: "otel-collector.observability.svc.cluster.local"},
			{Name: "OTEL_EXPORTER_OTLP_CA_FILE", Value: "/var/run/config/mattercodex/internal-rpc-authority/observability/otel-ca.pem"}, {Name: "OTEL_TRACES_SAMPLER_ARG", Value: "0.1"},
			{Name: "SENTRY_DSN_FILE", Value: "/var/run/secrets/mattercodex/internal-rpc-authority/observability/sentry-dsn"}, {Name: "SENTRY_EXPECTED_HOST", Value: "sentry-relay.observability.svc:8443"}},
		Ports: []corev1.ContainerPort{{Name: "auth-metrics", ContainerPort: 9091}}, SecurityContext: restrictedSecurityContext(29001), Resources: smallResources(),
		ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt32(9091)}}, PeriodSeconds: 5, TimeoutSeconds: 3},
		LivenessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/livez", Port: intstr.FromInt32(9091)}}, PeriodSeconds: 10, TimeoutSeconds: 2},
		VolumeMounts: []corev1.VolumeMount{{Name: "authority-sockets", MountPath: "/run/mattercodex"},
			{Name: "authority-snapshot", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/snapshot", ReadOnly: true}, {Name: "authority-manifest-trust", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/manifest-trust", ReadOnly: true},
			{Name: "authority-proof-trust", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/authority-proof-trust", ReadOnly: true}, {Name: "authority-issuer-key", MountPath: "/var/run/secrets/mattercodex/internal-rpc-authority/issuer", ReadOnly: true},
			{Name: "authority-workload-tls", MountPath: "/var/run/secrets/mattercodex/internal-rpc-authority/workload-tls", ReadOnly: true}, {Name: "authority-readback-ca", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/readback", ReadOnly: true},
			{Name: "authority-vault-ca", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/vault", ReadOnly: true}, {Name: "authority-vault-token", MountPath: "/var/run/secrets/tokens/vault", ReadOnly: true},
			{Name: "authority-restore-ca", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/restore", ReadOnly: true}, {Name: "authority-restore-certificate", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/restore/controller-trust", ReadOnly: true},
			{Name: "authority-restore-role-trust", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/restore/role-trust", ReadOnly: true}, {Name: "authority-postgres", MountPath: "/var/run/secrets/mattercodex/internal-rpc-authority/postgres", ReadOnly: true},
			{Name: "authority-postgres-ca", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/postgresql", ReadOnly: true}, {Name: "authority-observability-ca", MountPath: "/var/run/config/mattercodex/internal-rpc-authority/observability", ReadOnly: true},
			{Name: "authority-sentry-dsn", MountPath: "/var/run/secrets/mattercodex/internal-rpc-authority/observability", ReadOnly: true}}}
}

func (adapter *Adapter) ensureAccessProfile(ctx context.Context, execution entity.Execution) error {
	name := accessServiceAccountName(execution)
	if execution.AccessProfile != enum.AccessNone {
		return errs.ErrStateConflict
	}
	account, err := adapter.client.CoreV1().ServiceAccounts(adapter.config.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || account.AutomountServiceAccountToken == nil || *account.AutomountServiceAccountToken ||
		account.Labels[componentLabel] != "runtime-access" ||
		account.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID ||
		account.Annotations["runtime.mattercodex.dev/organization-id"] != execution.OrganizationID ||
		account.Annotations["runtime.mattercodex.dev/project-id"] != execution.ProjectID ||
		account.Annotations["runtime.mattercodex.dev/session-id"] != execution.SessionID {
		return errors.New("read exact runtime access identity")
	}
	return adapter.ensureRunnerHandoffRBAC(ctx, execution)
}

func (adapter *Adapter) ensureRunnerHandoffRBAC(ctx context.Context, execution entity.Execution) error {
	name := handoffName(execution)
	roles := adapter.client.RbacV1().Roles(adapter.config.Namespace)
	desired := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels(execution, "runtime-handoff")}, Rules: []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{configName(execution)}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{handoffName(execution)}, Verbs: []string{"get", "update"}},
	}}
	existing, err := roles.Get(ctx, name, metav1.GetOptions{})
	if err != nil || !handoffRulesMatch(existing.Rules, desired.Rules, execution) {
		return errors.New("read broker-owned runtime handoff role")
	}
	bindingDesired := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels(execution, "runtime-handoff")},
		Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: accessServiceAccountName(execution), Namespace: adapter.config.Namespace}},
		RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name}}
	bindings := adapter.client.RbacV1().RoleBindings(adapter.config.Namespace)
	binding, err := bindings.Get(ctx, name, metav1.GetOptions{})
	if err != nil || !reflect.DeepEqual(binding.Subjects, bindingDesired.Subjects) || binding.RoleRef != bindingDesired.RoleRef {
		return errors.New("read broker-owned runtime handoff binding")
	}
	return nil
}

func handoffRulesMatch(actual, base []rbacv1.PolicyRule, execution entity.Execution) bool {
	return len(actual) == 2 && len(base) == 2 && reflect.DeepEqual(actual, base) && execution.ID != ""
}

func executionCredentialVolume(
	execution entity.Execution,
	name string,
	index int,
) (corev1.Volume, corev1.VolumeMount) {
	volume, _ := executionCredentialVolumeForPurpose(execution, name, index, "")
	return volume, corev1.VolumeMount{Name: name, MountPath: "/var/run/secrets/mattercodex/runtime/" + name, ReadOnly: true}
}

func executionCredentialVolumeForPurpose(execution entity.Execution, name string, index int, purpose string) (corev1.Volume, []corev1.VolumeMount) {
	path := "/var/run/secrets/mattercodex/runtime/" + name
	secretName := executionCredentialSecretName(execution, index)
	volume := corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
		SecretName: secretName, DefaultMode: int32Pointer(0o440),
	}}}
	files := map[string][]string{
		"control-plane-application-grant":           {"application-grant.jws"},
		"runtime-materialization-application-grant": {"application-grant.jws"},
		"mcp-token": {"token"}, "provider-account": {"auth.json"}, "handoff-private-key": {"ed25519.key"},
		"control-plane-client-tls":       {"ca.pem", "tls.crt", "tls.key"},
		"interaction-gateway-client-tls": {"ca.pem", "tls.crt", "tls.key"},
		"mcp-client-tls":                 {"ca.pem", "tls.crt", "tls.key"},
	}[purpose]
	if len(files) == 0 {
		return volume, []corev1.VolumeMount{{Name: name, MountPath: path, ReadOnly: true}}
	}
	mounts := make([]corev1.VolumeMount, 0, len(files))
	for _, file := range files {
		mounts = append(mounts, corev1.VolumeMount{Name: name, MountPath: path + "/" + file, SubPath: file, ReadOnly: true})
	}
	return volume, mounts
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
		handoffConfig, handoffErr := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(
			ctx, handoffName(document.Execution), metav1.GetOptions{})
		if handoffErr == nil && len(handoffConfig.BinaryData["handoff.json"]) != 0 {
			trustedKeys, trustErr := adapter.handoffTrust(ctx)
			if trustErr != nil {
				return nil, trustErr
			}
			handoff, decodeErr := runtimecontract.DecodeSignedHandoffV2(handoffConfig.BinaryData["handoff.json"], trustedKeys)
			if decodeErr != nil {
				return nil, errors.New("verify signed runtime handoff")
			}
			status.Handoff = castHandoffV2(handoff)
		} else if handoffErr != nil && !apierrors.IsNotFound(handoffErr) {
			return nil, errors.New("read runtime handoff target")
		}
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
		status.PVCNotFoundAt = document.PVCNotFoundAt
		status.PVCDeletionProofSHA256 = document.PVCDeletionProofSHA256
		status.PVCDeletionAuthorizationID = document.PVCDeletionAuthorizationID
		status.PVCDeletionAuthorizationGeneration = document.PVCDeletionGeneration
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

func (adapter *Adapter) handoffTrust(ctx context.Context) (map[string]ed25519.PublicKey, error) {
	config, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(ctx, "agent-runner-handoff-trust", metav1.GetOptions{})
	if err != nil || len(config.BinaryData) == 0 || len(config.BinaryData) > 8 {
		return nil, errors.New("read runtime handoff trust")
	}
	result := make(map[string]ed25519.PublicKey, len(config.BinaryData))
	for keyID, raw := range config.BinaryData {
		if keyID == "" || len(raw) != ed25519.PublicKeySize || bytes.Equal(raw, make([]byte, ed25519.PublicKeySize)) {
			return nil, errors.New("runtime handoff trust is invalid")
		}
		result[keyID] = ed25519.PublicKey(slices.Clone(raw))
	}
	return result, nil
}

func castHandoffV2(source runtimecontract.HandoffV2) *entity.RuntimeHandoff {
	result := &entity.RuntimeHandoff{Schema: source.Schema, ExecutionID: source.ExecutionID,
		ExecutionVersion: source.ExecutionVersion, Fence: source.Fence, GrantGeneration: source.GrantGeneration,
		RuntimeRevisionSHA256: source.RuntimeRevisionSHA256, EffectiveRuntimeSHA256: source.EffectiveRuntimeSHA256,
		ImmutableInputSHA256: source.ImmutableInputSHA256, SessionID: source.SessionID, TurnID: source.TurnID,
		ScheduleOccurrenceID: source.ScheduleOccurrenceID,
		Attempt:              source.Attempt, ProviderBindingID: source.ProviderBindingID,
		ProviderBindingVersion: source.ProviderBindingVersion, ProviderBindingSHA256: source.ProviderBindingSHA256,
		Outcome: source.Outcome, ScheduledOutcome: source.ScheduledOutcome,
		TerminalReference: source.TerminalReference, TerminalSHA256: source.TerminalSHA256,
		CodexSessionID: source.CodexSessionID, ArchiveRelativePath: source.ArchiveRelativePath, ArchiveSHA256: source.ArchiveSHA256,
		ArchiveProvenance: source.ArchiveProvenance, ObservedAt: source.ObservedAt}
	for _, output := range source.Outputs {
		result.Outputs = append(result.Outputs, entity.RuntimeOutput{Kind: output.Kind, ArtifactID: output.ID,
			ArtifactVersion: output.Version, ArtifactSHA256: output.SHA256, ArtifactName: output.Name,
			ArtifactMediaType: output.MediaType, ArtifactPayload: slices.Clone(output.Payload),
			ArtifactStorageRef: output.StorageRef, ArtifactSizeBytes: output.SizeBytes,
			Sequence: output.Sequence, Total: output.Total})
	}
	return result
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
	status.RuntimeRevisionSHA256 = pod.Annotations["runtime.mattercodex.dev/revision-sha256"]
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
	if pod == nil {
		return errs.ErrStateConflict
	}
	name := pod.Spec.ServiceAccountName
	executionID := pod.Annotations["runtime.mattercodex.dev/execution-id"]
	if name == "" || executionID == "" {
		return errs.ErrStateConflict
	}
	switch enum.AccessProfile(strings.ToUpper(pod.Labels[accessLabel])) {
	case enum.AccessNone:
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
		return nil
	default:
		return errs.ErrStateConflict
	}
	if err := adapter.deleteRunnerHandoffRBAC(ctx, pod, name); err != nil {
		return err
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

func (adapter *Adapter) deleteRunnerHandoffRBAC(ctx context.Context, pod *corev1.Pod, serviceAccount string) error {
	name := "runtime-handoff-" + pod.Labels[executionLabel]
	if pod.Labels[executionLabel] == "" || name != "runtime-handoff-"+shortID(pod.Annotations["runtime.mattercodex.dev/execution-id"]) {
		return errs.ErrStateConflict
	}
	bindings := adapter.client.RbacV1().RoleBindings(adapter.config.Namespace)
	binding, err := bindings.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		expectedSubject := rbacv1.Subject{Kind: "ServiceAccount", Name: serviceAccount, Namespace: adapter.config.Namespace}
		if !sameBinding(binding.Subjects, expectedSubject) || binding.RoleRef != (rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName, Kind: "Role", Name: name,
		}) {
			return errs.ErrStateConflict
		}
		uid, version := binding.UID, binding.ResourceVersion
		err = bindings.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}})
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.New("revoke runtime handoff binding")
	}
	roles := adapter.client.RbacV1().Roles(adapter.config.Namespace)
	role, err := roles.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		uid, version := role.UID, role.ResourceVersion
		err = roles.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}})
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.New("revoke runtime handoff role")
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
		return nil
	default:
		return errs.ErrStateConflict
	}
	return nil
}

func (adapter *Adapter) DeletePVC(
	ctx context.Context,
	execution entity.Execution,
	status entity.RuntimeStatus,
) (entity.PVCDeletionProof, error) {
	if execution.CleanupAuthorizationState != "ACTIVE" ||
		execution.CleanupAuthorizationID == "" || execution.CleanupAuthorizationGeneration == 0 {
		return entity.PVCDeletionProof{}, errs.ErrCleanupUnauthorized
	}
	if status.PVCDeleted &&
		status.PVCDeletionAuthorizationID == execution.CleanupAuthorizationID &&
		status.PVCDeletionAuthorizationGeneration == execution.CleanupAuthorizationGeneration {
		return pvcDeletionProof(status)
	}
	if !status.RetentionOwner || status.PVCName == "" || status.PVCUID == "" || status.PVCResourceVersion == "" {
		return entity.PVCDeletionProof{}, errs.ErrStateConflict
	}
	uid := types.UID(status.PVCUID)
	resourceVersion := status.PVCResourceVersion
	if !status.PVCDeletionStarted {
		if _, err := adapter.recordPVCDeletion(ctx, execution, status, false); err != nil {
			return entity.PVCDeletionProof{}, err
		}
	}
	pvc, readErr := adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).Get(
		ctx, status.PVCName, metav1.GetOptions{},
	)
	if apierrors.IsNotFound(readErr) {
		return adapter.recordPVCDeletion(ctx, execution, status, true)
	}
	if readErr != nil {
		return entity.PVCDeletionProof{}, errors.New("read runtime PVC before guarded deletion")
	}
	if pvc.UID != uid ||
		pvc.Annotations["runtime.mattercodex.dev/retention-owner-execution-id"] != status.ExecutionID ||
		pvc.Annotations["runtime.mattercodex.dev/retention-owner-journal"] != journalName(status.ExecutionID) {
		return entity.PVCDeletionProof{}, errs.ErrStateConflict
	}
	if pvc.DeletionTimestamp == nil {
		if pvc.ResourceVersion != resourceVersion {
			return entity.PVCDeletionProof{}, errs.ErrStateConflict
		}
		err := adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).Delete(
			ctx, status.PVCName, metav1.DeleteOptions{
				Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion},
			},
		)
		if err != nil && !apierrors.IsNotFound(err) {
			if apierrors.IsConflict(err) {
				return entity.PVCDeletionProof{}, errs.ErrStateConflict
			}
			return entity.PVCDeletionProof{}, errors.New("delete exact runtime PVC")
		}
	}
	_, readErr = adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).Get(
		ctx, status.PVCName, metav1.GetOptions{},
	)
	if readErr == nil {
		return entity.PVCDeletionProof{}, errs.ErrDependency
	}
	if !apierrors.IsNotFound(readErr) {
		return entity.PVCDeletionProof{}, errors.New("read back deleted runtime PVC")
	}
	return adapter.recordPVCDeletion(ctx, execution, status, true)
}

func (adapter *Adapter) recordPVCDeletion(
	ctx context.Context,
	execution entity.Execution,
	status entity.RuntimeStatus,
	deleted bool,
) (entity.PVCDeletionProof, error) {
	configMap, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(
		ctx, journalName(status.ExecutionID), metav1.GetOptions{},
	)
	if err != nil {
		return entity.PVCDeletionProof{}, errors.New("read runtime journal for PVC deletion")
	}
	var document journalDocument
	if decodeJournal([]byte(configMap.Data[journalDataKey]), &document) != nil ||
		document.Execution.ID != status.ExecutionID || document.PVCName != status.PVCName ||
		(document.PVCUID != "" && document.PVCUID != status.PVCUID) ||
		(document.PVCResourceVersion != "" && document.PVCResourceVersion != status.PVCResourceVersion) ||
		document.PVCDeleted && !deleted || document.PVCDeletionOwner && document.PVCUID == "" {
		return entity.PVCDeletionProof{}, errs.ErrStateConflict
	}
	document.PVCUID = status.PVCUID
	document.PVCResourceVersion = status.PVCResourceVersion
	document.PVCDeletionOwner = true
	document.PVCDeleted = deleted
	if deleted && (document.PVCNotFoundAt.IsZero() ||
		document.PVCDeletionAuthorizationID != execution.CleanupAuthorizationID ||
		document.PVCDeletionGeneration != execution.CleanupAuthorizationGeneration) {
		document.PVCNotFoundAt = adapter.now().UTC().Truncate(time.Microsecond)
		document.PVCDeletionAuthorizationID = execution.CleanupAuthorizationID
		document.PVCDeletionGeneration = execution.CleanupAuthorizationGeneration
		document.PVCDeletionProofSHA256 = deletionProofSHA256(
			document.PVCName, document.PVCUID, document.PVCResourceVersion,
			document.PVCDeletionAuthorizationID, document.PVCDeletionGeneration,
			document.PVCNotFoundAt,
		)
	}
	raw, err := marshalJournal(document)
	if err != nil {
		return entity.PVCDeletionProof{}, err
	}
	configMap.Data[journalDataKey] = string(raw)
	if _, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Update(ctx, configMap, metav1.UpdateOptions{}); err != nil {
		if apierrors.IsConflict(err) {
			return entity.PVCDeletionProof{}, errs.ErrStateConflict
		}
		return entity.PVCDeletionProof{}, errors.New("persist runtime PVC deletion evidence")
	}
	if !deleted {
		return entity.PVCDeletionProof{}, nil
	}
	return entity.PVCDeletionProof{PVCName: document.PVCName, PVCUID: document.PVCUID,
		PVCResourceVersion: document.PVCResourceVersion, ObservedNotFoundAt: document.PVCNotFoundAt,
		SHA256:                         document.PVCDeletionProofSHA256,
		CleanupAuthorizationID:         document.PVCDeletionAuthorizationID,
		CleanupAuthorizationGeneration: document.PVCDeletionGeneration}, nil
}

func deletionProofSHA256(name, uid, resourceVersion, authorizationID string, generation uint64, observedAt time.Time) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"runtime-pvc-not-found-v2", name, uid, resourceVersion, authorizationID,
		strconv.FormatUint(generation, 10), observedAt.Format(time.RFC3339Nano),
	}, "\n")))
	return hex.EncodeToString(digest[:])
}

func pvcDeletionProof(status entity.RuntimeStatus) (entity.PVCDeletionProof, error) {
	proof := entity.PVCDeletionProof{PVCName: status.PVCName, PVCUID: status.PVCUID,
		PVCResourceVersion: status.PVCResourceVersion, ObservedNotFoundAt: status.PVCNotFoundAt,
		SHA256:                         status.PVCDeletionProofSHA256,
		CleanupAuthorizationID:         status.PVCDeletionAuthorizationID,
		CleanupAuthorizationGeneration: status.PVCDeletionAuthorizationGeneration}
	if proof.ObservedNotFoundAt.IsZero() || proof.SHA256 != deletionProofSHA256(
		proof.PVCName, proof.PVCUID, proof.PVCResourceVersion,
		proof.CleanupAuthorizationID, proof.CleanupAuthorizationGeneration, proof.ObservedNotFoundAt,
	) {
		return entity.PVCDeletionProof{}, errs.ErrStateConflict
	}
	return proof, nil
}

func (adapter *Adapter) EnsureArchiveJob(
	ctx context.Context,
	execution entity.Execution,
	status entity.RuntimeStatus,
) error {
	if !adapter.config.ArchiveRestoreEnabled {
		return errs.ErrStateConflict
	}
	ready, snapshotUID, err := adapter.ensureArchiveSnapshotPVC(ctx, execution, status)
	if err != nil || !ready {
		return err
	}
	status.ArchiveSnapshotPVCUID = snapshotUID
	return adapter.ensureWorkerJob(ctx, execution, status, archiveComponent)
}

func (adapter *Adapter) ensureArchiveSnapshotPVC(
	ctx context.Context,
	execution entity.Execution,
	status entity.RuntimeStatus,
) (bool, string, error) {
	if status.Handoff == nil || validateSnapshotSource(status) != nil {
		return false, "", errs.ErrStateConflict
	}
	source, err := adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).Get(
		ctx, status.PVCName, metav1.GetOptions{},
	)
	if err != nil || string(source.UID) != status.PVCUID || source.ResourceVersion != status.PVCResourceVersion {
		return false, "", errs.ErrStateConflict
	}
	name := archiveSnapshotPVCName(execution)
	claims := adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace)
	desired := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Name: name, Labels: labels(execution, "archive-snapshot"), Annotations: map[string]string{
			"runtime.mattercodex.dev/execution-id":                execution.ID,
			"runtime.mattercodex.dev/source-pvc-name":             status.PVCName,
			"runtime.mattercodex.dev/source-pvc-uid":              status.PVCUID,
			"runtime.mattercodex.dev/source-pvc-resource-version": status.PVCResourceVersion,
			"runtime.mattercodex.dev/created-at":                  adapter.now().UTC().Format(time.RFC3339Nano),
		},
	}, Spec: corev1.PersistentVolumeClaimSpec{
		AccessModes: source.Spec.AccessModes, Resources: source.Spec.Resources,
		StorageClassName: source.Spec.StorageClassName, VolumeMode: source.Spec.VolumeMode,
		DataSource: &corev1.TypedLocalObjectReference{Kind: "PersistentVolumeClaim", Name: status.PVCName},
	}}
	current, err := claims.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err := claims.Create(ctx, desired, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			return false, "", errors.New("create immutable runtime archive PVC clone")
		}
		return false, "", nil
	}
	if err != nil || current.Annotations["runtime.mattercodex.dev/source-pvc-uid"] != status.PVCUID ||
		current.Annotations["runtime.mattercodex.dev/source-pvc-resource-version"] != status.PVCResourceVersion ||
		current.Spec.DataSource == nil || current.Spec.DataSource.Kind != "PersistentVolumeClaim" ||
		current.Spec.DataSource.Name != status.PVCName {
		return false, "", errs.ErrStateConflict
	}
	return current.Status.Phase == corev1.ClaimBound && current.UID != "", string(current.UID), nil
}

func validateSnapshotSource(status entity.RuntimeStatus) error {
	if status.PVCName == "" || uuid.Validate(status.PVCUID) != nil || status.PVCResourceVersion == "" {
		return errs.ErrStateConflict
	}
	return nil
}

func (adapter *Adapter) EnsureRestoreVerifierJob(
	ctx context.Context,
	execution entity.Execution,
	status entity.RuntimeStatus,
) error {
	if !adapter.config.ArchiveRestoreEnabled {
		return errs.ErrStateConflict
	}
	return adapter.ensureWorkerJob(ctx, execution, status, restoreComponent)
}

func (adapter *Adapter) EnsureCleanupAuthorizerJob(
	ctx context.Context,
	execution entity.Execution,
	status entity.RuntimeStatus,
) error {
	return adapter.ensureWorkerJob(ctx, execution, status, cleanupComponent)
}

func (adapter *Adapter) OpenArchiveGate(ctx context.Context, execution entity.Execution, status entity.RuntimeStatus) error {
	if !execution.State.Terminal() || execution.ArchiveSHA256 == "" || execution.RestoreProofSHA256 == "" || status.PodName == "" {
		return errs.ErrStateConflict
	}
	pod, err := adapter.client.CoreV1().Pods(adapter.config.Namespace).Get(ctx, status.PodName, metav1.GetOptions{})
	if err != nil {
		return errors.New("read warm runtime archive gate")
	}
	if pod.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID ||
		pod.Annotations["runtime.mattercodex.dev/revision-sha256"] != execution.RuntimeRevisionSHA256 ||
		pod.Annotations["runtime.mattercodex.dev/archive-gate"] == "OPEN" {
		if pod.Annotations["runtime.mattercodex.dev/archive-gate"] == "OPEN" {
			return nil
		}
		return errs.ErrStateConflict
	}
	updated := pod.DeepCopy()
	updated.Annotations["runtime.mattercodex.dev/archive-gate"] = "OPEN"
	if _, err := adapter.client.CoreV1().Pods(adapter.config.Namespace).Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		return errors.New("open verified runtime archive gate")
	}
	return nil
}

func (adapter *Adapter) ensureCredentialBroker(
	ctx context.Context,
	execution entity.Execution,
) (bool, error) {
	name := credentialBrokerJobName(execution)
	desired := adapter.credentialBrokerJob(execution)
	existing, err := adapter.client.BatchV1().Jobs(adapter.config.Namespace).Get(
		ctx, name, metav1.GetOptions{},
	)
	if err == nil {
		if existing.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID ||
			existing.Annotations["runtime.mattercodex.dev/version"] != strconv.FormatUint(execution.Version, 10) ||
			existing.Annotations["runtime.mattercodex.dev/fence"] != strconv.FormatUint(execution.Fence, 10) ||
			existing.Annotations["runtime.mattercodex.dev/workload-ticket"] != execution.WorkloadTicket ||
			existing.Annotations["runtime.mattercodex.dev/credential-snapshot-sha256"] != execution.CredentialSnapshotSHA256 ||
			existing.Spec.Template.Spec.ServiceAccountName != desired.Spec.Template.Spec.ServiceAccountName ||
			len(existing.Spec.Template.Spec.Containers) != 1 ||
			len(existing.Spec.Template.Spec.Containers[0].Command) != 1 ||
			existing.Spec.Template.Spec.Containers[0].Command[0] != "/usr/local/bin/runtime-credential-broker" {
			return false, errs.ErrStateConflict
		}
		if existing.Status.Failed > 0 && existing.Status.Active == 0 {
			return false, errors.New("runtime credential broker failed")
		}
		return existing.Status.Succeeded == 1, nil
	}
	if !apierrors.IsNotFound(err) {
		return false, errors.New("read runtime credential broker job")
	}
	if _, err := adapter.client.BatchV1().Jobs(adapter.config.Namespace).Create(
		ctx, desired, metav1.CreateOptions{},
	); err != nil && !apierrors.IsAlreadyExists(err) {
		return false, errors.New("create runtime credential broker job")
	}
	return false, nil
}

func (adapter *Adapter) credentialBrokerJob(execution entity.Execution) *batchv1.Job {
	backoff, ttl, deadline := int32(2), int32(adapter.config.JobTTL/time.Second), int64(300)
	annotations := map[string]string{
		"runtime.mattercodex.dev/execution-id":               execution.ID,
		"runtime.mattercodex.dev/version":                    strconv.FormatUint(execution.Version, 10),
		"runtime.mattercodex.dev/fence":                      strconv.FormatUint(execution.Fence, 10),
		"runtime.mattercodex.dev/grant-generation":           strconv.FormatUint(execution.GrantGeneration, 10),
		"runtime.mattercodex.dev/revision-sha256":            execution.RuntimeRevisionSHA256,
		"runtime.mattercodex.dev/input-sha256":               execution.ImmutableInputSHA256,
		"runtime.mattercodex.dev/workload-ticket-sha256":     execution.WorkloadTicketSHA256,
		"runtime.mattercodex.dev/workload-ticket":            execution.WorkloadTicket,
		"runtime.mattercodex.dev/credential-snapshot-sha256": execution.CredentialSnapshotSHA256,
		"runtime.mattercodex.dev/runtime-config-name":        configName(execution),
	}
	jobLabels := labels(execution, credentialComponent)
	jobLabels["mattercodex.dev/environment"] = adapter.config.Environment
	brokerServiceAccount := adapter.config.CredentialBrokerServiceAccount
	switch execution.AccessProfile {
	case enum.AccessProjectRead:
		brokerServiceAccount = adapter.config.ProjectReadBrokerServiceAccount
	case enum.AccessClusterAdmin:
		brokerServiceAccount = adapter.config.ClusterAdminBrokerServiceAccount
	}
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: credentialBrokerJobName(execution), Labels: jobLabels, Annotations: annotations,
	}, Spec: batchv1.JobSpec{
		BackoffLimit: &backoff, TTLSecondsAfterFinished: &ttl, ActiveDeadlineSeconds: &deadline,
		Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: jobLabels, Annotations: annotations},
			Spec: corev1.PodSpec{
				ServiceAccountName:           brokerServiceAccount,
				AutomountServiceAccountToken: boolPointer(false), EnableServiceLinks: boolPointer(false),
				RestartPolicy: corev1.RestartPolicyOnFailure,
				SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true),
					FSGroup: int64Pointer(10001), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
				Containers: []corev1.Container{{
					Name: "credential-broker", Image: adapter.config.ControllerImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"/usr/local/bin/runtime-credential-broker"}, Args: []string{"snapshot"},
					Env: []corev1.EnvVar{
						{Name: "RUNTIME_EXECUTION_ID", Value: execution.ID},
						{Name: "RUNTIME_NAMESPACE", Value: adapter.config.Namespace},
						{Name: "RUNTIME_CREDENTIAL_AUTHORITY_URL", Value: "https://runtime-workload-materializer.mattercodex-system.svc:8443/v1/materialize"},
						{Name: "RUNTIME_CREDENTIAL_AUTHORITY_TLS_SERVER_NAME", Value: "runtime-workload-materializer.mattercodex-system.svc.cluster.local"},
					},
					SecurityContext: restrictedSecurityContext(10001),
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("25m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "runtime-config", MountPath: "/var/run/config/mattercodex/runtime", ReadOnly: true},
						{Name: "workload-ticket-trust", MountPath: "/var/run/config/mattercodex/runtime-workload-ticket", ReadOnly: true},
						{Name: "credential-authority-tls", MountPath: "/var/run/config/mattercodex/runtime-credential-authority", ReadOnly: true},
					},
				}},
				Volumes: []corev1.Volume{
					{Name: "runtime-config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configName(execution)}, DefaultMode: int32Pointer(0o440)}}},
					{Name: "workload-ticket-trust", VolumeSource: csiVolume("runtime-credential-broker-workload-ticket-trust")},
					{Name: "credential-authority-tls", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: brokerServiceAccount + "-mtls", DefaultMode: int32Pointer(0o400)}}},
				},
			},
		},
	}}
}

func (adapter *Adapter) ensureS3CredentialBroker(
	ctx context.Context,
	execution entity.Execution,
	action string,
) (bool, error) {
	if !adapter.config.ArchiveRestoreEnabled {
		return false, errs.ErrStateConflict
	}
	if action != "archive" && action != "restore" {
		return false, errs.ErrInvalidInput
	}
	name := s3CredentialBrokerJobName(execution, action)
	desired := adapter.s3CredentialBrokerJob(execution, action)
	expectedTicket := execution.RestoreWorkloadTicket
	if action == "archive" {
		expectedTicket = execution.ArchiveWorkloadTicket
	}
	if !validWorkloadTicket(expectedTicket) {
		return false, errs.ErrStateConflict
	}
	existing, err := adapter.client.BatchV1().Jobs(adapter.config.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		container := existing.Spec.Template.Spec.Containers
		if existing.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID ||
			existing.Annotations["runtime.mattercodex.dev/version"] != strconv.FormatUint(execution.Version, 10) ||
			existing.Annotations["runtime.mattercodex.dev/fence"] != strconv.FormatUint(execution.Fence, 10) ||
			existing.Annotations["runtime.mattercodex.dev/workload-ticket"] != expectedTicket ||
			existing.Annotations["runtime.mattercodex.dev/action"] != action ||
			existing.Spec.Template.Spec.ServiceAccountName != adapter.s3BrokerServiceAccount(action) ||
			len(container) != 1 || len(container[0].Command) != 1 || len(container[0].Args) != 1 ||
			container[0].Command[0] != "/usr/local/bin/runtime-credential-broker" || container[0].Args[0] != "s3-"+action {
			return false, errs.ErrStateConflict
		}
		if existing.Status.Failed > 0 && existing.Status.Active == 0 {
			return false, errors.New("runtime S3 credential broker failed")
		}
		return existing.Status.Succeeded == 1, nil
	}
	if !apierrors.IsNotFound(err) {
		return false, errors.New("read runtime S3 credential broker job")
	}
	if _, err := adapter.client.BatchV1().Jobs(adapter.config.Namespace).Create(ctx, desired, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return false, errors.New("create runtime S3 credential broker job")
	}
	return false, nil
}

func (adapter *Adapter) s3BrokerServiceAccount(action string) string {
	if action == "archive" {
		return adapter.config.S3ArchiveBrokerServiceAccount
	}
	return adapter.config.S3RestoreBrokerServiceAccount
}

func (adapter *Adapter) s3CredentialBrokerJob(execution entity.Execution, action string) *batchv1.Job {
	backoff, ttl, deadline := int32(2), int32(adapter.config.JobTTL/time.Second), int64(300)
	workloadTicket := execution.RestoreWorkloadTicket
	authorityName := "runtime-s3-restore-exchanger"
	if action == "archive" {
		workloadTicket = execution.ArchiveWorkloadTicket
		authorityName = "runtime-s3-archive-exchanger"
	}
	annotations := map[string]string{
		"runtime.mattercodex.dev/execution-id":           execution.ID,
		"runtime.mattercodex.dev/version":                strconv.FormatUint(execution.Version, 10),
		"runtime.mattercodex.dev/fence":                  strconv.FormatUint(execution.Fence, 10),
		"runtime.mattercodex.dev/grant-generation":       strconv.FormatUint(execution.GrantGeneration, 10),
		"runtime.mattercodex.dev/revision-sha256":        execution.RuntimeRevisionSHA256,
		"runtime.mattercodex.dev/input-sha256":           execution.ImmutableInputSHA256,
		"runtime.mattercodex.dev/workload-ticket-sha256": execution.WorkloadTicketSHA256,
		"runtime.mattercodex.dev/workload-ticket":        workloadTicket,
		"runtime.mattercodex.dev/action":                 action,
		"runtime.mattercodex.dev/runtime-config-name":    configName(execution),
	}
	jobLabels := labels(execution, credentialComponent)
	jobLabels["mattercodex.dev/environment"] = adapter.config.Environment
	jobLabels["runtime.mattercodex.dev/credential-action"] = action
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: s3CredentialBrokerJobName(execution, action), Labels: jobLabels, Annotations: annotations},
		Spec: batchv1.JobSpec{BackoffLimit: &backoff, TTLSecondsAfterFinished: &ttl, ActiveDeadlineSeconds: &deadline,
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: jobLabels, Annotations: annotations}, Spec: corev1.PodSpec{
				ServiceAccountName: adapter.s3BrokerServiceAccount(action), AutomountServiceAccountToken: boolPointer(false),
				EnableServiceLinks: boolPointer(false), RestartPolicy: corev1.RestartPolicyOnFailure,
				SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true), FSGroup: int64Pointer(10001),
					SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
				Containers: []corev1.Container{{
					Name: "s3-credential-broker", Image: adapter.config.ControllerImage, ImagePullPolicy: corev1.PullIfNotPresent,
					Command: []string{"/usr/local/bin/runtime-credential-broker"}, Args: []string{"s3-" + action},
					Env: []corev1.EnvVar{
						{Name: "RUNTIME_EXECUTION_ID", Value: execution.ID}, {Name: "RUNTIME_NAMESPACE", Value: adapter.config.Namespace},
						{Name: "RUNTIME_CREDENTIAL_AUTHORITY_URL", Value: "https://" + authorityName + ".mattercodex-system.svc:8443/v1/materialize"},
						{Name: "RUNTIME_CREDENTIAL_AUTHORITY_TLS_SERVER_NAME", Value: authorityName + ".mattercodex-system.svc.cluster.local"},
					},
					SecurityContext: restrictedSecurityContext(10001),
					Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("25m"), corev1.ResourceMemory: resource.MustParse("32Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("128Mi")}},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "runtime-config", MountPath: "/var/run/config/mattercodex/runtime", ReadOnly: true},
						{Name: "workload-ticket-trust", MountPath: "/var/run/config/mattercodex/runtime-workload-ticket", ReadOnly: true},
						{Name: "credential-authority-tls", MountPath: "/var/run/config/mattercodex/runtime-credential-authority", ReadOnly: true},
					},
				}},
				Volumes: []corev1.Volume{
					{Name: "runtime-config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configName(execution)}, DefaultMode: int32Pointer(0o440)}}},
					{Name: "workload-ticket-trust", VolumeSource: csiVolume("runtime-s3-" + action + "-workload-ticket-trust")},
					{Name: "credential-authority-tls", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "runtime-s3-" + action + "-broker-mtls", DefaultMode: int32Pointer(0o400)}}},
				},
			}}},
	}
}

func (adapter *Adapter) ensureWorkerJob(
	ctx context.Context,
	execution entity.Execution,
	status entity.RuntimeStatus,
	component string,
) error {
	if (!execution.State.Terminal() && (component != rehydrateComponent ||
		execution.RestoreSourceExecutionID == "")) || status.PVCName == "" {
		return errs.ErrStateConflict
	}
	name := workerJobName(component, execution)
	credentialSnapshotSHA256 := ""
	if component == archiveComponent || component == restoreComponent || component == rehydrateComponent {
		action := "archive"
		if component != archiveComponent {
			action = "restore"
		}
		ready, brokerErr := adapter.ensureS3CredentialBroker(ctx, execution, action)
		if brokerErr != nil {
			return brokerErr
		}
		if !ready {
			return errs.ErrDependency
		}
		var credentialErr error
		credentialSnapshotSHA256, credentialErr = adapter.readS3CredentialSnapshot(ctx, execution, action)
		if credentialErr != nil {
			return credentialErr
		}
	}
	existing, err := adapter.client.BatchV1().Jobs(adapter.config.Namespace).Get(
		ctx, name, metav1.GetOptions{},
	)
	if err == nil {
		if existing.Labels[executionLabel] != shortID(execution.ID) ||
			existing.Annotations["runtime.mattercodex.dev/fence"] != strconv.FormatUint(execution.Fence, 10) ||
			existing.Annotations["runtime.mattercodex.dev/execution-id"] != execution.ID ||
			existing.Annotations["runtime.mattercodex.dev/workload-ticket-sha256"] != execution.WorkloadTicketSHA256 ||
			existing.Annotations["runtime.mattercodex.dev/revision-sha256"] != execution.RuntimeRevisionSHA256 ||
			existing.Annotations["runtime.mattercodex.dev/input-sha256"] != execution.ImmutableInputSHA256 ||
			existing.Annotations["runtime.mattercodex.dev/s3-credential-snapshot-sha256"] != credentialSnapshotSHA256 {
			return errs.ErrStateConflict
		}
		if existing.Status.Failed > 0 && existing.Status.Active == 0 && component == cleanupComponent {
			uid, version := existing.UID, existing.ResourceVersion
			if deleteErr := adapter.client.BatchV1().Jobs(adapter.config.Namespace).Delete(
				ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}},
			); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
				return errors.New("replace failed cleanup eligibility probe")
			}
			return nil
		}
		if existing.Status.Failed > 0 && existing.Status.Active == 0 {
			return errs.ErrDependency
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return errors.New("read runtime worker job")
	}
	job, err := adapter.workerJob(execution, status, component, credentialSnapshotSHA256)
	if err != nil {
		return err
	}
	if err := adapter.ensureWorkerJournalRBAC(ctx, execution, component, job.Spec.Template.Spec.ServiceAccountName); err != nil {
		return err
	}
	if _, err := adapter.client.BatchV1().Jobs(adapter.config.Namespace).Create(
		ctx, job, metav1.CreateOptions{},
	); err != nil && !apierrors.IsAlreadyExists(err) {
		return errors.New("create runtime worker job")
	}
	return nil
}

func (adapter *Adapter) ensureWorkerJournalRBAC(
	ctx context.Context,
	execution entity.Execution,
	component, serviceAccount string,
) error {
	name := workerJobName(component, execution)
	desired := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels(execution, "worker-journal-authority")},
		Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"configmaps"},
			ResourceNames: []string{journalName(execution.ID)}, Verbs: []string{"get", "update"}}}}
	roles := adapter.client.RbacV1().Roles(adapter.config.Namespace)
	role, err := roles.Get(ctx, name, metav1.GetOptions{})
	if err != nil || !reflect.DeepEqual(role.Rules, desired.Rules) {
		return errors.New("read broker-owned worker journal role")
	}
	bindingDesired := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: desired.Labels},
		Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: serviceAccount, Namespace: adapter.config.Namespace}},
		RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name}}
	bindings := adapter.client.RbacV1().RoleBindings(adapter.config.Namespace)
	binding, err := bindings.Get(ctx, name, metav1.GetOptions{})
	if err != nil || !reflect.DeepEqual(binding.Subjects, bindingDesired.Subjects) || binding.RoleRef != bindingDesired.RoleRef {
		return errors.New("read broker-owned worker journal binding")
	}
	return nil
}

func (adapter *Adapter) workerJob(
	execution entity.Execution,
	status entity.RuntimeStatus,
	component string,
	credentialSnapshotSHA256 string,
) (*batchv1.Job, error) {
	if !validWorkloadTicket(execution.WorkloadTicket) {
		return nil, errs.ErrStateConflict
	}
	command, serviceAccount, workload, spiffe := "", "", "", ""
	switch component {
	case archiveComponent:
		command, serviceAccount, workload = "/usr/local/bin/runtime-archive", adapter.config.ArchiveServiceAccount, "runtime-archive"
		spiffe = "spiffe://mattercodex.local/ns/" + adapter.config.Namespace + "/sa/" + serviceAccount
	case restoreComponent:
		command, serviceAccount, workload = "/usr/local/bin/runtime-restore-verifier", adapter.config.RestoreServiceAccount, "runtime-restore-verifier"
		spiffe = "spiffe://mattercodex.local/ns/" + adapter.config.Namespace + "/sa/" + serviceAccount
	case rehydrateComponent:
		command, serviceAccount, workload = "/usr/local/bin/runtime-rehydrate", adapter.config.RestoreServiceAccount, "runtime-restore-verifier"
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
		{Name: "RUNTIME_PVC_NAME", Value: status.PVCName},
		{Name: "RUNTIME_PVC_UID", Value: status.PVCUID},
		{Name: "RUNTIME_PVC_RESOURCE_VERSION", Value: status.PVCResourceVersion},
		{Name: "RUNTIME_ARCHIVE_SNAPSHOT_PVC_UID", Value: status.ArchiveSnapshotPVCUID},
	}
	mainResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("512Mi")},
	}
	if component == archiveComponent || component == restoreComponent || component == rehydrateComponent {
		scratch := adapter.workerScratch(component)
		mainResources.Requests[corev1.ResourceEphemeralStorage] = scratch
		mainResources.Limits[corev1.ResourceEphemeralStorage] = scratch
	}
	annotations := map[string]string{
		"runtime.mattercodex.dev/fence":                  strconv.FormatUint(execution.Fence, 10),
		"runtime.mattercodex.dev/execution-id":           execution.ID,
		"runtime.mattercodex.dev/workload-ticket-sha256": execution.WorkloadTicketSHA256,
		"runtime.mattercodex.dev/workload-ticket":        execution.WorkloadTicket,
		"runtime.mattercodex.dev/revision-sha256":        execution.RuntimeRevisionSHA256,
		"runtime.mattercodex.dev/input-sha256":           execution.ImmutableInputSHA256,
	}
	if credentialSnapshotSHA256 != "" {
		annotations["runtime.mattercodex.dev/s3-credential-snapshot-sha256"] = credentialSnapshotSHA256
	}
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: workerJobName(component, execution), Labels: labels,
		Annotations: annotations},
		Spec: batchv1.JobSpec{BackoffLimit: &backoff, TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: annotations}, Spec: corev1.PodSpec{
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
					Ports: []corev1.ContainerPort{{Name: "auth-metrics", ContainerPort: 9091, Protocol: corev1.ProtocolTCP}},
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

func (adapter *Adapter) readS3CredentialSnapshot(
	ctx context.Context,
	execution entity.Execution,
	action string,
) (string, error) {
	if action != "archive" && action != "restore" {
		return "", errs.ErrInvalidInput
	}
	receiptName := "runtime-authority-receipt-" + stableHash(execution.ID+":s3-"+action, 24)
	receipt, err := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(
		ctx, receiptName, metav1.GetOptions{},
	)
	if err != nil || receipt.Immutable == nil || !*receipt.Immutable ||
		receipt.Data["execution_id"] != execution.ID || receipt.Data["mode"] != "s3-"+action ||
		receipt.Data["version"] != strconv.FormatUint(execution.Version, 10) ||
		receipt.Data["fence"] != strconv.FormatUint(execution.Fence, 10) ||
		receipt.Data["s3_execution_id"] != execution.ID ||
		receipt.Data["s3_organization_id"] != execution.OrganizationID ||
		receipt.Data["s3_project_id"] != execution.ProjectID ||
		receipt.Data["s3_session_id"] != execution.SessionID ||
		receipt.Data["s3_action"] != action ||
		receipt.Data["s3_secret_name"] != s3CredentialSecretName(execution, action) ||
		receipt.Data["s3_secret_uid"] == "" || receipt.Data["s3_secret_resource_version"] == "" ||
		!validSHA256Text(receipt.Data["request_sha256"]) ||
		!validSHA256Text(receipt.Data["s3_policy_sha256"]) ||
		!validSHA256Text(receipt.Data["s3_readback_sha256"]) ||
		!validSHA256Text(receipt.Data["s3_secret_data_sha256"]) ||
		!validSHA256Text(receipt.Data["s3_content_sha256"]) {
		return "", errors.New("read exact runtime S3 credential owner receipt")
	}
	expiresAt, err := time.Parse(time.RFC3339, receipt.Data["s3_expires_at"])
	now := adapter.now().UTC()
	if err != nil || !expiresAt.After(now.Add(time.Minute)) || expiresAt.After(now.Add(15*time.Minute)) {
		return "", errors.New("runtime S3 credential lifetime is invalid")
	}
	sourceExecutionID := execution.ID
	if action == "restore" {
		sourceExecutionID = execution.RestoreSourceExecutionID
		if sourceExecutionID == "" {
			sourceExecutionID = execution.ID
		}
	}
	if receipt.Data["s3_source_execution_id"] != sourceExecutionID {
		return "", errs.ErrStateConflict
	}
	payload, err := json.Marshal(struct {
		Name, UID, ResourceVersion, ExecutionID, OrganizationID, ProjectID string
		SessionID, SourceExecutionID, Action, PolicySHA256, ReadbackSHA256 string
		SecretDataSHA256, ExpiresAt                                        string
	}{receipt.Data["s3_secret_name"], receipt.Data["s3_secret_uid"],
		receipt.Data["s3_secret_resource_version"], execution.ID,
		execution.OrganizationID, execution.ProjectID, execution.SessionID,
		sourceExecutionID, action, receipt.Data["s3_policy_sha256"],
		receipt.Data["s3_readback_sha256"], receipt.Data["s3_secret_data_sha256"],
		expiresAt.UTC().Format(time.RFC3339)})
	if err != nil {
		return "", errors.New("encode runtime S3 credential snapshot")
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != receipt.Data["s3_content_sha256"] {
		return "", errors.New("runtime S3 credential owner receipt digest mismatch")
	}
	return receipt.Data["s3_content_sha256"], nil
}

func validSHA256Text(value string) bool {
	return len(value) == 64 && value == strings.ToLower(value) &&
		strings.Trim(value, "0123456789abcdef") == ""
}

func validWorkloadTicket(value string) bool {
	if len(value) < 80 || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	parts := strings.Split(value, ".")
	return len(parts) == 2 && len(parts[0]) > 20 && len(parts[1]) == 43
}

func (adapter *Adapter) workerVolumes(
	component string,
	execution entity.Execution,
	status entity.RuntimeStatus,
) ([]corev1.Volume, []corev1.VolumeMount) {
	workload := component
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
	if component == archiveComponent || component == restoreComponent || component == rehydrateComponent {
		action := "archive"
		if component == restoreComponent || component == rehydrateComponent {
			action = "restore"
		}
		volumes = append(volumes,
			corev1.Volume{Name: "s3-credential", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName: s3CredentialSecretName(execution, action), DefaultMode: int32Pointer(0o400),
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
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: archiveSnapshotPVCName(execution), ReadOnly: true},
		}})
		mounts = append(mounts, corev1.VolumeMount{Name: "session", MountPath: "/archive-source", SubPath: "session", ReadOnly: true})
	}
	if component == rehydrateComponent {
		volumes = append(volumes, corev1.Volume{Name: "session", VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: status.PVCName},
		}})
		mounts = append(mounts, corev1.VolumeMount{Name: "session", MountPath: "/restore-target"})
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
		{Name: "INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME", Value: "internal-rpc-authority-postgresql-rw.mattercodex-system.svc.cluster.local"},
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
		case archiveComponent, restoreComponent, rehydrateComponent, cleanupComponent:
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
		binding, err := adapter.client.RbacV1().RoleBindings(adapter.config.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
		if err == nil {
			bindingUID, bindingVersion := binding.UID, binding.ResourceVersion
			err = adapter.client.RbacV1().RoleBindings(adapter.config.Namespace).Delete(ctx, job.Name,
				metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &bindingUID, ResourceVersion: &bindingVersion}})
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return deleted, errors.New("delete exact worker journal binding")
		}
		role, err := adapter.client.RbacV1().Roles(adapter.config.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
		if err == nil {
			roleUID, roleVersion := role.UID, role.ResourceVersion
			err = adapter.client.RbacV1().Roles(adapter.config.Namespace).Delete(ctx, job.Name,
				metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &roleUID, ResourceVersion: &roleVersion}})
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return deleted, errors.New("delete exact worker journal role")
		}
		deleted++
	}
	snapshots, err := adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).List(
		ctx, metav1.ListOptions{LabelSelector: managedLabel + "=true," + componentLabel + "=archive-snapshot", Limit: 1000},
	)
	if err != nil {
		return deleted, errors.New("list temporary runtime archive snapshots")
	}
	for index := range snapshots.Items {
		snapshot := &snapshots.Items[index]
		createdAt, parseErr := time.Parse(time.RFC3339Nano, snapshot.Annotations["runtime.mattercodex.dev/created-at"])
		executionID := snapshot.Annotations["runtime.mattercodex.dev/execution-id"]
		if parseErr != nil || !createdAt.Before(before) || uuid.Validate(executionID) != nil {
			continue
		}
		journal, journalErr := adapter.client.CoreV1().ConfigMaps(adapter.config.Namespace).Get(
			ctx, journalName(executionID), metav1.GetOptions{},
		)
		var document journalDocument
		if journalErr != nil || decodeJournal([]byte(journal.Data[journalDataKey]), &document) != nil ||
			document.Execution.ID != executionID || document.Execution.ArchiveSHA256 == "" {
			continue
		}
		uid, resourceVersion := snapshot.UID, snapshot.ResourceVersion
		if err := adapter.client.CoreV1().PersistentVolumeClaims(adapter.config.Namespace).Delete(
			ctx, snapshot.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion}},
		); err != nil && !apierrors.IsNotFound(err) {
			return deleted, errors.New("delete exact runtime archive snapshot")
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
func archiveSnapshotPVCName(execution entity.Execution) string {
	return "runtime-archive-snapshot-" + shortID(execution.ID)
}
func s3CredentialSecretName(execution entity.Execution, action string) string {
	return "runtime-s3-" + shortID(execution.ID) + "-" + action
}
func executionCredentialSecretName(execution entity.Execution, index int) string {
	return "runtime-credential-" + shortID(execution.ID) + "-" + strconv.Itoa(index)
}
func credentialBrokerJobName(execution entity.Execution) string {
	return "runtime-credential-broker-" + shortID(execution.ID) + "-f" + strconv.FormatUint(execution.Fence, 10)
}
func s3CredentialBrokerJobName(execution entity.Execution, action string) string {
	return "runtime-s3-" + action + "-broker-" + shortID(execution.ID) + "-f" + strconv.FormatUint(execution.Fence, 10)
}
func configName(execution entity.Execution) string {
	return "runtime-config-" + shortID(execution.ID) + "-v" + strconv.FormatUint(execution.RuntimeRevisionVersion, 10)
}
func handoffName(execution entity.Execution) string {
	return "runtime-handoff-" + shortID(execution.ID)
}
func pvcName(execution entity.Execution) string {
	return "runtime-session-" + stableHash(execution.OrganizationID+":"+execution.ProjectID+":"+execution.SessionID, 20)
}
func podName(execution entity.Execution) string {
	return "runtime-role-" + stableHash(execution.RoleID+":"+execution.ThreadID+":"+execution.SessionID+":"+execution.ID, 24)
}
func accessServiceAccountName(execution entity.Execution) string {
	return "runtime-access-" + stableHash(execution.OrganizationID+":"+execution.ProjectID+":"+
		execution.SessionID+":"+execution.RoleID+":"+execution.ID, 24)
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
		document.PodName == "" || document.PVCName == "" || document.Phase == "" ||
		document.CreatedAt.IsZero() || document.LastTransition.Before(document.CreatedAt) ||
		!validRehydrateJournal(document) ||
		!validCapacityJournal(document) ||
		document.PVCDeleted && (!document.PVCDeletionOwner || document.PVCUID == "" || document.PVCResourceVersion == "" ||
			document.PVCDeletionAuthorizationID == "" || document.PVCDeletionGeneration == 0 ||
			document.PVCNotFoundAt.IsZero() || document.PVCDeletionProofSHA256 != deletionProofSHA256(
			document.PVCName, document.PVCUID, document.PVCResourceVersion,
			document.PVCDeletionAuthorizationID, document.PVCDeletionGeneration, document.PVCNotFoundAt,
		)) {
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
		!validRehydrateJournal(*document) ||
		!validCapacityJournal(*document) ||
		document.PVCDeletionOwner && !pvcEvidencePresent ||
		document.PVCDeleted && (!document.PVCDeletionOwner || document.PVCNotFoundAt.IsZero() ||
			document.PVCDeletionAuthorizationID == "" || document.PVCDeletionGeneration == 0 ||
			document.PVCDeletionProofSHA256 != deletionProofSHA256(
				document.PVCName, document.PVCUID, document.PVCResourceVersion,
				document.PVCDeletionAuthorizationID, document.PVCDeletionGeneration, document.PVCNotFoundAt,
			)) {
		return errs.ErrStateConflict
	}
	return nil
}

func validCapacityJournal(document journalDocument) bool {
	empty := document.CapacityProviderBindingID == "" && document.CapacityObservationRevision == 0 &&
		document.CapacityObservedAt.IsZero() && document.CapacityObservedUsage == 0 &&
		document.CapacityObservedLimit == 0 && document.CapacityObservationMaxAge == 0 &&
		document.CapacityOrganizationLimit == 0
	if empty {
		return true
	}
	maximumAge := time.Duration(document.CapacityObservationMaxAge)
	return uuid.Validate(document.CapacityProviderBindingID) == nil &&
		document.CapacityObservationRevision > 0 && !document.CapacityObservedAt.IsZero() &&
		document.CapacityObservedLimit > 0 && document.CapacityObservedUsage <= document.CapacityObservedLimit &&
		maximumAge >= time.Minute && maximumAge <= 24*time.Hour &&
		document.CapacityOrganizationLimit >= 1 && document.CapacityOrganizationLimit <= 10_000
}

func validRehydrateJournal(document journalDocument) bool {
	source := document.Execution.RestoreSourceExecutionID != ""
	switch document.RehydratePhase {
	case "NOT_REQUIRED":
		return !source && document.RehydratePVCUID == "" &&
			document.RehydrateProofReference == "" && document.RehydrateProofSHA256 == ""
	case "PENDING":
		return source && document.RehydratePVCUID == "" &&
			document.RehydrateProofReference == "" && document.RehydrateProofSHA256 == ""
	case "COMPLETE":
		return source && uuid.Validate(document.RehydratePVCUID) == nil &&
			document.RehydrateProofReference == "journal://"+document.Execution.ID+"/rehydrate-proof" &&
			len(document.RehydrateProofSHA256) == sha256.Size*2
	default:
		return false
	}
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

func sameBinding(subjects []rbacv1.Subject, expected rbacv1.Subject) bool {
	return len(subjects) == 1 && subjects[0] == expected
}

func podMatches(actual, expected *corev1.Pod) bool {
	if actual == nil || expected == nil || actual.DeletionTimestamp != nil ||
		actual.Spec.ServiceAccountName != expected.Spec.ServiceAccountName ||
		!reflect.DeepEqual(actual.Spec.AutomountServiceAccountToken, expected.Spec.AutomountServiceAccountToken) ||
		actual.Spec.RestartPolicy != expected.Spec.RestartPolicy ||
		!reflect.DeepEqual(actual.Spec.EnableServiceLinks, expected.Spec.EnableServiceLinks) ||
		!reflect.DeepEqual(actual.Spec.TerminationGracePeriodSeconds, expected.Spec.TerminationGracePeriodSeconds) ||
		!reflect.DeepEqual(actual.Spec.SecurityContext, expected.Spec.SecurityContext) ||
		!reflect.DeepEqual(actual.Spec.ImagePullSecrets, expected.Spec.ImagePullSecrets) ||
		len(actual.Spec.InitContainers) != len(expected.Spec.InitContainers) ||
		len(actual.Spec.InitContainers) != 2 ||
		len(actual.Spec.Containers) != len(expected.Spec.Containers) ||
		len(actual.Spec.Containers) != 3 ||
		!reflect.DeepEqual(actual.Spec.Volumes, expected.Spec.Volumes) {
		return false
	}
	for index := range expected.Spec.InitContainers {
		if !executableContainerMatches(actual.Spec.InitContainers[index], expected.Spec.InitContainers[index]) {
			return false
		}
	}
	for index := range expected.Spec.Containers {
		if !executableContainerMatches(actual.Spec.Containers[index], expected.Spec.Containers[index]) {
			return false
		}
	}
	return true
}

func executableContainerMatches(actual, expected corev1.Container) bool {
	return actual.Name == expected.Name && actual.Image == expected.Image &&
		actual.ImagePullPolicy == expected.ImagePullPolicy &&
		reflect.DeepEqual(actual.Command, expected.Command) &&
		reflect.DeepEqual(actual.Args, expected.Args) &&
		reflect.DeepEqual(actual.Env, expected.Env) &&
		reflect.DeepEqual(actual.Ports, expected.Ports) &&
		reflect.DeepEqual(actual.Resources, expected.Resources) &&
		reflect.DeepEqual(actual.SecurityContext, expected.SecurityContext) &&
		reflect.DeepEqual(actual.VolumeMounts, expected.VolumeMounts) &&
		reflect.DeepEqual(actual.StartupProbe, expected.StartupProbe) &&
		reflect.DeepEqual(actual.ReadinessProbe, expected.ReadinessProbe) &&
		reflect.DeepEqual(actual.LivenessProbe, expected.LivenessProbe)
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
