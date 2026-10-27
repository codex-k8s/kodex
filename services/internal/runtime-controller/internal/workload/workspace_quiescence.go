package workload

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// WorkspaceQuiescenceObservation carries authenticated frozen input and a current owned terminal witness.
// No field is a new execution grant; consumers commit the past fact before releasing retained resources.
// WorkspaceQuiescenceObservation содержит authenticated frozen input и current owned terminal witness.
// Поля не являются новым execution grant; past fact фиксируется до освобождения retained resources.
type WorkspaceQuiescenceObservation struct {
	Input       runtimecontract.RunnerInput
	Record      runtimecontract.WorkspaceQuiescence
	BlockReason string
}

// WorkspacePodAbsent reports only authoritative API NotFound for the frozen lease-derived Pod name.
// This is not a quiescence proof: the owner must already retain the exact positive writer record.
// WorkspacePodAbsent сообщает только authoritative API NotFound для frozen lease-derived Pod name.
// Это не quiescence proof: owner уже должен хранить exact positive writer record.
func (manager *Manager) WorkspacePodAbsent(ctx context.Context, input runtimecontract.RunnerInput) (bool, error) {
	if input.WorkspaceStorage == nil || input.WorkspaceStorage.ValidateForInput(input) != nil {
		return false, errors.New("runtime workspace absence input is invalid")
	}
	_, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeTurnPodName(input.LeaseRef), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, errors.New("runtime workspace absence readback is unavailable")
	}
	return false, nil
}

// stopWorkspacePod requests only a graceful exact-UID stop while retaining the finalizer and callbacks.
// A missing object is unknown, never evidence of stopped writers or permission to erase dependencies.
// stopWorkspacePod запрашивает только graceful exact-UID stop, сохраняя finalizer и callbacks.
// Missing object означает unknown, а не stopped writer или разрешение удалить dependencies.
func (manager *Manager) stopWorkspacePod(ctx context.Context, input runtimecontract.RunnerInput) error {
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeTurnPodName(input.LeaseRef), metav1.GetOptions{})
	if err != nil || !manager.workspaceStoragePodMatches(input, pod) || !slices.Contains(pod.Finalizers, workspaceWriterFinalizer) {
		return errors.New("runtime workspace stopped writer is unconfirmed")
	}
	if pod.DeletionTimestamp != nil {
		return nil
	}
	uid := pod.UID
	if err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Delete(ctx, pod.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil {
		return errors.New("request runtime workspace writer stop")
	}
	return nil
}

// PendingWorkspaceQuiescence scans retained owned Pods without removing incomplete or ambiguous evidence.
// It survives controller replacement through immutable ticket/input pins; absence remains unconfirmed.
// PendingWorkspaceQuiescence читает retained owned Pods, не удаляя incomplete или ambiguous evidence.
// Controller replacement использует immutable ticket/input pins; отсутствие остаётся unconfirmed.
func (manager *Manager) PendingWorkspaceQuiescence(ctx context.Context) ([]WorkspaceQuiescenceObservation, error) {
	tickets, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{LabelSelector: managedLabel + "=true," + modeLabel + "=turn", Limit: 128})
	if err != nil || tickets.Continue != "" {
		return nil, errors.New("read retained runtime workspace tickets")
	}
	var result []WorkspaceQuiescenceObservation
	for index := range tickets.Items {
		ticket := &tickets.Items[index]
		if ticket.Annotations[workspaceStorageProfileAnnotation] != runtimecontract.WorkspaceStorageProfile || ticket.Immutable == nil || !*ticket.Immutable {
			continue
		}
		input, err := runtimecontract.DecodeRunnerInput(ticket.Data[inputKey])
		if err != nil || input.WorkspaceStorage == nil || ticket.Name != ticketName(input.LeaseRef) || !managedMetadataMatches(ticket.ObjectMeta, metav1.ObjectMeta{Annotations: runtimeProjectionAnnotations(input, runtimecontract.RuntimeTurnPodName(input.LeaseRef))}) {
			continue
		}
		value := WorkspaceQuiescenceObservation{Input: input}
		pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeTurnPodName(input.LeaseRef), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			value.BlockReason = "POD_MISSING"
			result = append(result, value)
			continue
		}
		if err != nil {
			value.BlockReason = "POD_UNREACHABLE"
			result = append(result, value)
			continue
		}
		if !manager.workspaceStoragePodMatches(input, pod) || !slices.Contains(pod.Finalizers, workspaceWriterFinalizer) {
			value.BlockReason = "OWNERSHIP_CHANGED"
			result = append(result, value)
			continue
		}
		unreachable := pod.Status.Phase == corev1.PodUnknown || pod.Status.Reason == "NodeLost"
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionUnknown {
				unreachable = true
			}
		}
		if unreachable {
			value.BlockReason = "POD_UNREACHABLE"
			result = append(result, value)
			continue
		}
		if !workspaceAllContainersTerminated(pod) && !workspaceStoppedBeforeMain(pod) {
			if pod.DeletionTimestamp != nil && (pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded) {
				value.BlockReason = "WRITER_JOIN_UNCONFIRMED"
				result = append(result, value)
			}
			continue
		}
		found := false
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == "role-runtime" && status.State.Terminated != nil {
				value.Record, err = runtimecontract.DecodeWorkspaceQuiescence([]byte(status.State.Terminated.Message))
				found = err == nil
			}
		}
		if !found {
			value.BlockReason = "WRITER_JOIN_UNCONFIRMED"
			result = append(result, value)
			continue
		}
		pvcName, _ := runtimecontract.SessionPVCName(input.SessionRef)
		pvc, err := manager.client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Get(ctx, pvcName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			value.BlockReason = "PVC_MISSING"
		} else if err != nil {
			value.BlockReason = "POD_UNREACHABLE"
		} else if string(pvc.UID) != value.Record.Writer.PVCUID {
			value.BlockReason = "PVC_REPLACED"
		} else if manager.ObserveWorkspaceQuiescence(ctx, input, value.Record) != nil {
			value.BlockReason = "OWNERSHIP_CHANGED"
		}
		result = append(result, value)
	}
	return result, nil
}

// ObserveWorkspaceQuiescence verifies the positive record against all current owned terminal containers and PVC.
// Terminal phase, deletion timestamp, lease expiry or caller record alone never establishes this fact.
// ObserveWorkspaceQuiescence сверяет positive record со всеми current owned terminal containers и PVC.
// Terminal phase, deletion timestamp, lease expiry и caller record отдельно не доказывают этот факт.
func (manager *Manager) ObserveWorkspaceQuiescence(ctx context.Context, input runtimecontract.RunnerInput, record runtimecontract.WorkspaceQuiescence) error {
	denied := errors.New("runtime workspace quiescence is unconfirmed")
	if input.WorkspaceStorage == nil || input.WorkspaceStorage.ValidateForInput(input) != nil || record.Validate() != nil {
		return denied
	}
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeTurnPodName(input.LeaseRef), metav1.GetOptions{})
	if err != nil || !manager.workspaceStoragePodMatches(input, pod) || (!workspaceAllContainersTerminated(pod) && !workspaceStoppedBeforeMain(pod)) || !slices.Contains(pod.Finalizers, workspaceWriterFinalizer) {
		return denied
	}
	raw, err := runtimecontract.EncodeWorkspaceQuiescence(record)
	if err != nil {
		return denied
	}
	found := false
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "role-runtime" && status.State.Terminated != nil && status.State.Terminated.Message == string(raw) {
			found = true
		}
	}
	if !found && !workspaceStoppedBeforeMain(pod) {
		return denied
	}
	pvcName, err := runtimecontract.SessionPVCName(input.SessionRef)
	if err != nil {
		return denied
	}
	pvc, err := manager.client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Get(ctx, pvcName, metav1.GetOptions{})
	labels, annotations, metadataErr := runtimecontract.SessionVolumeMetadata(input.OrganizationRef, input.ProjectRef, input.SessionRef)
	desired := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: manager.config.RuntimeNamespace, Labels: labels, Annotations: annotations},
		Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: manager.pvcRequest}}}}
	if err != nil || metadataErr != nil || !sessionPVCMatches(pvc, desired, manager.config.StorageClass) || !record.Matches(input, *input.WorkspaceStorage, string(pod.UID), string(pvc.UID)) {
		return denied
	}
	return nil
}

// ReleaseWorkspacePod requires an exact authoritative owner ACK and rechecks positive physical quiescence.
// Only the owned finalizer changes under UID/resourceVersion CAS; no PVC or saved data is deleted here.
// ReleaseWorkspacePod требует exact authoritative owner ACK и повторно проверяет positive physical quiescence.
// По UID/resourceVersion CAS меняется только owned finalizer; PVC и saved data здесь не удаляются.
func (manager *Manager) ReleaseWorkspacePod(ctx context.Context, value WorkspaceQuiescenceObservation, ack runtimecontract.WorkspaceWriterAcknowledgement) error {
	if !ack.Matches(value.Input) || ack.Writer != value.Record.Writer || manager.ObserveWorkspaceQuiescence(ctx, value.Input, value.Record) != nil {
		return errors.New("runtime workspace owner acknowledgement does not match stopped writer")
	}
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeTurnPodName(value.Input.LeaseRef), metav1.GetOptions{})
	if err != nil || string(pod.UID) != ack.Writer.PodUID || !manager.workspaceStoragePodMatches(value.Input, pod) || (!workspaceAllContainersTerminated(pod) && !workspaceStoppedBeforeMain(pod)) || pod.DeletionTimestamp == nil {
		return errors.New("runtime workspace finalizer ownership is unconfirmed")
	}
	finalizers := slices.DeleteFunc(slices.Clone(pod.Finalizers), func(name string) bool { return name == workspaceWriterFinalizer })
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"uid": pod.UID, "resourceVersion": pod.ResourceVersion, "finalizers": finalizers}})
	if err != nil {
		return errors.New("encode runtime workspace finalizer acknowledgement")
	}
	if _, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Patch(ctx, pod.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return errors.New("commit runtime workspace finalizer acknowledgement")
	}
	return nil
}

// workspaceStoppedBeforeMain proves a deleting owned Pod stopped its initialized containers before any main start.
// This requires explicit failed init termination and waiting main statuses with no IDs, restarts or prior start.
// workspaceStoppedBeforeMain доказывает остановку deleting owned Pod до первого main start.
// Обязательны explicit failed init termination и waiting main statuses без IDs, restarts и prior start.
func workspaceStoppedBeforeMain(pod *corev1.Pod) bool {
	if pod == nil || pod.DeletionTimestamp == nil || pod.Status.Phase != corev1.PodFailed || len(pod.Spec.InitContainers) != 2 || len(pod.Spec.Containers) != 3 ||
		len(pod.Status.InitContainerStatuses) != 2 || len(pod.Status.ContainerStatuses) != 3 {
		return false
	}
	failed := false
	for index, container := range pod.Spec.InitContainers {
		count := 0
		for _, status := range pod.Status.InitContainerStatuses {
			if status.Name != container.Name {
				continue
			}
			count++
			if status.RestartCount != 0 || status.LastTerminationState.Terminated != nil {
				return false
			}
			if failed {
				if status.State.Waiting == nil || status.ContainerID != "" || status.Started != nil && *status.Started {
					return false
				}
			} else {
				if status.State.Terminated == nil || status.ContainerID == "" {
					return false
				}
				if status.State.Terminated.ExitCode != 0 {
					failed = true
				}
				if index == 1 && !failed {
					return false
				}
			}
		}
		if count != 1 {
			return false
		}
	}
	if !failed {
		return false
	}
	for _, container := range pod.Spec.Containers {
		count := 0
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == container.Name {
				count++
				if status.State.Waiting == nil || status.ContainerID != "" || status.RestartCount != 0 || status.LastTerminationState.Terminated != nil || status.Started != nil && *status.Started {
					return false
				}
			}
		}
		if count != 1 {
			return false
		}
	}
	return true
}

// QuiescenceBeforeMain derives a past fact only after authoritative ACK and current owned init-only stop proof.
// It cannot synthesize a model result, capture receipt or new writer admission.
// QuiescenceBeforeMain выводит past fact только после authoritative ACK и current owned init-only stop proof.
// Model result, capture receipt или новый writer admission не создаются.
func (manager *Manager) QuiescenceBeforeMain(ctx context.Context, input runtimecontract.RunnerInput, ack runtimecontract.WorkspaceWriterAcknowledgement) (runtimecontract.WorkspaceQuiescence, bool) {
	if !ack.Matches(input) {
		return runtimecontract.WorkspaceQuiescence{}, false
	}
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeTurnPodName(input.LeaseRef), metav1.GetOptions{})
	if err != nil || !workspaceStoppedBeforeMain(pod) || !manager.workspaceStoragePodMatches(input, pod) || string(pod.UID) != ack.Writer.PodUID {
		return runtimecontract.WorkspaceQuiescence{}, false
	}
	record := runtimecontract.WorkspaceQuiescence{Schema: runtimecontract.WorkspaceQuiescenceSchema, Scope: ack.Scope, WorkspaceRef: ack.WorkspaceRef, Generation: ack.Generation, Joined: true, Writer: ack.Writer}
	return record, manager.ObserveWorkspaceQuiescence(ctx, input, record) == nil
}

// CleanupWorkspaceDependencies requires a matching committed past fact and an absent or released stopped Pod.
// Unknown deletion/readback never permits cleanup; PVC and saved work are always retained here.
// CleanupWorkspaceDependencies требует matching committed past fact и absent либо released stopped Pod.
// Unknown deletion/readback запрещает cleanup; PVC и saved work здесь всегда сохраняются.
func (manager *Manager) CleanupWorkspaceDependencies(ctx context.Context, value WorkspaceQuiescenceObservation, ack runtimecontract.WorkspaceWriterAcknowledgement) error {
	if !ack.Matches(value.Input) || ack.Writer != value.Record.Writer || !value.Record.Matches(value.Input, *value.Input.WorkspaceStorage, ack.Writer.PodUID, ack.Writer.PVCUID) {
		return errors.New("runtime workspace cleanup proof is invalid")
	}
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeTurnPodName(value.Input.LeaseRef), metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.New("runtime workspace cleanup readback is unavailable")
	}
	if err == nil && (string(pod.UID) != ack.Writer.PodUID || slices.Contains(pod.Finalizers, workspaceWriterFinalizer) || !manager.workspaceStoragePodMatches(value.Input, pod) || !workspaceAllContainersTerminated(pod) && !workspaceStoppedBeforeMain(pod)) {
		return errors.New("runtime workspace cleanup is not released")
	}
	if err := manager.cleanupWorkspacePolicies(ctx, value.Input); err != nil {
		return err
	}
	projection, err := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Get(ctx, runtimeProjectionName(value.Input), metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.New("read acknowledged runtime workspace projection")
	}
	if err == nil {
		if !managedMetadataMatches(projection.ObjectMeta, metav1.ObjectMeta{Annotations: runtimeProjectionAnnotations(value.Input, runtimecontract.RuntimeTurnPodName(value.Input.LeaseRef))}) {
			return errors.New("runtime workspace projection ownership changed")
		}
		uid := projection.UID
		if uid == "" {
			return errors.New("runtime workspace projection identity is unavailable")
		}
		if err := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Delete(ctx, projection.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
			return errors.New("delete acknowledged runtime workspace projection")
		}
	}
	ticket, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, ticketName(value.Input.LeaseRef), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil || !runtimeTicketMatches(ticket, runtimecontract.RuntimeTurnPodName(value.Input.LeaseRef), "turn", value.Input, "", nil, nil) || ticket.UID == "" {
		return errors.New("runtime workspace ticket ownership changed")
	}
	uid := ticket.UID
	if err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Delete(ctx, ticket.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
		return errors.New("delete acknowledged runtime workspace ticket")
	}
	return nil
}

// cleanupWorkspacePolicies deletes only exact previously owned policy objects using observed UID preconditions.
// Controller replacement compares the frozen creator identity, not the new controller's process UID.
// cleanupWorkspacePolicies удаляет лишь exact owned policy objects по observed UID preconditions.
// Controller replacement сравнивает frozen creator identity, не новый process UID контроллера.
func (manager *Manager) cleanupWorkspacePolicies(ctx context.Context, input runtimecontract.RunnerInput) error {
	expected := manager.executionMetadata(input)
	expected.Annotations[controllerAnnotation] = input.WorkloadInstance
	for _, entry := range []struct {
		read   func() (metav1.Object, error)
		remove func(types.UID) error
	}{
		{read: func() (metav1.Object, error) {
			return manager.client.CoreV1().ServiceAccounts(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeServiceAccountName(input.LeaseRef), metav1.GetOptions{})
		},
			remove: func(uid types.UID) error {
				return manager.client.CoreV1().ServiceAccounts(manager.config.RuntimeNamespace).Delete(ctx, runtimecontract.RuntimeServiceAccountName(input.LeaseRef), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
			}},
		{read: func() (metav1.Object, error) {
			return manager.client.NetworkingV1().NetworkPolicies(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeNetworkPolicyName(input.LeaseRef), metav1.GetOptions{})
		},
			remove: func(uid types.UID) error {
				return manager.client.NetworkingV1().NetworkPolicies(manager.config.RuntimeNamespace).Delete(ctx, runtimecontract.RuntimeNetworkPolicyName(input.LeaseRef), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
			}},
		{read: func() (metav1.Object, error) {
			return manager.client.RbacV1().Roles(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeRoleName(input.LeaseRef), metav1.GetOptions{})
		},
			remove: func(uid types.UID) error {
				return manager.client.RbacV1().Roles(manager.config.RuntimeNamespace).Delete(ctx, runtimecontract.RuntimeRoleName(input.LeaseRef), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
			}},
		{read: func() (metav1.Object, error) {
			return manager.client.RbacV1().RoleBindings(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeRoleBindingName(input.LeaseRef), metav1.GetOptions{})
		},
			remove: func(uid types.UID) error {
				return manager.client.RbacV1().RoleBindings(manager.config.RuntimeNamespace).Delete(ctx, runtimecontract.RuntimeRoleBindingName(input.LeaseRef), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
			}},
	} {
		object, err := entry.read()
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil || object == nil || object.GetUID() == "" || !managedMetadataMatches(metav1.ObjectMeta{Labels: object.GetLabels(), Annotations: object.GetAnnotations()}, expected) {
			return errors.New("runtime workspace policy ownership changed")
		}
		if err := entry.remove(object.GetUID()); err != nil && !apierrors.IsNotFound(err) {
			return errors.New("delete acknowledged runtime workspace policy")
		}
	}
	return nil
}
