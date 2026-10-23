package admissioncontroller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

const (
	// firstPodUID supplies a deterministic valid first replica identity without resolving any real Pod.
	// firstPodUID задаёт постоянную корректную идентичность первой реплики без поиска настоящего Pod.
	firstPodUID = "11111111-1111-4111-8111-111111111111"
	// secondPodUID supplies a distinct valid contender so simulated CAS/election races cannot share holder identity.
	// secondPodUID задаёт другого корректного кандидата, чтобы искусственные гонки CAS и выборов не разделяли идентичность держателя.
	secondPodUID = "22222222-2222-4222-8222-222222222222"
)

// testElection shortens local lease/renew/retry intervals while preserving client-go's required ordering.
// testElection сокращает локальные интервалы аренды, продления и повторов, сохраняя обязательный порядок client-go.
var testElection = electionTiming{3 * time.Second, time.Second, 50 * time.Millisecond}

// workAvailabilityFunc injects a synthetic work-source callback for controlled cancellation and join tests.
// workAvailabilityFunc подставляет искусственный источник работы для управляемых проверок отмены и ожидания завершения.
type workAvailabilityFunc func(context.Context) (WorkAvailability, error)

// GetAvailability forwards the exact caller context and returns the callback result without introducing external I/O.
// GetAvailability передаёт точный контекст вызывающего и возвращает результат обработчика без добавления внешнего ввода-вывода.
func (fn workAvailabilityFunc) GetAvailability(ctx context.Context) (WorkAvailability, error) {
	return fn(ctx)
}

// testLeaderLease returns a fresh precreated, labelled lease fixture with stable UID and initial resource version.
// It has no holder until the synthetic election acquires it.
// testLeaderLease возвращает новую фикстуру заранее созданной аренды с меткой, постоянным UID и начальной версией ресурса.
// Держателя нет до захвата искусственными выборами.
func testLeaderLease() *coordinationv1.Lease {
	return &coordinationv1.Lease{ObjectMeta: metav1.ObjectMeta{Namespace: "kodex-system", Name: LeaderLeaseName,
		UID: "lease-fixture", ResourceVersion: "1", Labels: map[string]string{"app.kubernetes.io/name": "image-admission-controller"}}}
}

// leaderClient creates an available fake API with explicit resource-version CAS, which the plain tracker does not enforce.
// This prevents two local election loops from incorrectly proving exclusivity against an unconstrained fake.
// leaderClient создаёт доступный искусственный API с явным CAS версии ресурса, которого нет у обычного tracker.
// Это не даёт двум локальным циклам выборов ложно доказать исключительность на неограниченной заглушке.
func leaderClient() *fake.Clientset {
	return leaderClientWithOutage(nil)
}

// leaderClientWithOutage serializes fake lease updates, checks UID/resource version and advances versions on success.
// An optional atomic switch rejects updates to simulate renew failure; all operations stay in the fake object tracker.
// leaderClientWithOutage сериализует искусственные обновления аренды, проверяет UID и версию и продвигает версии при успехе.
// Необязательный атомарный переключатель отклоняет обновления для имитации сбоя продления; операции остаются в fake tracker.
func leaderClientWithOutage(failed *atomic.Bool) *fake.Clientset {
	client := fake.NewClientset(testPolicy(), testLeaderLease())
	var mutex sync.Mutex
	client.PrependReactor("update", "leases", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if failed != nil && failed.Load() {
			return true, nil, errors.New("fixture lease outage")
		}
		mutex.Lock()
		defer mutex.Unlock()
		resource := schema.GroupVersionResource{Group: "coordination.k8s.io", Version: "v1", Resource: "leases"}
		obj := action.(k8stesting.UpdateAction).GetObject().(*coordinationv1.Lease).DeepCopy()
		current, err := client.Tracker().Get(resource, "kodex-system", LeaderLeaseName)
		if err != nil {
			return true, nil, err
		}
		before := current.(*coordinationv1.Lease)
		if obj.UID != before.UID || obj.ResourceVersion != before.ResourceVersion {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Group: resource.Group, Resource: resource.Resource}, obj.Name, errors.New("fixture version conflict"))
		}
		version, _ := strconv.Atoi(before.ResourceVersion)
		obj.ResourceVersion = strconv.Itoa(version + 1)
		if err := client.Tracker().Update(resource, obj, obj.Namespace); err != nil {
			return true, nil, err
		}
		return true, obj, nil
	})
	return client
}

// leaderController constructs a controller against the fake client and renderer, injects optional work and pins leadership.
// It shortens only the reconcile interval and fails the test if construction or read-only lease configuration fails.
// leaderController создаёт контроллер с искусственными клиентом и renderer, подставляет необязательную работу и закрепляет лидерство.
// Сокращается только интервал согласования; ошибка создания или настройки аренды чтением завершает тест.
func leaderController(t *testing.T, client *fake.Clientset, identity string, source WorkSource) *Controller {
	t.Helper()
	config := testConfig()
	if source != nil {
		config.WorkSource = source
	}
	controller, err := New(client, testRenderer{}, config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	controller.config.ReconcileInterval = 10 * time.Millisecond
	if err := controller.ConfigureLeadership(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	return controller
}

// awaitLeaderCondition polls a synthetic-state predicate every ten milliseconds until it holds or the caller context expires.
// Its ticker is stopped on every return; timeout fails the current test instead of starting an unbounded wait.
// awaitLeaderCondition опрашивает условие искусственного состояния каждые десять миллисекунд до успеха или истечения контекста.
// При любом возврате ticker останавливается; таймаут завершает тест вместо неограниченного ожидания.
func awaitLeaderCondition(t *testing.T, ctx context.Context, condition func() bool) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !condition() {
		select {
		case <-ctx.Done():
			t.Fatal("leader condition timed out")
		case <-ticker.C:
		}
	}
}

// TestLeaderRequiresExactPrecreatedLeaseAndNeverCreatesIt rejects missing leases, non-UID identity and unconfigured Run.
// The fake action log must contain only reads, proving no synthetic startup object was created or updated.
// TestLeaderRequiresExactPrecreatedLeaseAndNeverCreatesIt отклоняет отсутствие аренды, идентичность не-UID и Run без настройки.
// Журнал искусственных действий должен содержать только чтение, без создания или обновления объекта при запуске.
func TestLeaderRequiresExactPrecreatedLeaseAndNeverCreatesIt(t *testing.T) {
	client := fake.NewClientset(testPolicy())
	controller, err := New(client, testRenderer{}, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if controller.ConfigureLeadership(t.Context(), firstPodUID) == nil {
		t.Fatal("missing lease accepted")
	}
	if controller.ConfigureLeadership(t.Context(), "pod-name") == nil {
		t.Fatal("non-UID identity accepted")
	}
	if controller.Run(t.Context()) == nil {
		t.Fatal("unconfigured leadership ran")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("startup performed a write")
		}
	}
}

// TestLeaderLockPreservesUIDAndUsesServerVersionCAS makes one contender win and rejects a stale update from the other.
// Replacing the fixture UID must also fail reads, readiness and cached updates; runtime creation remains forbidden.
// TestLeaderLockPreservesUIDAndUsesServerVersionCAS даёт победить одному кандидату и отклоняет устаревшее обновление второго.
// Замена UID фикстуры также должна нарушить чтение, готовность и кешированное обновление; создание во время работы запрещено.
func TestLeaderLockPreservesUIDAndUsesServerVersionCAS(t *testing.T) {
	client := leaderClient()
	one := leaderController(t, client, firstPodUID, nil)
	two := leaderController(t, client, secondPodUID, nil)
	first, _, err := one.leader.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := two.leader.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	now := metav1.Now()
	first.HolderIdentity, first.LeaseDurationSeconds, first.AcquireTime, first.RenewTime = firstPodUID, 45, now, now
	second.HolderIdentity, second.LeaseDurationSeconds, second.AcquireTime, second.RenewTime = secondPodUID, 45, now, now
	if err := one.leader.Update(t.Context(), *first); err != nil {
		t.Fatal(err)
	}
	if two.leader.Update(t.Context(), *second) == nil {
		t.Fatal("stale CAS won")
	}
	lease, _ := client.CoordinationV1().Leases("kodex-system").Get(t.Context(), LeaderLeaseName, metav1.GetOptions{})
	lease.UID = "replacement"
	resource := schema.GroupVersionResource{Group: "coordination.k8s.io", Version: "v1", Resource: "leases"}
	if err := client.Tracker().Update(resource, lease, lease.Namespace); err != nil {
		t.Fatal(err)
	}
	if _, _, err := one.leader.Get(t.Context()); err == nil {
		t.Fatal("replacement UID accepted")
	}
	if one.Check(t.Context()) == nil {
		t.Fatal("replacement lease left readiness healthy")
	}
	if one.leader.Update(t.Context(), *first) == nil {
		t.Fatal("cached old UID updated replacement")
	}
	if one.leader.Create(t.Context(), resourcelock.LeaderElectionRecord{}) == nil {
		t.Fatal("runtime created a lock")
	}
}

// TestTwoAdmissionReplicasMaterializeOnceAndTakeOverExistingGraph runs two local election loops against CAS-aware fake storage.
// It checks joined cancellation, no early release, eventual takeover without duplicate Jobs/PVCs and only get/update lease verbs.
// This is synthetic concurrency evidence, not a live Kubernetes availability test.
// TestTwoAdmissionReplicasMaterializeOnceAndTakeOverExistingGraph запускает два локальных цикла выборов с учитывающим CAS хранилищем.
// Проверяются ожидание отмены, отсутствие раннего освобождения, смена без дубликатов Jobs/PVC и только get/update для аренды.
// Это искусственное доказательство конкурентности, а не проверка доступности живого Kubernetes.
func TestTwoAdmissionReplicasMaterializeOnceAndTakeOverExistingGraph(t *testing.T) {
	client := leaderClient()
	controllers := []*Controller{leaderController(t, client, firstPodUID, nil), leaderController(t, client, secondPodUID, nil)}
	// Разные run ID проявили бы гонку, если бы обе replica вошли в reconcile.
	for index, c := range controllers {
		c.now = func() time.Time { return time.Date(2026, 10, 15, 10, 0, index, 0, time.UTC) }
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	contexts := make([]context.Context, 2)
	cancels := make([]context.CancelFunc, 2)
	results := []chan error{make(chan error, 1), make(chan error, 1)}
	for index, c := range controllers {
		contexts[index], cancels[index] = context.WithCancel(ctx)
		defer cancels[index]()
		go func(i int, c *Controller) { results[i] <- c.runLeader(contexts[i], testElection) }(index, c)
	}
	getLease := func() *coordinationv1.Lease {
		lease, err := client.CoordinationV1().Leases("kodex-system").Get(ctx, LeaderLeaseName, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return lease
	}
	awaitLeaderCondition(t, ctx, func() bool {
		jobs, _ := client.BatchV1().Jobs("kodex-system").List(ctx, metav1.ListOptions{})
		return len(jobs.Items) == 2
	})
	lease := getLease()
	if lease.Spec.HolderIdentity == nil {
		t.Fatal("no active holder")
	}
	leader := 0
	if *lease.Spec.HolderIdentity == secondPodUID {
		leader = 1
	}
	previousHolder := *lease.Spec.HolderIdentity
	cancels[leader]()
	select {
	case err := <-results[leader]:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("leader did not join")
	}
	// ReleaseOnCancel=false: handoff не обгоняет завершение reconcile.
	if *getLease().Spec.HolderIdentity != previousHolder {
		t.Fatal("lock released before natural expiry")
	}
	awaitLeaderCondition(t, ctx, func() bool { return *getLease().Spec.HolderIdentity != previousHolder })
	workspaces, _ := client.CoreV1().PersistentVolumeClaims("kodex-system").List(ctx, metav1.ListOptions{})
	jobs, _ := client.BatchV1().Jobs("kodex-system").List(ctx, metav1.ListOptions{})
	if len(workspaces.Items) != 1 || len(jobs.Items) != 2 {
		t.Fatal("takeover duplicated the existing graph")
	}
	cancels[1-leader]()
	select {
	case err := <-results[1-leader]:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("follower did not join")
	}
	for _, action := range client.Actions() {
		if action.GetResource().Resource == "leases" && action.GetVerb() != "get" && action.GetVerb() != "update" {
			t.Fatal("election used ungranted lease permission")
		}
	}
}

// TestLeadershipLossCancelsAndJoinsReconcileBeforeReturning blocks synthetic work after cancellation and forces renew failure.
// The leader must not return before work is released, and the failed worker must not create or delete objects.
// TestLeadershipLossCancelsAndJoinsReconcileBeforeReturning задерживает искусственную работу после отмены и вызывает сбой продления.
// Лидер не должен вернуться до разрешения завершить работу, а неуспешный worker не должен создавать или удалять объекты.
func TestLeadershipLossCancelsAndJoinsReconcileBeforeReturning(t *testing.T) {
	failed := &atomic.Bool{}
	client := leaderClientWithOutage(failed)
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	source := workAvailabilityFunc(func(ctx context.Context) (WorkAvailability, error) {
		once.Do(func() { close(entered) })
		<-ctx.Done()
		close(canceled)
		<-release
		return WorkAvailability{}, ctx.Err()
	})
	controller := leaderController(t, client, firstPodUID, source)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- controller.runLeader(ctx, testElection) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("reconcile did not start")
	}
	// Нормализованный отказ renew; worker context должен отмениться по deadline.
	failed.Store(true)
	select {
	case <-canceled:
	case <-ctx.Done():
		t.Fatal("lease loss did not cancel work")
	}
	select {
	case <-result:
		t.Fatal("leader returned before work joined")
	default:
	}
	close(release)
	select {
	case err := <-result:
		if err == nil || errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected loss result: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("leader did not finish after join")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "create" || action.GetVerb() == "delete" {
			t.Fatal("failed leader materialized work")
		}
	}
}

// TestCanceledLeaderContextDoesNotMaterialize requires an already canceled reconciler to return cancellation without writes.
// It also rejects an empty foreign lease; fake action history is inspected instead of a live cluster.
// TestCanceledLeaderContextDoesNotMaterialize требует возврата отмены уже отменённого согласования без записей.
// Также отклоняется пустая чужая аренда; проверяется история искусственных действий, а не живой кластер.
func TestCanceledLeaderContextDoesNotMaterialize(t *testing.T) {
	client := leaderClient()
	controller := leaderController(t, client, firstPodUID, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if !errors.Is(controller.runReconciler(ctx), context.Canceled) {
		t.Fatal("canceled reconciler ran")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("canceled reconcile wrote")
		}
	}
	if validLeaderLease(&coordinationv1.Lease{}, types.UID("wrong")) {
		t.Fatal("foreign lease accepted")
	}
}

// TestFollowerCannotEnterReconcileThroughFormerLeaderLoop rejects reconciliation when the configured contender is not holder.
// Every observed fake API action must remain read-only, with no bypass through the lower-level loop.
// TestFollowerCannotEnterReconcileThroughFormerLeaderLoop отклоняет согласование, если настроенный кандидат не держит аренду.
// Все наблюдаемые действия искусственного API должны оставаться чтением, без обхода через нижележащий цикл.
func TestFollowerCannotEnterReconcileThroughFormerLeaderLoop(t *testing.T) {
	client := leaderClient()
	controller := leaderController(t, client, firstPodUID, nil)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if controller.runReconciler(ctx) == nil {
		t.Fatal("non-holder entered reconcile")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("non-holder performed effects")
		}
	}
}
