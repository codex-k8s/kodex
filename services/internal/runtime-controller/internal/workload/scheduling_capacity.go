package workload

import (
	"context"
	"errors"
	"sort"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	resourcehelper "k8s.io/component-helpers/resource"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
)

// SchedulingCapacity reads bounded node/Pod inventories before claiming execution or materializing credentials.
// Observation failure prevents this poll's claims; Kubernetes remains the final scheduler, not this advisory accounting.
// SchedulingCapacity читает ограниченные списки узлов/Pod до захвата выполнения и размещения credentials.
// Ошибка наблюдения запрещает claims этого опроса; окончательное размещение принадлежит Kubernetes, а не учёту.
func (manager *Manager) SchedulingCapacity(ctx context.Context) (*controlplanev1.RuntimeSchedulingCapacity, error) {
	observed := time.Now()
	nodes, err := manager.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 512})
	if err != nil || nodes.Continue != "" || len(nodes.Items) > 512 {
		return nil, errors.New("observe runtime scheduling nodes")
	}
	var pods []corev1.Pod
	cursor := ""
	for page := 0; page < 32; page++ {
		batch, err := manager.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{Limit: 500, Continue: cursor})
		if err != nil {
			return nil, errors.New("observe runtime scheduling pods")
		}
		pods = append(pods, batch.Items...)
		cursor = batch.Continue
		if cursor == "" {
			break
		}
	}
	if cursor != "" {
		return nil, errors.New("runtime scheduling inventory exceeds budget")
	}
	if time.Since(observed) > 15*time.Second {
		return nil, errors.New("runtime scheduling inventory is stale")
	}
	return schedulingCapacity(nodes.Items, pods, manager.config.RuntimeNamespace, observed), nil
}

// schedulingCapacity charges assigned and pending workloads using Kubernetes' native effective requests.
// Pending reservations are deterministic and conservative; an unplaceable pending reservation closes admission.
// schedulingCapacity учитывает назначенные и ожидающие нагрузки по effective requests штатного Kubernetes helper.
// Резервирование Pending детерминировано и консервативно; неразмещаемый резерв закрывает новый допуск.
func schedulingCapacity(nodes []corev1.Node, pods []corev1.Pod, runtimeNamespace string, observed time.Time) *controlplanev1.RuntimeSchedulingCapacity {
	result := &controlplanev1.RuntimeSchedulingCapacity{ObservedAt: timestamppb.New(observed), Overhead: runtimeSchedulingOverhead()}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	eligible := make([]corev1.Node, 0, len(nodes))
	index := map[string]int{}
	for _, node := range nodes {
		if !schedulingNodeEligible(node) {
			continue
		}
		budget := schedulingRequests(node.Status.Allocatable)
		budget.PodSlots = node.Status.Allocatable.Pods().Value()
		budget.TotalCpuMilli = budget.CpuMilli
		budget.TotalMemoryBytes = budget.MemoryBytes
		budget.TotalEphemeralStorageBytes = budget.EphemeralStorageBytes
		budget.TotalPodSlots = budget.PodSlots
		index[node.Name] = len(eligible)
		eligible = append(eligible, node)
		result.Nodes = append(result.Nodes, budget)
	}
	var pending []corev1.Pod
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if pod.Spec.NodeName == "" {
			// Only acknowledged controller Pods reserve capacity before the scheduler assigns them.
			// Unscheduled foreign Pods are not allocations; their placement races remain with Kubernetes.
			// Только подтверждённые Pod контроллера резервируют место до назначения планировщиком.
			// Чужие неназначенные Pod не являются allocations; гонки их размещения решает Kubernetes.
			if pod.Namespace == runtimeNamespace && pod.Labels[managedLabel] == "true" {
				pending = append(pending, pod)
			}
			continue
		}
		if i, ok := index[pod.Spec.NodeName]; ok {
			subtractSchedulingRequests(result.Nodes[i], schedulingRequests(resourcehelper.PodRequests(&pod, resourcehelper.PodResourcesOptions{})))
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		if !pending[i].CreationTimestamp.Equal(&pending[j].CreationTimestamp) {
			return pending[i].CreationTimestamp.Before(&pending[j].CreationTimestamp)
		}
		if pending[i].Namespace != pending[j].Namespace {
			return pending[i].Namespace < pending[j].Namespace
		}
		return pending[i].Name < pending[j].Name
	})
	for _, pod := range pending {
		requests := schedulingRequests(resourcehelper.PodRequests(&pod, resourcehelper.PodResourcesOptions{}))
		placed := false
		for i, node := range eligible {
			matches, err := nodeaffinity.GetRequiredNodeAffinity(&pod).Match(&node)
			if err == nil && matches && schedulingRequestsFit(result.Nodes[i], requests) {
				subtractSchedulingRequests(result.Nodes[i], requests)
				placed = true
				break
			}
		}
		if !placed {
			// Preserve allocatable totals for a useful oversized-request diagnosis, but reserve all remaining headroom.
			// Полная ёмкость остаётся для диагностики слишком большого запроса, но свободный запас закрывается.
			for _, budget := range result.Nodes {
				budget.CpuMilli = 0
				budget.MemoryBytes = 0
				budget.EphemeralStorageBytes = 0
				budget.PodSlots = 0
			}
			break
		}
	}
	return result
}

// schedulingNodeEligible excludes unavailable, cordoned or tainted nodes because runtime Pods do not request tolerations.
// Readiness alone never grants access to a node or overrides Kubernetes scheduling decisions.
// schedulingNodeEligible исключает недоступные, cordoned и tainted узлы: runtime Pod не запрашивает tolerations.
// Готовность сама по себе не даёт доступа к узлу и не отменяет решения Kubernetes.
func schedulingNodeEligible(node corev1.Node) bool {
	if node.Spec.Unschedulable {
		return false
	}
	for _, taint := range node.Spec.Taints {
		if taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute {
			return false
		}
	}
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// schedulingRequests normalizes only the resources requested by generated runtime Pods and one Pod slot.
// Native PodRequests has already accounted for restartable init containers, Pod-level resources and overhead.
// schedulingRequests нормализует только ресурсы генерируемых runtime Pod и один слот Pod.
// Штатный PodRequests уже учёл restartable init, Pod-level resources и overhead.
func schedulingRequests(resources corev1.ResourceList) *controlplanev1.RuntimeNodeCapacity {
	return &controlplanev1.RuntimeNodeCapacity{CpuMilli: resources.Cpu().MilliValue(), MemoryBytes: resources.Memory().Value(), EphemeralStorageBytes: resources.StorageEphemeral().Value(), PodSlots: 1}
}

// runtimeSchedulingOverhead derives helper requests from the same function used by the Pod template.
// Sequential lightweight initializers do not exceed the two running helper containers' sum.
// runtimeSchedulingOverhead получает helper requests из той же функции, что шаблон Pod.
// Последовательные лёгкие init-контейнеры не превышают сумму двух работающих helper.
func runtimeSchedulingOverhead() *controlplanev1.RuntimeNodeCapacity {
	helpers := smallResources()
	value := schedulingRequests(helpers.Requests)
	value.CpuMilli *= 2
	value.MemoryBytes *= 2
	value.EphemeralStorageBytes *= 2
	return value
}

// schedulingRequestsFit checks one-node fit rather than adding unusable capacity across nodes.
// schedulingRequestsFit проверяет размещение на одном узле вместо сложения непригодной ёмкости разных узлов.
func schedulingRequestsFit(free, requests *controlplanev1.RuntimeNodeCapacity) bool {
	return free.CpuMilli >= requests.CpuMilli && free.MemoryBytes >= requests.MemoryBytes && free.EphemeralStorageBytes >= requests.EphemeralStorageBytes && free.PodSlots >= requests.PodSlots
}

// subtractSchedulingRequests saturates at zero when existing workload requests exceed current allocatable resources.
// It cannot manufacture capacity from an overcommitted or partially observed node.
// subtractSchedulingRequests ограничивает остаток нулём, если действующие requests превышают allocatable.
// Из перегруженного либо частично наблюдённого узла свободная ёмкость не создаётся.
func subtractSchedulingRequests(free, used *controlplanev1.RuntimeNodeCapacity) {
	free.CpuMilli = max(0, free.CpuMilli-used.CpuMilli)
	free.MemoryBytes = max(0, free.MemoryBytes-used.MemoryBytes)
	free.EphemeralStorageBytes = max(0, free.EphemeralStorageBytes-used.EphemeralStorageBytes)
	free.PodSlots = max(0, free.PodSlots-used.PodSlots)
}
