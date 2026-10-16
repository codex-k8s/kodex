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
	firstPodUID  = "11111111-1111-4111-8111-111111111111"
	secondPodUID = "22222222-2222-4222-8222-222222222222"
)

var testElection = electionTiming{3 * time.Second, time.Second, 50 * time.Millisecond}

type workAvailabilityFunc func(context.Context) (WorkAvailability, error)

func (fn workAvailabilityFunc) GetAvailability(ctx context.Context) (WorkAvailability, error) {
	return fn(ctx)
}

func testLeaderLease() *coordinationv1.Lease {
	return &coordinationv1.Lease{ObjectMeta: metav1.ObjectMeta{Namespace: "kodex-system", Name: LeaderLeaseName,
		UID: "lease-fixture", ResourceVersion: "1", Labels: map[string]string{"app.kubernetes.io/name": "image-admission-controller"}}}
}

// Fake tracker не проверяет RV автоматически. Reactor воспроизводит обязательный
// CAS API server, иначе тест двух election loops дал бы ложное доказательство.
func leaderClient() *fake.Clientset {
	return leaderClientWithOutage(nil)
}

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
