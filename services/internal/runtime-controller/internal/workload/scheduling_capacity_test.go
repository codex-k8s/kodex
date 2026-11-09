package workload

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	resourcehelper "k8s.io/component-helpers/resource"
)

// schedulingNodeFixture creates one synthetic ready node with explicit allocatable requests.
// It never changes a cluster or infers capacity from instantaneous usage.
// schedulingNodeFixture создаёт синтетический Ready узел с явной allocatable ёмкостью.
// Кластер не меняется, мгновенное потребление не подменяет requests.
func schedulingNodeFixture(name string) corev1.Node {
	return corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"pool": name}}, Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("1Gi"), corev1.ResourceEphemeralStorage: resource.MustParse("4Gi"), corev1.ResourcePods: resource.MustParse("10")}, Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
}

// schedulingPodFixture creates a request-bearing synthetic Pod; no image is pulled and no credentials exist.
// schedulingPodFixture создаёт синтетический Pod с requests; образы не скачиваются и credentials отсутствуют.
func schedulingPodFixture(name, node string) corev1.Pod {
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "runtime-test", Labels: map[string]string{managedLabel: "true"}}, Spec: corev1.PodSpec{NodeName: node, Containers: []corev1.Container{{Name: "provider", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("512Mi"), corev1.ResourceEphemeralStorage: resource.MustParse("1Gi")}}}}}}
}

// TestSchedulingCapacityChargesPendingAndReleasesTerminal proves the next turn waits for real headroom.
// Assigned and unscheduled Pod requests count once; completed Pods release capacity on the next observation.
// TestSchedulingCapacityChargesPendingAndReleasesTerminal проверяет ожидание настоящего свободного запаса.
// Назначенные и Pending requests учитываются один раз, terminal Pod освобождает ёмкость следующего снимка.
func TestSchedulingCapacityChargesPendingAndReleasesTerminal(t *testing.T) {
	nodes := []corev1.Node{schedulingNodeFixture("a")}
	first := schedulingPodFixture("first", "a")
	second := schedulingPodFixture("second", "")
	busy := schedulingCapacity(nodes, []corev1.Pod{first, second}, "runtime-test", time.Now())
	if len(busy.Nodes) != 1 || busy.Nodes[0].MemoryBytes != 0 || busy.Nodes[0].CpuMilli != 1000 || busy.Nodes[0].PodSlots != 8 {
		t.Fatal("pending reservation was lost or double charged")
	}
	first.Status.Phase = corev1.PodSucceeded
	free := schedulingCapacity(nodes, []corev1.Pod{first, second}, "runtime-test", time.Now())
	if free.Nodes[0].MemoryBytes != 512<<20 || free.Nodes[0].CpuMilli != 1500 || free.Nodes[0].EphemeralStorageBytes != 3<<30 {
		t.Fatal("terminal capacity was not released")
	}
	overhead := runtimeSchedulingOverhead()
	if overhead.MemoryBytes != 128<<20 || overhead.CpuMilli != 50 || overhead.PodSlots != 1 {
		t.Fatal("runtime sidecar overhead drifted")
	}
}

// TestSchedulingCapacityUsesNativeInitOverheadAndEligibility checks native resource accounting and eligible-node filtering.
// Sequential init requests are a maximum, not an extra simultaneous runtime; Pod overhead is added once.
// TestSchedulingCapacityUsesNativeInitOverheadAndEligibility проверяет штатный учёт ресурсов и пригодность узлов.
// Последовательные init requests учитываются максимумом, не суммой с runtime; Pod overhead добавляется один раз.
func TestSchedulingCapacityUsesNativeInitOverheadAndEligibility(t *testing.T) {
	nodes := []corev1.Node{schedulingNodeFixture("ready"), schedulingNodeFixture("cordoned"), schedulingNodeFixture("tainted"), schedulingNodeFixture("notready")}
	nodes[1].Spec.Unschedulable = true
	nodes[2].Spec.Taints = []corev1.Taint{{Key: "dedicated", Effect: corev1.TaintEffectNoSchedule}}
	nodes[3].Status.Conditions[0].Status = corev1.ConditionFalse
	pod := schedulingPodFixture("first", "ready")
	pod.Spec.InitContainers = []corev1.Container{{Name: "init", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("768Mi")}}}}
	pod.Spec.Overhead = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")}
	result := schedulingCapacity(nodes, []corev1.Pod{pod}, "runtime-test", time.Now())
	if len(result.Nodes) != 1 || result.Nodes[0].MemoryBytes != 192<<20 || result.Nodes[0].TotalMemoryBytes != 1<<30 {
		t.Fatal("native init/overhead or node eligibility changed")
	}
}

// TestSchedulingCapacityObservationFailureDoesNotReadCredentials checks the actual client boundary.
// Kubernetes observation errors return no capacity descriptor and never read Secrets or create a Pod.
// TestSchedulingCapacityObservationFailureDoesNotReadCredentials проверяет настоящую границу клиента.
// Ошибка наблюдения Kubernetes не возвращает ёмкость и не читает Secrets либо создаёт Pod.
func TestSchedulingCapacityObservationFailureDoesNotReadCredentials(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) { return true, nil, errors.New("unavailable") })
	manager := &Manager{client: client}
	if value, err := manager.SchedulingCapacity(context.Background()); err == nil || value != nil {
		t.Fatal("observation failure admitted work")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "list" || action.GetResource().Resource != "nodes" {
			t.Fatal("unexpected Kubernetes access")
		}
	}
}

// TestSchedulingOverheadMatchesPublishedPodTemplate compares admission arithmetic with the real generated Pod.
// A future sidecar/init request change must update accounting rather than silently overbooking nodes.
// TestSchedulingOverheadMatchesPublishedPodTemplate сопоставляет допуск с настоящим шаблоном Pod.
// Изменение requests sidecar/init требует обновить учёт вместо незаметного переполнения узлов.
func TestSchedulingOverheadMatchesPublishedPodTemplate(t *testing.T) {
	manager := newTestManager(t, fake.NewClientset())
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatal(err)
	}
	pod := manager.runtimePod(input, binding, nil, "runtime-ticket-fixture", "runtime-turn-fixture", "turn")
	if pod == nil {
		t.Fatal("Pod template rejected fixture")
	}
	actual := schedulingRequests(resourcehelper.PodRequests(pod, resourcehelper.PodResourcesOptions{}))
	overhead := runtimeSchedulingOverhead()
	main := input.EnvironmentPolicy.Resources
	if actual.CpuMilli != main.CPURequestMilli+overhead.CpuMilli || actual.MemoryBytes != main.MemoryRequestMiB<<20+overhead.MemoryBytes || actual.EphemeralStorageBytes != main.EphemeralStorageRequestMiB<<20+overhead.EphemeralStorageBytes {
		t.Fatal("admission overhead differs from published Pod resources")
	}
}
